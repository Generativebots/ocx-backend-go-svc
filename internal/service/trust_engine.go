package service

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/ocx/backend/internal/config"
	"github.com/ocx/backend/internal/core"
	"github.com/ocx/backend/internal/multitenancy"
)

// TrustEngine Orchestrates the trust evaluation
type TrustEngine struct {
	client        *http.Client
	configManager *config.Manager
	billing       *BillingService
	policyMap     map[string]string // Simple in-memory policy store: AgentID -> Status
}

// NewTrustEngine creates a new instance
func NewTrustEngine(cm *config.Manager, billing *BillingService) *TrustEngine {
	return &TrustEngine{
		client:        &http.Client{Timeout: 5 * time.Second},
		configManager: cm,
		billing:       billing,
		policyMap:     make(map[string]string),
	}
}

// UpdateAgentPolicy updates the runtime policy for an agent
func (e *TrustEngine) UpdateAgentPolicy(agentID, action string) {
	switch action {
	case "THROTTLE":
		e.policyMap[agentID] = "THROTTLED"
	case "RESTORE":
		delete(e.policyMap, agentID) // Remove throttle
	}
}

// EvaluateIntent calls the Python Trust Registry (The Jury)
// Now includes a "Fast Fail" check for throttled agents.
func (e *TrustEngine) EvaluateIntent(ctx context.Context, req core.TokenRequest) core.TrustScore {
	// 0. FAST FAIL: Check Local Policy
	if status, ok := e.policyMap[req.AgentID]; ok && status == "THROTTLED" {
		fmt.Printf("⛔ [Fast Fail] Agent %s is THROTTLED locally. Rejecting request.\n", req.AgentID)
		return core.TrustScore{
			Allowed:   false,
			Score:     0,
			Reason:    "Agent is currently THROTTLED due to reliability issues. (Fast Fail)",
			Timestamp: time.Now(),
		}
	}

	// Get Tenant ID
	tenantID, err := multitenancy.GetTenantID(ctx)
	if err != nil {
		fmt.Printf("⚠️ Tenant Context Missing: %v\n", err)
		// valid for non-tenant flows or fallback
		tenantID = "default-org"
	}

	// Resolve Configuration
	cfg := e.configManager.Get(tenantID)

	// 1. Prepare Request to Python Registry
	// In production, the AGENT signs this request.
	// Matching keys defined in verifier.py
	secretKey := cfg.Trust.Secrets.VisualKey
	if req.AgentID == "test-agent" {
		secretKey = cfg.Trust.Secrets.TestKey
	}

	payloadBytes, _ := json.Marshal(map[string]interface{}{
		"proposed_action": req.Action,
		"context":         req.Payload, // Assuming req.Payload is the correct field for context
	})

	// Create HMAC-SHA256
	mac := hmac.New(sha256.New, []byte(secretKey))
	mac.Write(payloadBytes)
	signature := hex.EncodeToString(mac.Sum(nil))

	registryReq := map[string]interface{}{
		"agent_id":        req.AgentID,
		"tenant_id":       tenantID, // Propagate Tenant Context
		"proposed_action": req.Action,
		"context":         req.Payload, // Assuming req.Payload is the correct field for context
		"signature":       signature,   // Added Protocol Signature
	}
	jsonData, _ := json.Marshal(registryReq)

	// 2. Call The Heart (Python Service)
	resp, err := http.Post("http://localhost:8000/evaluate", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		fmt.Printf("❌ Failed to contact Trust Registry: %v\n", err)
		return core.TrustScore{Allowed: false, Reason: "Trust Registry Unreachable", Score: 0}
	}
	defer resp.Body.Close()

	// 3. Parse Response
	var registryResp struct {
		TrustScore  float64            `json:"trust_score"`
		SafetyToken string             `json:"safety_token"`
		Reasoning   string             `json:"reasoning"`
		Status      string             `json:"status"`
		Breakdown   map[string]float64 `json:"breakdown"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&registryResp); err != nil {
		fmt.Printf("❌ Failed to decode Trust Registry response: %v\n", err)
		return core.TrustScore{Allowed: false, Reason: "Invalid Registry Response", Score: 0}
	}

	// 4. Map to Internal Domain
	allowed := registryResp.Status != "BLOCKED"

	// WEIGHTED TRUST CALCULATION
	// Formula: (w1 * Audit) + (w2 * Reputation) + (w3 * Attestation) + (w4 * History)
	weights := cfg.Trust.Weights
	breakdown := registryResp.Breakdown
	weightedScore := (weights.Audit * getScore(breakdown, "audit")) +
		(weights.Reputation * getScore(breakdown, "reputation")) +
		(weights.Attestation * getScore(breakdown, "attestation")) +
		(weights.History * getScore(breakdown, "history"))

	// Normalize if needed, or ensure python returns 0-1 or 0-100.
	// Assuming Python returns 0-1 component scores.

	// TRUST TAX & BILLING
	// Log transaction and calculate tax
	tax, _ := e.billing.LogTransaction(ctx, tenantID, fmt.Sprintf("req-%d", time.Now().Unix()), weightedScore)

	fmt.Printf("❤️  Trust Registry Verdict: %s (Calculated: %.2f | Tax: %.4f) | %s\n",
		registryResp.Status, weightedScore, tax, registryResp.Reasoning)

	return core.TrustScore{
		IntentID: fmt.Sprintf("intent-%d", time.Now().Unix()),
		// Score:     registryResp.TrustScore * 100,
		Score:     weightedScore * 100, // Use Weighted Score
		Auditors:  []string{"OCX-Trust-Registry-v1", "Vertex-AI-Judge"},
		Allowed:   allowed,
		Reason:    registryResp.Reasoning + fmt.Sprintf(" [Trust Tax: %.4f]", tax),
		Breakdown: registryResp.Breakdown,
		Timestamp: time.Now(),
	}
}

// Helper to ensure float zero value if missing
func getScore(m map[string]float64, key string) float64 {
	if v, ok := m[key]; ok {
		return v
	}
	return 0.0
}

// GenerateToken creates a mock signed token
func (e *TrustEngine) GenerateToken(agentID string, score float64) string {
	// In production, this would be a real JWT signed with a private key
	return fmt.Sprintf("ocx-trust-token.%s.%d.signature", agentID, int64(score))
}
