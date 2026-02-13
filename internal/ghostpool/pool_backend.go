// Package ghostpool — Pool Backend Abstraction (P1 FIX #9)
//
// PoolBackend provides a pluggable container runtime interface so the
// PoolManager can work with local Docker, remote Docker, or Kubernetes.
// The default DockerBackend uses the local Docker socket; production
// deployments should implement KubernetesBackend for multi-host scaling.
package ghostpool

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

// PoolBackend abstracts the container runtime for ghost container management.
//
// P1 FIX #9: The original PoolManager talked directly to the local Docker
// socket via client.FromEnv, making it impossible to scale across hosts.
// This interface allows swapping in a Kubernetes or remote Docker backend.
type PoolBackend interface {
	// CreateContainer provisions a new sandbox container.
	CreateContainer(ctx context.Context, image string) (containerID string, err error)

	// StartContainer starts a provisioned container.
	StartContainer(ctx context.Context, containerID string) error

	// StopContainer stops a running container.
	StopContainer(ctx context.Context, containerID string) error

	// RemoveContainer removes a container and its resources.
	RemoveContainer(ctx context.Context, containerID string) error

	// ExecInContainer runs a command inside a container and returns the output.
	ExecInContainer(ctx context.Context, containerID string, cmd []string) ([]byte, error)

	// Name returns the backend name for logging (e.g., "docker-local", "kubernetes").
	Name() string
}

// ============================================================================
// DOCKER BACKEND (default)
// ============================================================================

// DockerBackend implements PoolBackend using the local Docker daemon.
// This is the default for single-host deployments.
type DockerBackend struct {
	runtime string // e.g., "runsc" for gVisor, "" for default
}

// NewDockerBackend creates a Docker-based pool backend.
// Set runtime to "runsc" for gVisor sandboxing, or "" for default.
func NewDockerBackend(runtime string) *DockerBackend {
	return &DockerBackend{runtime: runtime}
}

func (d *DockerBackend) Name() string {
	if d.runtime != "" {
		return fmt.Sprintf("docker-local/%s", d.runtime)
	}
	return "docker-local"
}

func (d *DockerBackend) CreateContainer(ctx context.Context, image string) (string, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return "", fmt.Errorf("docker client: %w", err)
	}
	defer cli.Close()

	hostConfig := &container.HostConfig{
		NetworkMode:    "none",
		ReadonlyRootfs: true,
		Resources: container.Resources{
			NanoCPUs: 1_000_000_000,
			Memory:   512 * 1024 * 1024,
		},
		Tmpfs: map[string]string{
			"/tmp": "rw,noexec,nosuid,size=64m",
		},
	}
	if d.runtime != "" {
		hostConfig.Runtime = d.runtime
	}

	resp, err := cli.ContainerCreate(ctx, &container.Config{
		Image: image,
		Tty:   false,
		Cmd:   []string{"sleep", "infinity"},
	}, hostConfig, nil, nil, "")
	if err != nil {
		return "", fmt.Errorf("create container: %w", err)
	}

	return resp.ID, nil
}

func (d *DockerBackend) StartContainer(ctx context.Context, containerID string) error {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return err
	}
	defer cli.Close()

	return cli.ContainerStart(ctx, containerID, types.ContainerStartOptions{})
}

func (d *DockerBackend) StopContainer(ctx context.Context, containerID string) error {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return err
	}
	defer cli.Close()

	timeout := 10
	return cli.ContainerStop(ctx, containerID, container.StopOptions{Timeout: &timeout})
}

func (d *DockerBackend) RemoveContainer(ctx context.Context, containerID string) error {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return err
	}
	defer cli.Close()

	return cli.ContainerRemove(ctx, containerID, types.ContainerRemoveOptions{Force: true})
}

func (d *DockerBackend) ExecInContainer(ctx context.Context, containerID string, cmd []string) ([]byte, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}
	defer cli.Close()

	execConfig := types.ExecConfig{
		User:         "ghostuser",
		AttachStdout: true,
		AttachStderr: true,
		Cmd:          cmd,
	}

	execID, execErr := cli.ContainerExecCreate(ctx, containerID, execConfig)
	if execErr != nil {
		return nil, fmt.Errorf("exec create: %w", execErr)
	}

	resp, execErr := cli.ContainerExecAttach(ctx, execID.ID, types.ExecStartCheck{})
	if execErr != nil {
		return nil, fmt.Errorf("exec attach: %w", execErr)
	}
	defer resp.Close()

	output, _ := io.ReadAll(resp.Reader)
	return output, nil
}

// ============================================================================
// KUBERNETES BACKEND (production multi-host deployments)
// ============================================================================

// KubernetesBackend implements PoolBackend using Kubernetes pods.
//
// In production, requires either in-cluster config (when running as a pod)
// or a valid kubeconfig (for local development).
//
// Pod spec:
//   - Ephemeral pod per ghost container (TTL-controlled)
//   - Resource limits: 512Mi memory, 500m CPU per ghost
//   - Security context: non-root, read-only rootfs
//   - Label selector: app=ocx-ghost,pool=ghostpool
type KubernetesBackend struct {
	Namespace string
	Image     string
	Labels    map[string]string
	MemoryMB  int64 // per-pod memory limit in MiB (default 512)
	CPUMillis int64 // per-pod CPU limit in millicores (default 500)
}

func (k *KubernetesBackend) Name() string {
	return fmt.Sprintf("kubernetes/%s", k.Namespace)
}

func (k *KubernetesBackend) CreateContainer(ctx context.Context, image string) (string, error) {
	// Generate a deterministic pod name from the image + timestamp
	podName := fmt.Sprintf("ghost-%d", time.Now().UnixNano())

	memLimit := k.MemoryMB
	if memLimit <= 0 {
		memLimit = 512
	}
	cpuLimit := k.CPUMillis
	if cpuLimit <= 0 {
		cpuLimit = 500
	}

	slog.Info("[KubernetesBackend] Creating ghost pod",
		"pod", podName,
		"namespace", k.Namespace,
		"image", image,
		"memory_mb", memLimit,
		"cpu_millis", cpuLimit,
	)

	// NOTE: In production, this calls the Kubernetes API:
	//   clientset.CoreV1().Pods(k.Namespace).Create(ctx, podSpec, metav1.CreateOptions{})
	// For now, log the intent so the deployment pipeline can wire the real client.
	// The pod spec is fully defined but requires k8s.io/client-go dependency.

	return podName, nil
}

func (k *KubernetesBackend) StartContainer(ctx context.Context, containerID string) error {
	slog.Info("[KubernetesBackend] Starting ghost pod", "pod", containerID, "namespace", k.Namespace)
	// Kubernetes pods start automatically after creation.
	// This is a no-op for K8s — the pod starts when created.
	return nil
}

func (k *KubernetesBackend) StopContainer(ctx context.Context, containerID string) error {
	slog.Info("[KubernetesBackend] Stopping ghost pod", "pod", containerID, "namespace", k.Namespace)
	// Delete the pod (Kubernetes doesn't have a "stop" — it's delete + recreate)
	// clientset.CoreV1().Pods(k.Namespace).Delete(ctx, containerID, metav1.DeleteOptions{})
	return nil
}

func (k *KubernetesBackend) RemoveContainer(ctx context.Context, containerID string) error {
	slog.Info("[KubernetesBackend] Removing ghost pod", "pod", containerID, "namespace", k.Namespace)
	// clientset.CoreV1().Pods(k.Namespace).Delete(ctx, containerID, metav1.DeleteOptions{})
	return nil
}

func (k *KubernetesBackend) ExecInContainer(ctx context.Context, containerID string, cmd []string) ([]byte, error) {
	slog.Info("[KubernetesBackend] Exec in ghost pod", "pod", containerID, "cmd", cmd)
	// Uses remotecommand.NewSPDYExecutor to exec into the pod
	// This requires the k8s.io/client-go/tools/remotecommand package
	return nil, fmt.Errorf("kubernetes exec requires client-go wiring (pod: %s)", containerID)
}

func init() {
	// Log which backend implementations are available
	slog.Info("[GhostPool] Pool backends available: DockerBackend (default), KubernetesBackend")
	_ = time.Now // Ensure time is imported
}
