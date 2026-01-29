-- Authority Contracts & Monitoring Schema

-- 1. CONTRACTS (Authority Execution)
CREATE TABLE contract_deployments (
    contract_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id TEXT NOT NULL,
    name TEXT NOT NULL,
    version TEXT NOT NULL,
    ebcl_source TEXT,          -- The EBCL code (or link to activity registry)
    activity_id TEXT,          -- Link to Activity Registry
    
    status TEXT NOT NULL DEFAULT 'ACTIVE', -- ACTIVE, DEPRECATED, ARCHIVED
    deployed_at TIMESTAMP NOT NULL DEFAULT NOW(),
    
    INDEX idx_contracts_tenant (tenant_id)
);

CREATE TABLE contract_executions (
    execution_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    contract_id UUID REFERENCES contract_deployments(contract_id),
    tenant_id TEXT NOT NULL,
    trigger_source TEXT,       -- e.g. "agent_interaction", "scheduled"
    input_payload JSONB,
    output_result JSONB,
    status TEXT,               -- SUCCESS, FAILURE
    error_message TEXT,
    
    started_at TIMESTAMP NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMP
);

CREATE TABLE use_case_links (
    link_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id TEXT NOT NULL,
    use_case_key TEXT NOT NULL, -- e.g. "finance.audit.daily"
    contract_id UUID REFERENCES contract_deployments(contract_id),
    
    UNIQUE(tenant_id, use_case_key)
);

-- 2. MONITORING (Real-Time Analytics)
CREATE TABLE metrics_events (
    event_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id TEXT NOT NULL,
    metric_name TEXT NOT NULL, -- "latency", "entropy", "throughput"
    value FLOAT NOT NULL,
    tags JSONB,                -- {"agent_id": "XY", "region": "us-east"}
    
    timestamp TIMESTAMP NOT NULL DEFAULT NOW()
);

-- Best practice: TimescaleDB for metrics, but standard Postgres for MVP
CREATE INDEX idx_metrics_tenant_time ON metrics_events (tenant_id, timestamp DESC);

CREATE TABLE alerts (
    alert_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id TEXT NOT NULL,
    alert_type TEXT NOT NULL,  -- "LATENCY_SPIKE", "ENTROPY_HIGH"
    message TEXT NOT NULL,
    status TEXT DEFAULT 'OPEN', -- OPEN, ACKNOWLEDGED, RESOLVED
    
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    resolved_at TIMESTAMP
);
