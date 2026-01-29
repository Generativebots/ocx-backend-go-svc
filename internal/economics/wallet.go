package economics

import (
	"errors"
	"sync"
)

type ReputationWallet struct {
	AgentID      string
	Balance      float64
	TrustScore   float64
	PenaltyLevel int
}

type BillingEngine struct {
	mu      sync.Mutex
	Wallets map[string]*ReputationWallet
}

func NewBillingEngine() *BillingEngine {
	return &BillingEngine{
		Wallets: make(map[string]*ReputationWallet),
	}
}

// RegisterWallet initializes a wallet for an agent
func (be *BillingEngine) RegisterWallet(agentID string, initialBalance float64) {
	be.mu.Lock()
	defer be.mu.Unlock()
	be.Wallets[agentID] = &ReputationWallet{
		AgentID:      agentID,
		Balance:      initialBalance,
		TrustScore:   1.0,
		PenaltyLevel: 1,
	}
}

// CalculateAuditCost applies the "Governance Tax" based on drift history
func (be *BillingEngine) CalculateAuditCost(agentID string) (float64, error) {
	be.mu.Lock()
	defer be.mu.Unlock()

	wallet, exists := be.Wallets[agentID]
	if !exists {
		return 0, errors.New("AGENT_UNREGISTERED: No wallet found")
	}

	// Base cost for a standard Jury Audit
	baseCost := 1.0

	// THE GOVERNANCE TAX: Exponential increase for drifting agents
	// If Trust Score is low, the cost to "Prove Compliance" increases.
	multiplier := 1.0
	if wallet.TrustScore < 0.80 {
		multiplier = 1.5
	}
	if wallet.TrustScore < 0.70 {
		multiplier = 3.0 // Critical Audit Tax
	}

	totalCost := baseCost * multiplier

	// CHECK QUOTA: Kill task if wallet is bankrupt
	if wallet.Balance < totalCost {
		return 0, errors.New("INSUFFICIENT_REPUTATION: Wallet depleted. Human intervention required.")
	}

	// Deduct and update
	wallet.Balance -= totalCost
	return totalCost, nil
}

// InjectCredits allows for manual Bail Out
func (be *BillingEngine) InjectCredits(agentID string, amount float64, resetPenalties bool) error {
	be.mu.Lock()
	defer be.mu.Unlock()

	wallet, exists := be.Wallets[agentID]
	if !exists {
		return errors.New("AGENT_UNREGISTERED")
	}

	wallet.Balance += amount
	if resetPenalties {
		wallet.PenaltyLevel = 1
		wallet.TrustScore = 1.0 // Reset trust on manual override
	}
	return nil
}
