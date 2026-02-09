#!/bin/bash
# =============================================================================
# OCX Database Migration Script for Supabase
# =============================================================================
# 
# This script creates all required database tables for the OCX project
# 
# Usage: ./migrate.sh
#   - Will prompt for your Supabase database password
#   - Runs all migrations in the correct order
#
# =============================================================================

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

# Supabase connection
SUPABASE_HOST="db.aadfuooiusjogdnndobp.supabase.co"
SUPABASE_PORT="5432"
SUPABASE_DB="postgres"
SUPABASE_USER="postgres"

echo -e "${CYAN}========================================${NC}"
echo -e "${CYAN}OCX Supabase Database Migration${NC}"
echo -e "${CYAN}========================================${NC}"
echo ""
echo "Host: $SUPABASE_HOST"
echo "Database: $SUPABASE_DB"
echo ""

# Prompt for password securely
echo -n "Enter Supabase password: "
read -s SUPABASE_PASSWORD
echo ""

if [ -z "$SUPABASE_PASSWORD" ]; then
    echo -e "${RED}Error: Password cannot be empty${NC}"
    exit 1
fi

# Build connection string
export PGPASSWORD="$SUPABASE_PASSWORD"
CONNECTION_STRING="postgresql://${SUPABASE_USER}@${SUPABASE_HOST}:${SUPABASE_PORT}/${SUPABASE_DB}"

# Test connection
echo -e "${YELLOW}Testing connection...${NC}"
if ! psql "$CONNECTION_STRING" -c "SELECT 1;" > /dev/null 2>&1; then
    echo -e "${RED}Error: Could not connect to database${NC}"
    echo "Please check your password and try again"
    exit 1
fi
echo -e "${GREEN}✓ Connected successfully${NC}"
echo ""

# Function to run SQL
run_sql() {
    local description=$1
    local sql=$2
    
    echo -e "${YELLOW}→ $description${NC}"
    if psql "$CONNECTION_STRING" -c "$sql" > /dev/null 2>&1; then
        echo -e "${GREEN}  ✓ Done${NC}"
    else
        echo -e "${RED}  ✗ Failed (may already exist)${NC}"
    fi
}

# Function to run SQL file
run_sql_file() {
    local description=$1
    local file=$2
    
    echo -e "${YELLOW}→ $description${NC}"
    if [ -f "$file" ]; then
        if psql "$CONNECTION_STRING" -f "$file" > /dev/null 2>&1; then
            echo -e "${GREEN}  ✓ Done${NC}"
        else
            echo -e "${RED}  ✗ Some errors (may be existing objects)${NC}"
        fi
    else
        echo -e "${RED}  ✗ File not found: $file${NC}"
    fi
}

echo -e "${CYAN}========================================${NC}"
echo -e "${CYAN}Phase 1: Enable Extensions${NC}"
echo -e "${CYAN}========================================${NC}"

run_sql "Enable pgcrypto for hashing" "CREATE EXTENSION IF NOT EXISTS pgcrypto;"

echo ""
echo -e "${CYAN}========================================${NC}"
echo -e "${CYAN}Phase 2: Core Tables${NC}"
echo -e "${CYAN}========================================${NC}"

# Tenants (must be first - other tables reference it)
run_sql_file "Creating tenants schema" "../ocx-services-py-svc/ocx-services-py-svc/ocx-orchestrator/tenant_schema.sql"

# Trust Registry
run_sql_file "Creating trust registry schema" "../ocx-services-py-svc/ocx-services-py-svc/trust-registry/schema/supabase_registry.sql"

echo ""
echo -e "${CYAN}========================================${NC}"
echo -e "${CYAN}Phase 3: Go Backend Tables${NC}"
echo -e "${CYAN}========================================${NC}"

# Governance
run_sql_file "Creating governance schema" "db/migrations/governance.sql"

# Billing
run_sql_file "Creating billing schema" "db/migrations/billing.sql"

# Contracts & Monitoring
run_sql_file "Creating contracts schema" "db/migrations/contracts_monitoring.sql"

# Simulation & Impact
run_sql_file "Creating simulation schema" "db/migrations/simulation_impact.sql"

# Reputation (PostgreSQL version - inline since original is Spanner)
run_sql "Creating agents table" "
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
"

run_sql "Creating reputation audit table" "
CREATE TABLE IF NOT EXISTS reputation_audit (
    audit_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id TEXT NOT NULL REFERENCES agents_reputation(agent_id) ON DELETE CASCADE,
    transaction_id TEXT,
    verdict TEXT,
    entropy_delta FLOAT,
    tax_levied BIGINT,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_audit_agent ON reputation_audit(agent_id, created_at DESC);
"

echo ""
echo -e "${CYAN}========================================${NC}"
echo -e "${CYAN}Phase 4: Python Service Tables${NC}"
echo -e "${CYAN}========================================${NC}"

# Activity Registry
run_sql_file "Creating activity registry schema" "../ocx-services-py-svc/ocx-services-py-svc/activity-registry/schema.sql"

# Authority Discovery
run_sql_file "Creating authority schema" "../ocx-services-py-svc/ocx-services-py-svc/authority/schema.sql"

# Evidence Vault
run_sql_file "Creating evidence vault schema" "../ocx-services-py-svc/ocx-services-py-svc/evidence-vault/schema.sql"

echo ""
echo -e "${CYAN}========================================${NC}"
echo -e "${CYAN}Phase 5: Policy Tables (PostgreSQL version)${NC}"
echo -e "${CYAN}========================================${NC}"

# Policy Audits (PostgreSQL version of Spanner schema)
run_sql "Creating policy audits table" "
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
CREATE INDEX IF NOT EXISTS idx_policy_audits_agent ON policy_audits(agent_id, timestamp DESC);
"

run_sql "Creating policies table" "
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
CREATE INDEX IF NOT EXISTS idx_active_policies ON policies(is_active, tier, trigger_intent) WHERE is_active = TRUE;
"

run_sql "Creating policy extractions table" "
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
"

echo ""
echo -e "${CYAN}========================================${NC}"
echo -e "${CYAN}Phase 6: Row Level Security${NC}"
echo -e "${CYAN}========================================${NC}"

run_sql "Enabling RLS on key tables" "
ALTER TABLE agents ENABLE ROW LEVEL SECURITY;
ALTER TABLE rules ENABLE ROW LEVEL SECURITY;
ALTER TABLE activities ENABLE ROW LEVEL SECURITY;
ALTER TABLE evidence ENABLE ROW LEVEL SECURITY;
"

echo ""
echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}Migration Complete!${NC}"
echo -e "${GREEN}========================================${NC}"
echo ""
echo "Tables created:"
echo "  - tenants, tenant_features, tenant_agents, tenant_usage"
echo "  - agents, rules"
echo "  - committee_members, governance_proposals, governance_votes"
echo "  - billing_transactions"
echo "  - contract_deployments, contract_executions, use_case_links"
echo "  - metrics_events, alerts"
echo "  - simulation_scenarios, simulation_runs"
echo "  - impact_templates, impact_reports"
echo "  - agents_reputation, reputation_audit"
echo "  - activities, activity_deployments, activity_executions, etc."
echo "  - authority_gaps, a2a_use_cases, authority_contracts"
echo "  - evidence, evidence_chain, evidence_attestations"
echo "  - policy_audits, policies, policy_extractions"
echo ""
echo -e "${YELLOW}Note: Some tables may have failed if they already existed.${NC}"
echo -e "${YELLOW}This is expected and safe.${NC}"
echo ""

# Clean up
unset PGPASSWORD
