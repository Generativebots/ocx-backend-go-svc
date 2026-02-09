// Package marketplace — TemplateManager (Phase B.3 + Phase E built-ins)
//
// Manages governance template CRUD and built-in template seeding.
// All 5 templates from Phase E are seeded as built-in items.
package marketplace

import (
	"fmt"
	"time"
)

// TemplateManager handles template CRUD and built-in seeding.
type TemplateManager struct {
	svc *Service
}

// NewTemplateManager creates a new template manager.
func NewTemplateManager(svc *Service) *TemplateManager {
	return &TemplateManager{svc: svc}
}

// PublishTemplate allows a tenant to publish a template.
func (tm *TemplateManager) PublishTemplate(tenantID string, t *Template) error {
	tm.svc.mu.Lock()
	defer tm.svc.mu.Unlock()

	if _, exists := tm.svc.templates[t.ID]; exists {
		return fmt.Errorf("template %s already exists", t.ID)
	}

	t.PublisherID = tenantID
	t.CreatedAt = time.Now()
	tm.svc.templates[t.ID] = t

	tm.svc.logger.Printf("Published template: %s by tenant %s", t.Name, tenantID)
	return nil
}

// UpdateTemplate updates an existing template (only owner can update).
func (tm *TemplateManager) UpdateTemplate(tenantID string, t *Template) error {
	tm.svc.mu.Lock()
	defer tm.svc.mu.Unlock()

	existing, ok := tm.svc.templates[t.ID]
	if !ok {
		return fmt.Errorf("template %s not found", t.ID)
	}
	if existing.PublisherID != tenantID && existing.PublisherID != "ocx-system" {
		return fmt.Errorf("tenant %s not authorized to update template %s", tenantID, t.ID)
	}

	t.PublisherID = existing.PublisherID
	t.CreatedAt = existing.CreatedAt
	tm.svc.templates[t.ID] = t
	return nil
}

// SeedBuiltins loads all Phase E built-in templates.
func (tm *TemplateManager) SeedBuiltins() {
	builtins := []*Template{
		// =========================================================
		// GDPR Data Request — Compliance
		// =========================================================
		{
			ID:             "tmpl-gdpr-dsar",
			Name:           "GDPR Data Subject Access Request",
			Slug:           "gdpr-dsar",
			Description:    "Automated DSAR workflow. Receives data subject requests, identifies data across systems, compiles a response package, and ensures 30-day SLA compliance with full audit trail.",
			Category:       CatCompliance,
			PublisherID:    "ocx-system",
			PublisherName:  "OCX Built-in",
			Version:        "2.0.0",
			IsVerified:     true,
			IsPublic:       true,
			IsBuiltIn:      true,
			InstallCount:   1834,
			Rating:         4.9,
			PricingTier:    PricingPro,
			OneTimeCredits: 200,
			StepCount:      8,
			IndustryTags:   []string{"financial-services", "healthcare", "technology", "retail"},
			Dependencies:   []string{"conn-http-rest"},
			EBCLDefinition: map[string]interface{}{
				"steps": []interface{}{
					map[string]interface{}{"name": "receive_request", "type": "trigger", "description": "Intake DSAR via email, portal, or API"},
					map[string]interface{}{"name": "verify_identity", "type": "action", "description": "Verify data subject identity", "risk": "medium"},
					map[string]interface{}{"name": "scan_systems", "type": "action", "description": "Scan all connected systems for PII", "risk": "low"},
					map[string]interface{}{"name": "compile_data", "type": "action", "description": "Compile all data into response package", "risk": "low"},
					map[string]interface{}{"name": "legal_review", "type": "approval", "description": "Legal team review of response", "risk": "medium"},
					map[string]interface{}{"name": "dpo_approval", "type": "approval", "description": "DPO final approval", "risk": "high"},
					map[string]interface{}{"name": "deliver_response", "type": "action", "description": "Deliver response to data subject", "risk": "medium"},
					map[string]interface{}{"name": "close_and_audit", "type": "action", "description": "Close request and generate audit record", "risk": "low"},
				},
				"sla_days":    30,
				"regulations": []string{"GDPR Art. 15", "CCPA §1798.100"},
			},
		},

		// =========================================================
		// SOC2 Evidence Collection — Audit
		// =========================================================
		{
			ID:             "tmpl-soc2-evidence",
			Name:           "SOC2 Evidence Collection",
			Slug:           "soc2-evidence",
			Description:    "Monthly evidence gathering workflow. Automatically collects access reviews, change logs, vulnerability scans, and policy attestations. Maps evidence to Trust Service Criteria.",
			Category:       CatAudit,
			PublisherID:    "ocx-system",
			PublisherName:  "OCX Built-in",
			Version:        "1.3.0",
			IsVerified:     true,
			IsPublic:       true,
			IsBuiltIn:      true,
			InstallCount:   2156,
			Rating:         4.8,
			PricingTier:    PricingPro,
			OneTimeCredits: 300,
			StepCount:      6,
			IndustryTags:   []string{"technology", "saas", "fintech"},
			Dependencies:   []string{"conn-jira", "conn-slack"},
			EBCLDefinition: map[string]interface{}{
				"steps": []interface{}{
					map[string]interface{}{"name": "trigger_collection", "type": "cron", "description": "Monthly collection trigger (1st of month)", "schedule": "0 0 1 * *"},
					map[string]interface{}{"name": "collect_access_reviews", "type": "action", "description": "Pull access review logs from IAM systems", "risk": "low"},
					map[string]interface{}{"name": "collect_change_logs", "type": "action", "description": "Pull change management records", "risk": "low"},
					map[string]interface{}{"name": "run_vuln_scan", "type": "action", "description": "Trigger and collect vulnerability scan results", "risk": "medium"},
					map[string]interface{}{"name": "map_to_tsc", "type": "action", "description": "Map evidence to Trust Service Criteria", "risk": "low"},
					map[string]interface{}{"name": "generate_package", "type": "action", "description": "Generate evidence package for auditor", "risk": "low"},
				},
				"trust_service_criteria": []string{"CC6.1", "CC6.2", "CC6.3", "CC7.1", "CC7.2", "CC8.1"},
			},
		},

		// =========================================================
		// HIPAA Access Review — Healthcare
		// =========================================================
		{
			ID:             "tmpl-hipaa-access",
			Name:           "HIPAA Access Review",
			Slug:           "hipaa-access-review",
			Description:    "Quarterly access reviews for HIPAA compliance. Reviews all PHI access privileges, identifies excessive permissions, generates remediation tasks, and produces compliance reports.",
			Category:       CatHealthcare,
			PublisherID:    "ocx-system",
			PublisherName:  "OCX Built-in",
			Version:        "1.1.0",
			IsVerified:     true,
			IsPublic:       true,
			IsBuiltIn:      true,
			InstallCount:   987,
			Rating:         4.7,
			PricingTier:    PricingEnterprise,
			OneTimeCredits: 500,
			StepCount:      7,
			IndustryTags:   []string{"healthcare", "pharma", "insurance"},
			Dependencies:   []string{"conn-http-rest", "conn-servicenow"},
			EBCLDefinition: map[string]interface{}{
				"steps": []interface{}{
					map[string]interface{}{"name": "trigger_review", "type": "cron", "description": "Quarterly review trigger", "schedule": "0 0 1 */3 *"},
					map[string]interface{}{"name": "pull_access_lists", "type": "action", "description": "Pull access lists from EHR, databases, file shares", "risk": "low"},
					map[string]interface{}{"name": "analyze_privileges", "type": "action", "description": "Analyze for excessive or inappropriate access", "risk": "low"},
					map[string]interface{}{"name": "flag_violations", "type": "action", "description": "Flag potential HIPAA violations", "risk": "medium"},
					map[string]interface{}{"name": "manager_review", "type": "approval", "description": "Manager review of flagged access", "risk": "medium"},
					map[string]interface{}{"name": "remediate", "type": "action", "description": "Auto-remediate or create tickets for access removal", "risk": "high"},
					map[string]interface{}{"name": "generate_report", "type": "action", "description": "Generate HIPAA compliance report", "risk": "low"},
				},
				"regulations": []string{"HIPAA §164.312(a)(1)", "HIPAA §164.308(a)(3)"},
			},
		},

		// =========================================================
		// Financial Approval Chain — Governance
		// =========================================================
		{
			ID:             "tmpl-financial-approval",
			Name:           "Financial Approval Chain",
			Slug:           "financial-approval-chain",
			Description:    "Multi-level spend approval workflow with configurable thresholds. Routes approvals based on amount, cost center, and organizational hierarchy. Includes budget impact analysis.",
			Category:       CatGovernance,
			PublisherID:    "ocx-system",
			PublisherName:  "OCX Built-in",
			Version:        "1.5.0",
			IsVerified:     true,
			IsPublic:       true,
			IsBuiltIn:      true,
			InstallCount:   1543,
			Rating:         4.6,
			PricingTier:    PricingStarter,
			OneTimeCredits: 100,
			StepCount:      6,
			IndustryTags:   []string{"financial-services", "enterprise", "manufacturing"},
			Dependencies:   []string{"conn-sap", "conn-slack"},
			EBCLDefinition: map[string]interface{}{
				"steps": []interface{}{
					map[string]interface{}{"name": "submit_request", "type": "trigger", "description": "Submit purchase/spend request", "risk": "low"},
					map[string]interface{}{"name": "budget_check", "type": "action", "description": "Check budget availability and impact", "risk": "low"},
					map[string]interface{}{"name": "level_1_approval", "type": "approval", "description": "Direct manager approval (<$10K)", "risk": "medium"},
					map[string]interface{}{"name": "level_2_approval", "type": "approval", "description": "Director approval ($10K–$100K)", "risk": "medium", "condition": "amount >= 10000"},
					map[string]interface{}{"name": "level_3_approval", "type": "approval", "description": "VP/CFO approval (>$100K)", "risk": "high", "condition": "amount >= 100000"},
					map[string]interface{}{"name": "execute_payment", "type": "action", "description": "Post to ERP and execute payment", "risk": "high"},
				},
				"thresholds": map[string]interface{}{
					"level_1": 10000,
					"level_2": 100000,
					"level_3": 1000000,
				},
			},
		},

		// =========================================================
		// Incident Response — Security
		// =========================================================
		{
			ID:             "tmpl-incident-response",
			Name:           "Incident Response Playbook",
			Slug:           "incident-response",
			Description:    "Auto-escalation playbook for security incidents. Detects incidents, triages severity, notifies on-call, creates war rooms, tracks remediation, and generates post-mortem reports.",
			Category:       CatSecurity,
			PublisherID:    "ocx-system",
			PublisherName:  "OCX Built-in",
			Version:        "2.2.0",
			IsVerified:     true,
			IsPublic:       true,
			IsBuiltIn:      true,
			InstallCount:   2678,
			Rating:         4.9,
			PricingTier:    PricingFree,
			OneTimeCredits: 0,
			StepCount:      8,
			IndustryTags:   []string{"technology", "financial-services", "healthcare", "government"},
			Dependencies:   []string{"conn-slack", "conn-jira"},
			EBCLDefinition: map[string]interface{}{
				"steps": []interface{}{
					map[string]interface{}{"name": "detect_incident", "type": "trigger", "description": "Incident detection via SIEM/alert", "risk": "low"},
					map[string]interface{}{"name": "triage", "type": "action", "description": "Auto-triage severity (P1–P4)", "risk": "low"},
					map[string]interface{}{"name": "notify_oncall", "type": "action", "description": "Page on-call engineer via PagerDuty/Slack", "risk": "low"},
					map[string]interface{}{"name": "create_war_room", "type": "action", "description": "Create dedicated Slack channel and Jira epic", "risk": "medium"},
					map[string]interface{}{"name": "investigate", "type": "manual", "description": "Investigation phase with runbook guidance", "risk": "medium"},
					map[string]interface{}{"name": "remediate", "type": "action", "description": "Execute remediation actions", "risk": "high"},
					map[string]interface{}{"name": "verify_resolution", "type": "approval", "description": "Verify incident is resolved", "risk": "medium"},
					map[string]interface{}{"name": "post_mortem", "type": "action", "description": "Generate post-mortem report and action items", "risk": "low"},
				},
				"severity_levels": []string{"P1-Critical", "P2-High", "P3-Medium", "P4-Low"},
				"sla_minutes": map[string]interface{}{
					"P1": 15,
					"P2": 60,
					"P3": 240,
					"P4": 1440,
				},
			},
		},
	}

	tm.svc.mu.Lock()
	defer tm.svc.mu.Unlock()

	for _, t := range builtins {
		t.CreatedAt = time.Now()
		tm.svc.templates[t.ID] = t
	}
}
