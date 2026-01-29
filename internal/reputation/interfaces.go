package reputation

import (
	"context"
	"time"
)

// AgentReputation represents the public view of an agent's standing
type AgentReputation struct {
	AgentID                string
	Organization           string
	ReputationScore        float64
	TotalInteractions      int64
	SuccessfulInteractions int64
	FailedInteractions     int64
	LastUpdated            time.Time
	FirstSeen              time.Time
	Status                 string
	Blacklisted            bool
}

// ReputationStore defines the interface for any reputation backend (SQLite, Spanner)
type ReputationStore interface {
	CheckBalance(ctx context.Context, agentID string) (bool, error)
	ApplyPenalty(ctx context.Context, agentID, txID string, amount int64) error
	RewardAgent(ctx context.Context, agentID string, amount int64) error
	QuarantineAgent(ctx context.Context, agentID string) error
	ProcessRecovery(ctx context.Context, agentID string, stakeAmount int64) error
	GetAgentReputation(ctx context.Context, agentID string) (*AgentReputation, error)
	Close() error
}
