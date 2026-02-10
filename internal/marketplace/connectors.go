// Package marketplace — ConnectorManager (Phase B.2 + Phase D built-ins)
//
// Manages connector registration, publishing, and built-in connector seeding.
// All 7 connectors from Phase D are seeded as built-in items.
package marketplace

import (
	"fmt"
	"time"
)

// ConnectorManager handles connector CRUD and built-in seeding.
type ConnectorManager struct {
	svc *Service
}

// NewConnectorManager creates a new connector manager.
func NewConnectorManager(svc *Service) *ConnectorManager {
	return &ConnectorManager{svc: svc}
}

// PublishConnector allows a tenant to publish a connector to the marketplace.
func (cm *ConnectorManager) PublishConnector(tenantID string, c *Connector) error {
	cm.svc.mu.Lock()
	defer cm.svc.mu.Unlock()

	if _, exists := cm.svc.connectors[c.ID]; exists {
		return fmt.Errorf("connector %s already exists", c.ID)
	}

	c.PublisherID = tenantID
	c.CreatedAt = time.Now()
	cm.svc.connectors[c.ID] = c

	cm.svc.logger.Printf("Published connector: %s by tenant %s", c.Name, tenantID)
	return nil
}

// UpdateConnector updates an existing connector (only owner can update).
func (cm *ConnectorManager) UpdateConnector(tenantID string, c *Connector) error {
	cm.svc.mu.Lock()
	defer cm.svc.mu.Unlock()

	existing, ok := cm.svc.connectors[c.ID]
	if !ok {
		return fmt.Errorf("connector %s not found", c.ID)
	}
	if existing.PublisherID != tenantID && existing.PublisherID != "ocx-system" {
		return fmt.Errorf("tenant %s not authorized to update connector %s", tenantID, c.ID)
	}

	c.PublisherID = existing.PublisherID
	c.CreatedAt = existing.CreatedAt
	cm.svc.connectors[c.ID] = c
	return nil
}

// SeedBuiltins loads all Phase D built-in connectors.
func (cm *ConnectorManager) SeedBuiltins() {
	builtins := []*Connector{
		// =========================================================
		// P0 — Salesforce
		// =========================================================
		{
			ID:             "conn-salesforce",
			Name:           "Salesforce",
			Slug:           "salesforce",
			Description:    "Full CRM integration via OAuth + REST API. Sync leads, opportunities, accounts, and custom objects with bidirectional field mapping.",
			Category:       CatCRM,
			PublisherID:    "ocx-system",
			PublisherName:  "OCX Built-in",
			IconURL:        "/icons/salesforce.svg",
			Version:        "2.1.0",
			IsVerified:     true,
			IsPublic:       true,
			IsBuiltIn:      true,
			InstallCount:   2847,
			Rating:         4.8,
			PricingTier:    PricingPro,
			MonthlyCredits: 500,
			ConfigSchema: map[string]interface{}{
				"client_id":     map[string]interface{}{"type": "string", "required": true, "label": "OAuth Client ID"},
				"client_secret": map[string]interface{}{"type": "secret", "required": true, "label": "OAuth Client Secret"},
				"instance_url":  map[string]interface{}{"type": "url", "required": true, "label": "Instance URL", "placeholder": "https://yourorg.my.salesforce.com"},
				"api_version":   map[string]interface{}{"type": "string", "default": "v58.0", "label": "API Version"},
			},
			Actions: []ConnectorAction{
				{Name: "create_lead", Description: "Create a new lead in Salesforce", RiskLevel: "medium", InputSchema: map[string]interface{}{"first_name": "string", "last_name": "string", "email": "string", "company": "string"}},
				{Name: "update_opportunity", Description: "Update an opportunity stage or value", RiskLevel: "medium", InputSchema: map[string]interface{}{"opportunity_id": "string", "stage": "string", "amount": "number"}},
				{Name: "query_records", Description: "Execute a SOQL query", RiskLevel: "low", InputSchema: map[string]interface{}{"soql": "string"}},
				{Name: "bulk_upsert", Description: "Bulk upsert records via Bulk API 2.0", RiskLevel: "high", InputSchema: map[string]interface{}{"object_type": "string", "records": "array", "external_id_field": "string"}},
			},
		},

		// =========================================================
		// P0 — SAP
		// =========================================================
		{
			ID:             "conn-sap",
			Name:           "SAP ERP",
			Slug:           "sap-erp",
			Description:    "Enterprise ERP connector via RFC/BAPI. Execute BAPIs, read master data, post financial documents, and manage material movements.",
			Category:       CatERP,
			PublisherID:    "ocx-system",
			PublisherName:  "OCX Built-in",
			IconURL:        "/icons/sap.svg",
			Version:        "1.4.0",
			IsVerified:     true,
			IsPublic:       true,
			IsBuiltIn:      true,
			InstallCount:   1923,
			Rating:         4.6,
			PricingTier:    PricingEnterprise,
			MonthlyCredits: 1000,
			ConfigSchema: map[string]interface{}{
				"ashost":   map[string]interface{}{"type": "string", "required": true, "label": "Application Server Host"},
				"sysnr":    map[string]interface{}{"type": "string", "required": true, "label": "System Number"},
				"client":   map[string]interface{}{"type": "string", "required": true, "label": "SAP Client"},
				"user":     map[string]interface{}{"type": "string", "required": true, "label": "RFC Username"},
				"password": map[string]interface{}{"type": "secret", "required": true, "label": "RFC Password"},
			},
			Actions: []ConnectorAction{
				{Name: "execute_bapi", Description: "Execute an RFC-enabled BAPI", RiskLevel: "high", InputSchema: map[string]interface{}{"bapi_name": "string", "import_params": "object"}},
				{Name: "read_table", Description: "Read SAP table data via RFC_READ_TABLE", RiskLevel: "low", InputSchema: map[string]interface{}{"table_name": "string", "fields": "array", "where_clause": "string"}},
				{Name: "post_document", Description: "Post a financial document (FI/CO)", RiskLevel: "high", InputSchema: map[string]interface{}{"doc_type": "string", "company_code": "string", "line_items": "array"}},
				{Name: "create_material", Description: "Create or update material master data", RiskLevel: "high", InputSchema: map[string]interface{}{"material_number": "string", "material_type": "string", "views": "object"}},
			},
		},

		// =========================================================
		// P1 — ServiceNow
		// =========================================================
		{
			ID:             "conn-servicenow",
			Name:           "ServiceNow",
			Slug:           "servicenow",
			Description:    "ITSM connector via REST API. Create incidents, manage change requests, query CMDB, and automate workflows.",
			Category:       CatDevOps,
			PublisherID:    "ocx-system",
			PublisherName:  "OCX Built-in",
			IconURL:        "/icons/servicenow.svg",
			Version:        "1.2.0",
			IsVerified:     true,
			IsPublic:       true,
			IsBuiltIn:      true,
			InstallCount:   1456,
			Rating:         4.5,
			PricingTier:    PricingPro,
			MonthlyCredits: 300,
			ConfigSchema: map[string]interface{}{
				"instance_url": map[string]interface{}{"type": "url", "required": true, "label": "Instance URL", "placeholder": "https://yourorg.service-now.com"},
				"username":     map[string]interface{}{"type": "string", "required": true, "label": "API Username"},
				"password":     map[string]interface{}{"type": "secret", "required": true, "label": "API Password"},
			},
			Actions: []ConnectorAction{
				{Name: "create_incident", Description: "Create a new incident ticket", RiskLevel: "medium", InputSchema: map[string]interface{}{"short_description": "string", "urgency": "number", "category": "string"}},
				{Name: "update_change_request", Description: "Update a change request", RiskLevel: "medium", InputSchema: map[string]interface{}{"change_id": "string", "state": "string"}},
				{Name: "query_cmdb", Description: "Query Configuration Management Database", RiskLevel: "low", InputSchema: map[string]interface{}{"class": "string", "query": "string"}},
			},
		},

		// =========================================================
		// P1 — Slack
		// =========================================================
		{
			ID:             "conn-slack",
			Name:           "Slack",
			Slug:           "slack",
			Description:    "Team communication via OAuth + Events API. Send messages, create channels, manage workflows, and receive real-time event notifications.",
			Category:       CatCommunication,
			PublisherID:    "ocx-system",
			PublisherName:  "OCX Built-in",
			IconURL:        "/icons/slack.svg",
			Version:        "2.0.0",
			IsVerified:     true,
			IsPublic:       true,
			IsBuiltIn:      true,
			InstallCount:   3214,
			Rating:         4.9,
			PricingTier:    PricingFree,
			MonthlyCredits: 0,
			ConfigSchema: map[string]interface{}{
				"bot_token":       map[string]interface{}{"type": "secret", "required": true, "label": "Bot OAuth Token"},
				"signing_secret":  map[string]interface{}{"type": "secret", "required": true, "label": "Signing Secret"},
				"default_channel": map[string]interface{}{"type": "string", "required": false, "label": "Default Channel"},
			},
			Actions: []ConnectorAction{
				{Name: "send_message", Description: "Send a message to a channel or user", RiskLevel: "low", InputSchema: map[string]interface{}{"channel": "string", "text": "string", "blocks": "array"}},
				{Name: "create_channel", Description: "Create a new channel", RiskLevel: "medium", InputSchema: map[string]interface{}{"name": "string", "is_private": "boolean"}},
				{Name: "upload_file", Description: "Upload a file to a channel", RiskLevel: "low", InputSchema: map[string]interface{}{"channel": "string", "file_path": "string", "filename": "string"}},
			},
		},

		// =========================================================
		// P1 — Microsoft Teams
		// =========================================================
		{
			ID:             "conn-teams",
			Name:           "Microsoft Teams",
			Slug:           "ms-teams",
			Description:    "Enterprise communication via Microsoft Graph API. Send messages, manage teams and channels, schedule meetings, and post adaptive cards.",
			Category:       CatCommunication,
			PublisherID:    "ocx-system",
			PublisherName:  "OCX Built-in",
			IconURL:        "/icons/teams.svg",
			Version:        "1.3.0",
			IsVerified:     true,
			IsPublic:       true,
			IsBuiltIn:      true,
			InstallCount:   1876,
			Rating:         4.4,
			PricingTier:    PricingFree,
			MonthlyCredits: 0,
			ConfigSchema: map[string]interface{}{
				"tenant_id":     map[string]interface{}{"type": "string", "required": true, "label": "Azure AD Tenant ID"},
				"client_id":     map[string]interface{}{"type": "string", "required": true, "label": "App Client ID"},
				"client_secret": map[string]interface{}{"type": "secret", "required": true, "label": "App Client Secret"},
			},
			Actions: []ConnectorAction{
				{Name: "send_message", Description: "Send a message to a Teams channel", RiskLevel: "low", InputSchema: map[string]interface{}{"team_id": "string", "channel_id": "string", "message": "string"}},
				{Name: "create_team", Description: "Create a new team", RiskLevel: "high", InputSchema: map[string]interface{}{"display_name": "string", "description": "string"}},
				{Name: "post_adaptive_card", Description: "Post an interactive adaptive card", RiskLevel: "low", InputSchema: map[string]interface{}{"channel_id": "string", "card_json": "object"}},
			},
		},

		// =========================================================
		// P2 — Jira
		// =========================================================
		{
			ID:             "conn-jira",
			Name:           "Jira",
			Slug:           "jira",
			Description:    "Project management via REST API. Create issues, manage sprints, track epics, and sync workflow transitions.",
			Category:       CatDevOps,
			PublisherID:    "ocx-system",
			PublisherName:  "OCX Built-in",
			IconURL:        "/icons/jira.svg",
			Version:        "1.5.0",
			IsVerified:     true,
			IsPublic:       true,
			IsBuiltIn:      true,
			InstallCount:   2103,
			Rating:         4.7,
			PricingTier:    PricingStarter,
			MonthlyCredits: 100,
			ConfigSchema: map[string]interface{}{
				"domain":    map[string]interface{}{"type": "string", "required": true, "label": "Jira Domain", "placeholder": "yourorg.atlassian.net"},
				"email":     map[string]interface{}{"type": "string", "required": true, "label": "API Email"},
				"api_token": map[string]interface{}{"type": "secret", "required": true, "label": "API Token"},
			},
			Actions: []ConnectorAction{
				{Name: "create_issue", Description: "Create a new Jira issue", RiskLevel: "low", InputSchema: map[string]interface{}{"project_key": "string", "issue_type": "string", "summary": "string", "description": "string"}},
				{Name: "transition_issue", Description: "Move an issue to a new status", RiskLevel: "medium", InputSchema: map[string]interface{}{"issue_key": "string", "transition_id": "string"}},
				{Name: "search_issues", Description: "Search issues via JQL", RiskLevel: "low", InputSchema: map[string]interface{}{"jql": "string", "max_results": "number"}},
			},
		},

		// =========================================================
		// P0 — HTTP/REST (Generic)
		// =========================================================
		{
			ID:             "conn-http-rest",
			Name:           "HTTP/REST (Generic)",
			Slug:           "http-rest",
			Description:    "Universal REST API connector. Configure custom endpoints, authentication, headers, and response mapping for any HTTP-based service.",
			Category:       CatCustom,
			PublisherID:    "ocx-system",
			PublisherName:  "OCX Built-in",
			IconURL:        "/icons/http.svg",
			Version:        "3.0.0",
			IsVerified:     true,
			IsPublic:       true,
			IsBuiltIn:      true,
			InstallCount:   5432,
			Rating:         4.7,
			PricingTier:    PricingFree,
			MonthlyCredits: 0,
			ConfigSchema: map[string]interface{}{
				"base_url":   map[string]interface{}{"type": "url", "required": true, "label": "Base URL"},
				"auth_type":  map[string]interface{}{"type": "select", "options": []string{"none", "basic", "bearer", "api_key", "oauth2"}, "label": "Auth Type"},
				"auth_value": map[string]interface{}{"type": "secret", "required": false, "label": "Auth Value"},
				"headers":    map[string]interface{}{"type": "key_value", "required": false, "label": "Custom Headers"},
			},
			Actions: []ConnectorAction{
				{Name: "http_get", Description: "Execute a GET request", RiskLevel: "low", InputSchema: map[string]interface{}{"path": "string", "query_params": "object"}},
				{Name: "http_post", Description: "Execute a POST request", RiskLevel: "medium", InputSchema: map[string]interface{}{"path": "string", "body": "object"}},
				{Name: "http_put", Description: "Execute a PUT request", RiskLevel: "medium", InputSchema: map[string]interface{}{"path": "string", "body": "object"}},
				{Name: "http_delete", Description: "Execute a DELETE request", RiskLevel: "high", InputSchema: map[string]interface{}{"path": "string"}},
			},
		},

		// =========================================================
		// P1 — OpenAI GPT-4
		// =========================================================
		{
			ID:             "conn-openai",
			Name:           "OpenAI GPT-4",
			Slug:           "openai-gpt4",
			Description:    "Enterprise AI connector for OpenAI GPT-4 and GPT-4o models. Chat completions, embeddings, function calling, and vision. Governed by AOCS trust enforcement.",
			Category:       CatAI,
			PublisherID:    "ocx-system",
			PublisherName:  "OCX Built-in",
			IconURL:        "/icons/openai.svg",
			Version:        "1.0.0",
			IsVerified:     true,
			IsPublic:       true,
			IsBuiltIn:      true,
			InstallCount:   1250,
			Rating:         4.9,
			PricingTier:    PricingPro,
			MonthlyCredits: 200,
			ConfigSchema: map[string]interface{}{
				"api_key":      map[string]interface{}{"type": "secret", "required": true, "label": "OpenAI API Key"},
				"organization": map[string]interface{}{"type": "string", "required": false, "label": "Organization ID"},
				"model":        map[string]interface{}{"type": "select", "options": []string{"gpt-4", "gpt-4o", "gpt-4o-mini", "gpt-4-turbo"}, "default": "gpt-4o", "label": "Default Model"},
				"max_tokens":   map[string]interface{}{"type": "number", "default": 4096, "label": "Max Tokens"},
			},
			Actions: []ConnectorAction{
				{Name: "chat_completion", Description: "Generate a chat completion response", RiskLevel: "medium", InputSchema: map[string]interface{}{"messages": "array", "model": "string", "temperature": "number"}},
				{Name: "create_embedding", Description: "Generate text embeddings", RiskLevel: "low", InputSchema: map[string]interface{}{"input": "string", "model": "string"}},
				{Name: "function_call", Description: "Execute a function call via GPT-4", RiskLevel: "high", InputSchema: map[string]interface{}{"messages": "array", "functions": "array"}},
			},
		},

		// =========================================================
		// P1 — Google Gemini
		// =========================================================
		{
			ID:             "conn-gemini",
			Name:           "Google Gemini",
			Slug:           "google-gemini",
			Description:    "Enterprise AI connector for Google Gemini Pro and Ultra models. Multi-modal generation, structured output, and grounding. Governed by AOCS trust enforcement.",
			Category:       CatAI,
			PublisherID:    "ocx-system",
			PublisherName:  "OCX Built-in",
			IconURL:        "/icons/gemini.svg",
			Version:        "1.0.0",
			IsVerified:     true,
			IsPublic:       true,
			IsBuiltIn:      true,
			InstallCount:   890,
			Rating:         4.7,
			PricingTier:    PricingPro,
			MonthlyCredits: 200,
			ConfigSchema: map[string]interface{}{
				"api_key":    map[string]interface{}{"type": "secret", "required": true, "label": "Gemini API Key"},
				"project_id": map[string]interface{}{"type": "string", "required": false, "label": "Google Cloud Project ID"},
				"model":      map[string]interface{}{"type": "select", "options": []string{"gemini-pro", "gemini-ultra", "gemini-1.5-pro", "gemini-1.5-flash"}, "default": "gemini-1.5-pro", "label": "Default Model"},
				"region":     map[string]interface{}{"type": "string", "default": "us-central1", "label": "Region"},
			},
			Actions: []ConnectorAction{
				{Name: "generate_content", Description: "Generate content from text or multi-modal input", RiskLevel: "medium", InputSchema: map[string]interface{}{"prompt": "string", "model": "string", "temperature": "number"}},
				{Name: "create_embedding", Description: "Generate text embeddings via Gemini", RiskLevel: "low", InputSchema: map[string]interface{}{"content": "string", "model": "string"}},
				{Name: "structured_output", Description: "Generate structured JSON output with schema validation", RiskLevel: "medium", InputSchema: map[string]interface{}{"prompt": "string", "response_schema": "object"}},
			},
		},
	}

	cm.svc.mu.Lock()
	defer cm.svc.mu.Unlock()

	for _, c := range builtins {
		c.CreatedAt = time.Now()
		cm.svc.connectors[c.ID] = c
	}
}
