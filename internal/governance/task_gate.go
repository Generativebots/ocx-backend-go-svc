package governance

import (
	"errors"
	"sync"
)

// TaskGate manages the "One-at-a-Time" execution rule for Speculative agents
type TaskGate struct {
	mu          sync.RWMutex
	activeTasks map[string]string // Map[AgentID]TransactionID
}

func NewTaskGate() *TaskGate {
	return &TaskGate{
		activeTasks: make(map[string]string),
	}
}

// AcquireLock checks if the agent is already in a speculative state
func (tg *TaskGate) AcquireLock(agentID, txID string) error {
	tg.mu.Lock()
	defer tg.mu.Unlock()

	if existingTx, busy := tg.activeTasks[agentID]; busy {
		return errors.New("AGENT_BUSY: Task " + existingTx + " is still in Speculative Escrow. Please wait.")
	}

	tg.activeTasks[agentID] = txID
	return nil
}

// ReleaseLock frees the agent to start a new task after Commit or Revert
func (tg *TaskGate) ReleaseLock(agentID string) {
	tg.mu.Lock()
	defer tg.mu.Unlock()
	delete(tg.activeTasks, agentID)
}
