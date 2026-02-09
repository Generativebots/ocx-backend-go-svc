// Package escrow — Micropayment Escrow (Patent §4.2)
//
// Implements: "Programmable funds held in socket state, released upon
// verified intent delivery."
//
// Funds are escrowed when a Class B action enters the gate, and released
// (or refunded) once the tri-factor check completes.
package escrow

import (
	"fmt"
	"log"
	"sync"
	"time"
)

// MicropaymentEscrow holds funds in-flight until verified release.
// This bridges the EscrowGate (signal barrier) with the economics engine
// (billing) so that charges are only finalized upon successful governance.
type MicropaymentEscrow struct {
	mu     sync.Mutex
	ledger map[string]*EscrowedFund // escrowItemID -> fund
	logger *log.Logger

	// External callback to finalize billing (set by MonetizationEngine)
	onRelease func(tenantID, agentID string, amount float64) error
	onRefund  func(tenantID, agentID string, amount float64) error
}

// EscrowedFund represents funds held during speculative execution.
type EscrowedFund struct {
	ID            string
	TenantID      string
	AgentID       string
	Amount        float64
	ToolID        string
	ActionClass   string // "A" or "B"
	RiskMultipler float64
	HeldAt        time.Time
	Status        EscrowFundStatus
	ReleasedAt    *time.Time
}

// EscrowFundStatus tracks the state of escrowed funds.
type EscrowFundStatus string

const (
	FundStatusHeld     EscrowFundStatus = "HELD"
	FundStatusReleased EscrowFundStatus = "RELEASED"
	FundStatusRefunded EscrowFundStatus = "REFUNDED"
	FundStatusExpired  EscrowFundStatus = "EXPIRED"
)

// NewMicropaymentEscrow creates a new micropayment escrow instance.
func NewMicropaymentEscrow() *MicropaymentEscrow {
	return &MicropaymentEscrow{
		ledger: make(map[string]*EscrowedFund),
		logger: log.New(log.Writer(), "[MicropaymentEscrow] ", log.LstdFlags),
	}
}

// SetCallbacks wires the escrow to the economics engine for actual billing.
func (me *MicropaymentEscrow) SetCallbacks(
	onRelease func(tenantID, agentID string, amount float64) error,
	onRefund func(tenantID, agentID string, amount float64) error,
) {
	me.onRelease = onRelease
	me.onRefund = onRefund
}

// HoldFunds escrrows the computed cost of a tool call until governance completes.
// Called by EscrowGate.Hold() or EscrowGate.HoldWithAgent().
func (me *MicropaymentEscrow) HoldFunds(
	escrowItemID, tenantID, agentID, toolID, actionClass string,
	baseCost, riskMultiplier float64,
) (*EscrowedFund, error) {
	me.mu.Lock()
	defer me.mu.Unlock()

	if _, exists := me.ledger[escrowItemID]; exists {
		return nil, fmt.Errorf("funds already escrowed for item %s", escrowItemID)
	}

	amount := baseCost * riskMultiplier
	if actionClass == "B" {
		amount *= 2.0 // Class B actions are 2× cost
	}

	fund := &EscrowedFund{
		ID:            escrowItemID,
		TenantID:      tenantID,
		AgentID:       agentID,
		Amount:        amount,
		ToolID:        toolID,
		ActionClass:   actionClass,
		RiskMultipler: riskMultiplier,
		HeldAt:        time.Now(),
		Status:        FundStatusHeld,
	}

	me.ledger[escrowItemID] = fund
	me.logger.Printf("💰 Held $%.4f for item %s (agent=%s, tool=%s, class=%s)",
		amount, escrowItemID, agentID, toolID, actionClass)
	return fund, nil
}

// ReleaseFunds finalizes the charge after successful tri-factor verification.
// Called when ProcessSignal() completes with all 3 factors approved.
func (me *MicropaymentEscrow) ReleaseFunds(escrowItemID string) error {
	me.mu.Lock()
	defer me.mu.Unlock()

	fund, exists := me.ledger[escrowItemID]
	if !exists {
		return fmt.Errorf("no escrowed funds for item %s", escrowItemID)
	}
	if fund.Status != FundStatusHeld {
		return fmt.Errorf("funds for %s already %s", escrowItemID, fund.Status)
	}

	now := time.Now()
	fund.Status = FundStatusReleased
	fund.ReleasedAt = &now

	me.logger.Printf("✅ Released $%.4f for item %s (held for %v)",
		fund.Amount, escrowItemID, now.Sub(fund.HeldAt))

	// Finalize billing
	if me.onRelease != nil {
		if err := me.onRelease(fund.TenantID, fund.AgentID, fund.Amount); err != nil {
			me.logger.Printf("⚠️  Billing finalization failed for %s: %v", escrowItemID, err)
			return err
		}
	}

	return nil
}

// RefundFunds returns the escrowed amount after a rejected signal.
// Called when ProcessSignal() receives a REJECT from any factor.
func (me *MicropaymentEscrow) RefundFunds(escrowItemID string) error {
	me.mu.Lock()
	defer me.mu.Unlock()

	fund, exists := me.ledger[escrowItemID]
	if !exists {
		return fmt.Errorf("no escrowed funds for item %s", escrowItemID)
	}
	if fund.Status != FundStatusHeld {
		return fmt.Errorf("funds for %s already %s", escrowItemID, fund.Status)
	}

	now := time.Now()
	fund.Status = FundStatusRefunded
	fund.ReleasedAt = &now

	me.logger.Printf("🔄 Refunded $%.4f for item %s (signal rejected)",
		fund.Amount, escrowItemID)

	if me.onRefund != nil {
		if err := me.onRefund(fund.TenantID, fund.AgentID, fund.Amount); err != nil {
			me.logger.Printf("⚠️  Refund failed for %s: %v", escrowItemID, err)
		}
	}

	return nil
}

// GetFund returns the current state of an escrowed fund.
func (me *MicropaymentEscrow) GetFund(escrowItemID string) (*EscrowedFund, bool) {
	me.mu.Lock()
	defer me.mu.Unlock()
	fund, ok := me.ledger[escrowItemID]
	return fund, ok
}

// GetHeldFunds returns all currently held (in-flight) funds.
func (me *MicropaymentEscrow) GetHeldFunds() []*EscrowedFund {
	me.mu.Lock()
	defer me.mu.Unlock()

	var held []*EscrowedFund
	for _, fund := range me.ledger {
		if fund.Status == FundStatusHeld {
			held = append(held, fund)
		}
	}
	return held
}

// TotalHeld returns the total amount of funds currently in escrow.
func (me *MicropaymentEscrow) TotalHeld() float64 {
	me.mu.Lock()
	defer me.mu.Unlock()

	var total float64
	for _, fund := range me.ledger {
		if fund.Status == FundStatusHeld {
			total += fund.Amount
		}
	}
	return total
}

// ExpireStale marks funds older than maxAge as expired and refunds them.
func (me *MicropaymentEscrow) ExpireStale(maxAge time.Duration) int {
	me.mu.Lock()
	defer me.mu.Unlock()

	cutoff := time.Now().Add(-maxAge)
	expired := 0

	for _, fund := range me.ledger {
		if fund.Status == FundStatusHeld && fund.HeldAt.Before(cutoff) {
			now := time.Now()
			fund.Status = FundStatusExpired
			fund.ReleasedAt = &now
			expired++
			me.logger.Printf("⏰ Expired $%.4f for stale item %s (held since %s)",
				fund.Amount, fund.ID, fund.HeldAt.Format(time.RFC3339))
		}
	}

	return expired
}
