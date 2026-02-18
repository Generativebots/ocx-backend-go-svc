// Package governance provides tenant-configurable governance parameters.
//
// TenantGovernanceConfig holds all thresholds, weights, multipliers, and economic
// parameters that were previously hardcoded. Each tenant gets a single config row
// in Supabase; if none exists, it is auto-created with recommended defaults.
//
// GovernanceConfigCache loads config at session start and caches per-tenant.
// Existing sessions continue with cached values; new sessions pick up updates.
package governance

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// TenantGovernanceConfig contains all governance parameters for a single tenant.
// Fields map 1:1 to the tenant_governance_config Supabase table columns.
type TenantGovernanceConfig struct {
	ConfigID string `json:"config_id,omitempty"`
	TenantID string `json:"tenant_id"`

	// ── Trust Thresholds & Scores ──
	JuryTrustThreshold    float64 `json:"jury_trust_threshold"`
	JuryAuditWeight       float64 `json:"jury_audit_weight"`
	JuryReputationWeight  float64 `json:"jury_reputation_weight"`
	JuryAttestationWeight float64 `json:"jury_attestation_weight"`
	JuryHistoryWeight     float64 `json:"jury_history_weight"`
	NewAgentDefaultScore  float64 `json:"new_agent_default_score"`
	MinBalanceThreshold   float64 `json:"min_balance_threshold"`
	QuarantineScore       float64 `json:"quarantine_score"`
	PointToScoreFactor    float64 `json:"point_to_score_factor"`
	KillSwitchThreshold   float64 `json:"kill_switch_threshold"`
	QuorumThreshold       float64 `json:"quorum_threshold"`

	// ── Tax & Economics ──
	TrustTaxBaseRate      float64 `json:"trust_tax_base_rate"`
	FederationTaxBaseRate float64 `json:"federation_tax_base_rate"`
	PerEventTaxRate       float64 `json:"per_event_tax_rate"`
	MarketplaceCommission float64 `json:"marketplace_commission"`
	HITLCostMultiplier    float64 `json:"hitl_cost_multiplier"`

	// ── Tool Risk & Metering ──
	RiskMultipliers          map[string]float64 `json:"risk_multipliers"`
	MeterHighTrustThreshold  float64            `json:"meter_high_trust_threshold"`
	MeterHighTrustDiscount   float64            `json:"meter_high_trust_discount"`
	MeterMedTrustThreshold   float64            `json:"meter_med_trust_threshold"`
	MeterMedTrustDiscount    float64            `json:"meter_med_trust_discount"`
	MeterLowTrustThreshold   float64            `json:"meter_low_trust_threshold"`
	MeterLowTrustSurcharge   float64            `json:"meter_low_trust_surcharge"`
	MeterBaseCostPerFrame    float64            `json:"meter_base_cost_per_frame"`
	UnknownToolMinReputation float64            `json:"unknown_tool_min_reputation"`
	UnknownToolTaxCoeff      float64            `json:"unknown_tool_tax_coefficient"`

	// ── Tri-Factor Gate ──
	IdentityThreshold          float64 `json:"identity_threshold"`
	EntropyThreshold           float64 `json:"entropy_threshold"`
	JitterThreshold            float64 `json:"jitter_threshold"`
	CognitiveThreshold         float64 `json:"cognitive_threshold"`
	EntropyHighCap             float64 `json:"entropy_high_cap"`
	EntropyEncryptedThreshold  float64 `json:"entropy_encrypted_threshold"`
	EntropySuspiciousThreshold float64 `json:"entropy_suspicious_threshold"`

	// ── Security: Continuous Evaluation ──
	DriftThreshold   float64 `json:"drift_threshold"`
	AnomalyThreshold int     `json:"anomaly_threshold"`

	// ── Federation Trust Decay ──
	DecayHalfLifeHours     float64 `json:"decay_half_life_hours"`
	TrustEmaAlpha          float64 `json:"trust_ema_alpha"`
	FailurePenaltyFactor   float64 `json:"failure_penalty_factor"`
	SupermajorityThreshold float64 `json:"supermajority_threshold"`
	HandshakeMinTrust      float64 `json:"handshake_min_trust"`

	// ── Metadata ──
	UpdatedBy string    `json:"updated_by,omitempty"`
	CreatedAt time.Time `json:"created_at,omitempty"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
}

// DefaultConfig returns a TenantGovernanceConfig with all recommended defaults.
// These match the DEFAULT values in the tenant_governance_config Supabase table.
func DefaultConfig(tenantID string) *TenantGovernanceConfig {
	return &TenantGovernanceConfig{
		TenantID: tenantID,

		// Trust
		JuryTrustThreshold:    0.65,
		JuryAuditWeight:       0.40,
		JuryReputationWeight:  0.30,
		JuryAttestationWeight: 0.20,
		JuryHistoryWeight:     0.10,
		NewAgentDefaultScore:  0.30,
		MinBalanceThreshold:   0.20,
		QuarantineScore:       0.00,
		PointToScoreFactor:    0.01,
		KillSwitchThreshold:   0.30,
		QuorumThreshold:       0.66,

		// Economics
		TrustTaxBaseRate:      0.10,
		FederationTaxBaseRate: 0.10,
		PerEventTaxRate:       0.01,
		MarketplaceCommission: 0.30,
		HITLCostMultiplier:    10.0,

		// Risk & Metering
		RiskMultipliers: map[string]float64{
			"data_query":    1.0,
			"read_only":     0.5,
			"file_read":     1.0,
			"file_write":    3.0,
			"network_call":  2.0,
			"api_call":      2.5,
			"data_mutation": 4.0,
			"admin_action":  5.0,
			"exec_command":  5.0,
			"payment":       4.0,
			"pii_access":    3.5,
			"unknown":       2.0,
		},
		MeterHighTrustThreshold:  0.80,
		MeterHighTrustDiscount:   0.70,
		MeterMedTrustThreshold:   0.60,
		MeterMedTrustDiscount:    0.85,
		MeterLowTrustThreshold:   0.30,
		MeterLowTrustSurcharge:   1.50,
		MeterBaseCostPerFrame:    0.001,
		UnknownToolMinReputation: 0.95,
		UnknownToolTaxCoeff:      5.0,

		// Tri-Factor Gate
		IdentityThreshold:          0.65,
		EntropyThreshold:           7.5,
		JitterThreshold:            0.01,
		CognitiveThreshold:         0.65,
		EntropyHighCap:             4.8,
		EntropyEncryptedThreshold:  7.5,
		EntropySuspiciousThreshold: 6.0,

		// Security
		DriftThreshold:   0.20,
		AnomalyThreshold: 5,

		// Federation
		DecayHalfLifeHours:     168,
		TrustEmaAlpha:          0.3,
		FailurePenaltyFactor:   0.8,
		SupermajorityThreshold: 0.75,
		HandshakeMinTrust:      0.50,
	}
}

// Validate checks that the config parameters are within acceptable bounds.
func (c *TenantGovernanceConfig) Validate() error {
	// Jury weights must sum to approximately 1.0
	weightSum := c.JuryAuditWeight + c.JuryReputationWeight +
		c.JuryAttestationWeight + c.JuryHistoryWeight
	if weightSum < 0.99 || weightSum > 1.01 {
		return fmt.Errorf("jury weights must sum to 1.0, got %.4f", weightSum)
	}

	// Thresholds must be in [0, 1]
	for name, val := range map[string]float64{
		"jury_trust_threshold":       c.JuryTrustThreshold,
		"new_agent_default_score":    c.NewAgentDefaultScore,
		"min_balance_threshold":      c.MinBalanceThreshold,
		"quarantine_score":           c.QuarantineScore,
		"kill_switch_threshold":      c.KillSwitchThreshold,
		"quorum_threshold":           c.QuorumThreshold,
		"meter_high_trust_threshold": c.MeterHighTrustThreshold,
		"meter_high_trust_discount":  c.MeterHighTrustDiscount,
		"meter_med_trust_threshold":  c.MeterMedTrustThreshold,
		"meter_med_trust_discount":   c.MeterMedTrustDiscount,
		"meter_low_trust_threshold":  c.MeterLowTrustThreshold,
		"identity_threshold":         c.IdentityThreshold,
		"cognitive_threshold":        c.CognitiveThreshold,
		"drift_threshold":            c.DriftThreshold,
		"supermajority_threshold":    c.SupermajorityThreshold,
		"handshake_min_trust":        c.HandshakeMinTrust,
		"trust_ema_alpha":            c.TrustEmaAlpha,
		"failure_penalty_factor":     c.FailurePenaltyFactor,
	} {
		if val < 0 || val > 1 {
			return fmt.Errorf("%s must be between 0 and 1, got %.4f", name, val)
		}
	}

	// Positive values
	if c.HITLCostMultiplier <= 0 {
		return fmt.Errorf("hitl_cost_multiplier must be positive, got %.4f", c.HITLCostMultiplier)
	}
	if c.MeterBaseCostPerFrame <= 0 {
		return fmt.Errorf("meter_base_cost_per_frame must be positive, got %.6f", c.MeterBaseCostPerFrame)
	}
	if c.AnomalyThreshold <= 0 {
		return fmt.Errorf("anomaly_threshold must be positive, got %d", c.AnomalyThreshold)
	}

	return nil
}

// ──────────────────────────────────────────────────────────────────────────────
// GovernanceConfigCache — per-tenant session cache
// ──────────────────────────────────────────────────────────────────────────────

// ConfigLoader is an interface satisfied by the Supabase client.
type ConfigLoader interface {
	GetTenantGovernanceConfig(tenantID string) (*TenantGovernanceConfig, error)
	UpsertTenantGovernanceConfig(tenantID string, cfg *TenantGovernanceConfig) error
}

// GovernanceConfigCache loads governance configs from the DB at session start
// and caches them per-tenant. The cache is invalidated when a tenant updates
// their config via the API.
type GovernanceConfigCache struct {
	mu      sync.RWMutex
	configs map[string]*TenantGovernanceConfig // tenantID → config
	loader  ConfigLoader
}

// NewGovernanceConfigCache creates a new cache backed by the given loader.
func NewGovernanceConfigCache(loader ConfigLoader) *GovernanceConfigCache {
	return &GovernanceConfigCache{
		configs: make(map[string]*TenantGovernanceConfig),
		loader:  loader,
	}
}

// GetConfig returns the governance config for a tenant. On first call for a
// tenant, it loads from the DB and caches the result. If no DB row exists,
// it creates one with recommended defaults.
//
// If the DB is unreachable, it falls back to hardcoded defaults with a WARN log.
func (c *GovernanceConfigCache) GetConfig(tenantID string) *TenantGovernanceConfig {
	// 1. Check cache
	c.mu.RLock()
	if cfg, ok := c.configs[tenantID]; ok {
		c.mu.RUnlock()
		return cfg
	}
	c.mu.RUnlock()

	// 2. Load from DB
	cfg, err := c.loader.GetTenantGovernanceConfig(tenantID)
	if err != nil {
		slog.Warn("Failed to load tenant governance config from DB, using defaults",
			"tenant_id", tenantID, "error", err)
		cfg = DefaultConfig(tenantID)
	}

	if cfg == nil {
		// No row exists — create with defaults
		cfg = DefaultConfig(tenantID)
		if c.loader != nil {
			if err := c.loader.UpsertTenantGovernanceConfig(tenantID, cfg); err != nil {
				slog.Warn("Failed to create default governance config in DB",
					"tenant_id", tenantID, "error", err)
			} else {
				slog.Info("Created default governance config for tenant", "tenant_id", tenantID)
			}
		}
	}

	// 3. Cache
	c.mu.Lock()
	c.configs[tenantID] = cfg
	c.mu.Unlock()

	return cfg
}

// Invalidate removes a tenant's config from the cache, forcing a reload on
// the next GetConfig call. Call this after a config update via the API.
func (c *GovernanceConfigCache) Invalidate(tenantID string) {
	c.mu.Lock()
	delete(c.configs, tenantID)
	c.mu.Unlock()
	slog.Info("Governance config cache invalidated", "tenant_id", tenantID)
}

// InvalidateAll clears the entire cache.
func (c *GovernanceConfigCache) InvalidateAll() {
	c.mu.Lock()
	c.configs = make(map[string]*TenantGovernanceConfig)
	c.mu.Unlock()
}

// ──────────────────────────────────────────────────────────────────────────────
// GovernanceAuditLog — structured audit event
// ──────────────────────────────────────────────────────────────────────────────

// AuditEventType enumerates the types of governance audit events.
type AuditEventType string

const (
	AuditConfigChange  AuditEventType = "CONFIG_CHANGE"
	AuditTrustMutation AuditEventType = "TRUST_MUTATION"
	AuditVerdict       AuditEventType = "VERDICT"
	AuditEscrowAction  AuditEventType = "ESCROW_ACTION"
	AuditTokenIssued   AuditEventType = "TOKEN_ISSUED"
	AuditTokenRevoked  AuditEventType = "TOKEN_REVOKED"
	AuditMeterBilling  AuditEventType = "METER_BILLING"
	AuditHITLDecision  AuditEventType = "HITL_DECISION"
)

// GovernanceAuditEntry represents a single audit log entry.
type GovernanceAuditEntry struct {
	LogID     string            `json:"log_id,omitempty"`
	TenantID  string            `json:"tenant_id"`
	EventType AuditEventType    `json:"event_type"`
	ActorID   string            `json:"actor_id,omitempty"`
	TargetID  string            `json:"target_id,omitempty"`
	Action    string            `json:"action"`
	OldValue  json.RawMessage   `json:"old_value,omitempty"`
	NewValue  json.RawMessage   `json:"new_value,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	CreatedAt time.Time         `json:"created_at,omitempty"`
}

// AuditLogger writes governance audit events.
type AuditLogger interface {
	InsertGovernanceAuditLog(entry *GovernanceAuditEntry) error
}

// AuditService provides a structured API for logging governance events.
type AuditService struct {
	logger AuditLogger
}

// NewAuditService creates a new audit service backed by the given logger.
func NewAuditService(logger AuditLogger) *AuditService {
	return &AuditService{logger: logger}
}

// LogConfigChange records a governance config change with old and new values.
func (a *AuditService) LogConfigChange(tenantID, actorID, field string, oldVal, newVal interface{}) {
	oldJSON, _ := json.Marshal(map[string]interface{}{"field": field, "value": oldVal})
	newJSON, _ := json.Marshal(map[string]interface{}{"field": field, "value": newVal})

	entry := &GovernanceAuditEntry{
		TenantID:  tenantID,
		EventType: AuditConfigChange,
		ActorID:   actorID,
		Action:    "update_" + field,
		OldValue:  oldJSON,
		NewValue:  newJSON,
	}

	if err := a.logger.InsertGovernanceAuditLog(entry); err != nil {
		slog.Error("Failed to log governance config change", "error", err, "tenant_id", tenantID)
	}
}

// LogTrustMutation records a trust score change (levy, reward, quarantine).
func (a *AuditService) LogTrustMutation(tenantID, actorID, agentID, action string, oldScore, newScore float64) {
	oldJSON, _ := json.Marshal(map[string]interface{}{"trust_score": oldScore})
	newJSON, _ := json.Marshal(map[string]interface{}{"trust_score": newScore})

	entry := &GovernanceAuditEntry{
		TenantID:  tenantID,
		EventType: AuditTrustMutation,
		ActorID:   actorID,
		TargetID:  agentID,
		Action:    action,
		OldValue:  oldJSON,
		NewValue:  newJSON,
	}

	if err := a.logger.InsertGovernanceAuditLog(entry); err != nil {
		slog.Error("Failed to log trust mutation", "error", err, "tenant_id", tenantID)
	}
}

// LogVerdict records a jury or gate verdict.
func (a *AuditService) LogVerdict(tenantID, actorID, targetID, verdict string, trustLevel float64) {
	newJSON, _ := json.Marshal(map[string]interface{}{
		"verdict":     verdict,
		"trust_level": trustLevel,
	})

	entry := &GovernanceAuditEntry{
		TenantID:  tenantID,
		EventType: AuditVerdict,
		ActorID:   actorID,
		TargetID:  targetID,
		Action:    "verdict_" + verdict,
		NewValue:  newJSON,
	}

	if err := a.logger.InsertGovernanceAuditLog(entry); err != nil {
		slog.Error("Failed to log verdict", "error", err, "tenant_id", tenantID)
	}
}

// LogGeneric records a generic governance event with custom data.
func (a *AuditService) LogGeneric(tenantID string, eventType AuditEventType, actorID, targetID, action string, data interface{}) {
	dataJSON, _ := json.Marshal(data)

	entry := &GovernanceAuditEntry{
		TenantID:  tenantID,
		EventType: eventType,
		ActorID:   actorID,
		TargetID:  targetID,
		Action:    action,
		NewValue:  dataJSON,
	}

	if err := a.logger.InsertGovernanceAuditLog(entry); err != nil {
		slog.Error("Failed to log governance event", "error", err,
			"tenant_id", tenantID, "event_type", eventType)
	}
}
