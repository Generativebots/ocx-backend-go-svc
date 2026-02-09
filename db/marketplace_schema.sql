-- =============================================================================
-- OCX MARKETPLACE SCHEMA
-- =============================================================================
--
-- Extends master_schema.sql with marketplace-specific tables.
-- Run AFTER master_schema.sql.
--
-- Tables:
--   marketplace_connectors    — Connector catalog
--   marketplace_templates     — Template catalog
--   marketplace_installations — Tenant-scoped installs
--   marketplace_revenue       — Publisher revenue tracking
-- =============================================================================

-- =============================================================================
-- MARKETPLACE CONNECTORS
-- =============================================================================

CREATE TABLE IF NOT EXISTS marketplace_connectors (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    slug TEXT UNIQUE NOT NULL,
    description TEXT,
    category TEXT NOT NULL,
    CHECK (category IN ('crm', 'erp', 'ai', 'communication', 'devops', 'custom')),
    publisher_id TEXT REFERENCES tenants(tenant_id) ON DELETE SET NULL,
    publisher_name TEXT NOT NULL DEFAULT 'OCX',
    icon_url TEXT,
    config_schema JSONB DEFAULT '{}'::jsonb,
    actions JSONB DEFAULT '[]'::jsonb,
    version TEXT NOT NULL DEFAULT '1.0.0',
    is_verified BOOLEAN DEFAULT false,
    is_public BOOLEAN DEFAULT true,
    is_builtin BOOLEAN DEFAULT false,
    install_count INTEGER DEFAULT 0,
    rating DECIMAL(3,2) DEFAULT 0.00,
    pricing_tier TEXT NOT NULL DEFAULT 'free',
    CHECK (pricing_tier IN ('free', 'starter', 'pro', 'enterprise')),
    monthly_credits INTEGER DEFAULT 0,
    signature_hash TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_mkt_conn_category ON marketplace_connectors(category);
CREATE INDEX IF NOT EXISTS idx_mkt_conn_publisher ON marketplace_connectors(publisher_id);
CREATE INDEX IF NOT EXISTS idx_mkt_conn_slug ON marketplace_connectors(slug);
CREATE INDEX IF NOT EXISTS idx_mkt_conn_public ON marketplace_connectors(is_public) WHERE is_public = TRUE;

-- =============================================================================
-- MARKETPLACE TEMPLATES
-- =============================================================================

CREATE TABLE IF NOT EXISTS marketplace_templates (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    slug TEXT UNIQUE NOT NULL,
    description TEXT,
    category TEXT NOT NULL,
    CHECK (category IN ('governance', 'compliance', 'workflow', 'policy', 'security', 'audit', 'healthcare')),
    publisher_id TEXT REFERENCES tenants(tenant_id) ON DELETE SET NULL,
    publisher_name TEXT NOT NULL DEFAULT 'OCX',
    ebcl_definition JSONB DEFAULT '{}'::jsonb,
    dependencies TEXT[] DEFAULT '{}',
    industry_tags TEXT[] DEFAULT '{}',
    version TEXT NOT NULL DEFAULT '1.0.0',
    step_count INTEGER DEFAULT 0,
    is_verified BOOLEAN DEFAULT false,
    is_public BOOLEAN DEFAULT true,
    is_builtin BOOLEAN DEFAULT false,
    install_count INTEGER DEFAULT 0,
    rating DECIMAL(3,2) DEFAULT 0.00,
    pricing_tier TEXT NOT NULL DEFAULT 'free',
    CHECK (pricing_tier IN ('free', 'starter', 'pro', 'enterprise')),
    one_time_credits INTEGER DEFAULT 0,
    signature_hash TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_mkt_tmpl_category ON marketplace_templates(category);
CREATE INDEX IF NOT EXISTS idx_mkt_tmpl_publisher ON marketplace_templates(publisher_id);
CREATE INDEX IF NOT EXISTS idx_mkt_tmpl_slug ON marketplace_templates(slug);
CREATE INDEX IF NOT EXISTS idx_mkt_tmpl_public ON marketplace_templates(is_public) WHERE is_public = TRUE;

-- =============================================================================
-- MARKETPLACE INSTALLATIONS (Tenant-Scoped)
-- =============================================================================

CREATE TABLE IF NOT EXISTS marketplace_installations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id TEXT NOT NULL REFERENCES tenants(tenant_id) ON DELETE CASCADE,
    item_type TEXT NOT NULL,
    CHECK (item_type IN ('connector', 'template')),
    item_id UUID NOT NULL,
    item_name TEXT NOT NULL,
    config JSONB DEFAULT '{}'::jsonb,
    status TEXT NOT NULL DEFAULT 'active',
    CHECK (status IN ('active', 'paused', 'uninstalled', 'error')),
    installed_by TEXT,
    installed_at TIMESTAMPTZ DEFAULT NOW(),
    last_used_at TIMESTAMPTZ,
    uninstalled_at TIMESTAMPTZ,
    -- Link to activity when template is deployed
    activity_id UUID REFERENCES activities(activity_id) ON DELETE SET NULL,
    UNIQUE(tenant_id, item_type, item_id)
);

CREATE INDEX IF NOT EXISTS idx_mkt_inst_tenant ON marketplace_installations(tenant_id);
CREATE INDEX IF NOT EXISTS idx_mkt_inst_item ON marketplace_installations(item_type, item_id);
CREATE INDEX IF NOT EXISTS idx_mkt_inst_status ON marketplace_installations(status) WHERE status = 'active';

-- =============================================================================
-- MARKETPLACE REVENUE (Publisher Payouts)
-- =============================================================================

CREATE TABLE IF NOT EXISTS marketplace_revenue (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    publisher_id TEXT NOT NULL REFERENCES tenants(tenant_id) ON DELETE CASCADE,
    item_type TEXT NOT NULL,
    CHECK (item_type IN ('connector', 'template')),
    item_id UUID NOT NULL,
    item_name TEXT NOT NULL,
    transaction_type TEXT NOT NULL,
    CHECK (transaction_type IN ('install', 'subscription', 'usage')),
    buyer_tenant_id TEXT NOT NULL REFERENCES tenants(tenant_id) ON DELETE CASCADE,
    gross_credits INTEGER NOT NULL DEFAULT 0,
    ocx_commission INTEGER NOT NULL DEFAULT 0,
    publisher_payout INTEGER NOT NULL DEFAULT 0,
    commission_rate DECIMAL(3,2) NOT NULL DEFAULT 0.30,
    period_start TIMESTAMPTZ,
    period_end TIMESTAMPTZ,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_mkt_rev_publisher ON marketplace_revenue(publisher_id);
CREATE INDEX IF NOT EXISTS idx_mkt_rev_item ON marketplace_revenue(item_type, item_id);
CREATE INDEX IF NOT EXISTS idx_mkt_rev_buyer ON marketplace_revenue(buyer_tenant_id);
CREATE INDEX IF NOT EXISTS idx_mkt_rev_period ON marketplace_revenue(period_start, period_end);

-- =============================================================================
-- ROW LEVEL SECURITY
-- =============================================================================

ALTER TABLE marketplace_installations ENABLE ROW LEVEL SECURITY;
ALTER TABLE marketplace_revenue ENABLE ROW LEVEL SECURITY;

CREATE POLICY "Tenants can only see their own installations"
    ON marketplace_installations FOR SELECT
    USING (tenant_id = current_setting('app.tenant_id', true));

CREATE POLICY "Tenants can only see their own revenue"
    ON marketplace_revenue FOR SELECT
    USING (publisher_id = current_setting('app.tenant_id', true));

CREATE POLICY "Service role has full marketplace installation access"
    ON marketplace_installations FOR ALL
    USING (auth.role() = 'service_role');

CREATE POLICY "Service role has full marketplace revenue access"
    ON marketplace_revenue FOR ALL
    USING (auth.role() = 'service_role');

-- =============================================================================
-- SEED BUILT-IN CONNECTORS (Phase D)
-- =============================================================================

INSERT INTO marketplace_connectors (name, slug, description, category, pricing_tier, monthly_credits, is_verified, is_builtin, version, rating, install_count, config_schema, actions) VALUES
('Salesforce', 'salesforce', 'Enterprise CRM integration with full SOQL support, real-time CDC streaming, and OAuth 2.0 PKCE authentication.', 'crm', 'pro', 500, true, true, '2.1.0', 4.80, 12450,
 '{"instance_url":{"type":"url","label":"Salesforce Instance URL","required":true},"client_id":{"type":"string","label":"Connected App Client ID","required":true},"client_secret":{"type":"secret","label":"Connected App Secret","required":true},"api_version":{"type":"string","label":"API Version","required":false}}'::jsonb,
 '[{"name":"query_records","description":"Execute SOQL queries","risk_level":"low"},{"name":"create_record","description":"Create new Salesforce records","risk_level":"medium"},{"name":"update_record","description":"Update existing records","risk_level":"medium"},{"name":"delete_record","description":"Delete records permanently","risk_level":"high"},{"name":"bulk_upsert","description":"Bulk upsert via Bulk API 2.0","risk_level":"high"}]'::jsonb),

('SAP ERP', 'sap-erp', 'Direct SAP integration via RFC/BAPI calls with support for S/4HANA and ECC systems.', 'erp', 'enterprise', 1000, true, true, '1.5.0', 4.60, 3200,
 '{"sap_host":{"type":"url","label":"SAP Application Server","required":true},"system_number":{"type":"string","label":"System Number","required":true},"client":{"type":"string","label":"Client ID","required":true},"username":{"type":"string","label":"RFC Username","required":true},"password":{"type":"secret","label":"RFC Password","required":true}}'::jsonb,
 '[{"name":"call_bapi","description":"Execute SAP BAPI function","risk_level":"medium"},{"name":"read_table","description":"Read SAP table data via RFC_READ_TABLE","risk_level":"low"},{"name":"post_document","description":"Post financial documents","risk_level":"high"},{"name":"create_material","description":"Create material master records","risk_level":"high"}]'::jsonb),

('HTTP/REST (Generic)', 'http-rest', 'Configurable HTTP connector for any REST API. Supports OAuth, API keys, and custom headers.', 'custom', 'free', 0, true, true, '3.0.0', 4.90, 28500,
 '{"base_url":{"type":"url","label":"Base URL","required":true},"auth_type":{"type":"string","label":"Auth Type (none/apikey/oauth2/bearer)","required":true},"api_key":{"type":"secret","label":"API Key","required":false},"headers":{"type":"json","label":"Custom Headers","required":false}}'::jsonb,
 '[{"name":"get","description":"HTTP GET request","risk_level":"low"},{"name":"post","description":"HTTP POST request","risk_level":"medium"},{"name":"put","description":"HTTP PUT request","risk_level":"medium"},{"name":"delete","description":"HTTP DELETE request","risk_level":"high"},{"name":"patch","description":"HTTP PATCH request","risk_level":"medium"}]'::jsonb),

('ServiceNow', 'servicenow', 'ITSM integration with incident, change, and CMDB management via Table API.', 'devops', 'pro', 300, true, true, '2.0.0', 4.50, 5600,
 '{"instance_url":{"type":"url","label":"ServiceNow Instance","required":true},"username":{"type":"string","label":"Integration User","required":true},"password":{"type":"secret","label":"Password","required":true}}'::jsonb,
 '[{"name":"create_incident","description":"Create new incident","risk_level":"medium"},{"name":"update_incident","description":"Update incident state","risk_level":"medium"},{"name":"query_cmdb","description":"Query CMDB records","risk_level":"low"},{"name":"create_change","description":"Create change request","risk_level":"high"}]'::jsonb),

('Slack', 'slack', 'Team messaging integration with message posting, channel management, and event subscriptions.', 'communication', 'free', 0, true, true, '2.3.0', 4.70, 18700,
 '{"bot_token":{"type":"secret","label":"Bot Token (xoxb-)","required":true},"signing_secret":{"type":"secret","label":"Signing Secret","required":true},"app_id":{"type":"string","label":"App ID","required":false}}'::jsonb,
 '[{"name":"post_message","description":"Send message to channel","risk_level":"low"},{"name":"create_channel","description":"Create new channel","risk_level":"medium"},{"name":"list_users","description":"List workspace members","risk_level":"low"},{"name":"upload_file","description":"Upload file to channel","risk_level":"medium"}]'::jsonb),

('Microsoft Teams', 'ms-teams', 'Microsoft Teams integration via Graph API for messaging, meetings, and channel management.', 'communication', 'free', 0, true, true, '1.8.0', 4.40, 9300,
 '{"tenant_id":{"type":"string","label":"Azure AD Tenant ID","required":true},"client_id":{"type":"string","label":"App Registration Client ID","required":true},"client_secret":{"type":"secret","label":"Client Secret","required":true}}'::jsonb,
 '[{"name":"send_message","description":"Send chat or channel message","risk_level":"low"},{"name":"create_team","description":"Create new Team","risk_level":"high"},{"name":"list_channels","description":"List team channels","risk_level":"low"},{"name":"schedule_meeting","description":"Schedule Teams meeting","risk_level":"medium"}]'::jsonb),

('Jira', 'jira', 'Atlassian Jira integration for issue tracking, project management, and sprint planning.', 'devops', 'starter', 100, true, true, '2.2.0', 4.60, 15200,
 '{"jira_url":{"type":"url","label":"Jira Cloud URL","required":true},"email":{"type":"string","label":"User Email","required":true},"api_token":{"type":"secret","label":"API Token","required":true}}'::jsonb,
 '[{"name":"create_issue","description":"Create Jira issue","risk_level":"medium"},{"name":"update_issue","description":"Update issue fields","risk_level":"medium"},{"name":"search_issues","description":"JQL search","risk_level":"low"},{"name":"transition_issue","description":"Move issue through workflow","risk_level":"medium"},{"name":"delete_issue","description":"Delete issue permanently","risk_level":"high"}]'::jsonb)

ON CONFLICT (slug) DO NOTHING;

-- =============================================================================
-- SEED BUILT-IN TEMPLATES (Phase E)
-- =============================================================================

INSERT INTO marketplace_templates (name, slug, description, category, pricing_tier, one_time_credits, is_verified, is_builtin, version, rating, install_count, step_count, industry_tags, dependencies, ebcl_definition) VALUES
('GDPR Data Subject Access Request', 'gdpr-dsar', 'Automated DSAR workflow compliant with GDPR Articles 15-20. Handles data discovery, collection, review, redaction, and secure delivery within the 30-day deadline.', 'compliance', 'pro', 200, true, true, '2.0.0', 4.90, 3400, 8,
 ARRAY['Financial Services','Healthcare','Technology','Retail'],
 ARRAY['http-rest'],
 '{"steps":[{"name":"receive_request","type":"trigger","description":"Intake DSAR via portal or email"},{"name":"verify_identity","type":"action","description":"Verify requester identity against records"},{"name":"data_discovery","type":"action","description":"Scan all connected systems for PII"},{"name":"collect_data","type":"action","description":"Aggregate data from all sources"},{"name":"legal_review","type":"approval","description":"Legal team reviews for exemptions","risk":"medium"},{"name":"redact_sensitive","type":"action","description":"Auto-redact third-party PII"},{"name":"package_response","type":"action","description":"Generate portable data package"},{"name":"deliver_response","type":"action","description":"Secure delivery to requester"}]}'::jsonb),

('SOC2 Evidence Collection', 'soc2-evidence', 'Monthly SOC2 Type II evidence gathering automation. Collects access reviews, change logs, and security configurations.', 'audit', 'pro', 150, true, true, '1.5.0', 4.70, 2100, 6,
 ARRAY['Technology','Financial Services','SaaS'],
 ARRAY['jira','slack'],
 '{"steps":[{"name":"schedule_collection","type":"cron","description":"Monthly trigger on 1st of month"},{"name":"gather_access_logs","type":"action","description":"Pull access control evidence"},{"name":"collect_change_records","type":"action","description":"Gather change management tickets"},{"name":"security_config_snapshot","type":"action","description":"Snapshot firewall, IAM, encryption configs"},{"name":"generate_report","type":"action","description":"Compile evidence into audit report"},{"name":"auditor_review","type":"approval","description":"Internal review before external audit","risk":"medium"}]}'::jsonb),

('HIPAA Access Review', 'hipaa-access-review', 'Quarterly access reviews for HIPAA compliance. Reviews PHI access, privilege escalations, and generates remediation tasks.', 'healthcare', 'enterprise', 300, true, true, '1.2.0', 4.50, 890, 7,
 ARRAY['Healthcare','Insurance','Pharmaceutical'],
 ARRAY['http-rest','servicenow'],
 '{"steps":[{"name":"quarterly_trigger","type":"cron","description":"Trigger quarterly review cycle"},{"name":"enumerate_phi_access","type":"action","description":"List all users with PHI access"},{"name":"flag_anomalies","type":"action","description":"Detect over-privileged accounts"},{"name":"manager_review","type":"approval","description":"Managers review team access","risk":"medium"},{"name":"revoke_excess","type":"action","description":"Auto-revoke flagged privileges","risk":"high"},{"name":"generate_attestation","type":"action","description":"Create signed attestation documents"},{"name":"compliance_signoff","type":"manual","description":"HIPAA officer final signoff"}]}'::jsonb),

('Financial Approval Chain', 'financial-approval', 'Multi-level spend approval workflow with dynamic delegation, budget checking, and audit trail.', 'governance', 'starter', 100, true, true, '2.1.0', 4.80, 4200, 6,
 ARRAY['Financial Services','Manufacturing','Retail','Enterprise'],
 ARRAY['sap-erp','slack'],
 '{"steps":[{"name":"submit_request","type":"trigger","description":"Employee submits spend request"},{"name":"budget_check","type":"action","description":"Verify against department budget"},{"name":"manager_approval","type":"approval","description":"Direct manager reviews","risk":"medium"},{"name":"finance_approval","type":"approval","description":"Finance team reviews >$10k","risk":"high"},{"name":"exec_approval","type":"approval","description":"VP approval for >$50k","risk":"high"},{"name":"process_payment","type":"action","description":"Release payment via ERP"}]}'::jsonb),

('Incident Response Playbook', 'incident-response', 'Auto-escalation security incident playbook with severity classification, containment, and post-mortem workflows.', 'security', 'pro', 250, true, true, '1.8.0', 4.90, 5100, 8,
 ARRAY['Technology','Financial Services','Healthcare','Government'],
 ARRAY['slack','jira'],
 '{"steps":[{"name":"detect_incident","type":"trigger","description":"Alert from monitoring or manual report"},{"name":"classify_severity","type":"action","description":"Auto-classify P1-P4 severity"},{"name":"notify_responders","type":"action","description":"Page on-call via Slack/PagerDuty"},{"name":"contain_threat","type":"action","description":"Execute containment playbook","risk":"high"},{"name":"investigate_root_cause","type":"manual","description":"Security team investigation"},{"name":"remediate","type":"action","description":"Apply fix and verify","risk":"high"},{"name":"stakeholder_update","type":"action","description":"Update leadership and affected parties"},{"name":"post_mortem","type":"manual","description":"Blameless post-mortem and lessons learned"}]}'::jsonb)

ON CONFLICT (slug) DO NOTHING;

-- =============================================================================
-- MARKETPLACE SCHEMA COMPLETE
-- =============================================================================

SELECT 'OCX Marketplace Schema Created Successfully!' as result;
