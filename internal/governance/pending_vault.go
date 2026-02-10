package governance

import (
	"context"
	"sync"
)

type PendingVault struct {
	mu     sync.Mutex
	active map[string]*SpeculativeAction
}

func (v *PendingVault) Add(a *SpeculativeAction) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.active[a.ActionID] = a
}

func (v *PendingVault) HandleJuryVerdict(actionID string, approved bool) error {
	v.mu.Lock()
	action, exists := v.active[actionID]
	delete(v.active, actionID) // Clear from memory
	v.mu.Unlock()

	if !exists {
		return nil
	}

	if !approved {
		// THE REVERT: Jury failed, physically undo the action
		return action.Revert(context.Background())
	}

	// ACTION COMMITTED: No further action needed
	action.Status = "COMMITTED"
	return nil
}

// ExecuteSpeculatively runs a tool in "Draft Mode" and queues the revert
func ExecuteSpeculatively(vault *PendingVault, tool RevertibleTool, args map[string]interface{}, agentId string, actionId string) (interface{}, error) {
	result, revert, err := tool.Execute(context.TODO(), args)
	if err != nil {
		return nil, err
	}

	action := &SpeculativeAction{
		ActionID: actionId,
		AgentID:  agentId,
		Revert:   revert,
		Status:   "PENDING",
	}

	vault.Add(action)

	// Return result immediately (Zero Latency)
	return result, nil
}
