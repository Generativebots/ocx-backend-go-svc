-- =============================================================================
-- OCX MASTER DATABASE SCHEMA FOR SUPABASE
-- =============================================================================
-- 
-- SINGLE SOURCE OF TRUTH: This is the only database schema file for OCX.
-- Used by both Go backend and Python services.
--
-- HOW TO USE:
-- 1. Go to Supabase Dashboard → SQL Editor → New Query
-- 2. Paste this entire file
-- 3. Click "Run"
--
-- =============================================================================

-- Enable required extensions
CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- =============================================================================
-- SECTION 1: TENANTS & MULTI-TENANCY
-- =============================================================================

CREATE TABLE IF NOT EXISTS tenants (
    tenant_id TEXT PRIMARY KEY,
    tenant_name TEXT NOT NULL,
    organization_name TEXT NOT NULL,
    subscription_tier TEXT NOT NULL DEFAULT 'FREE',
    CHECK (subscription_tier IN ('FREE', 'STARTER', 'PROFESSIONAL', 'ENTERPRISE')),
    status TEXT NOT NULL DEFAULT 'ACTIVE',
    CHECK (status IN ('ACTIVE', 'SUSPENDED', 'TRIAL', 'CANCELLED')),
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    trial_ends_at TIMESTAMP,
    admin_email TEXT NOT NULL,
    admin_name TEXT,
    max_agents INTEGER DEFAULT 5,
    max_activities INTEGER DEFAULT 50,
    max_evidence_per_month INTEGER DEFAULT 10000,
    settings JSONB DEFAULT '{}'::jsonb
);

CREATE TABLE IF NOT EXISTS tenant_features (
    tenant_id TEXT NOT NULL REFERENCES tenants(tenant_id) ON DELETE CASCADE,
    feature_name TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    config JSONB DEFAULT '{}'::jsonb,
    enabled_at TIMESTAMP,
    enabled_by TEXT,
    PRIMARY KEY (tenant_id, feature_name)
);

CREATE TABLE IF NOT EXISTS tenant_agents (
    agent_id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL REFERENCES tenants(tenant_id) ON DELETE CASCADE,
    agent_name TEXT NOT NULL,
    agent_type TEXT NOT NULL,
    CHECK (agent_type IN ('HUMAN', 'SYSTEM', 'AI')),
    status TEXT NOT NULL DEFAULT 'ACTIVE',
    CHECK (status IN ('ACTIVE', 'INACTIVE', 'SUSPENDED')),
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    last_active_at TIMESTAMP,
    config JSONB DEFAULT '{}'::jsonb
);

CREATE TABLE IF NOT EXISTS tenant_usage (
    usage_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id TEXT NOT NULL REFERENCES tenants(tenant_id) ON DELETE CASCADE,
    period_start TIMESTAMP NOT NULL,
    period_end TIMESTAMP NOT NULL,
    activities_executed INTEGER DEFAULT 0,
    evidence_collected INTEGER DEFAULT 0,
    documents_processed INTEGER DEFAULT 0,
    api_calls INTEGER DEFAULT 0,
    storage_bytes BIGINT DEFAULT 0,
    estimated_cost DECIMAL(10,2) DEFAULT 0.00,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

-- =============================================================================
-- SECTION 2: AGENTS (Trust Registry)
-- =============================================================================

CREATE TABLE IF NOT EXISTS agents (
    agent_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL,
    name TEXT,
    provider TEXT,
    tier TEXT,
    auth_scope TEXT,
    public_key TEXT,
    status TEXT DEFAULT 'Active',
    full_schema_json JSONB,
    organization TEXT,
    trust_score FLOAT DEFAULT 0.5,
    behavioral_drift FLOAT DEFAULT 0.0,
    gov_tax_balance BIGINT DEFAULT 0,
    is_frozen BOOLEAN DEFAULT FALSE,
    reputation_score FLOAT DEFAULT 0.5,
    total_interactions BIGINT DEFAULT 0,
    successful_interactions BIGINT DEFAULT 0,
    failed_interactions BIGINT DEFAULT 0,
    blacklisted BOOLEAN DEFAULT FALSE,
    first_seen TIMESTAMP DEFAULT NOW(),
    last_updated TIMESTAMP DEFAULT NOW(),
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS rules (
    rule_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL,
    natural_language TEXT,
    logic_json JSONB,
    priority INT DEFAULT 1,
    status TEXT DEFAULT 'Active',
    created_at TIMESTAMPTZ DEFAULT NOW()
);

-- =============================================================================
-- SECTION 3: TRUST SCORES & REPUTATION
-- =============================================================================

CREATE TABLE IF NOT EXISTS trust_scores (
    score_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id TEXT NOT NULL,
    tenant_id TEXT NOT NULL,
    audit_score FLOAT NOT NULL DEFAULT 0.5,
    reputation_score FLOAT NOT NULL DEFAULT 0.5,
    attestation_score FLOAT NOT NULL DEFAULT 0.5,
    history_score FLOAT NOT NULL DEFAULT 0.5,
    trust_level FLOAT NOT NULL DEFAULT 0.5,
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    UNIQUE(agent_id, tenant_id)
);

CREATE INDEX IF NOT EXISTS idx_trust_scores_agent ON trust_scores(agent_id);
CREATE INDEX IF NOT EXISTS idx_trust_scores_tenant ON trust_scores(tenant_id);

CREATE TABLE IF NOT EXISTS agents_reputation (
    agent_id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    trust_score FLOAT NOT NULL DEFAULT 0.5,
    behavioral_drift FLOAT NOT NULL DEFAULT 0.0,
    gov_tax_balance BIGINT NOT NULL DEFAULT 0,
    is_frozen BOOLEAN NOT NULL DEFAULT FALSE,
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_agents_trust ON agents_reputation(trust_score DESC);

CREATE TABLE IF NOT EXISTS reputation_audit (
    audit_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id TEXT NOT NULL,
    tenant_id TEXT,
    transaction_id TEXT,
    verdict TEXT,
    entropy_delta FLOAT,
    tax_levied BIGINT,
    reasoning TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_audit_agent ON reputation_audit(agent_id, created_at DESC);

-- =============================================================================
-- SECTION 4: VERDICTS & DECISIONS
-- =============================================================================

CREATE TABLE IF NOT EXISTS verdicts (
    verdict_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id TEXT NOT NULL,
    request_id TEXT NOT NULL,
    agent_id TEXT,
    pid INTEGER,
    binary_hash TEXT,
    action TEXT NOT NULL,
    trust_level FLOAT,
    trust_tax FLOAT,
    reasoning TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_verdicts_tenant ON verdicts(tenant_id);
CREATE INDEX IF NOT EXISTS idx_verdicts_agent ON verdicts(agent_id);
CREATE INDEX IF NOT EXISTS idx_verdicts_request ON verdicts(request_id);

-- =============================================================================
-- SECTION 5: HANDSHAKE SESSIONS
-- =============================================================================

CREATE TABLE IF NOT EXISTS handshake_sessions (
    session_id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    initiator_id TEXT NOT NULL,
    responder_id TEXT NOT NULL,
    state TEXT NOT NULL DEFAULT 'INITIATED',
    nonce TEXT,
    challenge TEXT,
    proof TEXT,
    attestation JSONB,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMP NOT NULL,
    completed_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_handshake_tenant ON handshake_sessions(tenant_id);
CREATE INDEX IF NOT EXISTS idx_handshake_state ON handshake_sessions(state);

-- =============================================================================
-- SECTION 6: AGENT IDENTITIES (PID Mapping)
-- =============================================================================

CREATE TABLE IF NOT EXISTS agent_identities (
    identity_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    pid INTEGER NOT NULL,
    tenant_id TEXT NOT NULL,
    agent_id TEXT NOT NULL,
    binary_hash TEXT,
    trust_level FLOAT,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMP NOT NULL,
    UNIQUE(pid, tenant_id)
);

CREATE INDEX IF NOT EXISTS idx_agent_identities_pid ON agent_identities(pid);
CREATE INDEX IF NOT EXISTS idx_agent_identities_tenant ON agent_identities(tenant_id);

-- =============================================================================
-- SECTION 7: QUARANTINE & RECOVERY
-- =============================================================================

CREATE TABLE IF NOT EXISTS quarantine_records (
    quarantine_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id TEXT NOT NULL,
    agent_id TEXT NOT NULL,
    reason TEXT NOT NULL,
    alert_source TEXT,
    quarantined_at TIMESTAMP NOT NULL DEFAULT NOW(),
    released_at TIMESTAMP,
    is_active BOOLEAN NOT NULL DEFAULT TRUE
);

CREATE INDEX IF NOT EXISTS idx_quarantine_agent ON quarantine_records(agent_id);
CREATE INDEX IF NOT EXISTS idx_quarantine_active ON quarantine_records(is_active) WHERE is_active = TRUE;

CREATE TABLE IF NOT EXISTS recovery_attempts (
    attempt_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id TEXT NOT NULL,
    agent_id TEXT NOT NULL,
    stake_amount BIGINT NOT NULL,
    success BOOLEAN NOT NULL,
    attempt_number INTEGER NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_recovery_agent ON recovery_attempts(agent_id);

CREATE TABLE IF NOT EXISTS probation_periods (
    probation_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id TEXT NOT NULL,
    agent_id TEXT NOT NULL,
    started_at TIMESTAMP NOT NULL DEFAULT NOW(),
    ends_at TIMESTAMP NOT NULL,
    threshold FLOAT NOT NULL,
    is_active BOOLEAN NOT NULL DEFAULT TRUE
);

CREATE INDEX IF NOT EXISTS idx_probation_agent ON probation_periods(agent_id);
CREATE INDEX IF NOT EXISTS idx_probation_active ON probation_periods(is_active) WHERE is_active = TRUE;

-- =============================================================================
-- SECTION 8: GOVERNANCE
-- =============================================================================

CREATE TABLE IF NOT EXISTS committee_members (
    member_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id TEXT NOT NULL,
    member_name TEXT NOT NULL,
    email TEXT NOT NULL,
    role TEXT NOT NULL DEFAULT 'MEMBER',
    joined_at TIMESTAMP NOT NULL DEFAULT NOW(),
    is_active BOOLEAN DEFAULT TRUE,
    UNIQUE(email)
);

CREATE TABLE IF NOT EXISTS governance_proposals (
    proposal_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title TEXT NOT NULL,
    description TEXT NOT NULL,
    author_id UUID REFERENCES committee_members(member_id),
    status TEXT NOT NULL DEFAULT 'DRAFT',
    CHECK (status IN ('DRAFT', 'OPEN', 'PASSED', 'REJECTED', 'IMPLEMENTED')),
    target_version TEXT,
    backward_compatible BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    voting_starts_at TIMESTAMP,
    voting_ends_at TIMESTAMP,
    passed_at TIMESTAMP
);

CREATE TABLE IF NOT EXISTS governance_votes (
    vote_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    proposal_id UUID REFERENCES governance_proposals(proposal_id),
    member_id UUID REFERENCES committee_members(member_id),
    vote_choice TEXT NOT NULL,
    justification TEXT,
    voted_at TIMESTAMP NOT NULL DEFAULT NOW(),
    UNIQUE(proposal_id, member_id)
);

-- Governance Ledger (Immutable Audit Trail)
CREATE TABLE IF NOT EXISTS governance_ledger (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    transaction_id TEXT NOT NULL UNIQUE,
    agent_id TEXT NOT NULL,
    action TEXT NOT NULL,
    policy_version TEXT,
    jury_verdict TEXT,
    entropy_score REAL,
    sop_decision TEXT,
    pid_verified BOOLEAN DEFAULT FALSE,
    previous_hash TEXT NOT NULL,
    block_hash TEXT NOT NULL,
    timestamp TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_governance_ledger_agent ON governance_ledger(agent_id);
CREATE INDEX IF NOT EXISTS idx_governance_ledger_timestamp ON governance_ledger(timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_governance_ledger_hash ON governance_ledger(block_hash);

-- =============================================================================
-- SECTION 9: BILLING & REWARDS
-- =============================================================================

CREATE TABLE IF NOT EXISTS billing_transactions (
    transaction_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id TEXT NOT NULL,
    request_id TEXT NOT NULL,
    trust_score FLOAT NOT NULL,
    transaction_value FLOAT DEFAULT 1.0,
    trust_tax FLOAT NOT NULL,
    timestamp TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_billing_tenant_time ON billing_transactions (tenant_id, timestamp DESC);

CREATE TABLE IF NOT EXISTS reward_distributions (
    distribution_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id TEXT NOT NULL,
    agent_id TEXT NOT NULL,
    amount BIGINT NOT NULL,
    trust_score FLOAT,
    participation_count INTEGER,
    formula TEXT,
    distributed_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_rewards_agent ON reward_distributions(agent_id);

-- =============================================================================
-- SECTION 10: CONTRACTS & MONITORING
-- =============================================================================

CREATE TABLE IF NOT EXISTS contract_deployments (
    contract_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id TEXT NOT NULL,
    name TEXT NOT NULL,
    version TEXT NOT NULL,
    ebcl_source TEXT,
    activity_id TEXT,
    status TEXT NOT NULL DEFAULT 'ACTIVE',
    deployed_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS contract_executions (
    execution_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    contract_id UUID REFERENCES contract_deployments(contract_id),
    tenant_id TEXT NOT NULL,
    trigger_source TEXT,
    input_payload JSONB,
    output_result JSONB,
    status TEXT,
    error_message TEXT,
    started_at TIMESTAMP NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMP
);

CREATE TABLE IF NOT EXISTS use_case_links (
    link_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id TEXT NOT NULL,
    use_case_key TEXT NOT NULL,
    contract_id UUID REFERENCES contract_deployments(contract_id),
    UNIQUE(tenant_id, use_case_key)
);

CREATE TABLE IF NOT EXISTS metrics_events (
    event_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id TEXT NOT NULL,
    metric_name TEXT NOT NULL,
    value FLOAT NOT NULL,
    tags JSONB,
    timestamp TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_metrics_tenant_time ON metrics_events (tenant_id, timestamp DESC);

CREATE TABLE IF NOT EXISTS alerts (
    alert_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id TEXT NOT NULL,
    alert_type TEXT NOT NULL,
    message TEXT NOT NULL,
    status TEXT DEFAULT 'OPEN',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    resolved_at TIMESTAMP
);

-- =============================================================================
-- SECTION 11: SIMULATION & IMPACT
-- =============================================================================

CREATE TABLE IF NOT EXISTS simulation_scenarios (
    scenario_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id TEXT NOT NULL,
    name TEXT NOT NULL,
    description TEXT,
    parameters JSONB,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP
);

CREATE TABLE IF NOT EXISTS simulation_runs (
    run_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    scenario_id UUID REFERENCES simulation_scenarios(scenario_id),
    tenant_id TEXT NOT NULL,
    status TEXT DEFAULT 'PENDING',
    started_at TIMESTAMP,
    completed_at TIMESTAMP,
    results_summary JSONB
);

CREATE TABLE IF NOT EXISTS impact_templates (
    template_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id TEXT NOT NULL,
    name TEXT NOT NULL,
    base_assumptions JSONB,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS impact_reports (
    report_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id TEXT NOT NULL,
    template_id UUID REFERENCES impact_templates(template_id),
    name TEXT NOT NULL,
    user_assumptions JSONB,
    output_metrics JSONB,
    monte_carlo_results JSONB,
    generated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

-- =============================================================================
-- SECTION 12: ACTIVITIES
-- =============================================================================

CREATE TABLE IF NOT EXISTS activities (
    activity_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id TEXT NOT NULL,
    name TEXT NOT NULL,
    version TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'DRAFT',
    CHECK (status IN ('DRAFT', 'REVIEW', 'APPROVED', 'DEPLOYED', 'ACTIVE', 'SUSPENDED', 'RETIRED')),
    ebcl_source TEXT NOT NULL,
    compiled_artifact JSONB,
    owner TEXT NOT NULL,
    authority TEXT NOT NULL,
    created_by TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    approved_by TEXT,
    approved_at TIMESTAMP,
    deployed_by TEXT,
    deployed_at TIMESTAMP,
    hash TEXT NOT NULL,
    description TEXT,
    tags TEXT[],
    category TEXT,
    UNIQUE(name, version)
);

CREATE INDEX IF NOT EXISTS idx_activities_tenant ON activities(tenant_id);
CREATE INDEX IF NOT EXISTS idx_activities_status ON activities(status);

CREATE TABLE IF NOT EXISTS activity_deployments (
    deployment_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    activity_id UUID NOT NULL REFERENCES activities(activity_id) ON DELETE CASCADE,
    environment TEXT NOT NULL,
    CHECK (environment IN ('DEV', 'STAGING', 'PROD')),
    tenant_id TEXT NOT NULL,
    effective_from TIMESTAMP NOT NULL DEFAULT NOW(),
    effective_until TIMESTAMP,
    deployed_by TEXT NOT NULL,
    deployed_at TIMESTAMP NOT NULL DEFAULT NOW(),
    previous_deployment_id UUID REFERENCES activity_deployments(deployment_id),
    rollback_reason TEXT,
    deployment_notes TEXT,
    UNIQUE(activity_id, environment, tenant_id, effective_from)
);

CREATE TABLE IF NOT EXISTS activity_executions (
    execution_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    activity_id UUID NOT NULL REFERENCES activities(activity_id) ON DELETE CASCADE,
    activity_version TEXT NOT NULL,
    tenant_id TEXT NOT NULL,
    environment TEXT NOT NULL,
    agent_id TEXT NOT NULL,
    started_at TIMESTAMP NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMP,
    status TEXT NOT NULL DEFAULT 'RUNNING',
    CHECK (status IN ('RUNNING', 'COMPLETED', 'FAILED', 'TIMEOUT')),
    outcome TEXT,
    error_message TEXT,
    evidence_id UUID,
    input_data JSONB,
    output_data JSONB,
    duration_ms INTEGER,
    triggered_by TEXT,
    trigger_event TEXT
);

-- =============================================================================
-- SECTION 13: AUTHORITY (APE Engine)
-- =============================================================================

CREATE TABLE IF NOT EXISTS authority_gaps (
    gap_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id UUID NOT NULL,
    document_source TEXT NOT NULL,
    gap_type VARCHAR(50) NOT NULL,
    severity VARCHAR(20) NOT NULL,
    decision_point TEXT NOT NULL,
    current_authority_holder TEXT,
    execution_system TEXT,
    accountability_gap TEXT,
    override_frequency INT,
    time_sensitivity VARCHAR(20),
    a2a_candidacy_score FLOAT,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    status VARCHAR(20) DEFAULT 'PENDING'
);

CREATE TABLE IF NOT EXISTS a2a_use_cases (
    use_case_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id UUID NOT NULL,
    gap_id UUID REFERENCES authority_gaps(gap_id),
    pattern_type VARCHAR(50) NOT NULL,
    title TEXT NOT NULL,
    description TEXT NOT NULL,
    agents_involved JSONB NOT NULL,
    current_problem TEXT NOT NULL,
    ocx_proposal TEXT NOT NULL,
    authority_contract_id UUID,
    estimated_impact JSONB,
    status VARCHAR(20) DEFAULT 'PROPOSED',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS authority_contracts (
    contract_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    use_case_id UUID REFERENCES a2a_use_cases(use_case_id),
    company_id UUID NOT NULL,
    contract_yaml TEXT NOT NULL,
    contract_version VARCHAR(10) NOT NULL DEFAULT '1.0',
    agents_config JSONB NOT NULL,
    decision_point JSONB NOT NULL,
    authority_rules JSONB NOT NULL,
    enforcement JSONB NOT NULL,
    audit_config JSONB NOT NULL,
    status VARCHAR(20) DEFAULT 'DRAFT',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deployed_at TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

-- =============================================================================
-- SECTION 14: EVIDENCE VAULT
-- =============================================================================

CREATE TABLE IF NOT EXISTS evidence (
    evidence_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    activity_id UUID NOT NULL,
    activity_name TEXT NOT NULL,
    activity_version TEXT NOT NULL,
    execution_id UUID NOT NULL,
    agent_id TEXT NOT NULL,
    agent_type TEXT NOT NULL,
    tenant_id TEXT NOT NULL,
    environment TEXT NOT NULL,
    CHECK (environment IN ('DEV', 'STAGING', 'PROD')),
    event_type TEXT NOT NULL,
    event_data JSONB NOT NULL,
    decision TEXT,
    outcome TEXT,
    policy_reference TEXT NOT NULL,
    verified BOOLEAN DEFAULT FALSE,
    verification_status TEXT DEFAULT 'PENDING',
    CHECK (verification_status IN ('PENDING', 'VERIFIED', 'FAILED', 'DISPUTED')),
    verification_errors TEXT[],
    hash TEXT NOT NULL,
    previous_hash TEXT,
    signature TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    verified_at TIMESTAMP,
    tags TEXT[],
    metadata JSONB
);

CREATE INDEX IF NOT EXISTS idx_evidence_activity ON evidence(activity_id);
CREATE INDEX IF NOT EXISTS idx_evidence_tenant ON evidence(tenant_id);
CREATE INDEX IF NOT EXISTS idx_evidence_created_at ON evidence(created_at DESC);

CREATE TABLE IF NOT EXISTS evidence_chain (
    chain_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    evidence_id UUID NOT NULL REFERENCES evidence(evidence_id),
    block_number BIGSERIAL,
    previous_block_hash TEXT,
    merkle_root TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    UNIQUE(block_number)
);

CREATE TABLE IF NOT EXISTS evidence_attestations (
    attestation_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    evidence_id UUID NOT NULL REFERENCES evidence(evidence_id),
    attestor_type TEXT NOT NULL,
    attestor_id TEXT NOT NULL,
    attestation_status TEXT NOT NULL,
    CHECK (attestation_status IN ('APPROVED', 'REJECTED', 'DISPUTED')),
    confidence_score DECIMAL(3,2),
    reasoning TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    signature TEXT,
    proof JSONB
);

-- =============================================================================
-- SECTION 15: POLICIES
-- =============================================================================

CREATE TABLE IF NOT EXISTS policies (
    policy_id UUID NOT NULL,
    version INTEGER NOT NULL,
    tier TEXT NOT NULL,
    trigger_intent TEXT NOT NULL,
    logic JSONB NOT NULL,
    action JSONB NOT NULL,
    confidence FLOAT NOT NULL,
    source_name TEXT NOT NULL,
    roles TEXT[],
    expires_at TIMESTAMP,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    PRIMARY KEY (policy_id, version)
);

CREATE TABLE IF NOT EXISTS policy_audits (
    audit_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    policy_id UUID NOT NULL,
    agent_id UUID,
    trigger_intent TEXT NOT NULL,
    tier TEXT NOT NULL,
    violated BOOLEAN NOT NULL,
    action TEXT NOT NULL,
    data_payload JSONB,
    evaluation_time_ms FLOAT,
    timestamp TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_policy_audits_policy ON policy_audits(policy_id, timestamp DESC);

CREATE TABLE IF NOT EXISTS policy_extractions (
    extraction_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    source_name TEXT NOT NULL,
    document_hash TEXT NOT NULL,
    policies_extracted INTEGER NOT NULL,
    avg_confidence FLOAT,
    model_used TEXT NOT NULL,
    extraction_time_ms FLOAT,
    extracted_at TIMESTAMP NOT NULL DEFAULT NOW()
);

-- =============================================================================
-- SECTION 16: API KEYS
-- =============================================================================

CREATE TABLE IF NOT EXISTS api_keys (
    key_id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    name TEXT NOT NULL,
    key_hash TEXT NOT NULL,
    scopes TEXT[],
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    expires_at TIMESTAMP,
    last_used_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_api_keys_tenant ON api_keys(tenant_id);
CREATE INDEX IF NOT EXISTS idx_api_keys_active ON api_keys(is_active) WHERE is_active = TRUE;

-- =============================================================================
-- SECTION 17: ROW LEVEL SECURITY
-- =============================================================================

ALTER TABLE agents ENABLE ROW LEVEL SECURITY;
ALTER TABLE rules ENABLE ROW LEVEL SECURITY;
ALTER TABLE activities ENABLE ROW LEVEL SECURITY;
ALTER TABLE evidence ENABLE ROW LEVEL SECURITY;
ALTER TABLE governance_ledger ENABLE ROW LEVEL SECURITY;

-- Service role has full access to all tables
CREATE POLICY "Service role has full access to agents"
    ON agents FOR ALL
    USING (auth.role() = 'service_role');

CREATE POLICY "Service role has full access to rules"
    ON rules FOR ALL
    USING (auth.role() = 'service_role');

CREATE POLICY "Service role has full access to activities"
    ON activities FOR ALL
    USING (auth.role() = 'service_role');

CREATE POLICY "Service role has full access to evidence"
    ON evidence FOR ALL
    USING (auth.role() = 'service_role');

CREATE POLICY "Service role has full access to governance_ledger"
    ON governance_ledger FOR ALL
    USING (auth.role() = 'service_role');

-- =============================================================================
-- SECTION 18: SAMPLE DATA
-- =============================================================================

INSERT INTO tenants (tenant_id, tenant_name, organization_name, subscription_tier, admin_email, max_agents, max_activities, max_evidence_per_month) VALUES
('acme-corp', 'Acme Corporation', 'Acme Corp', 'ENTERPRISE', 'admin@acme.com', 100, 1000, 1000000),
('demo-tenant', 'Demo Tenant', 'Demo Organization', 'FREE', 'demo@example.com', 5, 50, 10000)
ON CONFLICT (tenant_id) DO NOTHING;

-- =============================================================================
-- MIGRATION COMPLETE!
-- =============================================================================
-- Total Tables: 43
-- Indexes: 25
-- RLS Policies: 5
-- =============================================================================

SELECT 'OCX Master Database Schema Created Successfully!' as result;
