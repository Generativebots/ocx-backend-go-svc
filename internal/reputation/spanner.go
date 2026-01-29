package reputation

import (
	"context"
	"fmt"
	"log"
	"time"

	"cloud.google.com/go/spanner"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
)

// SpannerWallet implements ReputationWallet using Cloud Spanner
type SpannerWallet struct {
	client *spanner.Client
	logger *log.Logger
}

// NewSpannerWallet creates a ReputationWallet backed by Spanner
func NewSpannerWallet(project, instance, dbName string) (ReputationStore, error) {
	ctx := context.Background()
	dbPath := fmt.Sprintf("projects/%s/instances/%s/databases/%s", project, instance, dbName)

	client, err := spanner.NewClient(ctx, dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create Spanner client: %w", err)
	}

	return &SpannerWallet{
		client: client,
		logger: log.New(log.Writer(), "[SpannerWallet] ", log.LstdFlags),
	}, nil
}

// WalletReputation internal struct for DB rows
type WalletReputation struct {
	AgentID         string
	TrustScore      float64
	BehavioralDrift float64
	GovTaxBalance   int64
	IsFrozen        bool
	UpdatedAt       time.Time
}

// CheckBalance verifies agent has sufficient GovTaxBalance and is not frozen
func (sw *SpannerWallet) CheckBalance(ctx context.Context, agentID string) (bool, error) {
	// Use stale read (15-second staleness) for performance
	roTx := sw.client.ReadOnlyTransaction().WithTimestampBound(spanner.MaxStaleness(15 * time.Second))
	defer roTx.Close()

	row, err := roTx.ReadRow(ctx, "Agents", spanner.Key{agentID}, []string{"GovTaxBalance", "IsFrozen"})
	if err != nil {
		if spanner.ErrCode(err) == codes.NotFound {
			// New agent - initialize
			if err := sw.initializeAgent(ctx, agentID); err != nil {
				return false, err
			}
			return true, nil
		}
		return false, err
	}

	var balance int64
	var isFrozen bool
	if err := row.Columns(&balance, &isFrozen); err != nil {
		return false, err
	}

	if isFrozen {
		sw.logger.Printf("❄️ Agent %s is frozen", agentID)
		return false, nil
	}

	if balance < 100 {
		sw.logger.Printf("💸 Agent %s has insufficient balance: %d", agentID, balance)
		return false, nil
	}

	return true, nil
}

// ApplyPenalty deducts from GovTaxBalance and increases BehavioralDrift
func (sw *SpannerWallet) ApplyPenalty(ctx context.Context, agentID, txID string, amount int64) error {
	_, err := sw.client.ReadWriteTransaction(ctx, func(ctx context.Context, txn *spanner.ReadWriteTransaction) error {
		// Fetch current state
		row, err := txn.ReadRow(ctx, "Agents", spanner.Key{agentID}, []string{"GovTaxBalance", "BehavioralDrift"})
		if err != nil {
			return err
		}

		var currentBalance int64
		var currentDrift float64
		if err := row.Columns(&currentBalance, &currentDrift); err != nil {
			return err
		}

		// Calculate new state
		newBalance := currentBalance - amount
		newDrift := currentDrift + 0.1

		// Update agent
		agentMutation := spanner.Update("Agents",
			[]string{"AgentID", "GovTaxBalance", "BehavioralDrift", "UpdatedAt"},
			[]interface{}{agentID, newBalance, newDrift, spanner.CommitTimestamp},
		)

		// Insert audit log
		auditID := fmt.Sprintf("penalty-%s-%d", agentID, time.Now().Unix())
		auditMutation := spanner.Insert("ReputationAudit",
			[]string{"AgentID", "AuditID", "TransactionID", "Verdict", "TaxLevied", "CreatedAt"},
			[]interface{}{agentID, auditID, txID, "FAILURE", amount, spanner.CommitTimestamp},
		)

		return txn.BufferWrite([]*spanner.Mutation{agentMutation, auditMutation})
	})

	if err == nil {
		sw.logger.Printf("💰 Applied penalty of %d to agent %s", amount, agentID)
	}

	return err
}

// RewardAgent increases GovTaxBalance and improves TrustScore
func (sw *SpannerWallet) RewardAgent(ctx context.Context, agentID string, amount int64) error {
	_, err := sw.client.ReadWriteTransaction(ctx, func(ctx context.Context, txn *spanner.ReadWriteTransaction) error {
		// Fetch current state
		row, err := txn.ReadRow(ctx, "Agents", spanner.Key{agentID}, []string{"GovTaxBalance", "TrustScore"})
		if err != nil {
			return err
		}

		var currentBalance int64
		var currentTrust float64
		if err := row.Columns(&currentBalance, &currentTrust); err != nil {
			return err
		}

		// Calculate new state
		newBalance := currentBalance + amount
		newTrust := min(1.0, currentTrust+0.01)

		// Update agent
		agentMutation := spanner.Update("Agents",
			[]string{"AgentID", "GovTaxBalance", "TrustScore", "UpdatedAt"},
			[]interface{}{agentID, newBalance, newTrust, spanner.CommitTimestamp},
		)

		// Log reward
		auditID := fmt.Sprintf("reward-%s-%d", agentID, time.Now().Unix())
		auditMutation := spanner.Insert("ReputationAudit",
			[]string{"AgentID", "AuditID", "Verdict", "TaxLevied", "CreatedAt"},
			[]interface{}{agentID, auditID, "REWARD", -amount, spanner.CommitTimestamp},
		)

		return txn.BufferWrite([]*spanner.Mutation{agentMutation, auditMutation})
	})

	return err
}

// QuarantineAgent sets IsFrozen flag
func (sw *SpannerWallet) QuarantineAgent(ctx context.Context, agentID string) error {
	_, err := sw.client.Apply(ctx, []*spanner.Mutation{
		spanner.Update("Agents",
			[]string{"AgentID", "IsFrozen", "UpdatedAt"},
			[]interface{}{agentID, true, spanner.CommitTimestamp},
		),
	})

	if err == nil {
		sw.logger.Printf("🔒 Quarantined agent %s", agentID)
	}

	return err
}

// ProcessRecovery allows agent to unfreeze by staking
func (sw *SpannerWallet) ProcessRecovery(ctx context.Context, agentID string, stakeAmount int64) error {
	const MinRecoveryStake = 5000

	if stakeAmount < MinRecoveryStake {
		return fmt.Errorf("insufficient stake: %d < %d", stakeAmount, MinRecoveryStake)
	}

	_, err := sw.client.ReadWriteTransaction(ctx, func(ctx context.Context, txn *spanner.ReadWriteTransaction) error {
		// Verify frozen status
		row, err := txn.ReadRow(ctx, "Agents", spanner.Key{agentID}, []string{"IsFrozen", "GovTaxBalance"})
		if err != nil {
			return err
		}

		var isFrozen bool
		var currentBalance int64
		if err := row.Columns(&isFrozen, &currentBalance); err != nil {
			return err
		}

		if !isFrozen {
			return fmt.Errorf("agent %s is not quarantined", agentID)
		}

		// Unfreeze and add stake
		agentMutation := spanner.Update("Agents",
			[]string{"AgentID", "IsFrozen", "GovTaxBalance", "BehavioralDrift", "UpdatedAt"},
			[]interface{}{agentID, false, currentBalance + stakeAmount, 0.0, spanner.CommitTimestamp},
		)

		// Log recovery
		auditID := fmt.Sprintf("recovery-%s-%d", agentID, time.Now().Unix())
		auditMutation := spanner.Insert("ReputationAudit",
			[]string{"AgentID", "AuditID", "Verdict", "TaxLevied", "CreatedAt"},
			[]interface{}{agentID, auditID, "RECOVERED", -stakeAmount, spanner.CommitTimestamp},
		)

		return txn.BufferWrite([]*spanner.Mutation{agentMutation, auditMutation})
	})

	if err == nil {
		sw.logger.Printf("🔓 Agent %s recovered with stake %d", agentID, stakeAmount)
	}

	return err
}

// GetAgentReputation retrieves current reputation state
// GetWalletReputation retrieves wallet reputation for an agent (internal method)
func (sw *SpannerWallet) GetWalletReputation(ctx context.Context, agentID string) (*WalletReputation, error) {
	row, err := sw.client.Single().ReadRow(ctx, "Agents", spanner.Key{agentID},
		[]string{"AgentID", "TrustScore", "BehavioralDrift", "GovTaxBalance", "IsFrozen", "UpdatedAt"},
	)
	if err != nil {
		return nil, err
	}

	var rep WalletReputation
	err = row.Columns(
		&rep.AgentID,
		&rep.TrustScore,
		&rep.BehavioralDrift,
		&rep.GovTaxBalance,
		&rep.IsFrozen,
		&rep.UpdatedAt,
	)

	return &rep, err
}

// GetAgentReputation implements ReputationStore interface
// Converts WalletReputation to AgentReputation for interface compatibility
func (sw *SpannerWallet) GetAgentReputation(ctx context.Context, agentID string) (*AgentReputation, error) {
	walletRep, err := sw.GetWalletReputation(ctx, agentID)
	if err != nil {
		return nil, err
	}

	// Convert WalletReputation to AgentReputation
	return &AgentReputation{
		AgentID:         walletRep.AgentID,
		ReputationScore: walletRep.TrustScore,
		LastUpdated:     walletRep.UpdatedAt,
		// Other fields would be populated from additional queries if needed
	}, nil
}

// GetJurorMetadata fetches trust scores for weighted voting
func (sw *SpannerWallet) GetJurorMetadata(ctx context.Context, jurorIDs []string) ([]JurorMetadata, error) {
	// Use stale read for performance
	roTx := sw.client.ReadOnlyTransaction().WithTimestampBound(spanner.MaxStaleness(15 * time.Second))
	defer roTx.Close()

	var jurors []JurorMetadata

	for _, jurorID := range jurorIDs {
		row, err := roTx.ReadRow(ctx, "Agents", spanner.Key{jurorID}, []string{"AgentID", "TrustScore", "IsFrozen"})
		if err != nil {
			continue // Skip missing jurors
		}

		var agentID string
		var trustScore float64
		var isFrozen bool
		if err := row.Columns(&agentID, &trustScore, &isFrozen); err != nil {
			continue
		}

		if !isFrozen {
			jurors = append(jurors, JurorMetadata{
				AgentID:    agentID,
				TrustScore: trustScore,
			})
		}
	}

	return jurors, nil
}

// GetHighTrustAgents returns agents eligible for tax redistribution
func (sw *SpannerWallet) GetHighTrustAgents(ctx context.Context, minTrust float64) ([]WalletReputation, error) {
	stmt := spanner.Statement{
		SQL: `SELECT AgentID, TrustScore, GovTaxBalance FROM Agents 
		      WHERE TrustScore > @minTrust AND IsFrozen = FALSE
		      ORDER BY TrustScore DESC`,
		Params: map[string]interface{}{"minTrust": minTrust},
	}

	iter := sw.client.Single().Query(ctx, stmt)
	defer iter.Stop()

	var agents []WalletReputation
	for {
		row, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}

		var agent WalletReputation
		if err := row.Columns(&agent.AgentID, &agent.TrustScore, &agent.GovTaxBalance); err != nil {
			return nil, err
		}

		agents = append(agents, agent)
	}

	return agents, nil
}

// initializeAgent creates default reputation entry
func (sw *SpannerWallet) initializeAgent(ctx context.Context, agentID string) error {
	_, err := sw.client.Apply(ctx, []*spanner.Mutation{
		spanner.Insert("Agents",
			[]string{"AgentID", "TrustScore", "BehavioralDrift", "GovTaxBalance", "IsFrozen", "UpdatedAt"},
			[]interface{}{agentID, 1.0, 0.0, 1000, false, spanner.CommitTimestamp},
		),
	})

	if err == nil {
		sw.logger.Printf("✨ Initialized agent %s with default reputation", agentID)
	}

	return err
}

// Close closes the Spanner client
func (sw *SpannerWallet) Close() error {
	sw.client.Close()
	return nil
}

// Helper types
type JurorMetadata struct {
	AgentID    string
	TrustScore float64
}

func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
