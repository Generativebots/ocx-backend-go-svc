package escrow

import "context"

// JuryClient defines the interface for communicating with the Jury service
type JuryClient interface {
	EvaluateTrace(ctx context.Context, traceID string, payload []byte) (bool, error)
	EvaluateAction(ctx context.Context, agentID, action string, context map[string]interface{}) (bool, error)
	Close() error
}

// EntropyMonitor defines the interface for monitoring entropy
type EntropyMonitor interface {
	MeasureEntropy(ctx context.Context, payload []byte) (float64, error)
	CheckEntropy(ctx context.Context, data []byte, agentID string) (bool, error) // Legacy/Mock support
	Close() error
}
