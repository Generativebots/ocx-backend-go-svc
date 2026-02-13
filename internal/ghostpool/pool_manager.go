package ghostpool

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
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
	backend     PoolBackend // Pluggable container runtime (Docker, K8s, etc.)
}

// NewPoolManager initializes the pool with a default DockerBackend("runsc") and starts pre-warming.
func NewPoolManager(minIdle, maxCap int, image string) *PoolManager {
	return NewPoolManagerWithBackend(minIdle, maxCap, image, NewDockerBackend("runsc"))
}

// NewPoolManagerWithBackend initializes the pool with an explicit PoolBackend.
func NewPoolManagerWithBackend(minIdle, maxCap int, image string, backend PoolBackend) *PoolManager {
	pm := &PoolManager{
		available:   make(chan *GhostContainer, maxCap),
		active:      make(map[string]*GhostContainer),
		minIdle:     minIdle,
		maxCapacity: maxCap,
		imageName:   image,
		backend:     backend,
	}
	slog.Info("PoolManager initialized", "backend", backend.Name(), "min_idle", minIdle, "max_capacity", maxCap)
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

// scrubContainer resets the environment via the backend.
func (pm *PoolManager) scrubContainer(ctx context.Context, c *GhostContainer) error {
	// Execute cleanup script inside the container
	_, err := pm.backend.ExecInContainer(ctx, c.ID, []string{"/bin/sh", "-c", "rm -rf /tmp/speculation/* && pkill -u ghostuser"})
	if err != nil {
		return fmt.Errorf("scrub failed: %w", err)
	}
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

	containerID, err := pm.backend.CreateContainer(ctx, pm.imageName)
	if err != nil {
		slog.Warn("Failed to create ghost container", "backend", pm.backend.Name(), "error", err)
		return
	}

	if err := pm.backend.StartContainer(ctx, containerID); err != nil {
		slog.Warn("Failed to start ghost container", "backend", pm.backend.Name(), "error", err)
		return
	}

	c := &GhostContainer{
		ID:       containerID,
		LastUsed: time.Now(),
	}

	pm.available <- c
	if len(containerID) >= 12 {
		slog.Info("Ghost Container pre-warmed", "backend", pm.backend.Name(), "id12", containerID[:12])
	} else {
		slog.Info("Ghost Container pre-warmed", "backend", pm.backend.Name(), "id", containerID)
	}
}

// destroyContainer removes a container and its resources via the backend.
func (pm *PoolManager) destroyContainer(ctx context.Context, c *GhostContainer) {
	if err := pm.backend.RemoveContainer(ctx, c.ID); err != nil {
		slog.Warn("Failed to remove container", "backend", pm.backend.Name(), "id", c.ID, "error", err)
	}
	slog.Info("Cleaned up container resources", "backend", pm.backend.Name(), "id", c.ID)
}

// ExecuteSpeculative runs a command inside a container via the backend.
func (pm *PoolManager) ExecuteSpeculative(ctx context.Context, containerID string, cmd []string, payload []byte) ([]byte, error) {
	output, err := pm.backend.ExecInContainer(ctx, containerID, cmd)
	if err != nil {
		return nil, fmt.Errorf("speculative exec via %s failed: %w", pm.backend.Name(), err)
	}
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

// MinSize returns the minimum idle container count for the pool.
func (pm *PoolManager) MinSize() int { return pm.minIdle }

// MaxSize returns the maximum pool capacity.
func (pm *PoolManager) MaxSize() int { return pm.maxCapacity }

// ImageName returns the Docker image used for ghost containers.
func (pm *PoolManager) ImageName() string { return pm.imageName }

// BackendName returns the name of the active pool backend (e.g. "docker-local/runsc").
func (pm *PoolManager) BackendName() string { return pm.backend.Name() }
