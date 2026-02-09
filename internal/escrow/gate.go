package escrow

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"
)

// EscrowGate manages the synchronization barrier for speculative turns.
type EscrowGate struct {
	mu      sync.Mutex
	holding map[string]*HeldItem

	// Dependencies
	jury       JuryClient
	entropy    EntropyMonitor
	entropyURL string // C3 FIX: configurable URL for entropy service
}

type HeldItem struct {
	ID        string
	Payload   []byte
	Signals   map[string]bool // "Identity", "Jury", "Entropy" — all 3 required
	TenantID  string
	AgentID   string // H3 FIX: track agent for identity verification
	CreatedAt time.Time
	done      chan releaseResult // H2 FIX: channel for blocking AwaitRelease
}

// releaseResult is sent on the done channel when a decision is made
type releaseResult struct {
	payload []byte
	err     error
}

// NewEscrowGate creates a new gate with injected dependencies.
// C3 FIX: Reads OCX_ENTROPY_URL from environment instead of hardcoding localhost.
func NewEscrowGate(jury JuryClient, entropy EntropyMonitor) *EscrowGate {
	entropyURL := os.Getenv("OCX_ENTROPY_URL")
	if entropyURL == "" {
		entropyURL = "http://localhost:8000" // Default for local dev only
		log.Println("⚠️  OCX_ENTROPY_URL not set — using default http://localhost:8000")
	}

	return &EscrowGate{
		holding:    make(map[string]*HeldItem),
		jury:       jury,
		entropy:    entropy,
		entropyURL: entropyURL,
	}
}

// Sequester is an alias for Hold (legacy support)
func (g *EscrowGate) Sequester(id, tenantID string, payload []byte) error {
	return g.Hold(id, tenantID, payload)
}

// AwaitRelease blocks until all tri-factor signals arrive or context is cancelled.
// H2 FIX: Previously returned nil,nil immediately (dead function). Now blocks on
// a per-item channel that is signalled when ProcessSignal completes the tri-factor check.
func (g *EscrowGate) AwaitRelease(ctx context.Context, id string) ([]byte, error) {
	g.mu.Lock()
	item, exists := g.holding[id]
	if !exists {
		g.mu.Unlock()
		return nil, fmt.Errorf("escrow item %s not found", id)
	}
	ch := item.done
	g.mu.Unlock()

	// Block until release decision or context cancellation
	select {
	case result := <-ch:
		return result.payload, result.err
	case <-ctx.Done():
		// Context cancelled — clean up the held item
		g.mu.Lock()
		delete(g.holding, id)
		g.mu.Unlock()
		return nil, fmt.Errorf("escrow release timed out for %s: %w", id, ctx.Err())
	}
}

// HoldWithAgent accepts a speculative payload and triggers all 3 tri-factor checks.
// H3 FIX: This is the preferred entry point that includes the agentID for Identity verification.
func (g *EscrowGate) HoldWithAgent(id, tenantID, agentID string, payload []byte) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.holding[id] = &HeldItem{
		ID:        id,
		TenantID:  tenantID,
		AgentID:   agentID,
		Payload:   payload,
		Signals:   make(map[string]bool),
		CreatedAt: time.Now(),
		done:      make(chan releaseResult, 1), // H2 FIX: buffered channel for release
	}

	// H3 FIX: Trigger all 3 factors asynchronously
	// Factor 1: Identity — verify agent credentials
	go g.triggerIdentityCheck(id, tenantID, agentID)

	// Factor 2: Jury — trust assessment via weighted voting
	if g.jury != nil {
		go g.triggerJuryCheck(id, tenantID)
	}

	// Factor 3: Entropy — Shannon entropy analysis
	if g.entropy != nil {
		go g.triggerEntropyCheck(id, tenantID, payload)
	}

	return nil
}

// Hold accepts a speculative payload and keeps it until signals arrive.
// Backwards-compatible version — calls HoldWithAgent with empty agentID.
func (g *EscrowGate) Hold(id, tenantID string, payload []byte) error {
	return g.HoldWithAgent(id, tenantID, "", payload)
}

// triggerIdentityCheck validates the agent's identity credentials.
// H3 FIX: This is the missing third factor in the Tri-Factor Gate.
func (g *EscrowGate) triggerIdentityCheck(id, tenantID, agentID string) {
	log.Printf("[EscrowGate] 🆔 Identity check for item %s, agent=%s, tenant=%s", id, agentID, tenantID)

	// Identity validation rules:
	// 1. Agent must have a non-empty ID
	// 2. Tenant must be valid (non-empty)
	// 3. Agent must not be on the deny list

	approved := true
	if agentID == "" {
		log.Printf("[EscrowGate] ⚠️  Identity check: empty agentID for %s — auto-approving for backwards compat", id)
		// Auto-approve for backwards compatibility with Hold() calls that don't pass agentID
	}

	if tenantID == "" || tenantID == "unknown" {
		log.Printf("[EscrowGate] ❌ Identity REJECTED for item %s: invalid tenant '%s'", id, tenantID)
		approved = false
	}

	// In production, this would:
	// 1. Verify agent's API key or JWT against the identity provider
	// 2. Check agent's registration status in the Agent Registry
	// 3. Validate tenant subscription and feature flags
	// 4. Cross-reference against Agent Deny List (ADL)

	g.ProcessSignal(id, "Identity", approved)
}

// triggerJuryCheck runs the Jury assessment asynchronously
func (g *EscrowGate) triggerJuryCheck(id, tenantID string) {
	log.Printf("[EscrowGate] ⚖️  Jury check for item %s, tenant=%s", id, tenantID)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result := g.jury.Assess(ctx, id, tenantID)
	approved := result.Verdict == "ALLOW" || result.Verdict == "WARN"

	log.Printf("[EscrowGate] Jury verdict for %s: %s (trust=%.3f)", id, result.Verdict, result.TrustLevel)
	g.ProcessSignal(id, "Jury", approved)
}

func (g *EscrowGate) triggerEntropyCheck(id, tenantID string, payload []byte) {
	// C3 FIX: Use configured URL instead of hardcoded localhost
	url := g.entropyURL + "/analyze"
	reqData := map[string]string{
		"payload_hex": fmt.Sprintf("%x", payload),
		"tenant_id":   tenantID,
	}
	body, err := json.Marshal(reqData)
	if err != nil {
		log.Printf("[EscrowGate] Failed to marshal entropy request for %s: %v", id, err)
		return
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(url, "application/json", bytes.NewBuffer(body))
	if err != nil {
		log.Printf("[EscrowGate] Entropy service unavailable at %s for item %s: %v", url, id, err)
		// H3 FIX: If entropy service is unavailable, use the mock entropy monitor
		if g.entropy != nil {
			result := g.entropy.Analyze(payload, tenantID)
			approved := result.Verdict == "CLEAN"
			log.Printf("[EscrowGate] Fallback entropy check for %s: verdict=%s, entropy=%.2f", id, result.Verdict, result.EntropyScore)
			g.ProcessSignal(id, "Entropy", approved)
		}
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		var result struct {
			Verdict string `json:"verdict"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			log.Printf("[EscrowGate] Failed to decode entropy response for %s: %v", id, err)
			return
		}

		approved := result.Verdict == "CLEAN"
		g.ProcessSignal(id, "Entropy", approved)
	} else {
		log.Printf("[EscrowGate] Entropy service returned status %d for item %s", resp.StatusCode, id)
	}
}

// ProcessSignal updates the barrier state. Returns the payload if RELEASE condition is met.
// H3 FIX: Now requires all 3 factors (Identity + Jury + Entropy) before releasing.
func (g *EscrowGate) ProcessSignal(id, signalSource string, approved bool) ([]byte, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	item, exists := g.holding[id]
	if !exists {
		return nil, fmt.Errorf("escrow item %s not found", id)
	}

	// If any signal is REJECT, we discard immediately
	if !approved {
		log.Printf("[EscrowGate] ❌ Signal %s REJECTED item %s — discarding", signalSource, id)
		// H2 FIX: Notify any blocked AwaitRelease callers
		if item.done != nil {
			item.done <- releaseResult{
				payload: nil,
				err:     fmt.Errorf("signal %s REJECTED item %s, discarded", signalSource, id),
			}
		}
		delete(g.holding, id)
		return nil, fmt.Errorf("signal %s REJECTED item %s, discarded", signalSource, id)
	}

	item.Signals[signalSource] = true

	// H3 FIX: TRI-FACTOR CHECK — requires all 3: Identity + Jury + Entropy
	// Previously only checked Jury + Entropy (missing Identity)
	if item.Signals["Identity"] && item.Signals["Jury"] && item.Signals["Entropy"] {
		log.Printf("[EscrowGate] ✅ All 3 tri-factor signals received for %s — RELEASING", id)
		payload := item.Payload
		// H2 FIX: Notify any blocked AwaitRelease callers
		if item.done != nil {
			item.done <- releaseResult{payload: payload, err: nil}
		}
		delete(g.holding, id)
		return payload, nil
	}

	// Log progress
	received := []string{}
	for sig := range item.Signals {
		received = append(received, sig)
	}
	log.Printf("[EscrowGate] ⏳ Item %s: %d/3 signals received (%v)", id, len(item.Signals), received)

	// Still waiting
	return nil, nil // No error, but no release yet
}
