// Package escrow — JIT Entitlements (Patent §4.3)
//
// Implements: "Just-In-Time ephemeral permissions with countdown timers."
//
// Agents receive temporary, scoped permissions that auto-expire after a
// configurable TTL. This prevents permission accumulation and ensures
// the principle of least-privilege is enforced at the socket layer.
package escrow

import (
	"fmt"
	"log"
	"sync"
	"time"
)

// JITEntitlementManager manages ephemeral, time-limited permissions for agents.
type JITEntitlementManager struct {
	mu     sync.RWMutex
	grants map[string]map[string]*Entitlement // agentID -> permission -> grant
	logger *log.Logger

	// Background cleanup
	cleanupTicker *time.Ticker
	stopCleanup   chan struct{}

	// Optional audit callback — called on every grant/revoke/expire event
	onAuditEvent func(event EntitlementEvent)
}

// Entitlement represents a single time-limited permission.
type Entitlement struct {
	ID         string
	AgentID    string
	Permission string
	GrantedAt  time.Time
	ExpiresAt  time.Time
	TTL        time.Duration
	GrantedBy  string // Who/what authorized this
	Reason     string
	Status     EntitlementStatus
	Metadata   map[string]interface{}
}

// EntitlementStatus tracks the state of an entitlement.
type EntitlementStatus string

const (
	EntitlementActive  EntitlementStatus = "ACTIVE"
	EntitlementExpired EntitlementStatus = "EXPIRED"
	EntitlementRevoked EntitlementStatus = "REVOKED"
)

// EntitlementEvent is emitted for audit logging on every lifecycle change.
type EntitlementEvent struct {
	Type       string // "GRANTED", "EXPIRED", "REVOKED", "CHECKED"
	AgentID    string
	Permission string
	Timestamp  time.Time
	TTL        time.Duration
	Reason     string
}

// NewJITEntitlementManager creates a manager with background cleanup.
func NewJITEntitlementManager() *JITEntitlementManager {
	mgr := &JITEntitlementManager{
		grants:        make(map[string]map[string]*Entitlement),
		logger:        log.New(log.Writer(), "[JITEntitlements] ", log.LstdFlags),
		cleanupTicker: time.NewTicker(10 * time.Second),
		stopCleanup:   make(chan struct{}),
	}

	go mgr.cleanupLoop()
	return mgr
}

// SetAuditCallback sets an optional callback for entitlement lifecycle events.
func (jm *JITEntitlementManager) SetAuditCallback(fn func(EntitlementEvent)) {
	jm.onAuditEvent = fn
}

// GrantEphemeral creates a time-limited permission for an agent.
// The permission auto-expires after the given TTL.
func (jm *JITEntitlementManager) GrantEphemeral(
	agentID, permission string,
	ttl time.Duration,
	grantedBy, reason string,
	metadata map[string]interface{},
) (*Entitlement, error) {
	jm.mu.Lock()
	defer jm.mu.Unlock()

	if ttl <= 0 {
		return nil, fmt.Errorf("TTL must be positive, got %v", ttl)
	}

	// Cap TTL to prevent indefinite permissions
	const maxTTL = 1 * time.Hour
	if ttl > maxTTL {
		jm.logger.Printf("⚠️  TTL capped from %v to %v for agent %s", ttl, maxTTL, agentID)
		ttl = maxTTL
	}

	now := time.Now()

	ent := &Entitlement{
		ID:         fmt.Sprintf("jit-%s-%s-%d", agentID, permission, now.UnixNano()),
		AgentID:    agentID,
		Permission: permission,
		GrantedAt:  now,
		ExpiresAt:  now.Add(ttl),
		TTL:        ttl,
		GrantedBy:  grantedBy,
		Reason:     reason,
		Status:     EntitlementActive,
		Metadata:   metadata,
	}

	if _, ok := jm.grants[agentID]; !ok {
		jm.grants[agentID] = make(map[string]*Entitlement)
	}
	jm.grants[agentID][permission] = ent

	jm.logger.Printf("🔑 Granted [%s] to agent %s for %v (by %s: %s)",
		permission, agentID, ttl, grantedBy, reason)

	jm.emitEvent(EntitlementEvent{
		Type:       "GRANTED",
		AgentID:    agentID,
		Permission: permission,
		Timestamp:  now,
		TTL:        ttl,
		Reason:     reason,
	})

	return ent, nil
}

// CheckEntitlement returns true if the agent currently holds the given permission.
func (jm *JITEntitlementManager) CheckEntitlement(agentID, permission string) bool {
	jm.mu.RLock()
	defer jm.mu.RUnlock()

	agentGrants, ok := jm.grants[agentID]
	if !ok {
		return false
	}

	ent, ok := agentGrants[permission]
	if !ok {
		return false
	}

	if ent.Status != EntitlementActive {
		return false
	}

	// Check expiry
	if time.Now().After(ent.ExpiresAt) {
		// Mark as expired (will be cleaned up by background goroutine)
		return false
	}

	return true
}

// RemainingTTL returns the time left on an entitlement, or 0 if expired/absent.
func (jm *JITEntitlementManager) RemainingTTL(agentID, permission string) time.Duration {
	jm.mu.RLock()
	defer jm.mu.RUnlock()

	if agentGrants, ok := jm.grants[agentID]; ok {
		if ent, ok := agentGrants[permission]; ok && ent.Status == EntitlementActive {
			remaining := time.Until(ent.ExpiresAt)
			if remaining > 0 {
				return remaining
			}
		}
	}
	return 0
}

// RevokeEntitlement immediately revokes a permission before its TTL expires.
func (jm *JITEntitlementManager) RevokeEntitlement(agentID, permission, reason string) error {
	jm.mu.Lock()
	defer jm.mu.Unlock()

	agentGrants, ok := jm.grants[agentID]
	if !ok {
		return fmt.Errorf("no entitlements for agent %s", agentID)
	}

	ent, ok := agentGrants[permission]
	if !ok {
		return fmt.Errorf("agent %s does not hold permission %s", agentID, permission)
	}

	if ent.Status != EntitlementActive {
		return fmt.Errorf("entitlement already %s", ent.Status)
	}

	ent.Status = EntitlementRevoked

	jm.logger.Printf("🚫 Revoked [%s] from agent %s (%s)",
		permission, agentID, reason)

	jm.emitEvent(EntitlementEvent{
		Type:       "REVOKED",
		AgentID:    agentID,
		Permission: permission,
		Timestamp:  time.Now(),
		Reason:     reason,
	})

	return nil
}

// GetActiveEntitlements returns all currently active entitlements for an agent.
func (jm *JITEntitlementManager) GetActiveEntitlements(agentID string) []*Entitlement {
	jm.mu.RLock()
	defer jm.mu.RUnlock()

	var active []*Entitlement
	now := time.Now()

	if agentGrants, ok := jm.grants[agentID]; ok {
		for _, ent := range agentGrants {
			if ent.Status == EntitlementActive && now.Before(ent.ExpiresAt) {
				active = append(active, ent)
			}
		}
	}
	return active
}

// GetAllHeldCount returns the total number of active entitlements across all agents.
func (jm *JITEntitlementManager) GetAllHeldCount() int {
	jm.mu.RLock()
	defer jm.mu.RUnlock()

	now := time.Now()
	count := 0
	for _, agentGrants := range jm.grants {
		for _, ent := range agentGrants {
			if ent.Status == EntitlementActive && now.Before(ent.ExpiresAt) {
				count++
			}
		}
	}
	return count
}

// cleanupLoop runs periodically to expire stale entitlements.
func (jm *JITEntitlementManager) cleanupLoop() {
	for {
		select {
		case <-jm.cleanupTicker.C:
			jm.cleanupExpired()
		case <-jm.stopCleanup:
			return
		}
	}
}

// cleanupExpired marks all past-TTL entitlements as expired.
func (jm *JITEntitlementManager) cleanupExpired() {
	jm.mu.Lock()
	defer jm.mu.Unlock()

	now := time.Now()
	expiredCount := 0

	for agentID, agentGrants := range jm.grants {
		for perm, ent := range agentGrants {
			if ent.Status == EntitlementActive && now.After(ent.ExpiresAt) {
				ent.Status = EntitlementExpired
				expiredCount++

				jm.logger.Printf("⏰ Expired [%s] for agent %s (TTL=%v)",
					perm, agentID, ent.TTL)

				jm.emitEvent(EntitlementEvent{
					Type:       "EXPIRED",
					AgentID:    agentID,
					Permission: perm,
					Timestamp:  now,
					TTL:        ent.TTL,
				})
			}
		}
	}

	if expiredCount > 0 {
		jm.logger.Printf("🧹 Cleaned up %d expired entitlements", expiredCount)
	}
}

// Close stops the background cleanup goroutine.
func (jm *JITEntitlementManager) Close() {
	jm.cleanupTicker.Stop()
	close(jm.stopCleanup)
}

// emitEvent fires the audit callback if registered.
func (jm *JITEntitlementManager) emitEvent(event EntitlementEvent) {
	if jm.onAuditEvent != nil {
		jm.onAuditEvent(event)
	}
}
