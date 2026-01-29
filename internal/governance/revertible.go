package governance

import "context"

// RevertibleTool defines a tool that can be speculatively executed
type RevertibleTool interface {
	// Execute performs the action but returns a RevertFunc
	Execute(ctx context.Context, args map[string]interface{}) (result interface{}, revert RevertFunc, err error)
}

// RevertFunc is the "Compensating Transaction" logic
type RevertFunc func(ctx context.Context) error

// SpeculativeAction tracks a pending commit
type SpeculativeAction struct {
	ActionID string
	AgentID  string
	Revert   RevertFunc
	Status   string // "PENDING", "COMMITTED", "REVERTED"
}
