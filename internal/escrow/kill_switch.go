package escrow

import (
	"fmt"
	"log"
	"sync"
	"time"
)

// KillSwitch provides an emergency halt mechanism for rogue agents.
// When activated, it immediately blocks all actions from a specific agent
// or optionally all agents across a tenant.
//
// Patent requirement: "Emergency stop mechanism to halt agent execution
// when anomalous or dangerous behavior is detected."
type KillSwitch struct {
	mu            sync.RWMutex
	killedAgents  map[string]*KillRecord // agentID → record
	killedTenants map[string]*KillRecord // tenantID → record
	logger        *log.Logger
}

// KillRecord stores the metadata of a kill switch activation.
type KillRecord struct {
	Target      string     `json:"target"` // Agent ID or Tenant ID
	Scope       string     `json:"scope"`  // "agent" or "tenant"
	Reason      string     `json:"reason"`
	TriggeredBy string     `json:"triggered_by"` // Who activated it
	TriggeredAt time.Time  `json:"triggered_at"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"` // nil = permanent
}

// NewKillSwitch creates a new kill switch instance.
func NewKillSwitch() *KillSwitch {
	return &KillSwitch{
		killedAgents:  make(map[string]*KillRecord),
		killedTenants: make(map[string]*KillRecord),
		logger:        log.New(log.Writer(), "[KILL-SWITCH] ", log.LstdFlags),
	}
}

// KillAgent immediately blocks all actions from a specific agent.
func (ks *KillSwitch) KillAgent(agentID, reason, triggeredBy string, ttl *time.Duration) *KillRecord {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	record := &KillRecord{
		Target:      agentID,
		Scope:       "agent",
		Reason:      reason,
		TriggeredBy: triggeredBy,
		TriggeredAt: time.Now(),
	}

	if ttl != nil {
		exp := time.Now().Add(*ttl)
		record.ExpiresAt = &exp
	}

	ks.killedAgents[agentID] = record
	ks.logger.Printf("🛑 KILL SWITCH ACTIVATED: agent=%s reason=%q by=%s", agentID, reason, triggeredBy)

	return record
}

// KillTenant blocks all agents for an entire tenant.
func (ks *KillSwitch) KillTenant(tenantID, reason, triggeredBy string, ttl *time.Duration) *KillRecord {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	record := &KillRecord{
		Target:      tenantID,
		Scope:       "tenant",
		Reason:      reason,
		TriggeredBy: triggeredBy,
		TriggeredAt: time.Now(),
	}

	if ttl != nil {
		exp := time.Now().Add(*ttl)
		record.ExpiresAt = &exp
	}

	ks.killedTenants[tenantID] = record
	ks.logger.Printf("🛑 KILL SWITCH ACTIVATED: tenant=%s reason=%q by=%s", tenantID, reason, triggeredBy)

	return record
}

// IsKilled checks if an agent or its tenant is currently killed.
// Returns (killed, reason). This is called in the hot path of handleGovern.
func (ks *KillSwitch) IsKilled(agentID, tenantID string) (bool, string) {
	ks.mu.RLock()
	defer ks.mu.RUnlock()

	// Check agent-level kill
	if record, ok := ks.killedAgents[agentID]; ok {
		if record.ExpiresAt == nil || record.ExpiresAt.After(time.Now()) {
			return true, fmt.Sprintf("Agent killed: %s", record.Reason)
		}
		// Expired — clean up lazily
		delete(ks.killedAgents, agentID)
	}

	// Check tenant-level kill
	if record, ok := ks.killedTenants[tenantID]; ok {
		if record.ExpiresAt == nil || record.ExpiresAt.After(time.Now()) {
			return true, fmt.Sprintf("Tenant killed: %s", record.Reason)
		}
		delete(ks.killedTenants, tenantID)
	}

	return false, ""
}

// Revive removes a kill switch for an agent or tenant.
func (ks *KillSwitch) Revive(target, scope string) bool {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	switch scope {
	case "agent":
		if _, ok := ks.killedAgents[target]; ok {
			delete(ks.killedAgents, target)
			ks.logger.Printf("✅ REVIVED: agent=%s", target)
			return true
		}
	case "tenant":
		if _, ok := ks.killedTenants[target]; ok {
			delete(ks.killedTenants, target)
			ks.logger.Printf("✅ REVIVED: tenant=%s", target)
			return true
		}
	}

	return false
}

// ListActive returns all currently active kill records.
func (ks *KillSwitch) ListActive() []*KillRecord {
	ks.mu.RLock()
	defer ks.mu.RUnlock()

	var records []*KillRecord
	now := time.Now()

	for _, r := range ks.killedAgents {
		if r.ExpiresAt == nil || r.ExpiresAt.After(now) {
			records = append(records, r)
		}
	}
	for _, r := range ks.killedTenants {
		if r.ExpiresAt == nil || r.ExpiresAt.After(now) {
			records = append(records, r)
		}
	}

	return records
}
