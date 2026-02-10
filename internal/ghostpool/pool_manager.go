package ghostpool

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

// GhostContainer represents a recyclable sandbox instance.
type GhostContainer struct {
	ID        string
	IPAddress string
	TenantID  string // Multi-Tenancy ownership
	LastUsed  time.Time
}

// PoolManager handles the lifecycle of GhostContainers: Pre-warm -> Acquire -> Scrub -> Release.
type PoolManager struct {
	mu          sync.Mutex
	available   chan *GhostContainer
	active      map[string]*GhostContainer
	minIdle     int
	maxCapacity int
	imageName   string
}

// NewPoolManager initializes the pool and starts pre-warming.
func NewPoolManager(minIdle, maxCap int, image string) *PoolManager {
	pm := &PoolManager{
		available:   make(chan *GhostContainer, maxCap),
		active:      make(map[string]*GhostContainer),
		minIdle:     minIdle,
		maxCapacity: maxCap,
		imageName:   image,
	}
	// Start background maintainer
	go pm.maintainPool()
	return pm
}

// Get retrieves a pre-warmed container or blocks until one is ready.
func (pm *PoolManager) Get(ctx context.Context, tenantID string) (*GhostContainer, error) {
	select {
	case container := <-pm.available:
		pm.mu.Lock()
		pm.active[container.ID] = container
		pm.mu.Unlock()

		// Update Usage Metadata
		container.LastUsed = time.Now()
		container.TenantID = tenantID

		return container, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Put returns a container to the pool after "scrubbing" its state.
func (pm *PoolManager) Put(c *GhostContainer) {
	go func() {
		// Create a background context for scrubbing (decoupled from request context)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := pm.scrubContainer(ctx, c); err != nil {
			slog.Warn("Failed to scrub container : Destroying.", "i_d", c.ID, "error", err)
			// If scrub fails, destroy the container instead of returning it
			pm.destroyContainer(ctx, c)
			return
		}

		pm.mu.Lock()
		delete(pm.active, c.ID)
		pm.mu.Unlock()
		pm.available <- c
	}()
}

// scrubContainer resets the environment using Docker Exec.
func (pm *PoolManager) scrubContainer(ctx context.Context, c *GhostContainer) error {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return err
	}
	defer cli.Close()

	// 1. Wipe temporary speculative data
	// We execute a cleanup script already present inside the Ghost image
	execConfig := types.ExecConfig{
		User:         "root",
		AttachStdout: true,
		AttachStderr: true,
		Cmd:          []string{"/bin/sh", "-c", "rm -rf /tmp/speculation/* && pkill -u ghostuser"},
	}

	execID, err := cli.ContainerExecCreate(ctx, c.ID, execConfig)
	if err != nil {
		return fmt.Errorf("failed to create scrub exec: %w", err)
	}

	err = cli.ContainerExecStart(ctx, execID.ID, types.ExecStartCheck{
		Detach: false,
		Tty:    false,
	})
	if err != nil {
		return fmt.Errorf("failed to start scrub: %w", err)
	}

	// 2. Optional: If using filesystem snapshots (Advanced)
	// cli.ContainerCommit(...) or using a CoW volume reset

	return nil
}

// maintainPool ensures the pool stays populated.
func (pm *PoolManager) maintainPool() {
	for {
		time.Sleep(2 * time.Second) // Check every 2s

		pm.mu.Lock()
		activeCount := len(pm.active)
		pm.mu.Unlock()

		availableCount := len(pm.available)
		total := activeCount + availableCount

		// 1. Scale Up if below minIdle and maxCapacity
		if availableCount < pm.minIdle && total < pm.maxCapacity {
			deficit := pm.minIdle - availableCount
			for i := 0; i < deficit; i++ {
				if activeCount+availableCount+i >= pm.maxCapacity {
					break
				}
				go pm.createContainer()
			}
		}
	}
}

func (pm *PoolManager) createContainer() {
	ctx := context.Background()
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		slog.Warn("Error creating docker client", "error", err)
		return
	}
	defer cli.Close()

	// Use gVisor runtime if available, else default
	// Load Quotas (In production, read from config/quotas.json)
	// Enforcing: --cpus=1.0 --memory=512m
	hostConfig := &container.HostConfig{
		Runtime:        "runsc", // gVisor
		NetworkMode:    "none",  // Network Jailing
		ReadonlyRootfs: true,    // CoW enforcement
		Resources: container.Resources{
			NanoCPUs: 1000000000,        // 1.0 CPU
			Memory:   512 * 1024 * 1024, // 512MB
		},
		Tmpfs: map[string]string{
			"/tmp": "rw,noexec,nosuid,size=64m",
		},
	}

	resp, err := cli.ContainerCreate(ctx, &container.Config{
		Image: pm.imageName,
		Tty:   false,
		Cmd:   []string{"sleep", "infinity"}, // Keep alive
	}, hostConfig, nil, nil, "")
	if err != nil {
		slog.Warn("Failed to create ghost container", "error", err)
		return
	}

	if err := cli.ContainerStart(ctx, resp.ID, types.ContainerStartOptions{}); err != nil {
		slog.Warn("Failed to start ghost container", "error", err)
		return
	}

	c := &GhostContainer{
		ID:       resp.ID,
		LastUsed: time.Now(),
	}

	pm.available <- c
	slog.Info("Ghost Container pre-warmed", "i_d12", resp.ID[:12])
}

// destroyContainer FORCEFULLY removes a container and its resources
func (pm *PoolManager) destroyContainer(ctx context.Context, c *GhostContainer) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		slog.Warn("Failed to create client for destroy", "error", err)
		return
	}
	defer cli.Close()

	// Force remove container
	if err := cli.ContainerRemove(ctx, c.ID, types.ContainerRemoveOptions{Force: true}); err != nil {
		slog.Warn("Failed to force remove container", "i_d", c.ID, "error", err)
	}

	// Remove sandbox directory
	dir := filepath.Join("/tmp", "ocx-sandboxes", c.ID)
	os.RemoveAll(dir)

	slog.Info("Cleaned up container resources", "i_d", c.ID)
}

// ExecuteSpeculative runs a command inside a specific container (using runsc/docker exec)
func (pm *PoolManager) ExecuteSpeculative(ctx context.Context, containerID string, cmd []string, payload []byte) ([]byte, error) {
	// 1. Write payload to container (stdin or file)
	// For simplicity, we pass it as an arg or echo it.
	// In production, we'd CopyToContainer.

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}
	defer cli.Close()

	// 2. Prepare Exec
	execConfig := types.ExecConfig{
		User:         "ghostuser", // Drop privileges
		AttachStdout: true,
		AttachStderr: true,
		Cmd:          cmd,
		// Env: []string{fmt.Sprintf("PAYLOAD=%s", string(payload))},
	}

	execID, err := cli.ContainerExecCreate(ctx, containerID, execConfig)
	if err != nil {
		return nil, fmt.Errorf("exec create failed: %w", err)
	}

	// 3. Run and Attach
	resp, err := cli.ContainerExecAttach(ctx, execID.ID, types.ExecStartCheck{})
	if err != nil {
		return nil, fmt.Errorf("exec attach failed: %w", err)
	}
	defer resp.Close()

	// 4. Read Output
	// resp.Reader holds stdout/stderr
	// Docker multiplexes stdout/stderr, we need stdcopy or simple read for now
	// Simplification for prototype:
	output, _ := io.ReadAll(resp.Reader)

	return output, nil
}

// Stats returns current pool statistics.
func (pm *PoolManager) Stats() map[string]interface{} {
	pm.mu.Lock()
	activeCount := len(pm.active)
	pm.mu.Unlock()

	availableCount := len(pm.available)

	return map[string]interface{}{
		"active_containers": activeCount,
		"idle_containers":   availableCount,
		"total_capacity":    pm.maxCapacity,
		"min_idle":          pm.minIdle,
	}
}
