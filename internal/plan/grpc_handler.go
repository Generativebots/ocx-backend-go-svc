package plan

import (
	"context"
	"sync"

	"github.com/ocx/backend/pb"
)

type PlanManager struct {
	pb.UnimplementedPlanServiceServer
	mu sync.RWMutex
	// Keyed by AgentID or PID.
	// Note: User snippet uses AgentID (string), but Gateway uses PID (int).
	// We will support AgentID for Registration, and assume a mapping happens later
	// or we store by AgentID and Gateway resolves AgentID from context.
	// For this step, following user snippet using string key.
	activePlans map[string]*pb.ExecutionPlan

	// Legacy support for PID based lookup if needed for existing main.go logic
	pidPlans map[uint32]*pb.ExecutionPlan
}

// RegisterIntent is called by the Python Orchestrator (The Brain)
func (pm *PlanManager) RegisterIntent(ctx context.Context, req *pb.ExecutionPlan) (*pb.ActionResponse, error) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	// Store the plan for the specific Agent
	pm.activePlans[req.AgentId] = req

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

// Validate is a helper for main.go that might still be using PID
func (pm *PlanManager) Validate(pid uint32, syscallName string) (bool, *pb.ExecutionPlan) {
	// Basic stub to keep main.go compiling if it uses this.
	// In a real scenario, we'd need to map PID -> AgentID.
	return true, nil
}
