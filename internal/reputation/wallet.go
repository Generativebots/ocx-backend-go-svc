package reputation

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/ocx/backend/internal/ledger"
)

// ReputationWallet manages 'Trust Scores' as a currency.
// It implements the 'Governance Tax' logic.
type ReputationWallet struct {
	db     *sql.DB // Abstracted Spanner/Postgres connection
	ledger *ledger.Ledger
	mu     sync.Mutex
	// In-memory cache for demo
	cache map[string]float64
}

// (Removed redundant function)

// NewWallet creates a SQLite-backed wallet (Legacy Helper)
func NewWallet(dbPath string) (ReputationStore, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite: %w", err)
	}
	return NewReputationWallet(db), nil
}

// NewReputationWallet creates a new SQLite wallet
func NewReputationWallet(db *sql.DB) *ReputationWallet {
	// Ensure table exists
	// ...
	return &ReputationWallet{
		db:     db,
		cache:  make(map[string]float64),
		ledger: ledger.NewLedger(), // Initialize ledger
	}
}

// Interface Compliance Methods

func (w *ReputationWallet) CheckBalance(ctx context.Context, agentID string) (bool, error) {
	score, _ := w.GetTrustScore(ctx, agentID, "default-tenant")
	return score > 0.2, nil // Minimum threshold
}

func (w *ReputationWallet) ApplyPenalty(ctx context.Context, agentID, txID string, amount int64) error {
	// Map int64 amount to float score (e.g. 1 point = 0.01 score)
	tax := float64(amount) * 0.01
	_, err := w.LevyTax(ctx, agentID, "default-tenant", tax, "Penalty Applied: "+txID)
	return err
}

func (w *ReputationWallet) RewardAgent(ctx context.Context, agentID string, amount int64) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	tenantID := "default-tenant" // TODO: Context should carry tenant
	key := tenantID + ":" + agentID
	current, ok := w.cache[key]
	if !ok {
		current = 0.5
	}
	newScore := current + (float64(amount) * 0.01)
	if newScore > 1.0 {
		newScore = 1.0
	}
	w.cache[key] = newScore
	w.ledger.Append(tenantID, "REWARD", fmt.Sprintf("Agent: %s, Diff: +%.2f", agentID, float64(amount)*0.01))
	return nil
}

func (w *ReputationWallet) QuarantineAgent(ctx context.Context, agentID string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	// Set score to 0 to block
	key := "default-tenant:" + agentID // Simplified for legacy interface
	w.cache[key] = 0.0
	w.ledger.Append("default-tenant", "QUARANTINE", fmt.Sprintf("Agent: %s Force Ejected", agentID))
	return nil
}

func (w *ReputationWallet) ProcessRecovery(ctx context.Context, agentID string, stakeAmount int64) error {
	return w.RewardAgent(ctx, agentID, stakeAmount)
}

func (w *ReputationWallet) GetAgentReputation(ctx context.Context, agentID string) (*AgentReputation, error) {
	score, _ := w.GetTrustScore(ctx, agentID, "default-tenant")
	return &AgentReputation{
		AgentID:         agentID,
		ReputationScore: score,
		LastUpdated:     time.Now(),
		FirstSeen:       time.Now(),
	}, nil
}

func (w *ReputationWallet) Close() error {
	if w.db != nil {
		return w.db.Close()
	}
	return nil
}

// GetTrustScore retrieves the current score (0.0 - 1.0).
func (w *ReputationWallet) GetTrustScore(ctx context.Context, agentID, tenantID string) (float64, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	key := tenantID + ":" + agentID
	if score, ok := w.cache[key]; ok {
		return score, nil
	}
	return 0.5, nil // Default neutral trust
}

// LevyTax deducts reputation points.
func (w *ReputationWallet) LevyTax(ctx context.Context, agentID, tenantID string, amount float64, reason string) (float64, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	key := tenantID + ":" + agentID
	current, ok := w.cache[key]
	if !ok {
		current = 0.5
	}

	newScore := current - amount
	if newScore < 0 {
		newScore = 0
	}
	w.cache[key] = newScore

	// Cryptographic Audit
	diff := fmt.Sprintf("-%.2f", amount)
	w.ledger.Append(tenantID, "LEVY_TAX", fmt.Sprintf("Agent: %s, Reason: %s, Diff: %s", agentID, reason, diff))

	log.Printf("⚖️ Governance Tax: -%.2f for %s (Reason: %s). New Score: %.2f", amount, agentID, reason, newScore)

	return newScore, nil
}
