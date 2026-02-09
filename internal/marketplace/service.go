// Package marketplace provides the multi-tenant OCX Marketplace service.
//
// The marketplace enables enterprises to:
//   - Browse and install pre-built connectors (Salesforce, SAP, Slack, etc.)
//   - Browse and install governance templates (GDPR, SOC2, HIPAA, etc.)
//   - Publish and monetize their own connectors and templates
//
// All operations are tenant-scoped for full multi-tenant isolation.
package marketplace

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

// =============================================================================
// CORE TYPES
// =============================================================================

// ItemType distinguishes connectors from templates.
type ItemType string

const (
	ItemTypeConnector ItemType = "connector"
	ItemTypeTemplate  ItemType = "template"
)

// PricingTier defines how an item is billed.
type PricingTier string

const (
	PricingFree       PricingTier = "free"
	PricingStarter    PricingTier = "starter"
	PricingPro        PricingTier = "pro"
	PricingEnterprise PricingTier = "enterprise"
)

// Category for connectors.
type ConnectorCategory string

const (
	CatCRM           ConnectorCategory = "crm"
	CatERP           ConnectorCategory = "erp"
	CatAI            ConnectorCategory = "ai"
	CatCommunication ConnectorCategory = "communication"
	CatDevOps        ConnectorCategory = "devops"
	CatCustom        ConnectorCategory = "custom"
)

// Category for templates.
type TemplateCategory string

const (
	CatGovernance TemplateCategory = "governance"
	CatCompliance TemplateCategory = "compliance"
	CatWorkflow   TemplateCategory = "workflow"
	CatPolicy     TemplateCategory = "policy"
	CatSecurity   TemplateCategory = "security"
	CatAudit      TemplateCategory = "audit"
	CatHealthcare TemplateCategory = "healthcare"
)

// Connector represents a marketplace connector listing.
type Connector struct {
	ID             string                 `json:"id"`
	Name           string                 `json:"name"`
	Slug           string                 `json:"slug"`
	Description    string                 `json:"description"`
	Category       ConnectorCategory      `json:"category"`
	PublisherID    string                 `json:"publisher_id"`
	PublisherName  string                 `json:"publisher_name"`
	IconURL        string                 `json:"icon_url"`
	ConfigSchema   map[string]interface{} `json:"config_schema"`
	Actions        []ConnectorAction      `json:"actions"`
	Version        string                 `json:"version"`
	IsVerified     bool                   `json:"is_verified"`
	IsPublic       bool                   `json:"is_public"`
	IsBuiltIn      bool                   `json:"is_builtin"`
	SignatureHash  string                 `json:"signature_hash,omitempty"`
	InstallCount   int                    `json:"install_count"`
	Rating         float64                `json:"rating"`
	PricingTier    PricingTier            `json:"pricing_tier"`
	MonthlyCredits int                    `json:"monthly_credits"`
	CreatedAt      time.Time              `json:"created_at"`
}

// ConnectorAction defines a single operation a connector can perform.
type ConnectorAction struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"input_schema"`
	RiskLevel   string                 `json:"risk_level"` // "low", "medium", "high"
}

// Template represents a marketplace template listing.
type Template struct {
	ID             string                 `json:"id"`
	Name           string                 `json:"name"`
	Slug           string                 `json:"slug"`
	Description    string                 `json:"description"`
	Category       TemplateCategory       `json:"category"`
	PublisherID    string                 `json:"publisher_id"`
	PublisherName  string                 `json:"publisher_name"`
	EBCLDefinition map[string]interface{} `json:"ebcl_definition"`
	Dependencies   []string               `json:"dependencies"`
	IndustryTags   []string               `json:"industry_tags"`
	Version        string                 `json:"version"`
	IsVerified     bool                   `json:"is_verified"`
	IsPublic       bool                   `json:"is_public"`
	IsBuiltIn      bool                   `json:"is_builtin"`
	SignatureHash  string                 `json:"signature_hash,omitempty"`
	InstallCount   int                    `json:"install_count"`
	Rating         float64                `json:"rating"`
	PricingTier    PricingTier            `json:"pricing_tier"`
	OneTimeCredits int                    `json:"one_time_credits"`
	StepCount      int                    `json:"step_count"`
	CreatedAt      time.Time              `json:"created_at"`
}

// Installation tracks a tenant's installed connector or template.
type Installation struct {
	ID          string                 `json:"id"`
	TenantID    string                 `json:"tenant_id"`
	ItemType    ItemType               `json:"item_type"`
	ItemID      string                 `json:"item_id"`
	ItemName    string                 `json:"item_name"`
	Config      map[string]interface{} `json:"config"`
	Status      string                 `json:"status"` // "active", "paused", "uninstalled"
	InstalledAt time.Time              `json:"installed_at"`
	LastUsedAt  *time.Time             `json:"last_used_at"`
}

// =============================================================================
// MARKETPLACE SERVICE
// =============================================================================

// Service is the multi-tenant marketplace engine.
type Service struct {
	mu sync.RWMutex

	connectors    map[string]*Connector      // id -> connector
	templates     map[string]*Template       // id -> template
	installations map[string][]*Installation // tenantID -> installations

	connectorMgr   *ConnectorManager
	templateMgr    *TemplateManager
	installer      *Installer
	billing        *BillingIntegration
	sigVerifier    *SignatureVerifier
	revenueMgr     *RevenueShareManager
	activityBridge *ActivityRegistryBridge

	logger *log.Logger
}

// NewService creates a new marketplace service with built-in items pre-loaded.
func NewService() *Service {
	svc := &Service{
		connectors:    make(map[string]*Connector),
		templates:     make(map[string]*Template),
		installations: make(map[string][]*Installation),
		logger:        log.New(log.Writer(), "[Marketplace] ", log.LstdFlags),
	}

	svc.connectorMgr = NewConnectorManager(svc)
	svc.templateMgr = NewTemplateManager(svc)
	svc.installer = NewInstaller(svc)
	svc.billing = NewBillingIntegration(svc)
	svc.sigVerifier = NewSignatureVerifier()
	svc.revenueMgr = NewRevenueShareManager(svc)
	svc.activityBridge = NewActivityRegistryBridge()

	// Seed built-in connectors and templates
	svc.connectorMgr.SeedBuiltins()
	svc.templateMgr.SeedBuiltins()

	svc.logger.Printf("Marketplace initialized with %d connectors, %d templates",
		len(svc.connectors), len(svc.templates))

	return svc
}

// ListConnectors returns connectors visible to a tenant, filtered by category.
func (s *Service) ListConnectors(tenantID string, category string) []*Connector {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var results []*Connector
	for _, c := range s.connectors {
		if !c.IsPublic && c.PublisherID != tenantID {
			continue
		}
		if category != "" && string(c.Category) != category {
			continue
		}
		results = append(results, c)
	}
	return results
}

// ListTemplates returns templates visible to a tenant, filtered by category.
func (s *Service) ListTemplates(tenantID string, category string) []*Template {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var results []*Template
	for _, t := range s.templates {
		if !t.IsPublic && t.PublisherID != tenantID {
			continue
		}
		if category != "" && string(t.Category) != category {
			continue
		}
		results = append(results, t)
	}
	return results
}

// GetConnector returns a single connector by ID.
func (s *Service) GetConnector(id string) (*Connector, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	c, ok := s.connectors[id]
	if !ok {
		return nil, fmt.Errorf("connector %s not found", id)
	}
	return c, nil
}

// GetTemplate returns a single template by ID.
func (s *Service) GetTemplate(id string) (*Template, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	t, ok := s.templates[id]
	if !ok {
		return nil, fmt.Errorf("template %s not found", id)
	}
	return t, nil
}

// GetInstallations returns all active installations for a tenant.
func (s *Service) GetInstallations(tenantID string) []*Installation {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var active []*Installation
	for _, inst := range s.installations[tenantID] {
		if inst.Status != "uninstalled" {
			active = append(active, inst)
		}
	}
	return active
}

// SearchAll searches both connectors and templates by name/description.
func (s *Service) SearchAll(tenantID, query string) ([]*Connector, []*Template) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var connectors []*Connector
	var templates []*Template

	lowerQuery := strings.ToLower(query)

	for _, c := range s.connectors {
		if !c.IsPublic && c.PublisherID != tenantID {
			continue
		}
		if strings.Contains(strings.ToLower(c.Name), lowerQuery) ||
			strings.Contains(strings.ToLower(c.Description), lowerQuery) {
			connectors = append(connectors, c)
		}
	}

	for _, t := range s.templates {
		if !t.IsPublic && t.PublisherID != tenantID {
			continue
		}
		if strings.Contains(strings.ToLower(t.Name), lowerQuery) ||
			strings.Contains(strings.ToLower(t.Description), lowerQuery) {
			templates = append(templates, t)
		}
	}

	return connectors, templates
}
