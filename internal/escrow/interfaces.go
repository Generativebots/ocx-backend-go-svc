package escrow

import "context"

// JuryResult represents the result of a Jury assessment
type JuryResult struct {
	Verdict    string  // ALLOW, BLOCK, HOLD
	TrustLevel float64 // 0.0 to 1.0
	Reasoning  string
}

// EntropyResult represents the result of entropy analysis
type EntropyResult struct {
	EntropyScore float64
	Verdict      string // CLEAN, SUSPICIOUS, ENCRYPTED
	Confidence   float64
}

// JuryClient defines the interface for communicating with the Jury service
type JuryClient interface {
	EvaluateTrace(ctx context.Context, traceID string, payload []byte) (bool, error)
	EvaluateAction(ctx context.Context, agentID, action string, context map[string]interface{}) (bool, error)

	// Assess performs a full trust assessment for Tri-Factor Gate
	Assess(ctx context.Context, transactionID, tenantID string) JuryResult

	Close() error
}

// EntropyMonitor defines the interface for monitoring entropy
type EntropyMonitor interface {
	MeasureEntropy(ctx context.Context, payload []byte) (float64, error)
	CheckEntropy(ctx context.Context, data []byte, agentID string) (bool, error) // Legacy/Mock support

	// Analyze performs full signal validation for Tri-Factor Gate
	Analyze(payload []byte, tenantID string) EntropyResult

	Close() error
}
