package gvisor

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

// StateCloner manages state snapshots for sandbox execution
type StateCloner struct {
	redisClient *redis.Client
	snapshotTTL time.Duration
}

// StateSnapshot represents a cloned state for sandbox
type StateSnapshot struct {
	SnapshotID    string                 `json:"snapshot_id"`
	TransactionID string                 `json:"transaction_id"`
	Timestamp     time.Time              `json:"timestamp"`
	RedisKeys     map[string]string      `json:"redis_keys"`
	DatabaseState map[string]interface{} `json:"database_state"`
	RevertToken   string                 `json:"revert_token"`
}

// NewStateCloner creates a new state cloner
func NewStateCloner(redisAddr string) *StateCloner {
	client := redis.NewClient(&redis.Options{
		Addr: redisAddr,
		DB:   0,
	})

	return &StateCloner{
		redisClient: client,
		snapshotTTL: 1 * time.Hour, // Snapshots expire after 1 hour
	}
}

// CloneState creates a snapshot of current state for sandbox execution
func (sc *StateCloner) CloneState(ctx context.Context, transactionID string, agentID string) (*StateSnapshot, error) {
	slog.Info("Creating state snapshot for transaction", "transaction_i_d", transactionID)
	snapshotID := fmt.Sprintf("snapshot:%s:%d", transactionID, time.Now().Unix())

	snapshot := &StateSnapshot{
		SnapshotID:    snapshotID,
		TransactionID: transactionID,
		Timestamp:     time.Now(),
		RedisKeys:     make(map[string]string),
		DatabaseState: make(map[string]interface{}),
	}

	// 1. Clone relevant Redis keys
	if err := sc.cloneRedisKeys(ctx, agentID, snapshot); err != nil {
		return nil, fmt.Errorf("failed to clone Redis keys: %w", err)
	}

	// 2. Clone database state (if needed)
	if err := sc.cloneDatabaseState(ctx, agentID, snapshot); err != nil {
		return nil, fmt.Errorf("failed to clone database state: %w", err)
	}

	// 3. Store snapshot metadata
	if err := sc.storeSnapshot(ctx, snapshot); err != nil {
		return nil, fmt.Errorf("failed to store snapshot: %w", err)
	}

	slog.Info("State snapshot created: ( Redis keys)", "snapshot_i_d", snapshotID, "redis_keys", len(snapshot.RedisKeys))
	return snapshot, nil
}

// cloneRedisKeys copies relevant Redis keys to snapshot namespace
func (sc *StateCloner) cloneRedisKeys(ctx context.Context, agentID string, snapshot *StateSnapshot) error {
	// Find all keys related to this agent
	pattern := fmt.Sprintf("agent:%s:*", agentID)
	keys, err := sc.redisClient.Keys(ctx, pattern).Result()
	if err != nil {
		return err
	}

	// Copy each key to snapshot namespace
	for _, key := range keys {
		value, err := sc.redisClient.Get(ctx, key).Result()
		if err != nil {
			if err == redis.Nil {
				continue // Key doesn't exist, skip
			}
			return err
		}

		// Store in snapshot namespace
		snapshotKey := fmt.Sprintf("%s:%s", snapshot.SnapshotID, key)
		if err := sc.redisClient.Set(ctx, snapshotKey, value, sc.snapshotTTL).Err(); err != nil {
			return err
		}

		snapshot.RedisKeys[key] = value
	}

	return nil
}

// cloneDatabaseState creates a snapshot of database state using PostgreSQL savepoints
func (sc *StateCloner) cloneDatabaseState(ctx context.Context, agentID string, snapshot *StateSnapshot) error {
	// Store savepoint information for later rollback/commit
	savepointName := fmt.Sprintf("sp_%s", snapshot.SnapshotID)

	snapshot.DatabaseState = map[string]interface{}{
		"agent_id":       agentID,
		"timestamp":      time.Now().Unix(),
		"savepoint_name": savepointName,
		"tables":         []string{"agents", "transactions", "balances"},
		"method":         "postgresql_savepoint",
	}

	// Note: Actual savepoint creation happens in the database transaction
	// The calling code should execute: SAVEPOINT <savepointName>
	// This is stored here for reference during commit/rollback

	return nil
}

// storeSnapshot saves snapshot metadata to Redis
func (sc *StateCloner) storeSnapshot(ctx context.Context, snapshot *StateSnapshot) error {
	data, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}

	key := fmt.Sprintf("snapshot:meta:%s", snapshot.SnapshotID)
	return sc.redisClient.Set(ctx, key, data, sc.snapshotTTL).Err()
}

// RestoreSnapshot restores state from a snapshot (for sandbox)
func (sc *StateCloner) RestoreSnapshot(ctx context.Context, snapshotID string) (*StateSnapshot, error) {
	// Retrieve snapshot metadata
	key := fmt.Sprintf("snapshot:meta:%s", snapshotID)
	data, err := sc.redisClient.Get(ctx, key).Result()
	if err != nil {
		return nil, fmt.Errorf("snapshot not found: %w", err)
	}

	var snapshot StateSnapshot
	if err := json.Unmarshal([]byte(data), &snapshot); err != nil {
		return nil, err
	}

	slog.Info("Restored snapshot", "snapshot_i_d", snapshotID)
	return &snapshot, nil
}

// RevertState discards a snapshot (called when execution is shredded)
func (sc *StateCloner) RevertState(ctx context.Context, revertToken string) error {
	slog.Info("Reverting state with token", "revert_token", revertToken)
	// Parse revert token to get snapshot ID
	// Token format: <transaction-id>:<timestamp>:<hash>
	// For now, find snapshot by transaction ID

	// Delete all snapshot keys
	pattern := fmt.Sprintf("snapshot:*%s*", revertToken[:8])
	keys, err := sc.redisClient.Keys(ctx, pattern).Result()
	if err != nil {
		return err
	}

	if len(keys) > 0 {
		if err := sc.redisClient.Del(ctx, keys...).Err(); err != nil {
			return err
		}
		slog.Info("Reverted snapshot keys", "count", len(keys))
	}

	return nil
}

// CommitState promotes a snapshot to production (called when execution is replayed)
func (sc *StateCloner) CommitState(ctx context.Context, snapshotID string) error {
	slog.Info("Committing state from snapshot", "snapshot_i_d", snapshotID)
	// Retrieve snapshot
	snapshot, err := sc.RestoreSnapshot(ctx, snapshotID)
	if err != nil {
		return err
	}

	// Copy snapshot keys back to production namespace
	for key, value := range snapshot.RedisKeys {
		if err := sc.redisClient.Set(ctx, key, value, 0).Err(); err != nil {
			return err
		}
	}

	// In production, this would also:
	// 1. Commit database transaction
	// 2. Update ledger
	// 3. Notify downstream services

	slog.Info("Committed keys to production", "redis_keys", len(snapshot.RedisKeys))
	return nil
}

// CleanupExpiredSnapshots removes old snapshots
func (sc *StateCloner) CleanupExpiredSnapshots(ctx context.Context) error {
	// Find all snapshot metadata keys
	keys, err := sc.redisClient.Keys(ctx, "snapshot:meta:*").Result()
	if err != nil {
		return err
	}

	cleaned := 0
	for _, key := range keys {
		// Check TTL
		ttl, err := sc.redisClient.TTL(ctx, key).Result()
		if err != nil {
			continue
		}

		// If expired or no TTL, delete
		if ttl <= 0 {
			sc.redisClient.Del(ctx, key)
			cleaned++
		}
	}

	if cleaned > 0 {
		slog.Info("Cleaned up expired snapshots", "cleaned", cleaned)
	}

	return nil
}

// Ping checks Redis connectivity. Returns true if the server responds.
func (sc *StateCloner) Ping() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return sc.redisClient.Ping(ctx).Err() == nil
}

// RedisAddr returns the configured Redis address.
func (sc *StateCloner) RedisAddr() string {
	return sc.redisClient.Options().Addr
}
