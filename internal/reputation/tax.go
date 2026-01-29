package reputation

import (
	"context"
	"fmt"
	"log"
	"math"
	"time"
)

// TaxRedistributor manages governance tax collection and redistribution
type TaxRedistributor struct {
	wallet ReputationStore
	logger *log.Logger
	config TaxConfig
}

// TaxConfig holds redistribution parameters
type TaxConfig struct {
	MinTrustScore       float64 // Minimum trust to receive rewards (e.g., 0.7)
	ParticipationWeight float64 // Weight for participation vs. trust (0.5 = equal)
	TaxPoolPercentage   float64 // Percentage of penalties to redistribute (e.g., 0.8 = 80%)
	MinPoolSize         int64   // Minimum pool size to trigger distribution
}

// RewardCalculation represents a single agent's reward
type RewardCalculation struct {
	AgentID       string
	TrustScore    float64
	Participation int // Number of successful transactions
	RewardAmount  int64
	RewardFormula string // For transparency
}

// NewTaxRedistributor creates a new tax redistribution engine
func NewTaxRedistributor(wallet ReputationStore, config TaxConfig) *TaxRedistributor {
	if config.MinTrustScore == 0 {
		config.MinTrustScore = 0.7
	}
	if config.ParticipationWeight == 0 {
		config.ParticipationWeight = 0.5
	}
	if config.TaxPoolPercentage == 0 {
		config.TaxPoolPercentage = 0.8
	}
	if config.MinPoolSize == 0 {
		config.MinPoolSize = 1000
	}

	return &TaxRedistributor{
		wallet: wallet,
		logger: log.New(log.Writer(), "[TaxRedistributor] ", log.LstdFlags),
		config: config,
	}
}

// DistributeGovernanceTax executes the redistribution algorithm
func (tr *TaxRedistributor) DistributeGovernanceTax(ctx context.Context) error {
	tr.logger.Println("🏦 Starting governance tax redistribution...")

	// 1. Calculate total tax pool
	taxPool, err := tr.calculateTaxPool(ctx)
	if err != nil {
		return fmt.Errorf("failed to calculate tax pool: %w", err)
	}

	if taxPool < tr.config.MinPoolSize {
		tr.logger.Printf("Tax pool too small (%d < %d), skipping distribution", taxPool, tr.config.MinPoolSize)
		return nil
	}

	redistributionAmount := int64(float64(taxPool) * tr.config.TaxPoolPercentage)
	tr.logger.Printf("💰 Tax pool: %d, Redistributing: %d (%.0f%%)", taxPool, redistributionAmount, tr.config.TaxPoolPercentage*100)

	// 2. Get eligible agents (high trust, not frozen)
	eligibleAgents, err := tr.getEligibleAgents(ctx)
	if err != nil {
		return fmt.Errorf("failed to get eligible agents: %w", err)
	}

	if len(eligibleAgents) == 0 {
		tr.logger.Println("No eligible agents for redistribution")
		return nil
	}

	// 3. Calculate rewards
	rewards := tr.calculateRewards(eligibleAgents, redistributionAmount)

	// 4. Distribute rewards
	successCount := 0
	for _, reward := range rewards {
		if err := tr.wallet.RewardAgent(ctx, reward.AgentID, reward.RewardAmount); err != nil {
			tr.logger.Printf("⚠️ Failed to reward agent %s: %v", reward.AgentID, err)
			continue
		}
		tr.logger.Printf("✅ Rewarded %s: %d credits (Trust: %.2f, Participation: %d)",
			reward.AgentID, reward.RewardAmount, reward.TrustScore, reward.Participation)
		successCount++
	}

	tr.logger.Printf("🎉 Distribution complete: %d/%d agents rewarded", successCount, len(rewards))
	return nil
}

// calculateTaxPool sums up all penalties collected
func (tr *TaxRedistributor) calculateTaxPool(context.Context) (int64, error) {
	// In production, this would query the ReputationAudit table:
	// SELECT SUM(tax_levied) FROM reputation_audit WHERE verdict = 'FAILURE' AND distributed = FALSE

	// For now, we'll use a mock calculation
	// In a real implementation, you'd track undistributed penalties
	return 5000, nil // Mock pool
}

// getEligibleAgents fetches agents eligible for rewards
func (tr *TaxRedistributor) getEligibleAgents(_ context.Context) ([]AgentReputation, error) {
	// TODO: Implement GetHighTrustAgents for different wallet backends
	// For now, return empty (will be implemented when wallet interface is extended)
	return []AgentReputation{}, nil
}

// calculateRewards implements the reward formula
func (tr *TaxRedistributor) calculateRewards(agents []AgentReputation, totalPool int64) []RewardCalculation {
	// Formula: Reward_i = Pool × (Trust_i × W_trust + Participation_i × W_participation) / Σ(all agents)

	wTrust := 1.0 - tr.config.ParticipationWeight
	wParticipation := tr.config.ParticipationWeight

	// Calculate weighted scores
	type weightedAgent struct {
		AgentID       string
		TrustScore    float64
		Participation int
		WeightedScore float64
	}

	var weightedAgents []weightedAgent
	var totalWeightedScore float64

	for _, agent := range agents {
		// Mock participation - in production, query from ReputationAudit
		participation := tr.getParticipationCount(agent.AgentID)

		// Normalize participation (assume max 100 transactions)
		normalizedParticipation := math.Min(float64(participation)/100.0, 1.0)

		weightedScore := (agent.ReputationScore * wTrust) + (normalizedParticipation * wParticipation)

		weightedAgents = append(weightedAgents, weightedAgent{
			AgentID:       agent.AgentID,
			TrustScore:    agent.ReputationScore,
			Participation: participation,
			WeightedScore: weightedScore,
		})

		totalWeightedScore += weightedScore
	}

	// Calculate individual rewards
	var rewards []RewardCalculation

	for _, wa := range weightedAgents {
		if totalWeightedScore == 0 {
			continue
		}

		rewardAmount := int64(float64(totalPool) * (wa.WeightedScore / totalWeightedScore))

		formula := fmt.Sprintf("%.0f × (%.2f × %.2f + %d/100 × %.2f) / %.2f",
			float64(totalPool), wa.TrustScore, wTrust, wa.Participation, wParticipation, totalWeightedScore)

		rewards = append(rewards, RewardCalculation{
			AgentID:       wa.AgentID,
			TrustScore:    wa.TrustScore,
			Participation: wa.Participation,
			RewardAmount:  rewardAmount,
			RewardFormula: formula,
		})
	}

	return rewards
}

// getParticipationCount returns the number of successful transactions for an agent
func (tr *TaxRedistributor) getParticipationCount(_ string) int {
	// In production, query ReputationAudit:
	// SELECT COUNT(*) FROM reputation_audit WHERE agent_id = ? AND verdict = 'SUCCESS' AND created_at > NOW() - INTERVAL '24 hours'

	// Mock data
	return 50
}

// RunDistributionCron starts a periodic redistribution job
func (tr *TaxRedistributor) RunDistributionCron(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	tr.logger.Printf("🕐 Starting tax redistribution cron (interval: %v)", interval)

	for {
		select {
		case <-ticker.C:
			if err := tr.DistributeGovernanceTax(ctx); err != nil {
				tr.logger.Printf("❌ Distribution failed: %v", err)
			}
		case <-ctx.Done():
			tr.logger.Println("Stopping tax redistribution cron")
			return
		}
	}
}
