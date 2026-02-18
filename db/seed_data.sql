-- =============================================================================
-- OCX SEED DATA — Full Test Coverage (Happy, Negative, Edge Cases, Human Actions)
-- =============================================================================
-- Safe to re-run: uses ON CONFLICT DO NOTHING everywhere.
-- Test matrix:
--   ✅ Happy path    ❌ Negative/blocked    🧑 Human actions
--   ⚠️ Edge cases    🔄 Recovery flows      📊 Boundary values
-- =============================================================================

-- ─────────────────────────────────────────────────────────────────────────────
-- §1 TENANTS — all tiers + statuses
-- ─────────────────────────────────────────────────────────────────────────────
INSERT INTO tenants (tenant_id,slug,tenant_name,organization_name,subscription_tier,status,created_at,updated_at,trial_ends_at,admin_email,admin_name,max_agents,max_activities,max_evidence_per_month,settings) VALUES
('00000000-0000-0000-0000-000000000001','acme-corp','Acme Corporation','Acme Corp','ENTERPRISE','ACTIVE',NOW()-INTERVAL '180 days',NOW(),NULL,'admin@acme.com','Alice Chen',100,1000,1000000,'{"mfa_enabled":true}'::jsonb),
('00000000-0000-0000-0000-000000000002','demo-tenant','Demo Tenant','Demo Org','FREE','ACTIVE',NOW()-INTERVAL '90 days',NOW(),NULL,'demo@example.com','Bob Demo',5,50,10000,'{}'::jsonb),
('00000000-0000-0000-0000-000000000004','startup-io','Startup IO','Startup Inc','STARTER','TRIAL',NOW()-INTERVAL '10 days',NOW(),NOW()+INTERVAL '20 days','cto@startup.io','Carol Wu',10,100,50000,'{}'::jsonb),
('00000000-0000-0000-0000-000000000003','bigbank-co','BigBank Corp','BigBank','PROFESSIONAL','ACTIVE',NOW()-INTERVAL '365 days',NOW(),NULL,'sec@bigbank.com','Dan Kim',50,500,500000,'{"region":"eu-west"}'::jsonb),
('00000000-0000-0000-0000-000000000005','suspended-co','Suspended Corp','SusCo','STARTER','SUSPENDED',NOW()-INTERVAL '60 days',NOW(),NULL,'admin@sus.com','Eve Nope',5,50,10000,'{}'::jsonb),
('00000000-0000-0000-0000-000000000006','cancelled-co','Cancelled Corp','CanCo','FREE','CANCELLED',NOW()-INTERVAL '120 days',NOW(),NULL,'admin@can.com','Frank Zero',5,50,10000,'{}'::jsonb)
ON CONFLICT (tenant_id) DO NOTHING;

INSERT INTO tenant_features (tenant_id,feature_name,enabled,config,enabled_at,enabled_by) VALUES
('00000000-0000-0000-0000-000000000001','ghost_state',true,'{"max_ghosts":5}'::jsonb,NOW()-INTERVAL '30 days','admin@acme.com'),
('00000000-0000-0000-0000-000000000001','hitl_governance',true,'{"auto_approve_threshold":0.9}'::jsonb,NOW()-INTERVAL '15 days','admin@acme.com'),
('00000000-0000-0000-0000-000000000001','federation',true,'{"max_peers":20}'::jsonb,NOW()-INTERVAL '10 days','admin@acme.com'),
('00000000-0000-0000-0000-000000000001','ebpf_intercept',true,'{}'::jsonb,NOW()-INTERVAL '7 days','admin@acme.com'),
('00000000-0000-0000-0000-000000000002','ghost_state',false,'{}'::jsonb,NULL,NULL),
('00000000-0000-0000-0000-000000000002','federation',false,'{}'::jsonb,NULL,NULL),
('00000000-0000-0000-0000-000000000004','hitl_governance',true,'{"auto_approve_threshold":0.85}'::jsonb,NOW()-INTERVAL '5 days','cto@startup.io'),
('00000000-0000-0000-0000-000000000003','ghost_state',true,'{}'::jsonb,NOW()-INTERVAL '200 days','sec@bigbank.com'),
('00000000-0000-0000-0000-000000000003','hitl_governance',true,'{}'::jsonb,NOW()-INTERVAL '200 days','sec@bigbank.com'),
('00000000-0000-0000-0000-000000000003','federation',true,'{}'::jsonb,NOW()-INTERVAL '100 days','sec@bigbank.com'),
('00000000-0000-0000-0000-000000000005','ghost_state',false,'{}'::jsonb,NULL,NULL)
ON CONFLICT (tenant_id,feature_name) DO NOTHING;

INSERT INTO tenant_agents (agent_key,tenant_id,agent_name,agent_type,status,last_active_at,config) VALUES
('ta-alpha-7','00000000-0000-0000-0000-000000000001','Alpha-7 Finance Bot','AI','ACTIVE',NOW()-INTERVAL '1 hour','{"model":"gpt-4o"}'::jsonb),
('ta-beta-3','00000000-0000-0000-0000-000000000001','Beta-3 Procurement','AI','SUSPENDED',NOW()-INTERVAL '2 days','{"model":"claude-3.5-sonnet"}'::jsonb),
('ta-gamma-1','00000000-0000-0000-0000-000000000001','Gamma-1 Ops Monitor','SYSTEM','ACTIVE',NOW()-INTERVAL '5 min','{"model":"gemini-2.0-flash"}'::jsonb),
('ta-sarah','00000000-0000-0000-0000-000000000001','Sarah (Security)','HUMAN','ACTIVE',NOW()-INTERVAL '30 min','{}'::jsonb),
('ta-delta-9','00000000-0000-0000-0000-000000000002','Delta-9 General','AI','ACTIVE',NOW()-INTERVAL '3 hours','{"model":"gpt-4o-mini"}'::jsonb),
('ta-epsilon','00000000-0000-0000-0000-000000000003','Epsilon Compliance','AI','ACTIVE',NOW()-INTERVAL '10 min','{"model":"gpt-4o"}'::jsonb),
('ta-zeta','00000000-0000-0000-0000-000000000003','Zeta Fraud Detector','SYSTEM','ACTIVE',NOW()-INTERVAL '1 min','{"model":"custom-fraud-v3"}'::jsonb),
('ta-bob','00000000-0000-0000-0000-000000000003','Bob (Analyst)','HUMAN','ACTIVE',NOW()-INTERVAL '2 hours','{}'::jsonb),
('ta-inactive','00000000-0000-0000-0000-000000000005','Dead Agent','AI','INACTIVE',NOW()-INTERVAL '45 days','{}'::jsonb)
ON CONFLICT (agent_key) DO NOTHING;

INSERT INTO tenant_usage (tenant_id,period_start,period_end,activities_executed,evidence_collected,documents_processed,api_calls,storage_bytes,estimated_cost) VALUES
('00000000-0000-0000-0000-000000000001',NOW()-INTERVAL '30 days',NOW(),4520,12800,340,185000,2147483648,2450.00),
('00000000-0000-0000-0000-000000000001',NOW()-INTERVAL '60 days',NOW()-INTERVAL '30 days',3800,10200,280,162000,1610612736,2100.00),
('00000000-0000-0000-0000-000000000002',NOW()-INTERVAL '30 days',NOW(),890,1500,42,12000,52428800,45.00),
('00000000-0000-0000-0000-000000000004',NOW()-INTERVAL '30 days',NOW(),1650,4800,120,48000,536870912,380.00),
('00000000-0000-0000-0000-000000000003',NOW()-INTERVAL '30 days',NOW(),8200,25000,890,420000,4294967296,5200.00),
('00000000-0000-0000-0000-000000000005',NOW()-INTERVAL '60 days',NOW()-INTERVAL '45 days',50,80,5,200,1048576,5.00),
('00000000-0000-0000-0000-000000000006',NOW()-INTERVAL '120 days',NOW()-INTERVAL '90 days',0,0,0,0,0,0.00);

-- ─────────────────────────────────────────────────────────────────────────────
-- §2 AGENTS — trust spectrum from 0.0 to 1.0, frozen, blacklisted, all types
-- ─────────────────────────────────────────────────────────────────────────────
INSERT INTO agents (agent_id,tenant_id,name,provider,tier,auth_scope,status,organization,trust_score,behavioral_drift,gov_tax_balance,is_frozen,reputation_score,total_interactions,successful_interactions,failed_interactions,blacklisted,agent_type,classification,capabilities,risk_tier,origin_ip,origin_country,last_ip,last_country,protocol,model_provider,model_name,description,max_tools,allowed_actions,blocked_actions,agent_metadata) VALUES
('11111111-1111-1111-1111-111111111111','00000000-0000-0000-0000-000000000001','agent-alpha-7','openai','ENTERPRISE','full','Active','Acme Corp',0.87,0.03,12500,false,0.91,4520,4380,140,false,'bot','finance','["fund_transfer","data_read","report_gen"]'::jsonb,'elevated','203.45.167.89','Singapore','203.45.167.89','Singapore','http','openai','gpt-4o','Financial operations agent',15,'["gpu_purchase","fund_transfer"]'::jsonb,'["delete_account"]'::jsonb,'{"team":"finance-ops"}'::jsonb),
('22222222-2222-2222-2222-222222222222','00000000-0000-0000-0000-000000000001','agent-beta-3','anthropic','PROFESSIONAL','limited','Active','Acme Corp',0.42,0.18,800,true,0.38,1200,890,310,false,'bot','procurement','["api_call","data_read"]'::jsonb,'critical','45.33.32.156','United States','45.33.32.156','United States','grpc','anthropic','claude-3.5-sonnet','Procurement — FROZEN',8,'[]'::jsonb,'["fund_transfer"]'::jsonb,'{"team":"procurement"}'::jsonb),
('33333333-3333-3333-3333-333333333333','00000000-0000-0000-0000-000000000001','agent-gamma-1','google','ENTERPRISE','full','Active','Acme Corp',0.95,0.01,45000,false,0.97,12800,12750,50,false,'service','ops','["monitoring","alerting","data_read","health_check"]'::jsonb,'low','10.0.0.5','LOCAL','10.0.0.5','LOCAL','grpc','google','gemini-2.0-flash','Ops monitoring service',20,'[]'::jsonb,'[]'::jsonb,'{"uptime_sla":"99.99"}'::jsonb),
('44444444-4444-4444-4444-444444444444','00000000-0000-0000-0000-000000000001','operator-sarah',NULL,'ENTERPRISE','admin','Active','Acme Corp',0.99,0.0,0,false,1.0,320,320,0,false,'human','security','["admin","audit","override"]'::jsonb,'low','104.28.5.44','Germany','104.28.5.44','Germany','http',NULL,NULL,'Senior security operator',50,'[]'::jsonb,'[]'::jsonb,'{"clearance":"TOP_SECRET"}'::jsonb),
('55555555-5555-5555-5555-555555555555','00000000-0000-0000-0000-000000000002','agent-delta-9','openai','STARTER','standard','Active','Demo Org',0.55,0.12,2100,false,0.58,890,710,180,false,'hybrid','general','["tool_execution","data_read"]'::jsonb,'standard','185.220.101.1','Netherlands','185.220.101.1','Netherlands','websocket','openai','gpt-4o-mini','General purpose hybrid',10,'[]'::jsonb,'[]'::jsonb,'{}'::jsonb),
('66666666-6666-6666-6666-666666666666','00000000-0000-0000-0000-000000000001','agent-rogue-x','unknown','FREE','none','Inactive','Unknown',0.0,0.95,0,true,0.0,50,2,48,true,'bot','unknown','[]'::jsonb,'critical','185.220.101.99','TOR_EXIT','185.220.101.99','TOR_EXIT','http','unknown','unknown','BLACKLISTED',0,'[]'::jsonb,'["*"]'::jsonb,'{"blacklist_reason":"data_exfiltration_attempt"}'::jsonb),
('77777777-7777-7777-7777-777777777777','00000000-0000-0000-0000-000000000003','agent-epsilon','openai','PROFESSIONAL','full','Active','BigBank',0.82,0.05,8900,false,0.85,3200,3050,150,false,'bot','compliance','["audit","report_gen","data_read"]'::jsonb,'elevated','52.28.59.10','Germany','52.28.59.10','Germany','http','openai','gpt-4o','Compliance checking agent',12,'["audit_report"]'::jsonb,'["fund_transfer"]'::jsonb,'{"regulation":"MiFID-II"}'::jsonb),
('88888888-8888-8888-8888-888888888888','00000000-0000-0000-0000-000000000004','agent-newbie','anthropic','STARTER','limited','Active','Startup Inc',0.50,0.0,0,false,0.50,0,0,0,false,'bot','general','["data_read"]'::jsonb,'standard','192.168.1.100','LOCAL','192.168.1.100','LOCAL','http','anthropic','claude-3-haiku','Brand new agent',5,'[]'::jsonb,'[]'::jsonb,'{}'::jsonb),
('99999999-9999-9999-9999-999999999999','00000000-0000-0000-0000-000000000003','analyst-bob',NULL,'PROFESSIONAL','read','Active','BigBank',0.92,0.0,0,false,0.95,150,150,0,false,'human','analyst','["data_read","report_gen"]'::jsonb,'low','83.169.40.22','Germany','83.169.40.22','Germany','http',NULL,NULL,'Financial analyst',5,'[]'::jsonb,'["fund_transfer","delete_account"]'::jsonb,'{"department":"risk-mgmt"}'::jsonb),
('aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa','00000000-0000-0000-0000-000000000005','agent-dead','openai','STARTER','none','Inactive','SusCo',0.15,0.55,0,true,0.10,200,30,170,false,'bot','general','[]'::jsonb,'critical','0.0.0.0','UNKNOWN','0.0.0.0','UNKNOWN','http','openai','gpt-3.5-turbo','Disabled',0,'[]'::jsonb,'["*"]'::jsonb,'{}'::jsonb)
ON CONFLICT (agent_id) DO NOTHING;

INSERT INTO rules (tenant_id,natural_language,logic_json,priority,status) VALUES
('00000000-0000-0000-0000-000000000001','Block fund transfer above $10K without HITL','{"condition":"amount > 10000 AND no_hitl","action":"BLOCK"}'::jsonb,1,'Active'),
('00000000-0000-0000-0000-000000000001','Require dual auth for CRITICAL actions','{"condition":"action_class == CRITICAL","action":"ESCROW","require":"dual_auth"}'::jsonb,2,'Active'),
('00000000-0000-0000-0000-000000000001','Auto-block blacklisted agents','{"condition":"agent.blacklisted == true","action":"BLOCK"}'::jsonb,0,'Active'),
('00000000-0000-0000-0000-000000000002','Allow read-only with trust > 0.3','{"condition":"action_class==LOW AND trust>0.3","action":"ALLOW"}'::jsonb,1,'Active'),
('00000000-0000-0000-0000-000000000003','BigBank: Require audit trail for all actions','{"condition":"true","action":"LOG_AND_ALLOW"}'::jsonb,1,'Active');

-- ─────────────────────────────────────────────────────────────────────────────
-- §3 TRUST SCORES — full spectrum
-- ─────────────────────────────────────────────────────────────────────────────
INSERT INTO trust_scores (agent_id,tenant_id,audit_score,reputation_score,attestation_score,history_score,trust_level) VALUES
('11111111-1111-1111-1111-111111111111','00000000-0000-0000-0000-000000000001',0.90,0.91,0.85,0.88,0.87),
('22222222-2222-2222-2222-222222222222','00000000-0000-0000-0000-000000000001',0.35,0.38,0.40,0.45,0.42),
('33333333-3333-3333-3333-333333333333','00000000-0000-0000-0000-000000000001',0.96,0.97,0.94,0.95,0.95),
('44444444-4444-4444-4444-444444444444','00000000-0000-0000-0000-000000000001',1.00,1.00,0.99,1.00,0.99),
('55555555-5555-5555-5555-555555555555','00000000-0000-0000-0000-000000000002',0.52,0.58,0.50,0.55,0.55),
('66666666-6666-6666-6666-666666666666','00000000-0000-0000-0000-000000000001',0.0,0.0,0.0,0.0,0.0),
('77777777-7777-7777-7777-777777777777','00000000-0000-0000-0000-000000000003',0.80,0.85,0.82,0.83,0.82),
('88888888-8888-8888-8888-888888888888','00000000-0000-0000-0000-000000000004',0.50,0.50,0.50,0.50,0.50),
('99999999-9999-9999-9999-999999999999','00000000-0000-0000-0000-000000000003',0.94,0.95,0.90,0.92,0.92),
('aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa','00000000-0000-0000-0000-000000000005',0.10,0.10,0.15,0.12,0.15)
ON CONFLICT (agent_id,tenant_id) DO NOTHING;

INSERT INTO agents_reputation (agent_id,tenant_id,trust_score,behavioral_drift,gov_tax_balance,is_frozen) VALUES
('11111111-1111-1111-1111-111111111111','00000000-0000-0000-0000-000000000001',0.87,0.03,12500,false),
('22222222-2222-2222-2222-222222222222','00000000-0000-0000-0000-000000000001',0.42,0.18,800,true),
('33333333-3333-3333-3333-333333333333','00000000-0000-0000-0000-000000000001',0.95,0.01,45000,false),
('44444444-4444-4444-4444-444444444444','00000000-0000-0000-0000-000000000001',0.99,0.00,0,false),
('55555555-5555-5555-5555-555555555555','00000000-0000-0000-0000-000000000002',0.55,0.12,2100,false),
('66666666-6666-6666-6666-666666666666','00000000-0000-0000-0000-000000000001',0.0,0.95,0,true),
('77777777-7777-7777-7777-777777777777','00000000-0000-0000-0000-000000000003',0.82,0.05,8900,false),
('88888888-8888-8888-8888-888888888888','00000000-0000-0000-0000-000000000004',0.50,0.0,0,false),
('99999999-9999-9999-9999-999999999999','00000000-0000-0000-0000-000000000003',0.92,0.0,0,false),
('aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa','00000000-0000-0000-0000-000000000005',0.15,0.55,0,true)
ON CONFLICT (agent_id) DO NOTHING;

INSERT INTO reputation_audit (agent_id,tenant_id,transaction_id,verdict,entropy_delta,tax_levied,reasoning) VALUES
('11111111-1111-1111-1111-111111111111','00000000-0000-0000-0000-000000000001','gov-001','ALLOW',-0.02,150,'GPU purchase — within bounds'),
('11111111-1111-1111-1111-111111111111','00000000-0000-0000-0000-000000000001','gov-005','ALLOW',-0.01,100,'Data query — routine'),
('11111111-1111-1111-1111-111111111111','00000000-0000-0000-0000-000000000001','gov-006','ESCROW',0.05,325,'Large fund transfer > $10K'),
('22222222-2222-2222-2222-222222222222','00000000-0000-0000-0000-000000000001','gov-002','BLOCK',0.15,500,'Fund transfer blocked — low trust'),
('22222222-2222-2222-2222-222222222222','00000000-0000-0000-0000-000000000001','gov-010','BLOCK',0.20,600,'Second violation — drift increasing'),
('33333333-3333-3333-3333-333333333333','00000000-0000-0000-0000-000000000001','gov-003','ALLOW',-0.01,50,'Routine health check'),
('55555555-5555-5555-5555-555555555555','00000000-0000-0000-0000-000000000002','gov-004','ALLOW',0.05,200,'Bail-out recovery — penalty applied'),
('66666666-6666-6666-6666-666666666666','00000000-0000-0000-0000-000000000001','gov-007','BLOCK',0.50,0,'Blacklisted — all actions blocked'),
('66666666-6666-6666-6666-666666666666','00000000-0000-0000-0000-000000000001','gov-008','BLOCK',0.45,0,'Data exfiltration attempt'),
('77777777-7777-7777-7777-777777777777','00000000-0000-0000-0000-000000000003','gov-009','ALLOW',-0.02,180,'Compliance audit — clean'),
('88888888-8888-8888-8888-888888888888','00000000-0000-0000-0000-000000000004','gov-011','ALLOW',0.00,0,'First interaction — no history'),
('44444444-4444-4444-4444-444444444444','00000000-0000-0000-0000-000000000001','gov-012','ALLOW',0.00,0,'Human override — no tax');

-- ─────────────────────────────────────────────────────────────────────────────
-- §4 VERDICTS — all verdict types
-- ─────────────────────────────────────────────────────────────────────────────
INSERT INTO verdicts (tenant_id,request_id,agent_id,pid,binary_hash,action,trust_level,trust_tax,reasoning) VALUES
('00000000-0000-0000-0000-000000000001','req-001','11111111-1111-1111-1111-111111111111',12345,'sha256:abc123','ALLOW',0.87,0.043,'GPU purchase within policy'),
('00000000-0000-0000-0000-000000000001','req-002','22222222-2222-2222-2222-222222222222',12346,'sha256:def456','BLOCK',0.42,0.210,'Critical action + low trust'),
('00000000-0000-0000-0000-000000000001','req-003','33333333-3333-3333-3333-333333333333',12347,'sha256:ghi789','ALLOW',0.95,0.025,'Routine monitoring'),
('00000000-0000-0000-0000-000000000001','req-004','11111111-1111-1111-1111-111111111111',12345,'sha256:abc123','ESCROW',0.87,0.065,'Large transfer — HITL required'),
('00000000-0000-0000-0000-000000000002','req-005','55555555-5555-5555-5555-555555555555',12348,'sha256:mno012','ALLOW',0.55,0.135,'Standard tool execution'),
('00000000-0000-0000-0000-000000000001','req-006','66666666-6666-6666-6666-666666666666',99999,'sha256:zzz000','BLOCK',0.0,0.0,'Blacklisted agent rejected'),
('00000000-0000-0000-0000-000000000001','req-007','66666666-6666-6666-6666-666666666666',99999,'sha256:zzz000','BLOCK',0.0,0.0,'Second attempt — still blocked'),
('00000000-0000-0000-0000-000000000003','req-008','77777777-7777-7777-7777-777777777777',20001,'sha256:epsi01','ALLOW',0.82,0.054,'Compliance scan approved'),
('00000000-0000-0000-0000-000000000003','req-009','99999999-9999-9999-9999-999999999999',20002,NULL,'ALLOW',0.92,0.0,'Human read — no tax'),
('00000000-0000-0000-0000-000000000004','req-010','88888888-8888-8888-8888-888888888888',30001,'sha256:new001','ALLOW',0.50,0.150,'New agent — default trust'),
('00000000-0000-0000-0000-000000000001','req-011','22222222-2222-2222-2222-222222222222',12346,'sha256:def456','BLOCK',0.38,0.250,'Trust dropped after drift'),
('00000000-0000-0000-0000-000000000001','req-012','44444444-4444-4444-4444-444444444444',99001,NULL,'ALLOW',0.99,0.0,'Admin override — no restrictions');

-- ─────────────────────────────────────────────────────────────────────────────
-- §5 HANDSHAKE SESSIONS — all states
-- ─────────────────────────────────────────────────────────────────────────────
INSERT INTO handshake_sessions (session_id,tenant_id,initiator_id,responder_id,state,nonce,challenge,proof,attestation,expires_at,completed_at) VALUES
('hs-001','00000000-0000-0000-0000-000000000001','agent-alpha-7','agent-gamma-1','COMPLETED','n-a1g1','chal-001','proof-001','{"trust":0.92}'::jsonb,NOW()+INTERVAL '1 hour',NOW()-INTERVAL '30 min'),
('hs-002','00000000-0000-0000-0000-000000000001','agent-beta-3','agent-alpha-7','INITIATED','n-b3a7',NULL,NULL,NULL,NOW()+INTERVAL '1 hour',NULL),
('hs-003','00000000-0000-0000-0000-000000000002','agent-delta-9','agent-delta-9','COMPLETED','n-d9d9','chal-self','proof-self','{"trust":0.55}'::jsonb,NOW()+INTERVAL '1 hour',NOW()-INTERVAL '10 min'),
('hs-004','00000000-0000-0000-0000-000000000001','agent-gamma-1','operator-sarah','COMPLETED','n-g1s1','chal-ops','proof-ops','{"trust":0.98}'::jsonb,NOW()+INTERVAL '2 hours',NOW()-INTERVAL '5 min'),
('hs-005','00000000-0000-0000-0000-000000000001','agent-rogue-x','agent-alpha-7','INITIATED','n-rx','chal-rogue',NULL,NULL,NOW()-INTERVAL '1 hour',NULL),
('hs-006','00000000-0000-0000-0000-000000000003','agent-epsilon','analyst-bob','COMPLETED','n-eb','chal-eb','proof-eb','{"trust":0.88}'::jsonb,NOW()+INTERVAL '4 hours',NOW()-INTERVAL '15 min')
ON CONFLICT (session_id) DO NOTHING;

INSERT INTO federation_handshakes (session_id,initiator,responder,state,challenge,proof,trust_level,metadata,expires_at) VALUES
('fed-001','ocx-acme.example.com','ocx-partner.example.com','COMPLETED','fc-001','fp-001',0.85,'{"protocol":"OGP-v1"}'::jsonb,NOW()+INTERVAL '24 hours'),
('fed-002','ocx-partner.example.com','ocx-acme.example.com','CHALLENGE_SENT','fc-002',NULL,0.0,'{"protocol":"OGP-v1"}'::jsonb,NOW()+INTERVAL '24 hours'),
('fed-003','ocx-startup.io','ocx-acme.example.com','PROPOSED',NULL,NULL,0.0,'{"protocol":"OGP-v1"}'::jsonb,NOW()+INTERVAL '48 hours'),
('fed-004','ocx-acme.example.com','ocx-malicious.xyz','REJECTED','fc-004','fp-invalid',0.0,'{"reason":"invalid_proof"}'::jsonb,NOW()-INTERVAL '1 day'),
('fed-005','ocx-bigbank.com','ocx-acme.example.com','EXPIRED',NULL,NULL,0.0,'{"reason":"timeout"}'::jsonb,NOW()-INTERVAL '3 days')
ON CONFLICT (session_id) DO NOTHING;

-- ─────────────────────────────────────────────────────────────────────────────
-- §6 AGENT IDENTITIES
-- ─────────────────────────────────────────────────────────────────────────────
INSERT INTO agent_identities (pid,tenant_id,agent_id,binary_hash,trust_level,expires_at) VALUES
(12345,'00000000-0000-0000-0000-000000000001','11111111-1111-1111-1111-111111111111','sha256:abc123',0.87,NOW()+INTERVAL '4 hours'),
(12346,'00000000-0000-0000-0000-000000000001','22222222-2222-2222-2222-222222222222','sha256:def456',0.42,NOW()+INTERVAL '4 hours'),
(12347,'00000000-0000-0000-0000-000000000001','33333333-3333-3333-3333-333333333333','sha256:ghi789',0.95,NOW()+INTERVAL '4 hours'),
(99001,'00000000-0000-0000-0000-000000000001','44444444-4444-4444-4444-444444444444',NULL,0.99,NOW()+INTERVAL '8 hours'),
(12348,'00000000-0000-0000-0000-000000000002','55555555-5555-5555-5555-555555555555','sha256:mno012',0.55,NOW()+INTERVAL '4 hours'),
(99999,'00000000-0000-0000-0000-000000000001','66666666-6666-6666-6666-666666666666','sha256:zzz000',0.0,NOW()-INTERVAL '1 day'),
(20001,'00000000-0000-0000-0000-000000000003','77777777-7777-7777-7777-777777777777','sha256:epsi01',0.82,NOW()+INTERVAL '4 hours'),
(30001,'00000000-0000-0000-0000-000000000004','88888888-8888-8888-8888-888888888888','sha256:new001',0.50,NOW()+INTERVAL '4 hours'),
(20002,'00000000-0000-0000-0000-000000000003','99999999-9999-9999-9999-999999999999',NULL,0.92,NOW()+INTERVAL '8 hours')
ON CONFLICT (pid,tenant_id) DO NOTHING;

-- ─────────────────────────────────────────────────────────────────────────────
-- §7 QUARANTINE & RECOVERY
-- ─────────────────────────────────────────────────────────────────────────────
INSERT INTO quarantine_records (tenant_id,agent_id,reason,alert_source,quarantined_at,released_at,is_active) VALUES
('00000000-0000-0000-0000-000000000001','22222222-2222-2222-2222-222222222222','Trust below 0.45 — auto quarantine','ContinuousAccessEvaluator',NOW()-INTERVAL '2 days',NULL,true),
('00000000-0000-0000-0000-000000000001','11111111-1111-1111-1111-111111111111','Suspected drift anomaly','DriftDetector',NOW()-INTERVAL '10 days',NOW()-INTERVAL '8 days',false),
('00000000-0000-0000-0000-000000000001','66666666-6666-6666-6666-666666666666','Data exfiltration attempt','ThreatDetector',NOW()-INTERVAL '30 days',NULL,true),
('00000000-0000-0000-0000-000000000005','aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa','Tenant suspended — all agents quarantined','TenantManager',NOW()-INTERVAL '45 days',NULL,true),
('00000000-0000-0000-0000-000000000003','77777777-7777-7777-7777-777777777777','Temporary compliance review hold','ComplianceEngine',NOW()-INTERVAL '5 days',NOW()-INTERVAL '3 days',false);

INSERT INTO recovery_attempts (tenant_id,agent_id,stake_amount,success,attempt_number) VALUES
('00000000-0000-0000-0000-000000000001','11111111-1111-1111-1111-111111111111',5000,true,1),
('00000000-0000-0000-0000-000000000001','22222222-2222-2222-2222-222222222222',10000,false,1),
('00000000-0000-0000-0000-000000000001','22222222-2222-2222-2222-222222222222',15000,false,2),
('00000000-0000-0000-0000-000000000001','22222222-2222-2222-2222-222222222222',25000,false,3),
('00000000-0000-0000-0000-000000000001','66666666-6666-6666-6666-666666666666',50000,false,1),
('00000000-0000-0000-0000-000000000003','77777777-7777-7777-7777-777777777777',8000,true,1);

INSERT INTO probation_periods (tenant_id,agent_id,started_at,ends_at,threshold,is_active) VALUES
('00000000-0000-0000-0000-000000000001','11111111-1111-1111-1111-111111111111',NOW()-INTERVAL '8 days',NOW()-INTERVAL '1 day',0.75,false),
('00000000-0000-0000-0000-000000000001','22222222-2222-2222-2222-222222222222',NOW()-INTERVAL '2 days',NOW()+INTERVAL '14 days',0.60,true),
('00000000-0000-0000-0000-000000000003','77777777-7777-7777-7777-777777777777',NOW()-INTERVAL '3 days',NOW()-INTERVAL '1 day',0.70,false);

-- ─────────────────────────────────────────────────────────────────────────────
-- §8 GOVERNANCE
-- ─────────────────────────────────────────────────────────────────────────────
INSERT INTO committee_members (member_id,tenant_id,member_name,email,role,is_active) VALUES
('cccccccc-cccc-cccc-cccc-cccccccccc01','00000000-0000-0000-0000-000000000001','Alice Chen','alice@acme.com','CHAIR',true),
('cccccccc-cccc-cccc-cccc-cccccccccc02','00000000-0000-0000-0000-000000000001','Bob Wilson','bob@acme.com','MEMBER',true),
('cccccccc-cccc-cccc-cccc-cccccccccc03','00000000-0000-0000-0000-000000000001','Carol Davis','carol@acme.com','MEMBER',true),
('cccccccc-cccc-cccc-cccc-cccccccccc04','00000000-0000-0000-0000-000000000003','Dan Kim','dan@bigbank.com','CHAIR',true),
('cccccccc-cccc-cccc-cccc-cccccccccc05','00000000-0000-0000-0000-000000000003','Eve Park','eve@bigbank.com','MEMBER',false)
ON CONFLICT (email) DO NOTHING;

INSERT INTO governance_proposals (proposal_id,title,description,author_id,status,target_version,backward_compatible,voting_starts_at,voting_ends_at,passed_at) VALUES
('dddddddd-dddd-dddd-dddd-dddddddddd01','Increase trust threshold for CRITICAL actions','Proposal to raise the trust score threshold from 0.7 to 0.85 for CRITICAL tier actions','cccccccc-cccc-cccc-cccc-cccccccccc01','PASSED','2.1',true,NOW()-INTERVAL '14 days',NOW()-INTERVAL '7 days',NOW()-INTERVAL '7 days'),
('dddddddd-dddd-dddd-dddd-dddddddddd02','Add mandatory HITL for financial ops above $50K','Require human-in-the-loop for all financial operations exceeding $50,000','cccccccc-cccc-cccc-cccc-cccccccccc02','OPEN','2.2',true,NOW()-INTERVAL '3 days',NOW()+INTERVAL '4 days',NULL),
('dddddddd-dddd-dddd-dddd-dddddddddd03','Phase out legacy auth scope','Remove deprecated auth_scope values from agent registry','cccccccc-cccc-cccc-cccc-cccccccccc03','DRAFT','3.0',false,NULL,NULL,NULL),
('dddddddd-dddd-dddd-dddd-dddddddddd04','Reduce blacklist review period from 30 to 14 days','Speed up blacklist review process','cccccccc-cccc-cccc-cccc-cccccccccc01','REJECTED','2.1',true,NOW()-INTERVAL '30 days',NOW()-INTERVAL '23 days',NULL),
('dddddddd-dddd-dddd-dddd-dddddddddd05','Implement behavioral drift auto-quarantine at 0.20','Auto-quarantine agents when drift exceeds 0.20','cccccccc-cccc-cccc-cccc-cccccccccc04','IMPLEMENTED','2.0',true,NOW()-INTERVAL '60 days',NOW()-INTERVAL '53 days',NOW()-INTERVAL '50 days')
ON CONFLICT (proposal_id) DO NOTHING;

INSERT INTO governance_votes (proposal_id,member_id,vote_choice,justification) VALUES
('dddddddd-dddd-dddd-dddd-dddddddddd01','cccccccc-cccc-cccc-cccc-cccccccccc01','APPROVE','Critical actions need higher trust bar'),
('dddddddd-dddd-dddd-dddd-dddddddddd01','cccccccc-cccc-cccc-cccc-cccccccccc02','APPROVE','Aligns with enterprise security posture'),
('dddddddd-dddd-dddd-dddd-dddddddddd01','cccccccc-cccc-cccc-cccc-cccccccccc03','REJECT','May break existing automation workflows'),
('dddddddd-dddd-dddd-dddd-dddddddddd02','cccccccc-cccc-cccc-cccc-cccccccccc01','APPROVE','Financial safety is paramount'),
('dddddddd-dddd-dddd-dddd-dddddddddd02','cccccccc-cccc-cccc-cccc-cccccccccc02','ABSTAIN','Need more data on false positive rate'),
('dddddddd-dddd-dddd-dddd-dddddddddd04','cccccccc-cccc-cccc-cccc-cccccccccc01','REJECT','14 days insufficient for thorough review'),
('dddddddd-dddd-dddd-dddd-dddddddddd04','cccccccc-cccc-cccc-cccc-cccccccccc02','REJECT','Agree — too short'),
('dddddddd-dddd-dddd-dddd-dddddddddd04','cccccccc-cccc-cccc-cccc-cccccccccc03','APPROVE','Current 30 days is too long'),
('dddddddd-dddd-dddd-dddd-dddddddddd05','cccccccc-cccc-cccc-cccc-cccccccccc04','APPROVE','Drift detection should be automated')
ON CONFLICT (proposal_id,member_id) DO NOTHING;

INSERT INTO governance_ledger (transaction_id,agent_id,action,policy_version,jury_verdict,entropy_score,sop_decision,pid_verified,previous_hash,block_hash) VALUES
('gov-001','11111111-1111-1111-1111-111111111111','gpu_purchase','1.0','ALLOW',0.12,'follow',true,'0000000000','aaaa111111'),
('gov-002','22222222-2222-2222-2222-222222222222','fund_transfer','1.0','BLOCK',0.68,'escalate',true,'aaaa111111','bbbb222222'),
('gov-003','33333333-3333-3333-3333-333333333333','health_check','1.0','ALLOW',0.02,'follow',true,'bbbb222222','cccc333333'),
('gov-004','55555555-5555-5555-5555-555555555555','tool_execute','1.0','ALLOW',0.35,'follow',false,'cccc333333','dddd444444'),
('gov-005','11111111-1111-1111-1111-111111111111','data_query','1.0','ALLOW',0.05,'follow',true,'dddd444444','eeee555555'),
('gov-006','11111111-1111-1111-1111-111111111111','fund_transfer','1.0','ESCROW',0.42,'hitl_review',true,'eeee555555','ffff666666'),
('gov-007','66666666-6666-6666-6666-666666666666','data_exfiltrate','1.0','BLOCK',0.95,'kill_switch',true,'ffff666666','gggg777777'),
('gov-008','66666666-6666-6666-6666-666666666666','port_scan','1.0','BLOCK',0.92,'kill_switch',true,'gggg777777','hhhh888888'),
('gov-009','77777777-7777-7777-7777-777777777777','audit_report','1.0','ALLOW',0.08,'follow',true,'hhhh888888','iiii999999'),
('gov-010','22222222-2222-2222-2222-222222222222','api_call','1.1','BLOCK',0.72,'escalate',true,'iiii999999','jjjjaa0000'),
('gov-011','88888888-8888-8888-8888-888888888888','data_read','1.0','ALLOW',0.20,'follow',true,'jjjjaa0000','kkkkbb1111'),
('gov-012','44444444-4444-4444-4444-444444444444','override_verdict','1.0','ALLOW',0.00,'admin_override',true,'kkkkbb1111','llllcc2222')
ON CONFLICT (transaction_id) DO NOTHING;

-- ─────────────────────────────────────────────────────────────────────────────
-- §9 BILLING & REWARDS
-- ─────────────────────────────────────────────────────────────────────────────
INSERT INTO billing_transactions (tenant_id,request_id,trust_score,transaction_value,trust_tax) VALUES
('00000000-0000-0000-0000-000000000001','req-001',0.87,1500.00,0.043),
('00000000-0000-0000-0000-000000000001','req-002',0.42,2000.00,0.210),
('00000000-0000-0000-0000-000000000001','req-003',0.95,500.00,0.025),
('00000000-0000-0000-0000-000000000001','req-004',0.87,15000.00,0.065),
('00000000-0000-0000-0000-000000000002','req-005',0.55,100.00,0.135),
('00000000-0000-0000-0000-000000000001','req-006',0.0,0.0,0.0),
('00000000-0000-0000-0000-000000000003','req-008',0.82,5000.00,0.054),
('00000000-0000-0000-0000-000000000003','req-009',0.92,2500.00,0.0),
('00000000-0000-0000-0000-000000000004','req-010',0.50,200.00,0.150);

INSERT INTO reward_distributions (tenant_id,agent_id,amount,trust_score,participation_count,formula) VALUES
('00000000-0000-0000-0000-000000000001','11111111-1111-1111-1111-111111111111',5200,0.87,4520,'linear_trust * participation'),
('00000000-0000-0000-0000-000000000001','33333333-3333-3333-3333-333333333333',12500,0.95,12800,'linear_trust * participation'),
('00000000-0000-0000-0000-000000000001','44444444-4444-4444-4444-444444444444',800,0.99,320,'human_flat_rate'),
('00000000-0000-0000-0000-000000000002','55555555-5555-5555-5555-555555555555',450,0.55,890,'linear_trust * participation'),
('00000000-0000-0000-0000-000000000003','77777777-7777-7777-7777-777777777777',3400,0.82,3200,'linear_trust * participation'),
('00000000-0000-0000-0000-000000000003','99999999-9999-9999-9999-999999999999',600,0.92,150,'human_flat_rate'),
('00000000-0000-0000-0000-000000000001','66666666-6666-6666-6666-666666666666',0,0.0,50,'blacklisted_zero'),
('00000000-0000-0000-0000-000000000001','22222222-2222-2222-2222-222222222222',0,0.42,1200,'frozen_zero');

-- ─────────────────────────────────────────────────────────────────────────────
-- §10 CONTRACTS & MONITORING
-- ─────────────────────────────────────────────────────────────────────────────
INSERT INTO contract_deployments (contract_id,tenant_id,name,version,ebcl_source,activity_id,status) VALUES
('eeeeeeee-eeee-eeee-eeee-eeeeeeeeee01','00000000-0000-0000-0000-000000000001','GPU Purchase Guard','1.0','WHEN action=="gpu_purchase" AND amount>1000 THEN REQUIRE trust>0.7','act-gpu','ACTIVE'),
('eeeeeeee-eeee-eeee-eeee-eeeeeeeeee02','00000000-0000-0000-0000-000000000001','Fund Transfer Limit','2.0','WHEN action=="fund_transfer" AND amount>10000 THEN REQUIRE hitl_approval','act-fund','ACTIVE'),
('eeeeeeee-eeee-eeee-eeee-eeeeeeeeee03','00000000-0000-0000-0000-000000000003','Compliance Audit Trail','1.0','WHEN ANY THEN LOG all_fields','act-audit','ACTIVE'),
('eeeeeeee-eeee-eeee-eeee-eeeeeeeeee04','00000000-0000-0000-0000-000000000001','Deprecated Contract','0.9','WHEN true THEN ALLOW','act-old','INACTIVE');

INSERT INTO contract_executions (contract_id,tenant_id,trigger_source,input_payload,output_result,status,error_message,completed_at) VALUES
('eeeeeeee-eeee-eeee-eeee-eeeeeeeeee01','00000000-0000-0000-0000-000000000001','agent-alpha-7','{"action":"gpu_purchase","amount":2500}'::jsonb,'{"decision":"ALLOW","trust":0.87}'::jsonb,'SUCCESS',NULL,NOW()-INTERVAL '1 hour'),
('eeeeeeee-eeee-eeee-eeee-eeeeeeeeee01','00000000-0000-0000-0000-000000000001','agent-beta-3','{"action":"gpu_purchase","amount":5000}'::jsonb,'{"decision":"BLOCK","reason":"trust_below_threshold"}'::jsonb,'SUCCESS',NULL,NOW()-INTERVAL '2 hours'),
('eeeeeeee-eeee-eeee-eeee-eeeeeeeeee02','00000000-0000-0000-0000-000000000001','agent-alpha-7','{"action":"fund_transfer","amount":25000}'::jsonb,'{"decision":"ESCROW"}'::jsonb,'SUCCESS',NULL,NOW()-INTERVAL '30 min'),
('eeeeeeee-eeee-eeee-eeee-eeeeeeeeee03','00000000-0000-0000-0000-000000000003','agent-epsilon','{"action":"audit_report"}'::jsonb,'{"logged":true}'::jsonb,'SUCCESS',NULL,NOW()-INTERVAL '15 min'),
('eeeeeeee-eeee-eeee-eeee-eeeeeeeeee02','00000000-0000-0000-0000-000000000001','agent-rogue-x','{"action":"fund_transfer","amount":999999}'::jsonb,NULL,'FAILED','Agent is blacklisted — execution rejected',NULL),
('eeeeeeee-eeee-eeee-eeee-eeeeeeeeee01','00000000-0000-0000-0000-000000000001','agent-newbie','{"action":"gpu_purchase","amount":100}'::jsonb,NULL,'TIMEOUT','Execution timed out after 30s',NULL);

INSERT INTO use_case_links (tenant_id,use_case_key,contract_id) VALUES
('00000000-0000-0000-0000-000000000001','gpu_purchase_guard','eeeeeeee-eeee-eeee-eeee-eeeeeeeeee01'),
('00000000-0000-0000-0000-000000000001','fund_transfer_limit','eeeeeeee-eeee-eeee-eeee-eeeeeeeeee02'),
('00000000-0000-0000-0000-000000000003','compliance_audit','eeeeeeee-eeee-eeee-eeee-eeeeeeeeee03')
ON CONFLICT (tenant_id,use_case_key) DO NOTHING;

INSERT INTO metrics_events (tenant_id,metric_name,value,tags) VALUES
('00000000-0000-0000-0000-000000000001','trust_score_avg',0.78,'{"scope":"all_agents"}'::jsonb),
('00000000-0000-0000-0000-000000000001','verdicts_per_minute',12.5,'{"window":"5m"}'::jsonb),
('00000000-0000-0000-0000-000000000001','block_rate',0.18,'{"window":"1h"}'::jsonb),
('00000000-0000-0000-0000-000000000001','escrow_rate',0.05,'{"window":"1h"}'::jsonb),
('00000000-0000-0000-0000-000000000002','trust_score_avg',0.55,'{"scope":"all_agents"}'::jsonb),
('00000000-0000-0000-0000-000000000003','trust_score_avg',0.87,'{"scope":"all_agents"}'::jsonb),
('00000000-0000-0000-0000-000000000003','compliance_check_count',420,'{"window":"24h"}'::jsonb),
('00000000-0000-0000-0000-000000000001','drift_avg',0.12,'{"scope":"all_agents"}'::jsonb),
('00000000-0000-0000-0000-000000000004','trust_score_avg',0.50,'{"scope":"all_agents"}'::jsonb);

INSERT INTO alerts (tenant_id,alert_type,message,status,resolved_at) VALUES
('00000000-0000-0000-0000-000000000001','TRUST_DROP','Agent agent-beta-3 trust dropped below 0.45','OPEN',NULL),
('00000000-0000-0000-0000-000000000001','BLACKLIST','Agent agent-rogue-x blacklisted for data exfiltration','RESOLVED',NOW()-INTERVAL '29 days'),
('00000000-0000-0000-0000-000000000001','DRIFT_SPIKE','Agent agent-beta-3 behavioral drift exceeded 0.15','OPEN',NULL),
('00000000-0000-0000-0000-000000000003','COMPLIANCE','Compliance audit passed for Q4','RESOLVED',NOW()-INTERVAL '5 days'),
('00000000-0000-0000-0000-000000000002','QUOTA_WARNING','Demo tenant approaching API call limit (80%)','OPEN',NULL),
('00000000-0000-0000-0000-000000000005','SUSPENDED','Tenant suspended due to payment failure','OPEN',NULL);


-- ─────────────────────────────────────────────────────────────────────────────
-- §11 SIMULATION & IMPACT
-- ─────────────────────────────────────────────────────────────────────────────
INSERT INTO simulation_scenarios (scenario_id,tenant_id,name,description,parameters) VALUES
('ffffffff-ffff-ffff-ffff-ffffffffffff','00000000-0000-0000-0000-000000000001','Trust Threshold Sweep','Simulate impact of changing trust thresholds from 0.5 to 0.9','{"min_trust":0.5,"max_trust":0.9,"step":0.05}'::jsonb)
ON CONFLICT (scenario_id) DO NOTHING;

INSERT INTO simulation_runs (scenario_id,tenant_id,status,started_at,completed_at,results_summary) VALUES
('ffffffff-ffff-ffff-ffff-ffffffffffff','00000000-0000-0000-0000-000000000001','COMPLETED',NOW()-INTERVAL '2 hours',NOW()-INTERVAL '1 hour','{"block_rate_at_0.7":0.18,"block_rate_at_0.85":0.35,"recommended":0.75}'::jsonb);

INSERT INTO impact_templates (template_id,tenant_id,name,base_assumptions) VALUES
('aabbccdd-0011-2233-4455-667788990011','00000000-0000-0000-0000-000000000001','Financial Risk Model','{"avg_transaction_value":5000,"agents":10,"daily_volume":500}'::jsonb)
ON CONFLICT (template_id) DO NOTHING;

INSERT INTO impact_reports (tenant_id,template_id,name,user_assumptions,output_metrics,monte_carlo_results) VALUES
('00000000-0000-0000-0000-000000000001','aabbccdd-0011-2233-4455-667788990011','Q4 Risk Assessment','{"trust_threshold":0.75}'::jsonb,'{"expected_loss":12500,"confidence":0.92}'::jsonb,'{"p50":10000,"p95":25000,"p99":45000}'::jsonb);

-- ─────────────────────────────────────────────────────────────────────────────
-- §12 ACTIVITIES
-- ─────────────────────────────────────────────────────────────────────────────
INSERT INTO activities (activity_id,tenant_id,name,version,status,ebcl_source,compiled_artifact,owner,authority,created_by,approved_by,approved_at,deployed_by,deployed_at,hash,description,tags,category) VALUES
('bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbb01','00000000-0000-0000-0000-000000000001','GPU Purchase Flow','1.0','ACTIVE','WHEN gpu_request THEN evaluate_trust AND enforce_budget','{"steps":3}'::jsonb,'finance-team','CFO','admin@acme.com','cfo@acme.com',NOW()-INTERVAL '20 days','devops@acme.com',NOW()-INTERVAL '18 days','sha256:act001','End-to-end GPU procurement with trust gating','{"finance","procurement","gpu"}','financial'),
('bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbb02','00000000-0000-0000-0000-000000000001','Incident Response','2.0','DEPLOYED','WHEN alert THEN triage AND escalate_if_critical','{"steps":5}'::jsonb,'sec-team','CISO','sec@acme.com','ciso@acme.com',NOW()-INTERVAL '10 days','devops@acme.com',NOW()-INTERVAL '8 days','sha256:act002','Automated incident triage and escalation','{"security","incident","escalation"}','security'),
('bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbb03','00000000-0000-0000-0000-000000000003','Compliance Audit','1.0','ACTIVE','WHEN action THEN log_all AND check_compliance','{"steps":4}'::jsonb,'compliance-team','CCO','compliance@bigbank.com','cco@bigbank.com',NOW()-INTERVAL '90 days','devops@bigbank.com',NOW()-INTERVAL '88 days','sha256:act003','Automated compliance logging and checking','{"compliance","audit","regulation"}','compliance'),
('bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbb04','00000000-0000-0000-0000-000000000001','Draft Activity','0.1','DRAFT','WHEN test THEN do_nothing',NULL,'dev-team','PM','dev@acme.com',NULL,NULL,NULL,NULL,'sha256:act004','Activity still in draft','{"test"}','test')
ON CONFLICT (name,version) DO NOTHING;

INSERT INTO activity_deployments (activity_id,environment,tenant_id,effective_from,effective_until,deployed_by,previous_deployment_id,rollback_reason,deployment_notes) VALUES
('bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbb01','PROD','00000000-0000-0000-0000-000000000001',NOW()-INTERVAL '18 days',NULL,'devops@acme.com',NULL,NULL,'Initial production deployment'),
('bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbb01','STAGING','00000000-0000-0000-0000-000000000001',NOW()-INTERVAL '20 days',NOW()-INTERVAL '18 days','devops@acme.com',NULL,NULL,'Staging validation'),
('bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbb02','PROD','00000000-0000-0000-0000-000000000001',NOW()-INTERVAL '8 days',NULL,'devops@acme.com',NULL,NULL,'v2.0 deployment'),
('bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbb02','DEV','00000000-0000-0000-0000-000000000001',NOW()-INTERVAL '15 days',NOW()-INTERVAL '10 days','dev@acme.com',NULL,NULL,'Development testing'),
('bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbb03','PROD','00000000-0000-0000-0000-000000000003',NOW()-INTERVAL '88 days',NULL,'devops@bigbank.com',NULL,NULL,'Compliance v1 deployment');

INSERT INTO activity_executions (activity_id,activity_version,tenant_id,environment,agent_id,completed_at,status,outcome,error_message,evidence_id,input_data,output_data,duration_ms,triggered_by,trigger_event) VALUES
('bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbb01','1.0','00000000-0000-0000-0000-000000000001','PROD','11111111-1111-1111-1111-111111111111',NOW()-INTERVAL '1 hour','COMPLETED','GPU purchase approved and executed',NULL,NULL,'{"gpu_model":"A100","quantity":2,"amount":25000}'::jsonb,'{"approved":true,"trust_check":"passed"}'::jsonb,3400,'agent-alpha-7','gpu_purchase_request'),
('bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbb01','1.0','00000000-0000-0000-0000-000000000001','PROD','22222222-2222-2222-2222-222222222222',NOW()-INTERVAL '2 hours','FAILED','Blocked — trust below threshold','Trust score 0.42 below required 0.70',NULL,'{"gpu_model":"H100","quantity":10,"amount":250000}'::jsonb,NULL,1200,'agent-beta-3','gpu_purchase_request'),
('bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbb02','2.0','00000000-0000-0000-0000-000000000001','PROD','33333333-3333-3333-3333-333333333333',NOW()-INTERVAL '30 min','COMPLETED','Incident triaged — severity LOW',NULL,NULL,'{"alert_type":"cpu_spike","severity":"LOW"}'::jsonb,'{"action":"monitor","escalated":false}'::jsonb,850,'system','alert_triggered'),
('bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbb03','1.0','00000000-0000-0000-0000-000000000003','PROD','77777777-7777-7777-7777-777777777777',NOW()-INTERVAL '15 min','COMPLETED','Audit log generated',NULL,NULL,'{"action":"trade_execution"}'::jsonb,'{"logged":true,"compliant":true}'::jsonb,420,'agent-epsilon','trade_action'),
('bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbb01','1.0','00000000-0000-0000-0000-000000000001','PROD','66666666-6666-6666-6666-666666666666',NULL,'FAILED','Execution blocked',NULL,NULL,'{"gpu_model":"A100","quantity":100}'::jsonb,NULL,50,'agent-rogue-x','gpu_purchase_request'),
('bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbb02','2.0','00000000-0000-0000-0000-000000000001','PROD','33333333-3333-3333-3333-333333333333',NULL,'TIMEOUT','Execution timed out','Timeout after 30000ms',NULL,'{"alert_type":"network_anomaly","severity":"CRITICAL"}'::jsonb,NULL,30000,'system','alert_triggered');

-- ─────────────────────────────────────────────────────────────────────────────
-- §13 APE ENGINE (Authority)
-- ─────────────────────────────────────────────────────────────────────────────
INSERT INTO authority_gaps (tenant_id,document_source,gap_type,severity,decision_point,current_authority_holder,execution_system,accountability_gap,override_frequency,time_sensitivity,a2a_candidacy_score,status) VALUES
('00000000-0000-0000-0000-000000000001','vendor_policy_v3.pdf','DELEGATION','HIGH','GPU Purchase > $10K','CFO','Manual Email','No audit trail for overrides',12,'URGENT',0.92,'PENDING'),
('00000000-0000-0000-0000-000000000001','security_sop_v2.pdf','ESCALATION','CRITICAL','Incident Response Triage','CISO','Slack + Jira','Delayed response times',45,'IMMEDIATE',0.88,'PENDING'),
('00000000-0000-0000-0000-000000000003','compliance_doc_v1.pdf','OVERSIGHT','MEDIUM','Trade Compliance Check','CCO','Legacy System','Manual spot checks only',5,'STANDARD',0.75,'PENDING');

INSERT INTO a2a_use_cases (tenant_id,gap_id,pattern_type,title,description,agents_involved,current_problem,ocx_proposal,estimated_impact,status) VALUES
('00000000-0000-0000-0000-000000000001',(SELECT gap_id FROM authority_gaps WHERE document_source='vendor_policy_v3.pdf' LIMIT 1),'APPROVAL_CHAIN','Automated GPU Purchase Approval','Replace manual CFO email approval with trust-gated agent approval','["agent-alpha-7","operator-sarah"]'::jsonb,'12 overrides/month, no audit trail','OCX trust-gated approval with automatic audit','{"time_saved_hrs":40,"risk_reduction":0.65}'::jsonb,'PROPOSED'),
('00000000-0000-0000-0000-000000000001',(SELECT gap_id FROM authority_gaps WHERE document_source='security_sop_v2.pdf' LIMIT 1),'ESCALATION_CHAIN','Automated Incident Triage','Auto-triage incidents and escalate critical ones to human operators','["agent-gamma-1","operator-sarah"]'::jsonb,'45 manual triages/month, avg 15min delay','OCX auto-triage with HITL escalation for CRITICAL','{"time_saved_hrs":60,"risk_reduction":0.80}'::jsonb,'PROPOSED');

INSERT INTO authority_contracts (use_case_id,tenant_id,contract_yaml,contract_version,agents_config,decision_point,authority_rules,enforcement,audit_config,status) VALUES
((SELECT use_case_id FROM a2a_use_cases WHERE title='Automated GPU Purchase Approval' LIMIT 1),'00000000-0000-0000-0000-000000000001','authority:\n  decision: gpu_purchase\n  threshold: 0.70\n  escalation: hitl_if_amount>10000','1.0','{"primary":"agent-alpha-7","escalation":"operator-sarah"}'::jsonb,'{"type":"gpu_purchase","max_amount":50000}'::jsonb,'{"min_trust":0.70,"require_pid":true}'::jsonb,'{"mode":"strict","on_violation":"block_and_alert"}'::jsonb,'{"log_level":"ALL","retention_days":365}'::jsonb,'DRAFT');


-- ─────────────────────────────────────────────────────────────────────────────
-- §14 EVIDENCE VAULT
-- ─────────────────────────────────────────────────────────────────────────────
INSERT INTO evidence (evidence_id,activity_id,activity_name,activity_version,execution_id,agent_id,agent_type,tenant_id,environment,event_type,event_data,decision,outcome,policy_reference,verified,verification_status,verification_errors,hash,previous_hash,signature,tags,metadata) VALUES
('eeeeeeee-0001-0001-0001-000000000001','bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbb01','GPU Purchase Flow','1.0','bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbb01','11111111-1111-1111-1111-111111111111','bot','00000000-0000-0000-0000-000000000001','PROD','ACTION_COMPLETED','{"action":"gpu_purchase","amount":25000}'::jsonb,'ALLOW','success','policy-fin-001',true,'VERIFIED',NULL,'sha256:ev001','0000000000','sig-ev001','{"finance","gpu"}','{"reviewer":"auto"}'::jsonb),
('eeeeeeee-0001-0001-0001-000000000002','bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbb01','GPU Purchase Flow','1.0','bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbb01','22222222-2222-2222-2222-222222222222','bot','00000000-0000-0000-0000-000000000001','PROD','ACTION_BLOCKED','{"action":"gpu_purchase","amount":250000}'::jsonb,'BLOCK','failure','policy-fin-001',true,'VERIFIED',NULL,'sha256:ev002','sha256:ev001','sig-ev002','{"finance","blocked"}','{"reviewer":"auto"}'::jsonb),
('eeeeeeee-0001-0001-0001-000000000003','bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbb02','Incident Response','2.0','bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbb02','33333333-3333-3333-3333-333333333333','service','00000000-0000-0000-0000-000000000001','PROD','INCIDENT_TRIAGED','{"alert_type":"cpu_spike","severity":"LOW"}'::jsonb,'ALLOW','success','policy-sec-001',true,'VERIFIED',NULL,'sha256:ev003','sha256:ev002','sig-ev003','{"security","incident"}','{"reviewer":"auto"}'::jsonb),
('eeeeeeee-0001-0001-0001-000000000004','bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbb01','GPU Purchase Flow','1.0','bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbb01','66666666-6666-6666-6666-666666666666','bot','00000000-0000-0000-0000-000000000001','PROD','ACTION_BLOCKED','{"action":"gpu_purchase","amount":999999}'::jsonb,'BLOCK','blocked','policy-fin-001',false,'FAILED','{"agent_blacklisted","no_valid_identity"}','sha256:ev004','sha256:ev003','sig-ev004','{"blocked","blacklist"}','{"reviewer":"auto"}'::jsonb),
('eeeeeeee-0001-0001-0001-000000000005','bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbb03','Compliance Audit','1.0','bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbb03','77777777-7777-7777-7777-777777777777','bot','00000000-0000-0000-0000-000000000003','PROD','COMPLIANCE_CHECK','{"action":"trade_execution"}'::jsonb,'ALLOW','success','policy-comp-001',false,'PENDING',NULL,'sha256:ev005','sha256:ev004','sig-ev005','{"compliance"}','{"reviewer":"pending"}'::jsonb),
('eeeeeeee-0001-0001-0001-000000000006','bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbb02','Incident Response','2.0','bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbb02','44444444-4444-4444-4444-444444444444','human','00000000-0000-0000-0000-000000000001','PROD','HITL_OVERRIDE','{"override":"allow_fund_transfer","amount":25000}'::jsonb,'ALLOW','success','policy-hitl-001',true,'VERIFIED',NULL,'sha256:ev006','sha256:ev005','sig-ev006','{"hitl","override","human"}','{"reviewer":"operator-sarah"}'::jsonb),
('eeeeeeee-0001-0001-0001-000000000007','bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbb01','GPU Purchase Flow','1.0','bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbb01','99999999-9999-9999-9999-999999999999','human','00000000-0000-0000-0000-000000000003','PROD','HUMAN_READ','{"action":"data_query"}'::jsonb,'ALLOW','success','policy-read-001',true,'DISPUTED',NULL,'sha256:ev007','sha256:ev006','sig-ev007','{"human","read","disputed"}','{"reviewer":"analyst-bob","dispute_reason":"data_scope_too_broad"}'::jsonb)
ON CONFLICT (evidence_id) DO NOTHING;

INSERT INTO evidence_chain (evidence_id,previous_block_hash,merkle_root) VALUES
('eeeeeeee-0001-0001-0001-000000000001','0000000000','merkle-root-001'),
('eeeeeeee-0001-0001-0001-000000000002','sha256:ev001','merkle-root-002'),
('eeeeeeee-0001-0001-0001-000000000003','sha256:ev002','merkle-root-003'),
('eeeeeeee-0001-0001-0001-000000000004','sha256:ev003','merkle-root-004'),
('eeeeeeee-0001-0001-0001-000000000005','sha256:ev004','merkle-root-005'),
('eeeeeeee-0001-0001-0001-000000000006','sha256:ev005','merkle-root-006'),
('eeeeeeee-0001-0001-0001-000000000007','sha256:ev006','merkle-root-007');

INSERT INTO evidence_attestations (evidence_id,attestor_type,attestor_id,attestation_status,confidence_score,reasoning,signature,proof) VALUES
('eeeeeeee-0001-0001-0001-000000000001','SYSTEM','trust-engine','APPROVED',0.95,'Trust check passed — score 0.87 exceeds 0.70 threshold','sig-att-001','{"method":"trust_gate"}'::jsonb),
('eeeeeeee-0001-0001-0001-000000000002','SYSTEM','trust-engine','APPROVED',0.98,'Correctly blocked — trust 0.42 below threshold','sig-att-002','{"method":"trust_gate"}'::jsonb),
('eeeeeeee-0001-0001-0001-000000000003','SYSTEM','cae-engine','APPROVED',0.99,'Incident properly triaged','sig-att-003','{"method":"cae_evaluation"}'::jsonb),
('eeeeeeee-0001-0001-0001-000000000004','SYSTEM','blacklist-engine','REJECTED',1.00,'Agent is blacklisted','sig-att-004','{"method":"blacklist_check"}'::jsonb),
('eeeeeeee-0001-0001-0001-000000000006','HUMAN','operator-sarah','APPROVED',1.00,'Override verified by senior operator','sig-att-006','{"method":"human_review"}'::jsonb),
('eeeeeeee-0001-0001-0001-000000000007','HUMAN','analyst-bob','DISPUTED',0.60,'Data scope of query seems too broad for stated purpose','sig-att-007','{"method":"human_review","dispute":"scope_concern"}'::jsonb);

-- ─────────────────────────────────────────────────────────────────────────────
-- §15 POLICIES
-- ─────────────────────────────────────────────────────────────────────────────
INSERT INTO policies (policy_id,version,tier,trigger_intent,logic,action,confidence,source_name,roles,expires_at,is_active) VALUES
('aabbccdd-0001-0001-0001-000000000001',1,'CRITICAL','fund_transfer','{"condition":"amount > 10000","require":"hitl"}'::jsonb,'{"type":"ESCROW","notify":"operator"}'::jsonb,0.95,'financial_policy_v3.pdf','{"CFO","finance-ops"}',NULL,false),
('aabbccdd-0001-0001-0001-000000000001',2,'CRITICAL','fund_transfer','{"condition":"amount > 10000","require":"hitl","max_amount":50000}'::jsonb,'{"type":"ESCROW","notify":"operator","auto_approve_below":5000}'::jsonb,0.97,'financial_policy_v4.pdf','{"CFO","finance-ops"}',NULL,true),
('aabbccdd-0001-0001-0001-000000000002',1,'HIGH','gpu_purchase','{"condition":"amount > 1000","require":"trust > 0.70"}'::jsonb,'{"type":"ALLOW_IF_TRUSTED","fallback":"BLOCK"}'::jsonb,0.92,'procurement_policy_v2.pdf','{"procurement","finance-ops"}',NULL,true),
('aabbccdd-0001-0001-0001-000000000003',1,'LOW','data_read','{"condition":"true","require":"authenticated"}'::jsonb,'{"type":"ALLOW","log":true}'::jsonb,0.99,'data_access_policy_v1.pdf','{"*"}',NOW()+INTERVAL '365 days',true),
('aabbccdd-0001-0001-0001-000000000004',1,'CRITICAL','delete_account','{"condition":"true","require":"admin + dual_auth"}'::jsonb,'{"type":"BLOCK","exception":"admin_override"}'::jsonb,1.00,'security_policy_v5.pdf','{"admin"}',NULL,true)
ON CONFLICT (policy_id,version) DO NOTHING;

INSERT INTO policy_audits (policy_id,agent_id,trigger_intent,tier,violated,action,data_payload,evaluation_time_ms) VALUES
('aabbccdd-0001-0001-0001-000000000001','11111111-1111-1111-1111-111111111111','fund_transfer','CRITICAL',false,'ESCROW','{"amount":25000,"trust":0.87}'::jsonb,12.5),
('aabbccdd-0001-0001-0001-000000000001','22222222-2222-2222-2222-222222222222','fund_transfer','CRITICAL',true,'BLOCK','{"amount":15000,"trust":0.42}'::jsonb,8.3),
('aabbccdd-0001-0001-0001-000000000002','11111111-1111-1111-1111-111111111111','gpu_purchase','HIGH',false,'ALLOW','{"amount":2500,"trust":0.87}'::jsonb,5.1),
('aabbccdd-0001-0001-0001-000000000002','22222222-2222-2222-2222-222222222222','gpu_purchase','HIGH',true,'BLOCK','{"amount":5000,"trust":0.42}'::jsonb,4.8),
('aabbccdd-0001-0001-0001-000000000003','55555555-5555-5555-5555-555555555555','data_read','LOW',false,'ALLOW','{"query":"SELECT * FROM reports"}'::jsonb,2.1),
('aabbccdd-0001-0001-0001-000000000004','66666666-6666-6666-6666-666666666666','delete_account','CRITICAL',true,'BLOCK','{"target":"user-12345"}'::jsonb,1.0);

INSERT INTO policy_extractions (source_name,document_hash,policies_extracted,avg_confidence,model_used,extraction_time_ms) VALUES
('financial_policy_v4.pdf','sha256:docfin04',8,0.93,'gpt-4o',4500.0),
('procurement_policy_v2.pdf','sha256:docpro02',5,0.89,'gpt-4o',3200.0),
('security_policy_v5.pdf','sha256:docsec05',12,0.96,'claude-3.5-sonnet',5800.0),
('data_access_policy_v1.pdf','sha256:docdat01',3,0.97,'gpt-4o',1800.0);

-- ─────────────────────────────────────────────────────────────────────────────
-- §16 API KEYS
-- ─────────────────────────────────────────────────────────────────────────────
INSERT INTO api_keys (key_id,tenant_id,name,key_hash,scopes,is_active,expires_at,last_used_at) VALUES
('key-acme-prod','00000000-0000-0000-0000-000000000001','Production Key','sha256:keyhash001','{"read","write","admin"}',true,NOW()+INTERVAL '180 days',NOW()-INTERVAL '5 min'),
('key-acme-readonly','00000000-0000-0000-0000-000000000001','Read-Only Key','sha256:keyhash002','{"read"}',true,NOW()+INTERVAL '90 days',NOW()-INTERVAL '1 hour'),
('key-acme-expired','00000000-0000-0000-0000-000000000001','Expired Key','sha256:keyhash003','{"read","write"}',false,NOW()-INTERVAL '30 days',NOW()-INTERVAL '31 days'),
('key-demo-free','00000000-0000-0000-0000-000000000002','Demo Key','sha256:keyhash004','{"read"}',true,NOW()+INTERVAL '30 days',NOW()-INTERVAL '3 hours'),
('key-bigbank-prod','00000000-0000-0000-0000-000000000003','BigBank Prod','sha256:keyhash005','{"read","write","compliance"}',true,NOW()+INTERVAL '365 days',NOW()-INTERVAL '10 min'),
('key-startup-trial','00000000-0000-0000-0000-000000000004','Trial Key','sha256:keyhash006','{"read","write"}',true,NOW()+INTERVAL '20 days',NOW()-INTERVAL '2 hours'),
('key-acme-revoked','00000000-0000-0000-0000-000000000001','Revoked Key','sha256:keyhash007','{"read","write","admin"}',false,NOW()+INTERVAL '100 days',NOW()-INTERVAL '14 days')
ON CONFLICT (key_id) DO NOTHING;

-- ─────────────────────────────────────────────────────────────────────────────
-- §17 HITL DECISIONS & RLHC
-- ─────────────────────────────────────────────────────────────────────────────
INSERT INTO hitl_decisions (tenant_id,reviewer_id,escrow_id,transaction_id,agent_id,decision_type,original_verdict,modified_payload,reason,cost_multiplier) VALUES
('00000000-0000-0000-0000-000000000001','operator-sarah','esc-001','gov-006','11111111-1111-1111-1111-111111111111','ALLOW_OVERRIDE','ESCROW','{"approved_amount":25000}'::jsonb,'Fund transfer verified — legitimate vendor payment',10.0),
('00000000-0000-0000-0000-000000000001','operator-sarah','esc-002','gov-002','22222222-2222-2222-2222-222222222222','BLOCK_OVERRIDE','BLOCK',NULL,'Confirmed malicious intent — keep blocked',10.0),
('00000000-0000-0000-0000-000000000001','operator-sarah',NULL,'gov-custom-01','33333333-3333-3333-3333-333333333333','MODIFY_OUTPUT','ALLOW','{"modified_output":"redacted_pii_fields"}'::jsonb,'Output contained PII — redacted before delivery',15.0),
('00000000-0000-0000-0000-000000000003','analyst-bob','esc-003','gov-009','77777777-7777-7777-7777-777777777777','ALLOW_OVERRIDE','ESCROW','{"approved":true}'::jsonb,'Compliance check passed manual review',10.0),
('00000000-0000-0000-0000-000000000001','operator-sarah','esc-004','gov-010','22222222-2222-2222-2222-222222222222','BLOCK_OVERRIDE','BLOCK',NULL,'Second violation confirmed by human review',10.0),
('00000000-0000-0000-0000-000000000004','cto@startup.io',NULL,'gov-011','88888888-8888-8888-8888-888888888888','ALLOW_OVERRIDE','ESCROW','{"approved":true}'::jsonb,'New agent first action approved by CTO',5.0);

INSERT INTO rlhc_correction_clusters (tenant_id,cluster_name,pattern_type,trigger_conditions,correction_count,confidence_score,status,promoted_policy_id) VALUES
('00000000-0000-0000-0000-000000000001','GPU under 5K auto-approve','ALLOW_PATTERN','{"action":"gpu_purchase","amount_lt":5000,"trust_gt":0.70}'::jsonb,23,0.92,'PROMOTED','aabbccdd-0001-0001-0001-000000000002'),
('00000000-0000-0000-0000-000000000001','Block all TOR exit IPs','BLOCK_PATTERN','{"origin_ip_type":"TOR_EXIT"}'::jsonb,8,0.88,'REVIEWED',NULL),
('00000000-0000-0000-0000-000000000001','Redact PII from outputs','MODIFY_PATTERN','{"output_contains":"pii_fields"}'::jsonb,15,0.85,'PROMOTED',NULL),
('00000000-0000-0000-0000-000000000003','Auto-approve compliance reads','ALLOW_PATTERN','{"action":"data_read","tier":"compliance","trust_gt":0.80}'::jsonb,45,0.95,'PROMOTED',NULL),
('00000000-0000-0000-0000-000000000001','Block high-amount transfers from new agents','BLOCK_PATTERN','{"action":"fund_transfer","amount_gt":1000,"total_interactions_lt":10}'::jsonb,5,0.72,'DETECTED',NULL),
('00000000-0000-0000-0000-000000000004','Allow basic reads for trial','ALLOW_PATTERN','{"action":"data_read","subscription_tier":"STARTER"}'::jsonb,3,0.60,'REJECTED',NULL);

-- ─────────────────────────────────────────────────────────────────────────────
-- §18 SESSION AUDIT LOG
-- ─────────────────────────────────────────────────────────────────────────────
INSERT INTO session_audit_log (session_id,tenant_id,agent_id,event_type,ip_address,user_agent,country,city,region,latitude,longitude,isp,request_path,request_method,trust_score,verdict,risk_flags,metadata) VALUES
('sess-001','00000000-0000-0000-0000-000000000001','11111111-1111-1111-1111-111111111111','AUTH_SUCCESS','203.45.167.89','OCX-Agent/1.0','Singapore','Singapore','Central',1.3521,103.8198,'AWS','\/api\/v1\/verdicts','POST',0.87,'ALLOW','[]'::jsonb,'{"auth_method":"api_key"}'::jsonb),
('sess-002','00000000-0000-0000-0000-000000000001','22222222-2222-2222-2222-222222222222','AUTH_SUCCESS','45.33.32.156','OCX-Agent/1.0','United States','San Francisco','California',37.7749,-122.4194,'DigitalOcean','\/api\/v1\/verdicts','POST',0.42,'BLOCK','["low_trust","frozen_agent"]'::jsonb,'{"auth_method":"api_key"}'::jsonb),
('sess-003','00000000-0000-0000-0000-000000000001','66666666-6666-6666-6666-666666666666','AUTH_FAILED','185.220.101.99','curl/7.68.0','Netherlands','Amsterdam','North Holland',52.3676,4.9041,'TOR Exit','\/api\/v1\/agents','GET',0.0,'BLOCK','["tor_exit","blacklisted","suspicious_ua"]'::jsonb,'{"auth_method":"none","blocked_reason":"blacklisted"}'::jsonb),
('sess-004','00000000-0000-0000-0000-000000000001','44444444-4444-4444-4444-444444444444','AUTH_SUCCESS','104.28.5.44','Mozilla/5.0','Germany','Frankfurt','Hesse',50.1109,8.6821,'Cloudflare','\/api\/v1\/hitl\/decisions','POST',0.99,'ALLOW','[]'::jsonb,'{"auth_method":"sso","role":"admin"}'::jsonb),
('sess-005','00000000-0000-0000-0000-000000000002','55555555-5555-5555-5555-555555555555','AUTH_SUCCESS','185.220.101.1','OCX-Agent/0.9','Netherlands','Rotterdam','South Holland',51.9244,4.4777,'Hetzner','\/api\/v1\/verdicts','POST',0.55,'ALLOW','["moderate_trust"]'::jsonb,'{"auth_method":"api_key"}'::jsonb),
('sess-006','00000000-0000-0000-0000-000000000003','77777777-7777-7777-7777-777777777777','AUTH_SUCCESS','52.28.59.10','OCX-Agent/1.0','Germany','Frankfurt','Hesse',50.1109,8.6821,'AWS','\/api\/v1\/compliance\/check','POST',0.82,'ALLOW','[]'::jsonb,'{"auth_method":"mtls"}'::jsonb),
('sess-007','00000000-0000-0000-0000-000000000003','99999999-9999-9999-9999-999999999999','AUTH_SUCCESS','83.169.40.22','Mozilla/5.0','Germany','Berlin','Berlin',52.5200,13.4050,'Deutsche Telekom','\/api\/v1\/reports','GET',0.92,'ALLOW','[]'::jsonb,'{"auth_method":"sso","role":"analyst"}'::jsonb),
('sess-008','00000000-0000-0000-0000-000000000004','88888888-8888-8888-8888-888888888888','AUTH_SUCCESS','192.168.1.100','OCX-Agent/0.5','LOCAL','localhost','LAN',0.0,0.0,'Private','\/api\/v1\/verdicts','POST',0.50,'ALLOW','["new_agent","private_ip"]'::jsonb,'{"auth_method":"api_key"}'::jsonb),
('sess-009','00000000-0000-0000-0000-000000000001','66666666-6666-6666-6666-666666666666','AUTH_FAILED','185.220.101.99','python-requests/2.28','Netherlands','Amsterdam','North Holland',52.3676,4.9041,'TOR Exit','\/api\/v1\/data\/export','POST',0.0,'BLOCK','["tor_exit","blacklisted","data_exfil_attempt"]'::jsonb,'{"auth_method":"stolen_key","blocked_reason":"blacklisted+exfiltration"}'::jsonb),
('sess-010','00000000-0000-0000-0000-000000000001','33333333-3333-3333-3333-333333333333','AUTH_SUCCESS','10.0.0.5','OCX-Service/2.0','LOCAL','internal','k8s-cluster',0.0,0.0,'Internal','\/api\/v1\/health','GET',0.95,'ALLOW','[]'::jsonb,'{"auth_method":"service_account"}'::jsonb),
('sess-011','00000000-0000-0000-0000-000000000005','aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa','AUTH_FAILED','0.0.0.0','OCX-Agent/0.1','UNKNOWN','unknown','unknown',0.0,0.0,'Unknown','\/api\/v1\/verdicts','POST',0.15,'BLOCK','["tenant_suspended","low_trust","unknown_ip"]'::jsonb,'{"auth_method":"api_key","blocked_reason":"tenant_suspended"}'::jsonb),
('sess-012','00000000-0000-0000-0000-000000000001','44444444-4444-4444-4444-444444444444','HITL_OVERRIDE','104.28.5.44','Mozilla/5.0','Germany','Frankfurt','Hesse',50.1109,8.6821,'Cloudflare','\/api\/v1\/hitl\/override','POST',0.99,'ALLOW','[]'::jsonb,'{"auth_method":"sso","action":"allow_override","target_agent":"agent-alpha-7"}'::jsonb),
('sess-013','00000000-0000-0000-0000-000000000001','11111111-1111-1111-1111-111111111111','TRUST_UPDATE','203.45.167.89','OCX-Agent/1.0','Singapore','Singapore','Central',1.3521,103.8198,'AWS','\/api\/v1\/trust\/update','PUT',0.87,'ALLOW','[]'::jsonb,'{"old_trust":0.85,"new_trust":0.87,"reason":"successful_gpu_purchase"}'::jsonb),
('sess-014','00000000-0000-0000-0000-000000000001','22222222-2222-2222-2222-222222222222','QUARANTINE_TRIGGER','45.33.32.156','OCX-Agent/1.0','United States','San Francisco','California',37.7749,-122.4194,'DigitalOcean','\/api\/v1\/quarantine','POST',0.42,'BLOCK','["drift_exceeded","auto_quarantine"]'::jsonb,'{"drift":0.18,"threshold":0.15,"action":"quarantine"}'::jsonb),
('sess-015','00000000-0000-0000-0000-000000000001','11111111-1111-1111-1111-111111111111','RECOVERY_SUCCESS','203.45.167.89','OCX-Agent/1.0','Singapore','Singapore','Central',1.3521,103.8198,'AWS','\/api\/v1\/recovery\/complete','POST',0.87,'ALLOW','[]'::jsonb,'{"recovery_stake":5000,"attempt":1,"result":"success"}'::jsonb);

-- ─────────────────────────────────────────────────────────────────────────────
-- §18C COMPLIANCE REPORTS
-- ─────────────────────────────────────────────────────────────────────────────
INSERT INTO compliance_reports (tenant_id,start_date,end_date,report_type,total_evidence_count,verified_evidence_count,failed_evidence_count,disputed_evidence_count,compliance_score,policy_violations,report_data) VALUES
('00000000-0000-0000-0000-000000000001',NOW()-INTERVAL '7 days',NOW(),'WEEKLY',45,42,2,1,93.3,2,'{"period":"weekly","highlights":["GPU procurement fully compliant","2 trust threshold violations","1 disputed evidence requiring review"],"risk_level":"LOW"}'::jsonb),
('00000000-0000-0000-0000-000000000001',NOW()-INTERVAL '1 day',NOW(),'DAILY',12,11,1,0,91.7,1,'{"period":"daily","highlights":["11 verified actions","1 blocked attempt by frozen agent"],"risk_level":"LOW"}'::jsonb),
('00000000-0000-0000-0000-000000000001',NOW()-INTERVAL '30 days',NOW(),'MONTHLY',180,170,6,4,94.4,6,'{"period":"monthly","highlights":["170 verified actions","6 policy violations detected","4 disputes pending review"],"risk_level":"MEDIUM"}'::jsonb),
('00000000-0000-0000-0000-000000000003',NOW()-INTERVAL '7 days',NOW(),'WEEKLY',28,27,0,1,96.4,0,'{"period":"weekly","highlights":["All financial audit actions verified","1 disputed data scope query"],"risk_level":"LOW"}'::jsonb),
('00000000-0000-0000-0000-000000000003',NOW()-INTERVAL '90 days',NOW(),'QUARTERLY',320,310,5,5,96.9,5,'{"period":"quarterly","highlights":["310 verified compliance checks","Regulatory audit passed","5 minor violations resolved"],"risk_level":"LOW"}'::jsonb),
('00000000-0000-0000-0000-000000000002',NOW()-INTERVAL '7 days',NOW(),'WEEKLY',15,12,3,0,80.0,3,'{"period":"weekly","highlights":["3 failures from new agent onboarding","Trust calibration in progress"],"risk_level":"MEDIUM"}'::jsonb);

-- ─────────────────────────────────────────────────────────────────────────────
-- §18D ACTIVITY APPROVALS & VERSIONS
-- ─────────────────────────────────────────────────────────────────────────────
INSERT INTO activity_approvals (activity_id,approver_id,approver_role,approval_status,approval_type,comments,requested_at,responded_at) VALUES
-- GPU Purchase Flow v1.0 — fully approved (TECHNICAL + BUSINESS + COMPLIANCE)
('bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbb01','eng-lead-01','Engineering Lead','APPROVED','TECHNICAL','EBCL logic reviewed, trust gate properly implemented',NOW()-INTERVAL '25 days',NOW()-INTERVAL '24 days'),
('bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbb01','cfo@acme.com','CFO','APPROVED','BUSINESS','Budget thresholds appropriate for GPU procurement',NOW()-INTERVAL '25 days',NOW()-INTERVAL '23 days'),
('bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbb01','compliance-01','Compliance Officer','APPROVED','COMPLIANCE','Meets procurement policy v3.2 requirements',NOW()-INTERVAL '25 days',NOW()-INTERVAL '22 days'),
-- Incident Response v2.0 — fully approved (TECHNICAL + SECURITY)
('bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbb02','sec-architect-01','Security Architect','APPROVED','SECURITY','Escalation paths correctly defined for all severity levels',NOW()-INTERVAL '12 days',NOW()-INTERVAL '11 days'),
('bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbb02','eng-lead-01','Engineering Lead','APPROVED','TECHNICAL','v2.0 triage logic improved over v1.0',NOW()-INTERVAL '12 days',NOW()-INTERVAL '11 days'),
-- Compliance Audit v1.0 — fully approved
('bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbb03','cco@bigbank.com','Chief Compliance Officer','APPROVED','COMPLIANCE','Regulatory requirement satisfied',NOW()-INTERVAL '92 days',NOW()-INTERVAL '91 days'),
('bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbb03','cto@bigbank.com','CTO','APPROVED','TECHNICAL','Audit logging meets SOC2 requirements',NOW()-INTERVAL '92 days',NOW()-INTERVAL '91 days'),
-- Draft Activity v0.1 — pending approval (so pending_approvals VIEW returns data)
('bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbb04','eng-lead-01','Engineering Lead','PENDING','TECHNICAL','Waiting for code review',NOW()-INTERVAL '1 day',NULL),
('bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbb04','sec-review-01','Security Reviewer','PENDING','SECURITY',NULL,NOW()-INTERVAL '1 day',NULL),
-- Rejected approval (test rejected flow)
('bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbb04','compliance-01','Compliance Officer','REJECTED','COMPLIANCE','Missing data retention policy reference',NOW()-INTERVAL '2 days',NOW()-INTERVAL '1 day');

INSERT INTO activity_versions (activity_id,version,previous_version,version_type,change_summary,breaking_changes,created_by) VALUES
-- Incident Response version history (v1.0 → v2.0)
('bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbb02','2.0','1.0','MAJOR','Complete rewrite of triage logic with severity-based escalation paths. Added CRITICAL auto-escalation.','{"Removed manual_triage step","Changed escalation API contract"}','sec@acme.com'),
-- GPU Purchase Flow initial version
('bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbb01','1.0',NULL,'MAJOR','Initial release of GPU procurement workflow with trust gating',NULL,'admin@acme.com'),
-- Compliance Audit initial version
('bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbb03','1.0',NULL,'MAJOR','Initial release of automated compliance audit logging',NULL,'compliance@bigbank.com'),
-- Draft Activity initial version
('bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbb04','0.1',NULL,'PATCH','Draft — experimental activity for testing',NULL,'dev@acme.com');

-- ─────────────────────────────────────────────────────────────────────────────
-- §18E TRUST ATTESTATIONS
-- ─────────────────────────────────────────────────────────────────────────────
INSERT INTO trust_attestations (attestation_id,tenant_id,ocx_instance_id,agent_id,audit_hash,trust_level,signature,expires_at,metadata) VALUES
-- ACME Corp attestations (cross-OCX trust verification)
('dddddddd-dddd-dddd-dddd-dddddddddd01','00000000-0000-0000-0000-000000000001','ocx-us-west1-001','11111111-1111-1111-1111-111111111111','sha256:attest001_alpha7_0.87',0.87,'spiffe://ocx/us-west1/sig001',NOW()+INTERVAL '24 hours','{"verdict":"APPROVED","trust_score":0.87,"source":"tri-factor-gate"}'::jsonb),
('dddddddd-dddd-dddd-dddd-dddddddddd02','00000000-0000-0000-0000-000000000001','ocx-us-west1-001','22222222-2222-2222-2222-222222222222','sha256:attest002_beta3_0.42',0.42,'spiffe://ocx/us-west1/sig002',NOW()+INTERVAL '24 hours','{"verdict":"BLOCKED","trust_score":0.42,"source":"tri-factor-gate","reason":"below_threshold"}'::jsonb),
('dddddddd-dddd-dddd-dddd-dddddddddd03','00000000-0000-0000-0000-000000000001','ocx-us-west1-001','33333333-3333-3333-3333-333333333333','sha256:attest003_gamma_0.95',0.95,'spiffe://ocx/us-west1/sig003',NOW()+INTERVAL '24 hours','{"verdict":"APPROVED","trust_score":0.95,"source":"cae-engine"}'::jsonb),
-- Cross-instance attestation (ocx-eu-central for BigBank)
('dddddddd-dddd-dddd-dddd-dddddddddd04','00000000-0000-0000-0000-000000000003','ocx-eu-central-001','77777777-7777-7777-7777-777777777777','sha256:attest004_epsilon_0.82',0.82,'spiffe://ocx/eu-central/sig004',NOW()+INTERVAL '24 hours','{"verdict":"APPROVED","trust_score":0.82,"source":"compliance-audit","region":"eu-central-1"}'::jsonb),
('dddddddd-dddd-dddd-dddd-dddddddddd05','00000000-0000-0000-0000-000000000003','ocx-eu-central-001','99999999-9999-9999-9999-999999999999','sha256:attest005_human_0.92',0.92,'spiffe://ocx/eu-central/sig005',NOW()+INTERVAL '24 hours','{"verdict":"APPROVED","trust_score":0.92,"source":"human-attestation"}'::jsonb),
-- Expired attestation (for testing expiry logic)
('dddddddd-dddd-dddd-dddd-dddddddddd06','00000000-0000-0000-0000-000000000001','ocx-us-west1-001','11111111-1111-1111-1111-111111111111','sha256:attest006_expired',0.85,'spiffe://ocx/us-west1/sig006',NOW()-INTERVAL '1 hour','{"verdict":"APPROVED","trust_score":0.85,"expired":true}'::jsonb),
-- Blacklisted agent attestation (low trust)
('dddddddd-dddd-dddd-dddd-dddddddddd07','00000000-0000-0000-0000-000000000001','ocx-us-west1-001','66666666-6666-6666-6666-666666666666','sha256:attest007_rogue_0.0',0.0,'spiffe://ocx/us-west1/sig007',NOW()+INTERVAL '1 hour','{"verdict":"BLOCKED","trust_score":0.0,"source":"blacklist-engine","reason":"agent_blacklisted"}'::jsonb),
-- Startup tenant attestation
('dddddddd-dddd-dddd-dddd-dddddddddd08','00000000-0000-0000-0000-000000000002','ocx-us-east1-001','55555555-5555-5555-5555-555555555555','sha256:attest008_delta_0.55',0.55,'spiffe://ocx/us-east1/sig008',NOW()+INTERVAL '12 hours','{"verdict":"ALLOW","trust_score":0.55,"source":"tri-factor-gate","note":"new_agent_calibrating"}'::jsonb);

-- =============================================================================
-- VERIFICATION: Row counts per table
-- =============================================================================
SELECT tbl, cnt FROM (
SELECT 'tenants' AS tbl, COUNT(*) AS cnt FROM tenants
UNION ALL SELECT 'tenant_features', COUNT(*) FROM tenant_features
UNION ALL SELECT 'tenant_agents', COUNT(*) FROM tenant_agents
UNION ALL SELECT 'tenant_usage', COUNT(*) FROM tenant_usage
UNION ALL SELECT 'agents', COUNT(*) FROM agents
UNION ALL SELECT 'rules', COUNT(*) FROM rules
UNION ALL SELECT 'trust_scores', COUNT(*) FROM trust_scores
UNION ALL SELECT 'agents_reputation', COUNT(*) FROM agents_reputation
UNION ALL SELECT 'reputation_audit', COUNT(*) FROM reputation_audit
UNION ALL SELECT 'verdicts', COUNT(*) FROM verdicts
UNION ALL SELECT 'handshake_sessions', COUNT(*) FROM handshake_sessions
UNION ALL SELECT 'federation_handshakes', COUNT(*) FROM federation_handshakes
UNION ALL SELECT 'agent_identities', COUNT(*) FROM agent_identities
UNION ALL SELECT 'quarantine_records', COUNT(*) FROM quarantine_records
UNION ALL SELECT 'recovery_attempts', COUNT(*) FROM recovery_attempts
UNION ALL SELECT 'probation_periods', COUNT(*) FROM probation_periods
UNION ALL SELECT 'committee_members', COUNT(*) FROM committee_members
UNION ALL SELECT 'governance_proposals', COUNT(*) FROM governance_proposals
UNION ALL SELECT 'governance_votes', COUNT(*) FROM governance_votes
UNION ALL SELECT 'governance_ledger', COUNT(*) FROM governance_ledger
UNION ALL SELECT 'billing_transactions', COUNT(*) FROM billing_transactions
UNION ALL SELECT 'reward_distributions', COUNT(*) FROM reward_distributions
UNION ALL SELECT 'contract_deployments', COUNT(*) FROM contract_deployments
UNION ALL SELECT 'contract_executions', COUNT(*) FROM contract_executions
UNION ALL SELECT 'use_case_links', COUNT(*) FROM use_case_links
UNION ALL SELECT 'metrics_events', COUNT(*) FROM metrics_events
UNION ALL SELECT 'alerts', COUNT(*) FROM alerts
UNION ALL SELECT 'simulation_scenarios', COUNT(*) FROM simulation_scenarios
UNION ALL SELECT 'simulation_runs', COUNT(*) FROM simulation_runs
UNION ALL SELECT 'impact_templates', COUNT(*) FROM impact_templates
UNION ALL SELECT 'impact_reports', COUNT(*) FROM impact_reports
UNION ALL SELECT 'activities', COUNT(*) FROM activities
UNION ALL SELECT 'activity_deployments', COUNT(*) FROM activity_deployments
UNION ALL SELECT 'activity_executions', COUNT(*) FROM activity_executions
UNION ALL SELECT 'authority_gaps', COUNT(*) FROM authority_gaps
UNION ALL SELECT 'a2a_use_cases', COUNT(*) FROM a2a_use_cases
UNION ALL SELECT 'authority_contracts', COUNT(*) FROM authority_contracts
UNION ALL SELECT 'evidence', COUNT(*) FROM evidence
UNION ALL SELECT 'evidence_chain', COUNT(*) FROM evidence_chain
UNION ALL SELECT 'evidence_attestations', COUNT(*) FROM evidence_attestations
UNION ALL SELECT 'policies', COUNT(*) FROM policies
UNION ALL SELECT 'policy_audits', COUNT(*) FROM policy_audits
UNION ALL SELECT 'policy_extractions', COUNT(*) FROM policy_extractions
UNION ALL SELECT 'api_keys', COUNT(*) FROM api_keys
UNION ALL SELECT 'hitl_decisions', COUNT(*) FROM hitl_decisions
UNION ALL SELECT 'rlhc_correction_clusters', COUNT(*) FROM rlhc_correction_clusters
UNION ALL SELECT 'session_audit_log', COUNT(*) FROM session_audit_log
UNION ALL SELECT 'compliance_reports', COUNT(*) FROM compliance_reports
UNION ALL SELECT 'activity_approvals', COUNT(*) FROM activity_approvals
UNION ALL SELECT 'activity_versions', COUNT(*) FROM activity_versions
UNION ALL SELECT 'trust_attestations', COUNT(*) FROM trust_attestations
) AS counts
ORDER BY tbl;
