package plan

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/ocx/backend/pb"
)

// PlanManager manages execution plans registered by the Python Orchestrator.
// It validates syscalls against registered plans before the Escrow Barrier
// releases them.
type PlanManager struct {
	pb.UnimplementedPlanServiceServer
	mu sync.RWMutex

	// activePlans is keyed by AgentID (string).
	activePlans map[string]*pb.ExecutionPlan

	// pidToAgent maps a kernel PID to its AgentID.
	// Populated by the eBPF identity mapper on process start.
	pidToAgent map[uint32]string
}

// NewPlanManager creates a PlanManager with initialized maps.
func NewPlanManager() *PlanManager {
	return &PlanManager{
		activePlans: make(map[string]*pb.ExecutionPlan),
		pidToAgent:  make(map[uint32]string),
	}
}

// RegisterIntent is called by the Python Orchestrator (The Brain)
func (pm *PlanManager) RegisterIntent(ctx context.Context, req *pb.ExecutionPlan) (*pb.ActionResponse, error) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	// Store the plan for the specific Agent
	pm.activePlans[req.AgentId] = req

	slog.Info("[PlanManager] Registered plan",
		"agent_id", req.AgentId,
		"plan_id", req.PlanId,
	)

	return &pb.ActionResponse{
		Success: true,
		Message: "Plan registered for Handshake",
	}, nil
}

// GetPlan retrieves a plan by AgentID
func (pm *PlanManager) GetPlan(agentID string) *pb.ExecutionPlan {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.activePlans[agentID]
}

// MapPIDToAgent registers a PID→AgentID mapping.
// Called by the eBPF identity mapper when a new agent process starts.
func (pm *PlanManager) MapPIDToAgent(pid uint32, agentID string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.pidToAgent[pid] = agentID
	slog.Info("[PlanManager] PID mapped to agent", "pid", pid, "agent_id", agentID)
}

// UnmapPID removes a PID→AgentID mapping (on process exit).
func (pm *PlanManager) UnmapPID(pid uint32) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	delete(pm.pidToAgent, pid)
}

// Validate checks whether a PID is allowed to execute the given syscall.
//
// Logic:
//  1. Resolve PID → AgentID via the identity map.
//  2. Look up the agent's registered ExecutionPlan.
//  3. Check whether syscallName appears in the plan's AllowedCalls list.
//  4. Return (allowed, plan).
//
// If no plan is registered for the agent, the action is denied by default
// (fail-closed security posture).
func (pm *PlanManager) Validate(pid uint32, syscallName string) (bool, *pb.ExecutionPlan) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	// Step 1: Resolve PID → AgentID
	agentID, mapped := pm.pidToAgent[pid]
	if !mapped {
		slog.Warn("[PlanManager] Validate: unknown PID, denying",
			"pid", pid,
			"syscall", syscallName,
		)
		return false, nil
	}

	// Step 2: Lookup the agent's active plan
	plan, hasPlan := pm.activePlans[agentID]
	if !hasPlan || plan == nil {
		slog.Warn("[PlanManager] Validate: no plan registered for agent, denying",
			"pid", pid,
			"agent_id", agentID,
			"syscall", syscallName,
		)
		return false, nil
	}

	// Step 3: Check AllowedCalls list
	for _, call := range plan.AllowedCalls {
		if call == syscallName {
			slog.Debug("[PlanManager] Validate: allowed",
				"pid", pid,
				"agent_id", agentID,
				"syscall", syscallName,
			)
			return true, plan
		}
	}

	// Step 4: Not in allowed list → deny
	slog.Warn("[PlanManager] Validate: syscall not in allowed list, denying",
		"pid", pid,
		"agent_id", agentID,
		"syscall", syscallName,
		"allowed_calls", fmt.Sprintf("%v", plan.AllowedCalls),
	)
	return false, plan
}

// RevokePlan removes an agent's registered plan (e.g., after ejection).
func (pm *PlanManager) RevokePlan(agentID string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	delete(pm.activePlans, agentID)
	slog.Info("[PlanManager] Revoked plan", "agent_id", agentID)
}
