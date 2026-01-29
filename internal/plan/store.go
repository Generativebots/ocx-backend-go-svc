package plan

import (
	"github.com/ocx/backend/pb"
	"sync"
)

// PlanStore handles the validation of Syscalls against the Manifest.
type PlanStore struct {
	mu          sync.RWMutex
	ActivePlans map[uint32]*pb.ExecutionPlan // Key: PID
}

func NewPlanStore() *PlanStore {
	return &PlanStore{
		ActivePlans: make(map[uint32]*pb.ExecutionPlan),
	}
}

// RegisterPlan associates a PID with a Plan (Handshake).
func (ps *PlanStore) RegisterPlan(pid uint32, plan *pb.ExecutionPlan) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.ActivePlans[pid] = plan
}

// Validate checks if a specific syscall is allowed for the PID.
func (ps *PlanStore) Validate(pid uint32, syscallName string) (allowed bool, plan *pb.ExecutionPlan) {
	ps.mu.RLock()
	plan, exists := ps.ActivePlans[pid]
	ps.mu.RUnlock()

	if !exists {
		// Default Policy: If no plan exists, what do we do?
		// For now, we return FALSE (Fail-Closed) or TRUE (Audit-Only) depending on mode.
		// We will return nil to indicate "No Plan"
		return true, nil
	}

	// Check Allow-List
	for _, call := range plan.AllowedCalls {
		if call == syscallName || call == "*" {
			return true, plan
		}
	}

	return false, plan
}
