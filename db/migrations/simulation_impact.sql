-- Simulation & Impact Schema

-- 1. SIMULATION (Scenarios & Batch Runs)
CREATE TABLE simulation_scenarios (
    scenario_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id TEXT NOT NULL,
    name TEXT NOT NULL,
    description TEXT,
    parameters JSONB,       -- { "agent_count": 100, "malicious_ratio": 0.1, ... }
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP
);

CREATE TABLE simulation_runs (
    run_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    scenario_id UUID REFERENCES simulation_scenarios(scenario_id),
    tenant_id TEXT NOT NULL,
    status TEXT DEFAULT 'PENDING', -- PENDING, RUNNING, COMPLETED, FAILED
    
    started_at TIMESTAMP,
    completed_at TIMESTAMP,
    
    results_summary JSONB      -- { "avg_trust": 0.8, "network_value": 5000 }
);

-- 2. IMPACT ESTIMATION (ROI & Monte Carlo)
CREATE TABLE impact_templates (
    template_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id TEXT NOT NULL,   -- "global" for system templates
    name TEXT NOT NULL,        -- e.g. "FinTech Risk Model"
    base_assumptions JSONB,    -- { "fraud_cost": 5000, "manual_review_cost": 20 }
    
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE impact_reports (
    report_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id TEXT NOT NULL,
    template_id UUID REFERENCES impact_templates(template_id),
    name TEXT NOT NULL,        -- e.g. "Q1 Risk Analysis"
    
    user_assumptions JSONB,    -- Overrides applied to base
    output_metrics JSONB,      -- { "roi": 250%, "risk_reduction": 0.4 }
    monte_carlo_results JSONB, -- Distribution data for charts
    
    generated_at TIMESTAMP NOT NULL DEFAULT NOW()
);
