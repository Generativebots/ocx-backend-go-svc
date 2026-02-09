// Package marketplace — Activity Registry Integration (Gap 7 Fix)
//
// Wires template installation to the Activity Registry so that
// installed templates create deployable activities in the tenant's
// activity pipeline.
package marketplace

import (
	"encoding/json"
	"fmt"
	"log"
	"time"
)

// ActivityDeployment represents a template deployed as an activity.
type ActivityDeployment struct {
	ActivityID     string                 `json:"activity_id"`
	InstallationID string                 `json:"installation_id"`
	TemplateID     string                 `json:"template_id"`
	TemplateName   string                 `json:"template_name"`
	TenantID       string                 `json:"tenant_id"`
	EBCLSource     string                 `json:"ebcl_source"`
	Status         string                 `json:"status"` // "draft", "active", "suspended"
	Version        string                 `json:"version"`
	DeployedAt     time.Time              `json:"deployed_at"`
	Config         map[string]interface{} `json:"config"`
}

// ActivityRegistryBridge provides the bridge between marketplace templates
// and the Activity Registry (§ Section 12 of master_schema.sql).
type ActivityRegistryBridge struct {
	// In production: database connection for INSERT INTO activities
	deployments map[string]*ActivityDeployment // tenantID:templateID → deployment
}

// NewActivityRegistryBridge creates a new bridge.
func NewActivityRegistryBridge() *ActivityRegistryBridge {
	return &ActivityRegistryBridge{
		deployments: make(map[string]*ActivityDeployment),
	}
}

// DeployTemplate creates an activity from a template's EBCL definition.
// This is called during template installation to wire it into the execution pipeline.
func (arb *ActivityRegistryBridge) DeployTemplate(
	tenantID string,
	template *Template,
	installationID string,
	config map[string]interface{},
) (*ActivityDeployment, error) {
	if template == nil {
		return nil, fmt.Errorf("template is nil")
	}

	// 1. Serialize EBCL definition to YAML/JSON source
	ebclBytes, err := json.MarshalIndent(template.EBCLDefinition, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to serialize EBCL definition: %w", err)
	}

	// 2. Create activity record
	// In production: INSERT INTO activities (tenant_id, name, version, status, ebcl_source, ...)
	activityID := fmt.Sprintf("act-%s-%s-%d", tenantID, template.ID, time.Now().UnixMilli())

	deployment := &ActivityDeployment{
		ActivityID:     activityID,
		InstallationID: installationID,
		TemplateID:     template.ID,
		TemplateName:   template.Name,
		TenantID:       tenantID,
		EBCLSource:     string(ebclBytes),
		Status:         "active",
		Version:        template.Version,
		DeployedAt:     time.Now(),
		Config:         config,
	}

	// 3. Store deployment
	key := fmt.Sprintf("%s:%s", tenantID, template.ID)
	arb.deployments[key] = deployment

	log.Printf("🚀 Template deployed as activity: template=%s activity=%s tenant=%s",
		template.Name, activityID, tenantID)

	// 4. In production, would also:
	//    - INSERT INTO activity_deployments (activity_id, environment, tenant_id, ...)
	//    - Notify the EBCL runtime to start executing the activity
	//    - Register triggers (cron, webhook, etc.) from step definitions

	return deployment, nil
}

// UndeployTemplate removes the activity created from a template.
func (arb *ActivityRegistryBridge) UndeployTemplate(tenantID, templateID string) error {
	key := fmt.Sprintf("%s:%s", tenantID, templateID)
	deployment, exists := arb.deployments[key]
	if !exists {
		return fmt.Errorf("no deployment found for template %s in tenant %s", templateID, tenantID)
	}

	deployment.Status = "suspended"
	log.Printf("⏹️ Template activity suspended: activity=%s tenant=%s", deployment.ActivityID, tenantID)

	// In production: UPDATE activities SET status = 'SUSPENDED', deactivate triggers
	return nil
}

// GetDeployment returns the deployment for a template in a tenant.
func (arb *ActivityRegistryBridge) GetDeployment(tenantID, templateID string) *ActivityDeployment {
	key := fmt.Sprintf("%s:%s", tenantID, templateID)
	return arb.deployments[key]
}

// ListDeployments returns all template deployments for a tenant.
func (arb *ActivityRegistryBridge) ListDeployments(tenantID string) []*ActivityDeployment {
	results := make([]*ActivityDeployment, 0)
	for _, d := range arb.deployments {
		if d.TenantID == tenantID {
			results = append(results, d)
		}
	}
	return results
}
