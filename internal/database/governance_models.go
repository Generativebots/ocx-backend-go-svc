// Package database — governance config and audit log data models.
package database

import (
	"encoding/json"
)

// TenantGovernanceConfigRow mirrors the tenant_governance_config Supabase table.
type TenantGovernanceConfigRow struct {
	ConfigID string `json:"config_id,omitempty"`
	TenantID string `json:"tenant_id"`

	// Trust
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

	// Economics
	TrustTaxBaseRate      float64 `json:"trust_tax_base_rate"`
	FederationTaxBaseRate float64 `json:"federation_tax_base_rate"`
	PerEventTaxRate       float64 `json:"per_event_tax_rate"`
	MarketplaceCommission float64 `json:"marketplace_commission"`
	HITLCostMultiplier    float64 `json:"hitl_cost_multiplier"`

	// Risk & Metering
	RiskMultipliers           json.RawMessage `json:"risk_multipliers"`
	MeterHighTrustThreshold   float64         `json:"meter_high_trust_threshold"`
	MeterHighTrustDiscount    float64         `json:"meter_high_trust_discount"`
	MeterMedTrustThreshold    float64         `json:"meter_med_trust_threshold"`
	MeterMedTrustDiscount     float64         `json:"meter_med_trust_discount"`
	MeterLowTrustThreshold    float64         `json:"meter_low_trust_threshold"`
	MeterLowTrustSurcharge    float64         `json:"meter_low_trust_surcharge"`
	MeterBaseCostPerFrame     float64         `json:"meter_base_cost_per_frame"`
	UnknownToolMinReputation  float64         `json:"unknown_tool_min_reputation"`
	UnknownToolTaxCoefficient float64         `json:"unknown_tool_tax_coefficient"`

	// Tri-Factor Gate
	IdentityThreshold          float64 `json:"identity_threshold"`
	EntropyThreshold           float64 `json:"entropy_threshold"`
	JitterThreshold            float64 `json:"jitter_threshold"`
	CognitiveThreshold         float64 `json:"cognitive_threshold"`
	EntropyHighCap             float64 `json:"entropy_high_cap"`
	EntropyEncryptedThreshold  float64 `json:"entropy_encrypted_threshold"`
	EntropySuspiciousThreshold float64 `json:"entropy_suspicious_threshold"`

	// Security
	DriftThreshold   float64 `json:"drift_threshold"`
	AnomalyThreshold int     `json:"anomaly_threshold"`

	// Federation
	DecayHalfLifeHours     float64 `json:"decay_half_life_hours"`
	TrustEmaAlpha          float64 `json:"trust_ema_alpha"`
	FailurePenaltyFactor   float64 `json:"failure_penalty_factor"`
	SupermajorityThreshold float64 `json:"supermajority_threshold"`
	HandshakeMinTrust      float64 `json:"handshake_min_trust"`

	// Metadata
	UpdatedBy string `json:"updated_by,omitempty"`
	CreatedAt string `json:"created_at,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

// GovernanceAuditLogRow mirrors the governance_audit_log Supabase table.
type GovernanceAuditLogRow struct {
	LogID     string          `json:"log_id,omitempty"`
	TenantID  string          `json:"tenant_id"`
	EventType string          `json:"event_type"`
	ActorID   string          `json:"actor_id,omitempty"`
	TargetID  string          `json:"target_id,omitempty"`
	Action    string          `json:"action"`
	OldValue  json.RawMessage `json:"old_value,omitempty"`
	NewValue  json.RawMessage `json:"new_value,omitempty"`
	Metadata  json.RawMessage `json:"metadata,omitempty"`
	CreatedAt string          `json:"created_at,omitempty"`
}

// OrderOpts is not needed - supabase-go provides its own.
