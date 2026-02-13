// Package fabric — Redis-backed Hub Store for multi-pod spoke routing.
//
// In a multi-pod deployment (e.g., Cloud Run), each pod runs its own Hub
// instance. Without a shared store, spoke registrations on pod 1 are invisible
// to pod 2. This RedisHubStore backs the spoke registry, routing table,
// capability index, and tenant index with Redis.
package fabric

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"
)

// RedisClient is a minimal interface that any Redis library (go-redis, redigo)
// can satisfy. The Hub doesn't import a specific driver — code in cmd/api/main
// creates the concrete client and injects it.
type RedisClient interface {
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	Get(ctx context.Context, key string) ([]byte, error)
	Del(ctx context.Context, keys ...string) error
	SAdd(ctx context.Context, key string, members ...string) error
	SRem(ctx context.Context, key string, members ...string) error
	SMembers(ctx context.Context, key string) ([]string, error)
	Publish(ctx context.Context, channel string, message []byte) error
}

// RedisHubStore persists spoke registrations in Redis so that all pods
// in a multi-instance deployment share the same routing table.
type RedisHubStore struct {
	client    RedisClient
	keyPrefix string // e.g. "ocx:hub:" to namespace keys
	spokeTTL  time.Duration
}

// NewRedisHubStore creates a new Redis-backed hub store.
func NewRedisHubStore(client RedisClient, keyPrefix string, spokeTTL time.Duration) *RedisHubStore {
	if keyPrefix == "" {
		keyPrefix = "ocx:hub:"
	}
	if spokeTTL == 0 {
		spokeTTL = 10 * time.Minute // spokes re-register via heartbeat
	}
	return &RedisHubStore{
		client:    client,
		keyPrefix: keyPrefix,
		spokeTTL:  spokeTTL,
	}
}

// spokeJSON is the serializable form of SpokeInfo for Redis storage.
type spokeJSON struct {
	ID           string   `json:"id"`
	TenantID     string   `json:"tenant_id"`
	AgentID      string   `json:"agent_id"`
	VirtualAddr  string   `json:"virtual_addr"`
	Capabilities []string `json:"capabilities"`
	TrustScore   float64  `json:"trust_score"`
	Entitlements []string `json:"entitlements"`
	ConnectedAt  string   `json:"connected_at"`
}

func spokeToJSON(s *SpokeInfo) *spokeJSON {
	caps := make([]string, len(s.Capabilities))
	for i, c := range s.Capabilities {
		caps[i] = string(c)
	}
	return &spokeJSON{
		ID:           string(s.ID),
		TenantID:     s.TenantID,
		AgentID:      s.AgentID,
		VirtualAddr:  string(s.VirtualAddr),
		Capabilities: caps,
		TrustScore:   s.TrustScore,
		Entitlements: s.Entitlements,
		ConnectedAt:  s.ConnectedAt.Format(time.RFC3339),
	}
}

// SaveSpoke persists a spoke registration to Redis.
func (rs *RedisHubStore) SaveSpoke(ctx context.Context, spoke *SpokeInfo) error {
	data, err := json.Marshal(spokeToJSON(spoke))
	if err != nil {
		return fmt.Errorf("marshal spoke: %w", err)
	}

	// Store spoke data
	spokeKey := rs.keyPrefix + "spoke:" + string(spoke.ID)
	if err := rs.client.Set(ctx, spokeKey, data, rs.spokeTTL); err != nil {
		return fmt.Errorf("redis SET spoke: %w", err)
	}

	// Add to routing index (virtual address → spoke ID)
	routeKey := rs.keyPrefix + "route:" + string(spoke.VirtualAddr)
	if err := rs.client.SAdd(ctx, routeKey, string(spoke.ID)); err != nil {
		return fmt.Errorf("redis SADD route: %w", err)
	}

	// Add to capability indexes
	for _, cap := range spoke.Capabilities {
		capKey := rs.keyPrefix + "cap:" + string(cap)
		if err := rs.client.SAdd(ctx, capKey, string(spoke.ID)); err != nil {
			slog.Warn("[RedisHubStore] Failed to index capability", "cap", cap, "error", err)
		}
	}

	// Add to tenant index
	tenantKey := rs.keyPrefix + "tenant:" + spoke.TenantID
	if err := rs.client.SAdd(ctx, tenantKey, string(spoke.ID)); err != nil {
		slog.Warn("[RedisHubStore] Failed to index tenant", "tenant", spoke.TenantID, "error", err)
	}

	slog.Info("[RedisHubStore] Saved spoke", "spoke_id", spoke.ID)
	return nil
}

// LoadSpoke retrieves a spoke registration from Redis.
func (rs *RedisHubStore) LoadSpoke(ctx context.Context, spokeID SpokeID) (*SpokeInfo, error) {
	spokeKey := rs.keyPrefix + "spoke:" + string(spokeID)
	data, err := rs.client.Get(ctx, spokeKey)
	if err != nil {
		return nil, fmt.Errorf("redis GET spoke: %w", err)
	}

	var sj spokeJSON
	if err := json.Unmarshal(data, &sj); err != nil {
		return nil, fmt.Errorf("unmarshal spoke: %w", err)
	}

	connectedAt, _ := time.Parse(time.RFC3339, sj.ConnectedAt)
	caps := make([]Capability, len(sj.Capabilities))
	for i, c := range sj.Capabilities {
		caps[i] = Capability(c)
	}

	spoke := &SpokeInfo{
		ID:           SpokeID(sj.ID),
		TenantID:     sj.TenantID,
		AgentID:      sj.AgentID,
		VirtualAddr:  VirtualAddress(sj.VirtualAddr),
		Capabilities: caps,
		TrustScore:   sj.TrustScore,
		Entitlements: sj.Entitlements,
		ConnectedAt:  connectedAt,
	}
	spoke.LastSeen.Store(time.Now())
	return spoke, nil
}

// DeleteSpoke removes a spoke and its index entries from Redis.
func (rs *RedisHubStore) DeleteSpoke(ctx context.Context, spoke *SpokeInfo) error {
	spokeKey := rs.keyPrefix + "spoke:" + string(spoke.ID)
	routeKey := rs.keyPrefix + "route:" + string(spoke.VirtualAddr)
	tenantKey := rs.keyPrefix + "tenant:" + spoke.TenantID

	// Remove from indexes
	_ = rs.client.SRem(ctx, routeKey, string(spoke.ID))
	_ = rs.client.SRem(ctx, tenantKey, string(spoke.ID))
	for _, cap := range spoke.Capabilities {
		capKey := rs.keyPrefix + "cap:" + string(cap)
		_ = rs.client.SRem(ctx, capKey, string(spoke.ID))
	}

	// Remove spoke data
	return rs.client.Del(ctx, spokeKey)
}

// GetSpokesByCapability returns all spoke IDs with a given capability.
func (rs *RedisHubStore) GetSpokesByCapability(ctx context.Context, cap Capability) ([]SpokeID, error) {
	capKey := rs.keyPrefix + "cap:" + string(cap)
	members, err := rs.client.SMembers(ctx, capKey)
	if err != nil {
		return nil, err
	}
	ids := make([]SpokeID, len(members))
	for i, m := range members {
		ids[i] = SpokeID(m)
	}
	return ids, nil
}

// GetSpokesByTenant returns all spoke IDs for a given tenant.
func (rs *RedisHubStore) GetSpokesByTenant(ctx context.Context, tenantID string) ([]SpokeID, error) {
	tenantKey := rs.keyPrefix + "tenant:" + tenantID
	members, err := rs.client.SMembers(ctx, tenantKey)
	if err != nil {
		return nil, err
	}
	ids := make([]SpokeID, len(members))
	for i, m := range members {
		ids[i] = SpokeID(m)
	}
	return ids, nil
}
