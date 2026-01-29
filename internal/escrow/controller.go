package escrow

import (
	"context"
	"errors"
	"github.com/ocx/backend/internal/governance"
	"sync"
	"time"
)

type ValidationStatus int

const (
	Pending ValidationStatus = iota
	Validated
	Failed
)

type EscrowTransaction struct {
	ID             string
	ToolResponse   interface{}
	LayerStatus    map[string]ValidationStatus
	mu             sync.Mutex
	ReleaseChannel chan bool
}

type EscrowController struct {
	mu       sync.Mutex
	Registry map[string]*EscrowTransaction
}

func NewEscrowController() *EscrowController {
	return &EscrowController{
		Registry: make(map[string]*EscrowTransaction),
	}
}

func (ec *EscrowController) NewTransaction(id string, response interface{}) *EscrowTransaction {
	tx := &EscrowTransaction{
		ID:           id,
		ToolResponse: response,
		LayerStatus: map[string]ValidationStatus{
			"Identity": Pending, // Layer 1
			"Logic":    Pending, // Layer 2
			"Signal":   Pending, // Layer 3
		},
		ReleaseChannel: make(chan bool, 1),
	}
	ec.mu.Lock()
	ec.Registry[id] = tx
	ec.mu.Unlock()
	return tx
}

// UpdateLayer verifies a specific security layer and checks if the Escrow can be released
func (ec *EscrowController) UpdateLayer(id string, layer string, status ValidationStatus) {
	ec.mu.Lock()
	tx, exists := ec.Registry[id]
	ec.mu.Unlock()

	if !exists {
		return
	}

	tx.mu.Lock()
	defer tx.mu.Unlock()
	tx.LayerStatus[layer] = status

	// If any layer fails, we kill the transaction immediately
	if status == Failed {
		// Non-blocking send
		select {
		case tx.ReleaseChannel <- false:
		default:
		}
		return
	}

	// Check if all Layers are 'Validated'
	for _, s := range tx.LayerStatus {
		if s != Validated {
			return
		}
	}

	// If we reach here, Layer 1, 2, and 3 are all Green
	select {
	case tx.ReleaseChannel <- true:
	default:
	}
}

// Mock Tool Request for the example
type ToolRequest struct {
	RequestID string
	AgentID   string
	Args      map[string]interface{}
}

// ExecuteWithEscrow orchestrates the Atomic Commit
func (ec *EscrowController) ExecuteWithEscrow(ctx context.Context, vault *governance.PendingVault, tool governance.RevertibleTool, req ToolRequest) (interface{}, error) {
	// 1. Speculative Execution (Task 2)
	// We use the ID from the request
	fakeActionId := req.RequestID
	// ExecuteSpeculatively returns result, but stores RevertFunc in Vault
	rawResponse, err := governance.ExecuteSpeculatively(vault, tool, req.Args, req.AgentID, fakeActionId)
	if err != nil {
		return nil, err
	}

	// 2. Place Response in Escrow
	tx := ec.NewTransaction(req.RequestID, rawResponse)

	// 3. Trigger Async Audits (Simulation)
	// In real app, these are triggered by events. Here we simulate them passing after short delay
	go func() {
		time.Sleep(100 * time.Millisecond)
		ec.UpdateLayer(req.RequestID, "Identity", Validated)
		time.Sleep(50 * time.Millisecond)
		ec.UpdateLayer(req.RequestID, "Signal", Validated)
		time.Sleep(200 * time.Millisecond)
		ec.UpdateLayer(req.RequestID, "Logic", Validated)
	}()

	// 4. WAIT FOR ESCROW RELEASE (The "Gate")
	select {
	case success := <-tx.ReleaseChannel:
		if success {
			// SUCCESS: Release the data to the Agent
			// Mark as Committed in Vault
			vault.HandleJuryVerdict(fakeActionId, true)
			return tx.ToolResponse, nil
		} else {
			// FAILURE: Kill the transaction and Revert state
			vault.HandleJuryVerdict(fakeActionId, false)
			return nil, errors.New("ESCROW_REJECTED: Security Layer Violation")
		}
	case <-time.After(2 * time.Second):
		// TIMEOUT: Fail-Safe protection
		vault.HandleJuryVerdict(fakeActionId, false)
		return nil, errors.New("ESCROW_TIMEOUT: Governance audit took too long")
	}
}
