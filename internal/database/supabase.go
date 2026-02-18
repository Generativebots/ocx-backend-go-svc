package database

import (
	"context"
	"fmt"
	"os"
	"time"

	supabase "github.com/supabase-community/supabase-go"
)

// ============================================================================
// SUPABASE CLIENT - Complete CRUD Operations for All Tables
// ============================================================================

// SupabaseClient wraps the Supabase Go client with all OCX operations
type SupabaseClient struct {
	client *supabase.Client
}

// NewSupabaseClient creates a new Supabase client
func NewSupabaseClient() (*SupabaseClient, error) {
	url := os.Getenv("SUPABASE_URL")
	key := os.Getenv("SUPABASE_SERVICE_KEY")

	if url == "" || key == "" {
		return nil, fmt.Errorf("SUPABASE_URL and SUPABASE_SERVICE_KEY must be set")
	}

	client, err := supabase.NewClient(url, key, &supabase.ClientOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to create Supabase client: %w", err)
	}

	return &SupabaseClient{client: client}, nil
}

// ============================================================================
// DATA MODELS
// ============================================================================

// Tenant represents a tenant organization
type Tenant struct {
	TenantID         string                 `json:"tenant_id"`
	TenantName       string                 `json:"tenant_name"`
	OrganizationName string                 `json:"organization_name"`
	SubscriptionTier string                 `json:"subscription_tier"`
	Status           string                 `json:"status"`
	Settings         map[string]interface{} `json:"settings"`
	CreatedAt        string                 `json:"created_at"` // String to handle Supabase timestamp format
}

// TenantFeature represents a feature flag for a tenant
type TenantFeature struct {
	TenantID    string                 `json:"tenant_id"`
	FeatureName string                 `json:"feature_name"`
	Enabled     bool                   `json:"enabled"`
	Config      map[string]interface{} `json:"config"`
}

// APIKey represents an API key for a tenant
type APIKey struct {
	KeyID      string     `json:"key_id"`
	TenantID   string     `json:"tenant_id"`
	Name       string     `json:"name"`
	KeyHash    string     `json:"key_hash"`
	Scopes     []string   `json:"scopes"`
	IsActive   bool       `json:"is_active"`
	ExpiresAt  *time.Time `json:"expires_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
}

// Agent represents an agent in the system
type Agent struct {
	AgentID                string  `json:"agent_id"`
	TenantID               string  `json:"tenant_id"`
	Organization           string  `json:"organization"`
	TrustScore             float64 `json:"trust_score"`
	BehavioralDrift        float64 `json:"behavioral_drift"`
	GovTaxBalance          int64   `json:"gov_tax_balance"`
	IsFrozen               bool    `json:"is_frozen"`
	ReputationScore        float64 `json:"reputation_score"`
	TotalInteractions      int64   `json:"total_interactions"`
	SuccessfulInteractions int64   `json:"successful_interactions"`
	FailedInteractions     int64   `json:"failed_interactions"`
	Blacklisted            bool    `json:"blacklisted"`
	FirstSeen              string  `json:"first_seen,omitempty"`
	LastUpdated            string  `json:"last_updated,omitempty"`
	CreatedAt              string  `json:"created_at,omitempty"`
	// Enriched profile fields
	AgentType      string                 `json:"agent_type,omitempty"`
	Classification string                 `json:"classification,omitempty"`
	Capabilities   []string               `json:"capabilities,omitempty"`
	RiskTier       string                 `json:"risk_tier,omitempty"`
	OriginIP       string                 `json:"origin_ip,omitempty"`
	OriginCountry  string                 `json:"origin_country,omitempty"`
	LastIP         string                 `json:"last_ip,omitempty"`
	LastCountry    string                 `json:"last_country,omitempty"`
	Protocol       string                 `json:"protocol,omitempty"`
	ModelProvider  string                 `json:"model_provider,omitempty"`
	ModelName      string                 `json:"model_name,omitempty"`
	Description    string                 `json:"description,omitempty"`
	MaxTools       int                    `json:"max_tools,omitempty"`
	AllowedActions []string               `json:"allowed_actions,omitempty"`
	BlockedActions []string               `json:"blocked_actions,omitempty"`
	AgentMetadata  map[string]interface{} `json:"agent_metadata,omitempty"`
}

// TrustScores represents trust score breakdown
type TrustScores struct {
	AgentID          string  `json:"agent_id"`
	TenantID         string  `json:"tenant_id"`
	AuditScore       float64 `json:"audit_score"`
	ReputationScore  float64 `json:"reputation_score"`
	AttestationScore float64 `json:"attestation_score"`
	HistoryScore     float64 `json:"history_score"`
	TrustLevel       float64 `json:"trust_level"`
	UpdatedAt        string  `json:"updated_at,omitempty"`
}

// ReputationAudit represents an audit log entry
type ReputationAudit struct {
	AuditID       string  `json:"audit_id,omitempty"`
	TenantID      string  `json:"tenant_id"`
	AgentID       string  `json:"agent_id"`
	TransactionID string  `json:"transaction_id,omitempty"`
	Verdict       string  `json:"verdict"`
	TaxLevied     int64   `json:"tax_levied"`
	EntropyDelta  float64 `json:"entropy_delta"`
	Reasoning     string  `json:"reasoning,omitempty"`
	CreatedAt     string  `json:"created_at,omitempty"`
}

// Verdict represents a trust verdict
type Verdict struct {
	VerdictID  string  `json:"verdict_id,omitempty"`
	TenantID   string  `json:"tenant_id"`
	RequestID  string  `json:"request_id"`
	AgentID    string  `json:"agent_id,omitempty"`
	PID        int32   `json:"pid,omitempty"`
	BinaryHash string  `json:"binary_hash,omitempty"`
	Action     string  `json:"action"`
	TrustLevel float64 `json:"trust_level,omitempty"`
	TrustTax   float64 `json:"trust_tax,omitempty"`
	Reasoning  string  `json:"reasoning,omitempty"`
	CreatedAt  string  `json:"created_at,omitempty"`
}

// HandshakeSession represents a handshake session
type HandshakeSession struct {
	SessionID   string                 `json:"session_id"`
	TenantID    string                 `json:"tenant_id"`
	InitiatorID string                 `json:"initiator_id"`
	ResponderID string                 `json:"responder_id"`
	State       string                 `json:"state"`
	Nonce       string                 `json:"nonce,omitempty"`
	Challenge   string                 `json:"challenge,omitempty"`
	Proof       string                 `json:"proof,omitempty"`
	Attestation map[string]interface{} `json:"attestation,omitempty"`
	CreatedAt   string                 `json:"created_at,omitempty"`
	ExpiresAt   string                 `json:"expires_at,omitempty"`
	CompletedAt string                 `json:"completed_at,omitempty"`
}

// AgentIdentity represents PID to AgentID mapping
type AgentIdentity struct {
	PID        int32   `json:"pid"`
	TenantID   string  `json:"tenant_id"`
	AgentID    string  `json:"agent_id"`
	BinaryHash string  `json:"binary_hash,omitempty"`
	TrustLevel float64 `json:"trust_level,omitempty"`
	CreatedAt  string  `json:"created_at,omitempty"`
	ExpiresAt  string  `json:"expires_at,omitempty"`
}

// QuarantineRecord represents a quarantine record
type QuarantineRecord struct {
	QuarantineID  string  `json:"quarantine_id,omitempty"`
	TenantID      string  `json:"tenant_id"`
	AgentID       string  `json:"agent_id"`
	Reason        string  `json:"reason"`
	AlertSource   string  `json:"alert_source,omitempty"`
	QuarantinedAt string  `json:"quarantined_at,omitempty"`
	ReleasedAt    *string `json:"released_at,omitempty"`
	IsActive      bool    `json:"is_active"`
}

// RecoveryAttempt represents a recovery attempt
type RecoveryAttempt struct {
	AttemptID     string `json:"attempt_id,omitempty"`
	TenantID      string `json:"tenant_id"`
	AgentID       string `json:"agent_id"`
	StakeAmount   int64  `json:"stake_amount"`
	Success       bool   `json:"success"`
	AttemptNumber int    `json:"attempt_number"`
	CreatedAt     string `json:"created_at,omitempty"`
}

// ProbationPeriod represents a probation period
type ProbationPeriod struct {
	ProbationID string  `json:"probation_id,omitempty"`
	TenantID    string  `json:"tenant_id"`
	AgentID     string  `json:"agent_id"`
	StartedAt   string  `json:"started_at,omitempty"`
	EndsAt      string  `json:"ends_at"`
	Threshold   float64 `json:"threshold"`
	IsActive    bool    `json:"is_active"`
}

// RewardDistribution represents a reward distribution
type RewardDistribution struct {
	DistributionID     string  `json:"distribution_id,omitempty"`
	TenantID           string  `json:"tenant_id"`
	AgentID            string  `json:"agent_id"`
	Amount             int64   `json:"amount"`
	TrustScore         float64 `json:"trust_score,omitempty"`
	ParticipationCount int     `json:"participation_count,omitempty"`
	Formula            string  `json:"formula,omitempty"`
	DistributedAt      string  `json:"distributed_at,omitempty"`
}

// ============================================================================
// AGENTS OPERATIONS
// ============================================================================

// GetAgent retrieves an agent by ID and Tenant
func (sc *SupabaseClient) GetAgent(ctx context.Context, tenantID, agentID string) (*Agent, error) {
	var agents []Agent
	_, err := sc.client.From("agents").
		Select("*", "", false).
		Eq("agent_id", agentID).
		Eq("tenant_id", tenantID).
		ExecuteTo(&agents)

	if err != nil {
		return nil, fmt.Errorf("failed to get agent: %w", err)
	}
	if len(agents) == 0 {
		return nil, nil
	}
	return &agents[0], nil
}

// CreateAgent creates a new agent
func (sc *SupabaseClient) CreateAgent(ctx context.Context, agent *Agent) error {
	var result []Agent
	_, err := sc.client.From("agents").
		Insert(agent, false, "", "", "").
		ExecuteTo(&result)
	return err
}

// UpdateAgent updates an agent
func (sc *SupabaseClient) UpdateAgent(ctx context.Context, agent *Agent) error {
	var result []Agent
	_, err := sc.client.From("agents").
		Update(agent, "", "").
		Eq("agent_id", agent.AgentID).
		Eq("tenant_id", agent.TenantID).
		ExecuteTo(&result)
	return err
}

// ListAgents lists all agents for a tenant with optional filters
func (sc *SupabaseClient) ListAgents(ctx context.Context, tenantID string, limit int) ([]Agent, error) {
	var agents []Agent
	_, err := sc.client.From("agents").
		Select("*", "", false).
		Eq("tenant_id", tenantID).
		Limit(limit, "").
		ExecuteTo(&agents)
	return agents, err
}

// ListAllAgents lists all agents across all tenants
func (sc *SupabaseClient) ListAllAgents(ctx context.Context, limit int) ([]Agent, error) {
	var agents []Agent
	_, err := sc.client.From("agents").
		Select("*", "", false).
		Limit(limit, "").
		Order("last_updated", nil).
		ExecuteTo(&agents)
	return agents, err
}

// ============================================================================
// SESSION AUDIT LOG OPERATIONS
// ============================================================================

// InsertAuditLog inserts a session audit log entry
func (sc *SupabaseClient) InsertAuditLog(entry interface{}) error {
	var result []map[string]interface{}
	_, err := sc.client.From("session_audit_log").
		Insert(entry, false, "", "", "").
		ExecuteTo(&result)
	return err
}

// QueryAuditLogs queries session audit logs with filters
func (sc *SupabaseClient) QueryAuditLogs(agentID, tenantID, eventType, ip, since, until string, limit, offset int) ([]map[string]interface{}, error) {
	query := sc.client.From("session_audit_log").
		Select("*", "", false).
		Order("created_at", nil)

	if agentID != "" {
		query = query.Eq("agent_id", agentID)
	}
	if tenantID != "" {
		query = query.Eq("tenant_id", tenantID)
	}
	if eventType != "" {
		query = query.Eq("event_type", eventType)
	}
	if ip != "" {
		query = query.Eq("ip_address", ip)
	}
	if since != "" {
		query = query.Gte("created_at", since)
	}
	if until != "" {
		query = query.Lte("created_at", until)
	}
	if limit <= 0 {
		limit = 50
	}
	query = query.Limit(limit, "")

	var logs []map[string]interface{}
	_, err := query.ExecuteTo(&logs)
	return logs, err
}

// ============================================================================
// TRUST SCORES OPERATIONS
// ============================================================================

// GetTrustScores retrieves trust scores for an agent
func (sc *SupabaseClient) GetTrustScores(ctx context.Context, tenantID, agentID string) (*TrustScores, error) {
	var scores []TrustScores
	_, err := sc.client.From("trust_scores").
		Select("*", "", false).
		Eq("agent_id", agentID).
		Eq("tenant_id", tenantID).
		ExecuteTo(&scores)

	if err != nil {
		return nil, err
	}
	if len(scores) == 0 {
		return nil, nil
	}
	return &scores[0], nil
}

// UpsertTrustScores updates or inserts trust scores
func (sc *SupabaseClient) UpsertTrustScores(ctx context.Context, scores *TrustScores) error {
	var result []TrustScores
	_, err := sc.client.From("trust_scores").
		Upsert(scores, "", "", "").
		ExecuteTo(&result)
	return err
}

// ============================================================================
// REPUTATION AUDIT OPERATIONS
// ============================================================================

// CreateAuditEntry creates a new audit log entry
func (sc *SupabaseClient) CreateAuditEntry(ctx context.Context, audit *ReputationAudit) error {
	var result []ReputationAudit
	_, err := sc.client.From("reputation_audit").
		Insert(audit, false, "", "", "").
		ExecuteTo(&result)
	return err
}

// GetAuditHistory retrieves audit history for an agent
func (sc *SupabaseClient) GetAuditHistory(ctx context.Context, tenantID, agentID string, limit int) ([]ReputationAudit, error) {
	var audits []ReputationAudit
	_, err := sc.client.From("reputation_audit").
		Select("*", "", false).
		Eq("agent_id", agentID).
		Eq("tenant_id", tenantID).
		Order("created_at", nil).
		Limit(limit, "").
		ExecuteTo(&audits)
	return audits, err
}

// ============================================================================
// VERDICTS OPERATIONS
// ============================================================================

// RecordVerdict records a verdict
func (sc *SupabaseClient) RecordVerdict(ctx context.Context, verdict *Verdict) error {
	var result []Verdict
	_, err := sc.client.From("verdicts").
		Insert(verdict, false, "", "", "").
		ExecuteTo(&result)
	return err
}

// GetRecentVerdicts retrieves recent verdicts for an agent
func (sc *SupabaseClient) GetRecentVerdicts(ctx context.Context, tenantID, agentID string, limit int) ([]Verdict, error) {
	var verdicts []Verdict
	_, err := sc.client.From("verdicts").
		Select("*", "", false).
		Eq("agent_id", agentID).
		Eq("tenant_id", tenantID).
		Order("created_at", nil).
		Limit(limit, "").
		ExecuteTo(&verdicts)
	return verdicts, err
}

// ============================================================================
// HANDSHAKE OPERATIONS
// ============================================================================

// CreateHandshakeSession creates a new handshake session
func (sc *SupabaseClient) CreateHandshakeSession(ctx context.Context, session *HandshakeSession) error {
	var result []HandshakeSession
	_, err := sc.client.From("handshake_sessions").
		Insert(session, false, "", "", "").
		ExecuteTo(&result)
	return err
}

// GetHandshakeSession retrieves a handshake session
func (sc *SupabaseClient) GetHandshakeSession(ctx context.Context, tenantID, sessionID string) (*HandshakeSession, error) {
	var sessions []HandshakeSession
	_, err := sc.client.From("handshake_sessions").
		Select("*", "", false).
		Eq("session_id", sessionID).
		Eq("tenant_id", tenantID).
		ExecuteTo(&sessions)

	if err != nil {
		return nil, err
	}
	if len(sessions) == 0 {
		return nil, nil
	}
	return &sessions[0], nil
}

// UpdateHandshakeSession updates a handshake session
func (sc *SupabaseClient) UpdateHandshakeSession(ctx context.Context, session *HandshakeSession) error {
	var result []HandshakeSession
	_, err := sc.client.From("handshake_sessions").
		Update(session, "", "").
		Eq("session_id", session.SessionID).
		Eq("tenant_id", session.TenantID).
		ExecuteTo(&result)
	return err
}

// ============================================================================
// AGENT IDENTITY OPERATIONS
// ============================================================================

// CreateAgentIdentity creates a PID to AgentID mapping
func (sc *SupabaseClient) CreateAgentIdentity(ctx context.Context, identity *AgentIdentity) error {
	var result []AgentIdentity
	_, err := sc.client.From("agent_identities").
		Upsert(identity, "", "", "").
		ExecuteTo(&result)
	return err
}

// GetAgentIdentity retrieves agent identity by PID
func (sc *SupabaseClient) GetAgentIdentity(ctx context.Context, tenantID string, pid int32) (*AgentIdentity, error) {
	var identities []AgentIdentity
	_, err := sc.client.From("agent_identities").
		Select("*", "", false).
		Eq("pid", fmt.Sprintf("%d", pid)).
		Eq("tenant_id", tenantID).
		ExecuteTo(&identities)

	if err != nil {
		return nil, err
	}
	if len(identities) == 0 {
		return nil, nil
	}
	return &identities[0], nil
}

// ============================================================================
// QUARANTINE OPERATIONS
// ============================================================================

// CreateQuarantineRecord creates a quarantine record
func (sc *SupabaseClient) CreateQuarantineRecord(ctx context.Context, record *QuarantineRecord) error {
	var result []QuarantineRecord
	_, err := sc.client.From("quarantine_records").
		Insert(record, false, "", "", "").
		ExecuteTo(&result)
	return err
}

// GetActiveQuarantines retrieves active quarantines for an agent
func (sc *SupabaseClient) GetActiveQuarantines(ctx context.Context, tenantID, agentID string) ([]QuarantineRecord, error) {
	var records []QuarantineRecord
	_, err := sc.client.From("quarantine_records").
		Select("*", "", false).
		Eq("agent_id", agentID).
		Eq("tenant_id", tenantID).
		Eq("is_active", "true").
		ExecuteTo(&records)
	return records, err
}

// ReleaseQuarantine releases a quarantine
func (sc *SupabaseClient) ReleaseQuarantine(ctx context.Context, tenantID, quarantineID string) error {
	now := time.Now()
	update := map[string]interface{}{
		"released_at": now,
		"is_active":   false,
	}
	var result []QuarantineRecord
	_, err := sc.client.From("quarantine_records").
		Update(update, "", "").
		Eq("quarantine_id", quarantineID).
		Eq("tenant_id", tenantID).
		ExecuteTo(&result)
	return err
}

// ============================================================================
// RECOVERY OPERATIONS
// ============================================================================

// CreateRecoveryAttempt creates a recovery attempt record
func (sc *SupabaseClient) CreateRecoveryAttempt(ctx context.Context, attempt *RecoveryAttempt) error {
	var result []RecoveryAttempt
	_, err := sc.client.From("recovery_attempts").
		Insert(attempt, false, "", "", "").
		ExecuteTo(&result)
	return err
}

// GetRecoveryAttempts retrieves recovery attempts for an agent
func (sc *SupabaseClient) GetRecoveryAttempts(ctx context.Context, tenantID, agentID string) ([]RecoveryAttempt, error) {
	var attempts []RecoveryAttempt
	_, err := sc.client.From("recovery_attempts").
		Select("*", "", false).
		Eq("agent_id", agentID).
		Eq("tenant_id", tenantID).
		Order("created_at", nil).
		ExecuteTo(&attempts)
	return attempts, err
}

// ============================================================================
// PROBATION OPERATIONS
// ============================================================================

// CreateProbationPeriod creates a probation period
func (sc *SupabaseClient) CreateProbationPeriod(ctx context.Context, period *ProbationPeriod) error {
	var result []ProbationPeriod
	_, err := sc.client.From("probation_periods").
		Insert(period, false, "", "", "").
		ExecuteTo(&result)
	return err
}

// GetActiveProbation retrieves active probation for an agent
func (sc *SupabaseClient) GetActiveProbation(ctx context.Context, tenantID, agentID string) (*ProbationPeriod, error) {
	var periods []ProbationPeriod
	_, err := sc.client.From("probation_periods").
		Select("*", "", false).
		Eq("agent_id", agentID).
		Eq("tenant_id", tenantID).
		Eq("is_active", "true").
		ExecuteTo(&periods)

	if err != nil {
		return nil, err
	}
	if len(periods) == 0 {
		return nil, nil
	}
	return &periods[0], nil
}

// ============================================================================
// REWARD DISTRIBUTION OPERATIONS
// ============================================================================

// CreateRewardDistribution creates a reward distribution record
func (sc *SupabaseClient) CreateRewardDistribution(ctx context.Context, distribution *RewardDistribution) error {
	var result []RewardDistribution
	_, err := sc.client.From("reward_distributions").
		Insert(distribution, false, "", "", "").
		ExecuteTo(&result)
	return err
}

// GetRewardHistory retrieves reward history for an agent
func (sc *SupabaseClient) GetRewardHistory(ctx context.Context, tenantID, agentID string, limit int) ([]RewardDistribution, error) {
	var distributions []RewardDistribution
	_, err := sc.client.From("reward_distributions").
		Select("*", "", false).
		Eq("agent_id", agentID).
		Eq("tenant_id", tenantID).
		Order("distributed_at", nil).
		Limit(limit, "").
		ExecuteTo(&distributions)
	return distributions, err
}

// ============================================================================
// TENANT OPERATIONS
// ============================================================================

// GetTenant retrieves a tenant by ID
func (sc *SupabaseClient) GetTenant(ctx context.Context, tenantID string) (*Tenant, error) {
	var tenants []Tenant
	_, err := sc.client.From("tenants").
		Select("*", "", false).
		Eq("tenant_id", tenantID).
		ExecuteTo(&tenants)

	if err != nil {
		return nil, fmt.Errorf("failed to get tenant: %w", err)
	}
	if len(tenants) == 0 {
		return nil, nil
	}
	return &tenants[0], nil
}

// UpdateTenantSettings updates the settings JSONB column for a tenant.
// The caller provides the full settings map which replaces the existing value.
func (sc *SupabaseClient) UpdateTenantSettings(ctx context.Context, tenantID string, settings map[string]interface{}) error {
	update := map[string]interface{}{
		"settings": settings,
	}
	var result []Tenant
	_, err := sc.client.From("tenants").
		Update(update, "", "").
		Eq("tenant_id", tenantID).
		ExecuteTo(&result)
	return err
}

// GetTenantFeatures retrieves all features for a tenant
func (sc *SupabaseClient) GetTenantFeatures(ctx context.Context, tenantID string) ([]TenantFeature, error) {
	var features []TenantFeature
	_, err := sc.client.From("tenant_features").
		Select("*", "", false).
		Eq("tenant_id", tenantID).
		ExecuteTo(&features)
	return features, err
}

// GetAPIKey retrieves an API key by ID (public part)
// CHANGED: Use keyID instead of keyHash lookup for efficiency/design match with TenantManager
func (sc *SupabaseClient) GetAPIKey(ctx context.Context, keyID string) (*APIKey, error) {
	var keys []APIKey
	_, err := sc.client.From("api_keys").
		Select("*", "", false).
		Eq("key_id", keyID).
		ExecuteTo(&keys)

	if err != nil {
		return nil, fmt.Errorf("failed to get api key: %w", err)
	}
	if len(keys) == 0 {
		return nil, nil
	}
	return &keys[0], nil
}

// CreateAPIKey creates a new API key
func (sc *SupabaseClient) CreateAPIKey(ctx context.Context, apiKey *APIKey) error {
	var result []APIKey
	_, err := sc.client.From("api_keys").
		Insert(apiKey, false, "", "", "").
		ExecuteTo(&result)
	return err
}

// ============================================================================
// HELPER METHODS
// ============================================================================

// ApplyPenalty deducts from gov tax balance
func (sc *SupabaseClient) ApplyPenalty(ctx context.Context, tenantID, agentID string, amount int64) error {
	agent, err := sc.GetAgent(ctx, tenantID, agentID)
	if err != nil {
		return err
	}
	if agent == nil {
		return fmt.Errorf("agent not found: %s", agentID)
	}

	agent.GovTaxBalance -= amount
	agent.BehavioralDrift += 0.1

	return sc.UpdateAgent(ctx, agent)
}

// RewardAgent increases gov tax balance
func (sc *SupabaseClient) RewardAgent(ctx context.Context, tenantID, agentID string, amount int64) error {
	agent, err := sc.GetAgent(ctx, tenantID, agentID)
	if err != nil {
		return err
	}
	if agent == nil {
		return fmt.Errorf("agent not found: %s", agentID)
	}

	agent.GovTaxBalance += amount
	agent.TrustScore = min(1.0, agent.TrustScore+0.01)

	return sc.UpdateAgent(ctx, agent)
}

// ============================================================================
// GENERIC HELPERS — used by evidence store and other integrations
// ============================================================================

// InsertRow inserts a single row into any table.
func (sc *SupabaseClient) InsertRow(table string, row interface{}) error {
	_, _, err := sc.client.From(table).Insert(row, false, "", "", "").Execute()
	return err
}

// QueryRows queries rows from a table filtered by a single column.
func (sc *SupabaseClient) QueryRows(table, selectCols, filterCol, filterVal string, dest interface{}) error {
	_, err := sc.client.From(table).
		Select(selectCols, "", false).
		Eq(filterCol, filterVal).
		ExecuteTo(dest)
	return err
}

// ============================================================================
// TENANT GOVERNANCE CONFIG — CRUD for tenant_governance_config table
// ============================================================================

// GetTenantGovernanceConfig retrieves the governance config for a tenant.
// Returns nil (not error) if no config row exists.
func (sc *SupabaseClient) GetTenantGovernanceConfig(tenantID string) (*TenantGovernanceConfigRow, error) {
	var results []TenantGovernanceConfigRow
	_, err := sc.client.From("tenant_governance_config").
		Select("*", "", false).
		Eq("tenant_id", tenantID).
		ExecuteTo(&results)
	if err != nil {
		return nil, fmt.Errorf("query tenant_governance_config: %w", err)
	}
	if len(results) == 0 {
		return nil, nil
	}
	return &results[0], nil
}

// UpsertTenantGovernanceConfig creates or updates the governance config for a tenant.
func (sc *SupabaseClient) UpsertTenantGovernanceConfig(tenantID string, cfg *TenantGovernanceConfigRow) error {
	cfg.TenantID = tenantID
	_, _, err := sc.client.From("tenant_governance_config").
		Upsert(cfg, "tenant_id", "", "").
		Execute()
	return err
}

// ============================================================================
// GOVERNANCE AUDIT LOG — Insert and Query for governance_audit_log table
// ============================================================================

// InsertGovernanceAuditLog inserts a single audit log entry.
func (sc *SupabaseClient) InsertGovernanceAuditLog(entry *GovernanceAuditLogRow) error {
	_, _, err := sc.client.From("governance_audit_log").
		Insert(entry, false, "", "", "").
		Execute()
	return err
}

// GetGovernanceAuditLogs retrieves audit log entries for a tenant, newest first.
func (sc *SupabaseClient) GetGovernanceAuditLogs(tenantID string, limit int) ([]GovernanceAuditLogRow, error) {
	var results []GovernanceAuditLogRow
	_, err := sc.client.From("governance_audit_log").
		Select("*", "", false).
		Eq("tenant_id", tenantID).
		Order("created_at", nil).
		Limit(limit, "").
		ExecuteTo(&results)
	if err != nil {
		return nil, fmt.Errorf("query governance_audit_log: %w", err)
	}
	return results, nil
}
