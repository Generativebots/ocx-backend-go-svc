package core

import "time"

// Agent represents an AI agent in the system.
type Agent struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Type       string    `json:"type"` // e.g., "Finance", "Procurement"
	Reputation float64   `json:"reputation"`
	CreatedAt  time.Time `json:"created_at"`
}

// Intent represents a proposed action by an agent.
type Intent struct {
	ID        string                 `json:"id"`
	AgentID   string                 `json:"agent_id"`
	Action    string                 `json:"action"`  // e.g., "BUY_GPU"
	Payload   map[string]interface{} `json:"payload"` // Details of the action
	Timestamp time.Time              `json:"timestamp"`
}

// TrustScore represents the evaluation of an intent.
type TrustScore struct {
	IntentID  string             `json:"intent_id"`
	Score     float64            `json:"score"`    // 0-100
	Auditors  []string           `json:"auditors"` // IDs of agents who audited
	Allowed   bool               `json:"allowed"`
	Status    string             `json:"status"` // ALLOWED, BLOCKED, NEEDS_APPROVAL
	Reason    string             `json:"reason"`
	Breakdown map[string]float64 `json:"breakdown"`
	Timestamp time.Time          `json:"timestamp"`
}

// TokenRequest is the payload for /verify-intent
type TokenRequest struct {
	AgentID string                 `json:"agent_id"`
	Action  string                 `json:"action"`
	Payload map[string]interface{} `json:"payload"`
}

type TokenResponse struct {
	Token      string             `json:"token"`
	Authorized bool               `json:"authorized"`
	Score      float64            `json:"score"`
	Reasoning  string             `json:"reasoning,omitempty"`
	Breakdown  map[string]float64 `json:"breakdown,omitempty"`
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
