package escrow

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// EscrowGate manages the synchronization barrier for speculative turns.
type EscrowGate struct {
	mu      sync.Mutex
	holding map[string]*HeldItem

	// Dependencies
	jury    JuryClient
	entropy EntropyMonitor
}

type HeldItem struct {
	ID        string
	Payload   []byte
	Signals   map[string]bool // "Jury", "Entropy"
	TenantID  string
	CreatedAt time.Time
}

// NewEscrowGate creates a new gate with injected dependencies
func NewEscrowGate(jury JuryClient, entropy EntropyMonitor) *EscrowGate {
	return &EscrowGate{
		holding: make(map[string]*HeldItem),
		jury:    jury,
		entropy: entropy,
	}
}

// Sequester is an alias for Hold (legacy support)
func (g *EscrowGate) Sequester(id, tenantID string, payload []byte) error {
	return g.Hold(id, tenantID, payload)
}

// AwaitRelease blocks until a decision is made (Mock implementation for loadtest)
func (g *EscrowGate) AwaitRelease(ctx context.Context, id string) ([]byte, error) {
	return nil, nil
}

// Hold accepts a speculative payload and keeps it until signals arrive.
func (g *EscrowGate) Hold(id, tenantID string, payload []byte) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.holding[id] = &HeldItem{
		ID:        id,
		TenantID:  tenantID,
		Payload:   payload,
		Signals:   make(map[string]bool),
		CreatedAt: time.Now(),
	}

	// Async: Trigger Entropy Check
	if g.entropy != nil {
		// We use a mock check here for now, or assume the monitor handles it
		go g.triggerEntropyCheck(id, tenantID, payload)
	}

	return nil
}

func (g *EscrowGate) triggerEntropyCheck(id, tenantID string, payload []byte) {
	// Simple HTTP call to Python service
	// In production, use gRPC or Message Queue
	url := "http://localhost:8000/analyze"
	reqData := map[string]string{
		"payload_hex": fmt.Sprintf("%x", payload),
		"tenant_id":   tenantID,
	}
	body, _ := json.Marshal(reqData)

	resp, err := http.Post(url, "application/json", bytes.NewBuffer(body))
	if err != nil {
		fmt.Printf("Entropy Service Unavailable: %v\n", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		var result struct {
			Verdict string `json:"verdict"`
		}
		json.NewDecoder(resp.Body).Decode(&result)

		approved := result.Verdict == "CLEAN"
		g.ProcessSignal(id, "Entropy", approved)
	}
}

// ProcessSignal updates the barrier state. Returns the payload if RELEASE condition is met.
func (g *EscrowGate) ProcessSignal(id, signalSource string, approved bool) ([]byte, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	item, exists := g.holding[id]
	if !exists {
		return nil, fmt.Errorf("escrow item %s not found", id)
	}

	// If any signal is REJECT, we discard immediately
	if !approved {
		delete(g.holding, id)
		return nil, fmt.Errorf("signal %s REJECTED item %s, discarded", signalSource, id)
	}

	item.Signals[signalSource] = true

	// TRI-FACTOR CHECK (Jury + Entropy)
	// In production, this logic would be more complex/configurable
	if item.Signals["Jury"] && item.Signals["Entropy"] {
		// Success! Release payload
		payload := item.Payload
		delete(g.holding, id)
		return payload, nil
	}

	// Still waiting
	return nil, nil // No error, but no release yet
}
