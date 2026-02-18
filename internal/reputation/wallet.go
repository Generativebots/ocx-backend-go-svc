package reputation

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/ocx/backend/internal/database"
	"github.com/ocx/backend/internal/governance"
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
// Trust scores are backed by Supabase — the agents table is the source of truth.

// Default reputation parameters — overridden by tenant governance config.
var (
	// pointToScoreFactor converts int64 point amounts to float64 score deltas.
	defaultPointToScoreFactor = 0.01
	// quarantineScore is the score set when an agent is force-ejected.
	defaultQuarantineScore = 0.0
	// minCheckBalanceThreshold is the minimum score to pass CheckBalance.
	defaultMinCheckBalanceThreshold = 0.2
	// newAgentDefaultScore is the score for agents not found in the DB.
	defaultNewAgentDefaultScore = 0.3
)

type ReputationWallet struct {
	db     *database.SupabaseClient // Supabase connection for real-time trust queries
	ledger *ledger.Ledger
	mu     sync.Mutex
	// In-memory cache — only for within-request caching and local mutations
	// before flush. Supabase is the authoritative source.
	cache map[string]float64

	// Governance config — tenant-specific reputation parameters
	govConfig *governance.GovernanceConfigCache
}

// NewReputationWallet creates a Supabase-backed reputation wallet.
// The db parameter should be a valid SupabaseClient for DB-backed trust.
// If db is nil, the wallet operates in degraded in-memory mode (not recommended).
func NewReputationWallet(db *database.SupabaseClient) *ReputationWallet {
	return &ReputationWallet{
		db:     db,
		cache:  make(map[string]float64),
		ledger: ledger.NewLedger(),
	}
}

// SetGovernanceConfig attaches a governance config cache to the wallet.
// When set, trust parameters are read from tenant-specific config.
func (w *ReputationWallet) SetGovernanceConfig(cache *governance.GovernanceConfigCache) {
	w.govConfig = cache
}

// getPointToScoreFactor returns the tenant-specific conversion factor.
func (w *ReputationWallet) getPointToScoreFactor(tenantID string) float64 {
	if w.govConfig != nil {
		return w.govConfig.GetConfig(tenantID).PointToScoreFactor
	}
	return defaultPointToScoreFactor
}

// getMinBalanceThreshold returns the tenant-specific minimum balance.
func (w *ReputationWallet) getMinBalanceThreshold(tenantID string) float64 {
	if w.govConfig != nil {
		return w.govConfig.GetConfig(tenantID).MinBalanceThreshold
	}
	return defaultMinCheckBalanceThreshold
}

// getQuarantineScore returns the tenant-specific quarantine score.
func (w *ReputationWallet) getQuarantineScore(tenantID string) float64 {
	if w.govConfig != nil {
		return w.govConfig.GetConfig(tenantID).QuarantineScore
	}
	return defaultQuarantineScore
}

// getNewAgentDefaultScore returns the tenant-specific default for new agents.
func (w *ReputationWallet) getNewAgentDefaultScore(tenantID string) float64 {
	if w.govConfig != nil {
		return w.govConfig.GetConfig(tenantID).NewAgentDefaultScore
	}
	return defaultNewAgentDefaultScore
}

// Interface Compliance Methods

func (w *ReputationWallet) CheckBalance(ctx context.Context, agentID string) (bool, error) {
	tenantID := tenantFromContext(ctx) // M3 FIX
	score, _ := w.GetTrustScore(ctx, agentID, tenantID)
	return score > w.getMinBalanceThreshold(tenantID), nil
}

func (w *ReputationWallet) ApplyPenalty(ctx context.Context, agentID, txID string, amount int64) error {
	tenantID := tenantFromContext(ctx) // M3 FIX
	tax := float64(amount) * w.getPointToScoreFactor(tenantID)
	_, err := w.LevyTax(ctx, agentID, tenantID, tax, "Penalty Applied: "+txID)
	return err
}

func (w *ReputationWallet) RewardAgent(ctx context.Context, agentID string, amount int64) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	tenantID := tenantFromContext(ctx) // M3 FIX

	// Get current score from DB
	current := w.getScoreFromDB(ctx, agentID, tenantID)

	newScore := current + (float64(amount) * w.getPointToScoreFactor(tenantID))
	if newScore > 1.0 {
		newScore = 1.0
	}

	// Write back to DB and cache
	w.setScore(ctx, agentID, tenantID, newScore)
	w.ledger.Append(tenantID, "REWARD", fmt.Sprintf("Agent: %s, Diff: +%.2f, New: %.4f", agentID, float64(amount)*w.getPointToScoreFactor(tenantID), newScore))
	return nil
}

func (w *ReputationWallet) QuarantineAgent(ctx context.Context, agentID string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	tenantID := tenantFromContext(ctx) // M3 FIX

	// Set trust to zero in DB
	w.setScore(ctx, agentID, tenantID, w.getQuarantineScore(tenantID))
	w.ledger.Append(tenantID, "QUARANTINE", fmt.Sprintf("Agent: %s Force Ejected, trust=0.00", agentID))
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
	// Supabase client lifecycle is managed externally
	return nil
}

// GetTrustScore retrieves the current trust score (0.0 - 1.0) from Supabase.
// Priority: local cache (for in-flight mutations) → Supabase agents table → default.
func (w *ReputationWallet) GetTrustScore(ctx context.Context, agentID, tenantID string) (float64, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	key := tenantID + ":" + agentID

	// 1. Check in-memory cache (holds recent mutations not yet flushed)
	if score, ok := w.cache[key]; ok {
		return score, nil
	}

	// 2. Query Supabase for the authoritative trust score
	if w.db != nil {
		agent, err := w.db.GetAgent(ctx, tenantID, agentID)
		if err != nil {
			slog.Warn("Failed to fetch trust score from Supabase, using default",
				"agent_id", agentID, "tenant_id", tenantID, "error", err)
			return w.getNewAgentDefaultScore(tenantID), nil
		}
		if agent != nil {
			// Cache the DB value for subsequent calls in this request
			w.cache[key] = agent.TrustScore
			slog.Debug("Trust score loaded from Supabase",
				"agent_id", agentID, "tenant_id", tenantID, "trust_score", agent.TrustScore)
			return agent.TrustScore, nil
		}
		// Agent not found in DB — truly unknown
		slog.Info("Agent not found in Supabase, using new-agent default",
			"agent_id", agentID, "tenant_id", tenantID, "default", w.getNewAgentDefaultScore(tenantID))
	}

	// 3. Fallback: DB not available or agent not found
	return w.getNewAgentDefaultScore(tenantID), nil
}

// LevyTax deducts reputation points and persists to Supabase.
func (w *ReputationWallet) LevyTax(ctx context.Context, agentID, tenantID string, amount float64, reason string) (float64, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Get current score from DB
	current := w.getScoreFromDB(ctx, agentID, tenantID)

	newScore := current - amount
	if newScore < 0 {
		newScore = 0
	}

	// Write back to DB and cache
	w.setScore(ctx, agentID, tenantID, newScore)

	// Cryptographic Audit
	diff := fmt.Sprintf("-%.2f", amount)
	w.ledger.Append(tenantID, "LEVY_TAX", fmt.Sprintf("Agent: %s, Reason: %s, Diff: %s, New: %.4f", agentID, reason, diff, newScore))

	slog.Info("Governance Tax levied",
		"amount", amount, "agent_id", agentID, "reason", reason,
		"old_score", current, "new_score", newScore)
	return newScore, nil
}

// getScoreFromDB retrieves score from cache or DB (caller must hold lock).
func (w *ReputationWallet) getScoreFromDB(ctx context.Context, agentID, tenantID string) float64 {
	key := tenantID + ":" + agentID
	if score, ok := w.cache[key]; ok {
		return score
	}

	if w.db != nil {
		agent, err := w.db.GetAgent(ctx, tenantID, agentID)
		if err == nil && agent != nil {
			w.cache[key] = agent.TrustScore
			return agent.TrustScore
		}
	}

	return w.getNewAgentDefaultScore(tenantID)
}

// setScore updates trust score in both cache and Supabase (caller must hold lock).
func (w *ReputationWallet) setScore(ctx context.Context, agentID, tenantID string, score float64) {
	key := tenantID + ":" + agentID
	w.cache[key] = score

	// Write back to Supabase
	if w.db != nil {
		err := w.db.UpdateAgent(ctx, &database.Agent{
			AgentID:    agentID,
			TenantID:   tenantID,
			TrustScore: score,
		})
		if err != nil {
			slog.Error("Failed to write trust score to Supabase",
				"agent_id", agentID, "tenant_id", tenantID,
				"score", score, "error", err)
		} else {
			slog.Info("Trust score written to Supabase",
				"agent_id", agentID, "tenant_id", tenantID, "score", score)
		}
	}
}
