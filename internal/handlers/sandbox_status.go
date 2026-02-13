package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/ocx/backend/internal/ghostpool"
	"github.com/ocx/backend/internal/gvisor"
)

// SandboxStatusResponse is the JSON response for GET /api/v1/sandbox/status
type SandboxStatusResponse struct {
	GVisorAvailable bool               `json:"gvisor_available"`
	RunscPath       string             `json:"runsc_path"`
	DemoMode        bool               `json:"demo_mode"`
	Pool            *PoolStats         `json:"pool_stats,omitempty"`
	StateCloner     *StateClonerStatus `json:"state_cloner,omitempty"`
	Timestamp       time.Time          `json:"timestamp"`
}

// PoolStats summarises the ghost container pool
type PoolStats struct {
	MinContainers int    `json:"min_containers"`
	MaxContainers int    `json:"max_containers"`
	Image         string `json:"image"`
	Backend       string `json:"backend,omitempty"`
}

// StateClonerStatus reports Redis/DB connectivity for state snapshots
type StateClonerStatus struct {
	RedisConnected bool   `json:"redis_connected"`
	RedisAddr      string `json:"redis_addr,omitempty"`
}

// HandleSandboxStatus returns the current gVisor sandbox subsystem status.
// GET /api/v1/sandbox/status
func HandleSandboxStatus(
	sandbox *gvisor.SandboxExecutor,
	pool *ghostpool.PoolManager,
	cloner *gvisor.StateCloner,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp := SandboxStatusResponse{
			Timestamp: time.Now(),
		}

		// SandboxExecutor status
		if sandbox != nil {
			resp.GVisorAvailable = sandbox.IsAvailable()
			resp.RunscPath = sandbox.RunscPath()
			resp.DemoMode = !sandbox.IsAvailable()
		}

		// GhostPool stats
		if pool != nil {
			resp.Pool = &PoolStats{
				MinContainers: pool.MinSize(),
				MaxContainers: pool.MaxSize(),
				Image:         pool.ImageName(),
				Backend:       pool.BackendName(),
			}
		}

		// StateCloner connectivity
		if cloner != nil {
			resp.StateCloner = &StateClonerStatus{
				RedisConnected: cloner.Ping(),
				RedisAddr:      cloner.RedisAddr(),
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}
