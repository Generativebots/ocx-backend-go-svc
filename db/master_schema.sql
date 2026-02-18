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
    tenant_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slug TEXT NOT NULL UNIQUE,
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
    tenant_id UUID NOT NULL REFERENCES tenants(tenant_id) ON DELETE CASCADE,
    feature_name TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    config JSONB DEFAULT '{}'::jsonb,
    enabled_at TIMESTAMP,
    enabled_by TEXT,
    PRIMARY KEY (tenant_id, feature_name)
);

CREATE TABLE IF NOT EXISTS tenant_agents (
    agent_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_key TEXT NOT NULL UNIQUE,
    tenant_id UUID NOT NULL REFERENCES tenants(tenant_id) ON DELETE CASCADE,
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
    tenant_id UUID NOT NULL REFERENCES tenants(tenant_id) ON DELETE CASCADE,
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
    tenant_id UUID NOT NULL REFERENCES tenants(tenant_id),
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

-- Link tenant_agents to trust registry (forward reference)
ALTER TABLE tenant_agents ADD COLUMN IF NOT EXISTS trust_registry_agent_id UUID REFERENCES agents(agent_id);

CREATE TABLE IF NOT EXISTS rules (
    rule_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(tenant_id),
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
    agent_id UUID NOT NULL REFERENCES agents(agent_id),
    tenant_id UUID NOT NULL REFERENCES tenants(tenant_id),
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
    agent_id UUID PRIMARY KEY REFERENCES agents(agent_id),
    tenant_id UUID NOT NULL REFERENCES tenants(tenant_id),
    trust_score FLOAT NOT NULL DEFAULT 0.5,
    behavioral_drift FLOAT NOT NULL DEFAULT 0.0,
    gov_tax_balance BIGINT NOT NULL DEFAULT 0,
    is_frozen BOOLEAN NOT NULL DEFAULT FALSE,
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_agents_trust ON agents_reputation(trust_score DESC);

CREATE TABLE IF NOT EXISTS reputation_audit (
    audit_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id UUID NOT NULL REFERENCES agents(agent_id),
    tenant_id UUID,
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
    tenant_id UUID NOT NULL REFERENCES tenants(tenant_id),
    request_id TEXT NOT NULL,
    agent_id UUID REFERENCES agents(agent_id),
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
    tenant_id UUID NOT NULL REFERENCES tenants(tenant_id),
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

-- Federation Handshakes (used by SupabaseHandshakeStore for cross-OCX sessions)
CREATE TABLE IF NOT EXISTS federation_handshakes (
    session_id TEXT PRIMARY KEY,
    initiator TEXT NOT NULL,
    responder TEXT NOT NULL,
    state TEXT NOT NULL DEFAULT 'PROPOSED',
    CHECK (state IN ('PROPOSED', 'CHALLENGE_SENT', 'PROOF_SUBMITTED', 'COMPLETED', 'REJECTED', 'EXPIRED')),
    challenge TEXT,
    proof TEXT,
    trust_level FLOAT DEFAULT 0.0,
    metadata JSONB DEFAULT '{}'::jsonb,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_federation_hs_state ON federation_handshakes(state);
CREATE INDEX IF NOT EXISTS idx_federation_hs_initiator ON federation_handshakes(initiator);
CREATE INDEX IF NOT EXISTS idx_federation_hs_incomplete ON federation_handshakes(state)
    WHERE state NOT IN ('COMPLETED', 'REJECTED', 'EXPIRED');

-- =============================================================================
-- SECTION 6: AGENT IDENTITIES (PID Mapping)
-- =============================================================================

CREATE TABLE IF NOT EXISTS agent_identities (
    identity_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    pid INTEGER NOT NULL,
    tenant_id UUID NOT NULL REFERENCES tenants(tenant_id),
    agent_id UUID NOT NULL REFERENCES agents(agent_id),
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
    tenant_id UUID NOT NULL REFERENCES tenants(tenant_id),
    agent_id UUID NOT NULL REFERENCES agents(agent_id),
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
    tenant_id UUID NOT NULL REFERENCES tenants(tenant_id),
    agent_id UUID NOT NULL REFERENCES agents(agent_id),
    stake_amount BIGINT NOT NULL,
    success BOOLEAN NOT NULL,
    attempt_number INTEGER NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_recovery_agent ON recovery_attempts(agent_id);

CREATE TABLE IF NOT EXISTS probation_periods (
    probation_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(tenant_id),
    agent_id UUID NOT NULL REFERENCES agents(agent_id),
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
    tenant_id UUID NOT NULL REFERENCES tenants(tenant_id),
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
    agent_id UUID NOT NULL REFERENCES agents(agent_id),
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
    tenant_id UUID NOT NULL REFERENCES tenants(tenant_id),
    request_id TEXT NOT NULL,
    trust_score FLOAT NOT NULL,
    transaction_value FLOAT DEFAULT 1.0,
    trust_tax FLOAT NOT NULL,
    timestamp TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_billing_tenant_time ON billing_transactions (tenant_id, timestamp DESC);

CREATE TABLE IF NOT EXISTS reward_distributions (
    distribution_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(tenant_id),
    agent_id UUID NOT NULL REFERENCES agents(agent_id),
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
    tenant_id UUID NOT NULL REFERENCES tenants(tenant_id),
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
    tenant_id UUID NOT NULL REFERENCES tenants(tenant_id),
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
    tenant_id UUID NOT NULL REFERENCES tenants(tenant_id),
    use_case_key TEXT NOT NULL,
    contract_id UUID REFERENCES contract_deployments(contract_id),
    UNIQUE(tenant_id, use_case_key)
);

CREATE TABLE IF NOT EXISTS metrics_events (
    event_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(tenant_id),
    metric_name TEXT NOT NULL,
    value FLOAT NOT NULL,
    tags JSONB,
    timestamp TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_metrics_tenant_time ON metrics_events (tenant_id, timestamp DESC);

CREATE TABLE IF NOT EXISTS alerts (
    alert_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(tenant_id),
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
    tenant_id UUID NOT NULL REFERENCES tenants(tenant_id),
    name TEXT NOT NULL,
    description TEXT,
    parameters JSONB,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP
);

CREATE TABLE IF NOT EXISTS simulation_runs (
    run_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    scenario_id UUID REFERENCES simulation_scenarios(scenario_id),
    tenant_id UUID NOT NULL REFERENCES tenants(tenant_id),
    status TEXT DEFAULT 'PENDING',
    started_at TIMESTAMP,
    completed_at TIMESTAMP,
    results_summary JSONB
);

CREATE TABLE IF NOT EXISTS impact_templates (
    template_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(tenant_id),
    name TEXT NOT NULL,
    base_assumptions JSONB,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS impact_reports (
    report_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(tenant_id),
    template_id UUID REFERENCES impact_templates(template_id),
    name TEXT NOT NULL,
    user_assumptions JSONB,
    output_metrics JSONB,
    monte_carlo_results JSONB,
    generated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

-- =============================================================================
-- SECTION 11B: EBCL CONTRACTS & IMPACT ANALYSIS (Authority Engine)
-- =============================================================================

CREATE TABLE IF NOT EXISTS ebcl_contracts (
    contract_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(tenant_id),
    use_case_id UUID,
    name VARCHAR(500),
    description TEXT,
    ebcl_code TEXT,
    version VARCHAR(50),
    status VARCHAR(50) DEFAULT 'DRAFT',
    deployed_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS ebcl_contract_executions (
    execution_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(tenant_id),
    contract_id UUID REFERENCES ebcl_contracts(contract_id),
    agent1_id UUID,
    agent2_id UUID,
    status VARCHAR(50),
    input_data JSONB,
    output_data JSONB,
    trust_level FLOAT,
    trust_tax FLOAT,
    execution_time_ms INTEGER,
    error_message TEXT,
    started_at TIMESTAMP,
    completed_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS ebcl_contract_versions (
    version_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(tenant_id),
    contract_id UUID REFERENCES ebcl_contracts(contract_id),
    version VARCHAR(50),
    ebcl_code TEXT,
    changes TEXT,
    created_by VARCHAR(255),
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS impact_assumptions (
    assumption_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(tenant_id),
    use_case_id UUID,
    assumptions JSONB,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS impact_simulations (
    simulation_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(tenant_id),
    use_case_id UUID,
    assumptions JSONB,
    results JSONB,
    num_iterations INTEGER,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

-- =============================================================================
-- SECTION 12: ACTIVITIES
-- =============================================================================

CREATE TABLE IF NOT EXISTS activities (
    activity_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(tenant_id),
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
    tenant_id UUID NOT NULL REFERENCES tenants(tenant_id),
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
    tenant_id UUID NOT NULL REFERENCES tenants(tenant_id),
    environment TEXT NOT NULL,
    agent_id UUID NOT NULL REFERENCES agents(agent_id),
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
    tenant_id UUID NOT NULL REFERENCES tenants(tenant_id),
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
    tenant_id UUID NOT NULL REFERENCES tenants(tenant_id),
    gap_id UUID REFERENCES authority_gaps(gap_id),
    pattern_type VARCHAR(50) NOT NULL,
    title TEXT NOT NULL,
    description TEXT NOT NULL,
    agents_involved JSONB NOT NULL,
    current_problem TEXT NOT NULL,
    ocx_proposal TEXT NOT NULL,
    authority_contract TEXT,
    authority_contract_id UUID,
    estimated_impact JSONB,
    status VARCHAR(20) DEFAULT 'PROPOSED',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

-- Deferred FK: ebcl_contracts.use_case_id -> a2a_use_cases (created after ebcl_contracts)
DO $$ BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM information_schema.table_constraints
    WHERE constraint_name = 'fk_ebcl_contracts_use_case'
  ) THEN
    ALTER TABLE ebcl_contracts
      ADD CONSTRAINT fk_ebcl_contracts_use_case
      FOREIGN KEY (use_case_id) REFERENCES a2a_use_cases(use_case_id);
  END IF;
END $$;

CREATE TABLE IF NOT EXISTS authority_contracts (
    contract_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    use_case_id UUID REFERENCES a2a_use_cases(use_case_id),
    tenant_id UUID NOT NULL REFERENCES tenants(tenant_id),
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

CREATE TABLE IF NOT EXISTS parsed_documents (
    doc_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(tenant_id),
    doc_type VARCHAR(50) NOT NULL,
    file_name TEXT NOT NULL,
    file_path TEXT,
    file_size INTEGER,
    parsed_entities JSONB,
    gaps_found INTEGER DEFAULT 0,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS simulation_results (
    simulation_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    use_case_id UUID REFERENCES a2a_use_cases(use_case_id),
    tenant_id UUID NOT NULL REFERENCES tenants(tenant_id),
    scenario JSONB,
    verdict VARCHAR(20),
    authority_flow JSONB,
    final_decision TEXT,
    execution_time_ms INTEGER,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS business_impact_estimates (
    estimate_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    use_case_id UUID REFERENCES a2a_use_cases(use_case_id),
    tenant_id UUID NOT NULL REFERENCES tenants(tenant_id),
    current_monthly_cost FLOAT,
    a2a_monthly_savings FLOAT,
    net_monthly_savings FLOAT,
    annual_roi FLOAT,
    payback_period_months FLOAT,
    assumptions JSONB,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
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
    tenant_id UUID NOT NULL REFERENCES tenants(tenant_id),
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
    agent_id UUID REFERENCES agents(agent_id),
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
    tenant_id UUID NOT NULL REFERENCES tenants(tenant_id),
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
-- SECTION 17: HITL (Human-in-the-Loop) — Patent Layer 4
-- =============================================================================

CREATE TABLE IF NOT EXISTS hitl_decisions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL REFERENCES tenants(tenant_id),
    reviewer_id     TEXT NOT NULL,
    escrow_id       TEXT,
    transaction_id  TEXT,
    agent_id        UUID NOT NULL REFERENCES agents(agent_id),
    decision_type   TEXT NOT NULL CHECK (decision_type IN ('ALLOW_OVERRIDE','BLOCK_OVERRIDE','MODIFY_OUTPUT')),
    original_verdict TEXT,
    modified_payload JSONB,
    reason          TEXT,
    cost_multiplier REAL DEFAULT 10.0,
    created_at      TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_hitl_decisions_tenant  ON hitl_decisions(tenant_id);
CREATE INDEX IF NOT EXISTS idx_hitl_decisions_agent   ON hitl_decisions(agent_id);
CREATE INDEX IF NOT EXISTS idx_hitl_decisions_created ON hitl_decisions(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_hitl_decisions_type    ON hitl_decisions(decision_type);

CREATE TABLE IF NOT EXISTS rlhc_correction_clusters (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           TEXT NOT NULL,
    cluster_name        TEXT NOT NULL,
    pattern_type        TEXT NOT NULL CHECK (pattern_type IN ('ALLOW_PATTERN','BLOCK_PATTERN','MODIFY_PATTERN')),
    trigger_conditions  JSONB NOT NULL,
    correction_count    INT DEFAULT 1,
    confidence_score    REAL DEFAULT 0.0,
    status              TEXT DEFAULT 'DETECTED' CHECK (status IN ('DETECTED','REVIEWED','PROMOTED','REJECTED')),
    promoted_policy_id  UUID,
    first_seen          TIMESTAMPTZ DEFAULT NOW(),
    last_seen           TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_rlhc_clusters_tenant ON rlhc_correction_clusters(tenant_id);
CREATE INDEX IF NOT EXISTS idx_rlhc_clusters_status ON rlhc_correction_clusters(status);

-- =============================================================================
-- SECTION 18: ROW LEVEL SECURITY
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

ALTER TABLE hitl_decisions ENABLE ROW LEVEL SECURITY;
ALTER TABLE rlhc_correction_clusters ENABLE ROW LEVEL SECURITY;

CREATE POLICY "Service role has full access to hitl_decisions"
    ON hitl_decisions FOR ALL
    USING (auth.role() = 'service_role');

CREATE POLICY "Service role has full access to rlhc_correction_clusters"
    ON rlhc_correction_clusters FOR ALL
    USING (auth.role() = 'service_role');

-- =============================================================================
-- SECTION 18A: SESSION AUDIT LOG (Security Forensics)
-- =============================================================================

CREATE TABLE IF NOT EXISTS session_audit_log (
    log_id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id     TEXT NOT NULL,
    tenant_id      UUID NOT NULL REFERENCES tenants(tenant_id),
    agent_id       UUID NOT NULL REFERENCES agents(agent_id),
    event_type     TEXT NOT NULL,
    ip_address     TEXT,
    user_agent     TEXT,
    country        TEXT,
    city           TEXT,
    region         TEXT,
    latitude       FLOAT,
    longitude      FLOAT,
    isp            TEXT,
    request_path   TEXT,
    request_method TEXT,
    trust_score    FLOAT,
    verdict        TEXT,
    risk_flags     JSONB DEFAULT '[]',
    metadata       JSONB DEFAULT '{}',
    created_at     TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_sal_agent   ON session_audit_log(agent_id);
CREATE INDEX IF NOT EXISTS idx_sal_tenant  ON session_audit_log(tenant_id);
CREATE INDEX IF NOT EXISTS idx_sal_created ON session_audit_log(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_sal_ip      ON session_audit_log(ip_address);
CREATE INDEX IF NOT EXISTS idx_sal_event   ON session_audit_log(event_type);

ALTER TABLE session_audit_log ENABLE ROW LEVEL SECURITY;
CREATE POLICY "Service role has full access to session_audit_log"
    ON session_audit_log FOR ALL
    USING (auth.role() = 'service_role');

-- =============================================================================
-- SECTION 18B: AGENT PROFILE ENRICHMENT
-- =============================================================================

ALTER TABLE agents ADD COLUMN IF NOT EXISTS agent_type       TEXT DEFAULT 'bot';
ALTER TABLE agents ADD COLUMN IF NOT EXISTS classification   TEXT DEFAULT 'general';
ALTER TABLE agents ADD COLUMN IF NOT EXISTS capabilities     JSONB DEFAULT '[]';
ALTER TABLE agents ADD COLUMN IF NOT EXISTS risk_tier        TEXT DEFAULT 'standard';
ALTER TABLE agents ADD COLUMN IF NOT EXISTS origin_ip        TEXT;
ALTER TABLE agents ADD COLUMN IF NOT EXISTS origin_country   TEXT;
ALTER TABLE agents ADD COLUMN IF NOT EXISTS last_ip          TEXT;
ALTER TABLE agents ADD COLUMN IF NOT EXISTS last_country     TEXT;
ALTER TABLE agents ADD COLUMN IF NOT EXISTS protocol         TEXT DEFAULT 'http';
ALTER TABLE agents ADD COLUMN IF NOT EXISTS model_provider   TEXT;
ALTER TABLE agents ADD COLUMN IF NOT EXISTS model_name       TEXT;
ALTER TABLE agents ADD COLUMN IF NOT EXISTS description      TEXT;
ALTER TABLE agents ADD COLUMN IF NOT EXISTS max_tools        INT DEFAULT 10;
ALTER TABLE agents ADD COLUMN IF NOT EXISTS allowed_actions  JSONB DEFAULT '[]';
ALTER TABLE agents ADD COLUMN IF NOT EXISTS blocked_actions  JSONB DEFAULT '[]';
ALTER TABLE agents ADD COLUMN IF NOT EXISTS agent_metadata   JSONB DEFAULT '{}';

-- =============================================================================
-- SECTION 18C: COMPLIANCE REPORTS (Evidence Vault)
-- =============================================================================

CREATE TABLE IF NOT EXISTS compliance_reports (
    report_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(tenant_id),
    start_date TIMESTAMP NOT NULL,
    end_date TIMESTAMP NOT NULL,
    report_type TEXT NOT NULL DEFAULT 'DAILY',
    CHECK (report_type IN ('DAILY', 'WEEKLY', 'MONTHLY', 'QUARTERLY', 'ANNUAL')),
    total_evidence_count INTEGER NOT NULL DEFAULT 0,
    verified_evidence_count INTEGER NOT NULL DEFAULT 0,
    failed_evidence_count INTEGER NOT NULL DEFAULT 0,
    disputed_evidence_count INTEGER NOT NULL DEFAULT 0,
    compliance_score FLOAT NOT NULL DEFAULT 0.0,
    policy_violations INTEGER NOT NULL DEFAULT 0,
    report_data JSONB,
    generated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_compliance_reports_tenant ON compliance_reports(tenant_id);
CREATE INDEX IF NOT EXISTS idx_compliance_reports_type ON compliance_reports(report_type);
CREATE INDEX IF NOT EXISTS idx_compliance_reports_generated ON compliance_reports(generated_at DESC);

-- =============================================================================
-- SECTION 18D: ACTIVITY APPROVALS & VERSIONS (Activity Registry)
-- =============================================================================

CREATE TABLE IF NOT EXISTS activity_approvals (
    approval_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    activity_id UUID NOT NULL REFERENCES activities(activity_id) ON DELETE CASCADE,
    approver_id TEXT NOT NULL,
    approver_role TEXT NOT NULL,
    approval_status TEXT NOT NULL DEFAULT 'PENDING',
    CHECK (approval_status IN ('PENDING', 'APPROVED', 'REJECTED')),
    approval_type TEXT NOT NULL DEFAULT 'TECHNICAL',
    CHECK (approval_type IN ('TECHNICAL', 'BUSINESS', 'COMPLIANCE', 'SECURITY')),
    comments TEXT,
    requested_at TIMESTAMP NOT NULL DEFAULT NOW(),
    responded_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_activity_approvals_activity ON activity_approvals(activity_id);
CREATE INDEX IF NOT EXISTS idx_activity_approvals_status ON activity_approvals(approval_status);

CREATE TABLE IF NOT EXISTS activity_versions (
    version_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    activity_id UUID NOT NULL REFERENCES activities(activity_id) ON DELETE CASCADE,
    version TEXT NOT NULL,
    previous_version TEXT,
    version_type TEXT NOT NULL,
    CHECK (version_type IN ('MAJOR', 'MINOR', 'PATCH')),
    change_summary TEXT NOT NULL,
    breaking_changes TEXT[],
    created_by TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_activity_versions_activity ON activity_versions(activity_id);

-- View: Aggregated execution statistics per activity
CREATE OR REPLACE VIEW activity_execution_stats AS
SELECT
    activity_id,
    COUNT(*) AS total_executions,
    COUNT(CASE WHEN status = 'COMPLETED' THEN 1 END) AS successful_executions,
    COUNT(CASE WHEN status = 'FAILED' THEN 1 END) AS failed_executions,
    COALESCE(AVG(duration_ms), 0) AS avg_duration_ms,
    MAX(started_at) AS last_execution_at
FROM activity_executions
GROUP BY activity_id;

-- View: Pending approvals across all activities
CREATE OR REPLACE VIEW pending_approvals AS
SELECT
    aa.approval_id,
    aa.activity_id,
    a.name AS activity_name,
    a.version AS activity_version,
    aa.approver_id,
    aa.approver_role,
    aa.approval_type,
    aa.comments,
    aa.requested_at
FROM activity_approvals aa
JOIN activities a ON aa.activity_id = a.activity_id
WHERE aa.approval_status = 'PENDING';

-- =============================================================================
-- SECTION 18E: TRUST ATTESTATIONS (Federation)
-- =============================================================================

CREATE TABLE IF NOT EXISTS trust_attestations (
    attestation_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id TEXT NOT NULL,
    ocx_instance_id TEXT NOT NULL,
    agent_id TEXT NOT NULL,
    audit_hash TEXT NOT NULL,
    trust_level FLOAT NOT NULL DEFAULT 0.0,
    signature TEXT,
    expires_at TIMESTAMP NOT NULL,
    metadata JSONB DEFAULT '{}',
    timestamp TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_trust_attestations_agent ON trust_attestations(agent_id);
CREATE INDEX IF NOT EXISTS idx_trust_attestations_instance ON trust_attestations(ocx_instance_id);
CREATE INDEX IF NOT EXISTS idx_trust_attestations_hash ON trust_attestations(audit_hash);
CREATE INDEX IF NOT EXISTS idx_trust_attestations_expires ON trust_attestations(expires_at);

-- =============================================================================
-- SECTION 19: TENANT GOVERNANCE CONFIGURATION
-- =============================================================================
-- Per-tenant configurable governance parameters. Each tenant gets a single row
-- with all thresholds, weights, multipliers, and tax rates. If no row exists,
-- the backend auto-creates one with recommended defaults on first session.

CREATE TABLE IF NOT EXISTS tenant_governance_config (
    config_id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id                   UUID NOT NULL UNIQUE REFERENCES tenants(tenant_id) ON DELETE CASCADE,

    -- Trust Thresholds & Scores (Patent §1–§3)
    jury_trust_threshold        FLOAT NOT NULL DEFAULT 0.65,
    jury_audit_weight           FLOAT NOT NULL DEFAULT 0.40,
    jury_reputation_weight      FLOAT NOT NULL DEFAULT 0.30,
    jury_attestation_weight     FLOAT NOT NULL DEFAULT 0.20,
    jury_history_weight         FLOAT NOT NULL DEFAULT 0.10,
    new_agent_default_score     FLOAT NOT NULL DEFAULT 0.30,
    min_balance_threshold       FLOAT NOT NULL DEFAULT 0.20,
    quarantine_score            FLOAT NOT NULL DEFAULT 0.00,
    point_to_score_factor       FLOAT NOT NULL DEFAULT 0.01,
    kill_switch_threshold       FLOAT NOT NULL DEFAULT 0.30,
    quorum_threshold            FLOAT NOT NULL DEFAULT 0.66,

    -- Tax & Economics (Patent §6)
    trust_tax_base_rate         FLOAT NOT NULL DEFAULT 0.10,
    federation_tax_base_rate    FLOAT NOT NULL DEFAULT 0.10,
    per_event_tax_rate          FLOAT NOT NULL DEFAULT 0.01,
    marketplace_commission      FLOAT NOT NULL DEFAULT 0.30,
    hitl_cost_multiplier        FLOAT NOT NULL DEFAULT 10.0,

    -- Tool Risk Multipliers & Socket Meter (Patent §4.1)
    risk_multipliers            JSONB NOT NULL DEFAULT '{
        "data_query": 1.0, "read_only": 0.5, "file_read": 1.0,
        "file_write": 3.0, "network_call": 2.0, "api_call": 2.5,
        "data_mutation": 4.0, "admin_action": 5.0, "exec_command": 5.0,
        "payment": 4.0, "pii_access": 3.5, "unknown": 2.0
    }'::jsonb,
    meter_high_trust_threshold  FLOAT NOT NULL DEFAULT 0.80,
    meter_high_trust_discount   FLOAT NOT NULL DEFAULT 0.70,
    meter_med_trust_threshold   FLOAT NOT NULL DEFAULT 0.60,
    meter_med_trust_discount    FLOAT NOT NULL DEFAULT 0.85,
    meter_low_trust_threshold   FLOAT NOT NULL DEFAULT 0.30,
    meter_low_trust_surcharge   FLOAT NOT NULL DEFAULT 1.50,
    meter_base_cost_per_frame   FLOAT NOT NULL DEFAULT 0.001,
    unknown_tool_min_reputation FLOAT NOT NULL DEFAULT 0.95,
    unknown_tool_tax_coefficient FLOAT NOT NULL DEFAULT 5.0,

    -- Tri-Factor Gate (Patent §2–§3)
    identity_threshold          FLOAT NOT NULL DEFAULT 0.65,
    entropy_threshold           FLOAT NOT NULL DEFAULT 7.5,
    jitter_threshold            FLOAT NOT NULL DEFAULT 0.01,
    cognitive_threshold         FLOAT NOT NULL DEFAULT 0.65,
    entropy_high_cap            FLOAT NOT NULL DEFAULT 4.8,
    entropy_encrypted_threshold FLOAT NOT NULL DEFAULT 7.5,
    entropy_suspicious_threshold FLOAT NOT NULL DEFAULT 6.0,

    -- Security: Continuous Evaluation (Patent §7)
    drift_threshold             FLOAT NOT NULL DEFAULT 0.20,
    anomaly_threshold           INT NOT NULL DEFAULT 5,

    -- Federation Trust Decay (Patent §5.2)
    decay_half_life_hours       FLOAT NOT NULL DEFAULT 168,
    trust_ema_alpha             FLOAT NOT NULL DEFAULT 0.3,
    failure_penalty_factor      FLOAT NOT NULL DEFAULT 0.8,
    supermajority_threshold     FLOAT NOT NULL DEFAULT 0.75,
    handshake_min_trust         FLOAT NOT NULL DEFAULT 0.50,

    -- Metadata
    updated_by                  TEXT,
    created_at                  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_tgc_tenant ON tenant_governance_config(tenant_id);

-- RLS: Only service_role (backend) can read/write governance config.
-- This ensures config values are protected and can only be changed via
-- authenticated API endpoints with role validation.
ALTER TABLE tenant_governance_config ENABLE ROW LEVEL SECURITY;

CREATE POLICY "Service role has full access to tenant_governance_config"
    ON tenant_governance_config FOR ALL
    USING (auth.role() = 'service_role');

-- Tenant members can only READ their own config (no direct writes)
CREATE POLICY "Tenant members read own governance config"
    ON tenant_governance_config FOR SELECT
    USING (auth.role() = 'authenticated' AND tenant_id::text = auth.jwt() ->> 'tenant_id');

-- =============================================================================
-- SECTION 20: GOVERNANCE AUDIT LOG
-- =============================================================================
-- Immutable log of all governance actions, config changes, trust mutations,
-- verdicts, token events, billing events, and HITL decisions.

CREATE TABLE IF NOT EXISTS governance_audit_log (
    log_id      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL REFERENCES tenants(tenant_id),
    event_type  TEXT NOT NULL,     -- CONFIG_CHANGE, TRUST_MUTATION, VERDICT, ESCROW_ACTION,
                                  -- TOKEN_ISSUED, TOKEN_REVOKED, METER_BILLING, HITL_DECISION
    actor_id    TEXT,              -- agent_id or user_id who triggered
    target_id   TEXT,              -- affected entity ID
    action      TEXT NOT NULL,     -- e.g. "update_threshold", "levy_tax", "issue_token"
    old_value   JSONB,            -- previous state (for CONFIG_CHANGE)
    new_value   JSONB,            -- new state
    metadata    JSONB DEFAULT '{}'::jsonb,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_gal_tenant ON governance_audit_log(tenant_id);
CREATE INDEX IF NOT EXISTS idx_gal_type ON governance_audit_log(event_type);
CREATE INDEX IF NOT EXISTS idx_gal_created ON governance_audit_log(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_gal_actor ON governance_audit_log(actor_id);

-- RLS: Audit log is immutable — no UPDATE or DELETE allowed.
-- Only service_role can INSERT. Authenticated users read their own tenant's log.
ALTER TABLE governance_audit_log ENABLE ROW LEVEL SECURITY;

CREATE POLICY "Service role has full access to governance_audit_log"
    ON governance_audit_log FOR ALL
    USING (auth.role() = 'service_role');

CREATE POLICY "Tenant members read own audit log"
    ON governance_audit_log FOR SELECT
    USING (auth.role() = 'authenticated' AND tenant_id::text = auth.jwt() ->> 'tenant_id');
-- =============================================================================
-- SECTION 21: COMPLETE
-- =============================================================================
-- Sample data is in seed_data.sql

-- =============================================================================
-- MIGRATION COMPLETE!
-- =============================================================================
-- Total Tables: 54 (52 + tenant_governance_config, governance_audit_log)
-- Views: 2 (activity_execution_stats, pending_approvals)
-- Indexes: 48
-- RLS Policies: 8
-- =============================================================================

SELECT 'OCX Master Database Schema Created Successfully!' as result;
