package sdk

import "time"

// Verdict constants returned by the governance pipeline
const (
	// VerdictAllow — tool call approved, safe to execute
	VerdictAllow = "ALLOW"

	// VerdictBlock — tool call rejected by governance
	VerdictBlock = "BLOCK"

	// VerdictEscrow — tool call held for human review (Class B)
	VerdictEscrow = "ESCROW"

	// VerdictEscalate — tool call escalated to governance team
	VerdictEscalate = "ESCALATE"
)

// ToolRequest is what the SDK sends to the OCX gateway
type ToolRequest struct {
	// ToolName is the tool being called (e.g. "execute_payment", "delete_data")
	ToolName string `json:"tool_name"`

	// AgentID identifies the calling agent
	AgentID string `json:"agent_id"`

	// TenantID identifies the organization
	TenantID string `json:"tenant_id"`

	// Arguments are the tool call parameters
	Arguments map[string]interface{} `json:"arguments,omitempty"`

	// Model is the LLM model making the decision (optional)
	Model string `json:"model,omitempty"`

	// SessionID links multiple calls in the same conversation
	SessionID string `json:"session_id,omitempty"`

	// Protocol identifies the source framework ("openai", "langchain", "mcp", etc.)
	Protocol string `json:"protocol,omitempty"`

	// Timestamp of the request
	Timestamp time.Time `json:"timestamp"`
}

// GovernanceResult is what the OCX gateway returns
type GovernanceResult struct {
	// TransactionID is the unique ID for this governance decision
	TransactionID string `json:"transaction_id"`

	// Verdict is the governance decision: ALLOW, BLOCK, ESCROW, ESCALATE
	Verdict string `json:"verdict"`

	// ActionClass is the tool classification: CLASS_A (reversible) or CLASS_B (irreversible)
	ActionClass string `json:"action_class"`

	// Reason explains why the verdict was made
	Reason string `json:"reason,omitempty"`

	// TrustScore is the agent's current trust score (0.0-1.0)
	TrustScore float64 `json:"trust_score"`

	// GovernanceTax is the cost charged for this governance decision
	GovernanceTax float64 `json:"governance_tax"`

	// EscrowID is set when the verdict is ESCROW — use to poll for release
	EscrowID string `json:"escrow_id,omitempty"`

	// EntitlementID is the JIT permission granted for this tool call
	EntitlementID string `json:"entitlement_id,omitempty"`

	// EvidenceHash is the hash-chain evidence record ID
	EvidenceHash string `json:"evidence_hash,omitempty"`

	// TriFactorResult contains the three validation scores
	TriFactorResult *TriFactorScore `json:"tri_factor,omitempty"`

	// ProcessedAt is when the decision was made
	ProcessedAt time.Time `json:"processed_at"`
}

// TriFactorScore holds the three validation scores from §2
type TriFactorScore struct {
	Identity  ValidationResult `json:"identity"`
	Signal    ValidationResult `json:"signal"`
	Cognitive ValidationResult `json:"cognitive"`
	AllPassed bool             `json:"all_passed"`
}

// ValidationResult is one factor of the tri-factor validation
type ValidationResult struct {
	Valid  bool    `json:"valid"`
	Score  float64 `json:"score"`
	Source string  `json:"source"`
}
