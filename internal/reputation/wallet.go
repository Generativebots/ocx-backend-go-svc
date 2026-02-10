package reputation

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/ocx/backend/internal/ledger"
	"github.com/ocx/backend/internal/multitenancy"
)

// tenantFromContext extracts the tenant ID from context.
// M3 FIX: Falls back to "default-tenant" for callers without tenant context
// (e.g. gRPC interceptors, tests, internal calls).
func tenantFromContext(ctx context.Context) string {
	if id, err := multitenancy.GetTenantID(ctx); err == nil && id != "" {
		return id
	}
	return "default-tenant"
}

// ReputationWallet manages 'Trust Scores' as a currency.
// It implements the 'Governance Tax' logic.

const (
	// defaultWalletTrustScore is the neutral starting score for new agents.
	defaultWalletTrustScore = 0.5
	// pointToScoreFactor converts int64 point amounts to float64 score deltas.
	pointToScoreFactor = 0.01
	// quarantineScore is the score set when an agent is force-ejected.
	quarantineScore = 0.0
	// minCheckBalanceThreshold is the minimum score to pass CheckBalance.
	minCheckBalanceThreshold = 0.2
)

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
	tenantID := tenantFromContext(ctx) // M3 FIX
	score, _ := w.GetTrustScore(ctx, agentID, tenantID)
	return score > minCheckBalanceThreshold, nil
}

func (w *ReputationWallet) ApplyPenalty(ctx context.Context, agentID, txID string, amount int64) error {
	tenantID := tenantFromContext(ctx) // M3 FIX
	tax := float64(amount) * pointToScoreFactor
	_, err := w.LevyTax(ctx, agentID, tenantID, tax, "Penalty Applied: "+txID)
	return err
}

func (w *ReputationWallet) RewardAgent(ctx context.Context, agentID string, amount int64) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	tenantID := tenantFromContext(ctx) // M3 FIX
	key := tenantID + ":" + agentID
	current, ok := w.cache[key]
	if !ok {
		current = defaultWalletTrustScore
	}
	newScore := current + (float64(amount) * pointToScoreFactor)
	if newScore > 1.0 {
		newScore = 1.0
	}
	w.cache[key] = newScore
	w.ledger.Append(tenantID, "REWARD", fmt.Sprintf("Agent: %s, Diff: +%.2f", agentID, float64(amount)*pointToScoreFactor))
	return nil
}

func (w *ReputationWallet) QuarantineAgent(ctx context.Context, agentID string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	tenantID := tenantFromContext(ctx) // M3 FIX
	key := tenantID + ":" + agentID
	w.cache[key] = quarantineScore
	w.ledger.Append(tenantID, "QUARANTINE", fmt.Sprintf("Agent: %s Force Ejected", agentID))
	return nil
}

func (w *ReputationWallet) ProcessRecovery(ctx context.Context, agentID string, stakeAmount int64) error {
	return w.RewardAgent(ctx, agentID, stakeAmount)
}

func (w *ReputationWallet) GetAgentReputation(ctx context.Context, agentID string) (*AgentReputation, error) {
	tenantID := tenantFromContext(ctx) // M3 FIX
	score, _ := w.GetTrustScore(ctx, agentID, tenantID)
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
	return defaultWalletTrustScore, nil // Default neutral trust
}

// LevyTax deducts reputation points.
func (w *ReputationWallet) LevyTax(ctx context.Context, agentID, tenantID string, amount float64, reason string) (float64, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	key := tenantID + ":" + agentID
	current, ok := w.cache[key]
	if !ok {
		current = defaultWalletTrustScore
	}

	newScore := current - amount
	if newScore < 0 {
		newScore = 0
	}
	w.cache[key] = newScore

	// Cryptographic Audit
	diff := fmt.Sprintf("-%.2f", amount)
	w.ledger.Append(tenantID, "LEVY_TAX", fmt.Sprintf("Agent: %s, Reason: %s, Diff: %s", agentID, reason, diff))

	slog.Info("Governance Tax: - for (Reason: ). New Score", "amount", amount, "agent_i_d", agentID, "reason", reason, "new_score", newScore)
	return newScore, nil
}
