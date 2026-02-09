// Package marketplace — Installer (Phase B.4)
//
// Manages installation, uninstallation, and configuration of marketplace
// items within a tenant's environment. All operations are tenant-scoped.
package marketplace

import (
	"fmt"
	"time"
)

// Installer manages installs/uninstalls for a tenant.
type Installer struct {
	svc *Service
}

// NewInstaller creates a new installer.
func NewInstaller(svc *Service) *Installer {
	return &Installer{svc: svc}
}

// InstallConnector installs a connector for a tenant with the given config.
func (inst *Installer) InstallConnector(tenantID, connectorID string, config map[string]interface{}) (*Installation, error) {
	inst.svc.mu.Lock()
	defer inst.svc.mu.Unlock()

	connector, ok := inst.svc.connectors[connectorID]
	if !ok {
		return nil, fmt.Errorf("connector %s not found", connectorID)
	}

	// Check for duplicate installation
	for _, existing := range inst.svc.installations[tenantID] {
		if existing.ItemID == connectorID && existing.Status == "active" {
			return nil, fmt.Errorf("connector %s already installed for tenant %s", connectorID, tenantID)
		}
	}

	// Gap 4 fix: Validate connector signature before installation
	if inst.svc.sigVerifier != nil {
		result := inst.svc.sigVerifier.ValidateConnector(connector)
		if !result.Valid {
			inst.svc.logger.Printf("⚠️ Signature validation failed for connector %s: %s", connectorID, result.Reason)
			// Allow built-in connectors through; reject unsigned third-party
			if !connector.IsBuiltIn {
				return nil, fmt.Errorf("signature validation failed: %s", result.Reason)
			}
		}
	}

	installation := &Installation{
		ID:          fmt.Sprintf("inst-%s-%s-%d", tenantID, connectorID, time.Now().UnixNano()),
		TenantID:    tenantID,
		ItemType:    ItemTypeConnector,
		ItemID:      connectorID,
		ItemName:    connector.Name,
		Config:      config,
		Status:      "active",
		InstalledAt: time.Now(),
	}

	inst.svc.installations[tenantID] = append(inst.svc.installations[tenantID], installation)
	connector.InstallCount++

	// Gap 2 fix: Record revenue for paid connectors
	if inst.svc.revenueMgr != nil && connector.MonthlyCredits > 0 {
		// Must release lock before calling revenue manager (it acquires its own lock)
		go inst.svc.revenueMgr.RecordInstallRevenue(tenantID, ItemTypeConnector, connectorID)
	}

	inst.svc.logger.Printf("Installed connector %s for tenant %s", connector.Name, tenantID)
	return installation, nil
}

// InstallTemplate installs a template for a tenant.
func (inst *Installer) InstallTemplate(tenantID, templateID string, config map[string]interface{}) (*Installation, error) {
	inst.svc.mu.Lock()
	defer inst.svc.mu.Unlock()

	template, ok := inst.svc.templates[templateID]
	if !ok {
		return nil, fmt.Errorf("template %s not found", templateID)
	}

	// Check dependencies — all required connectors must be installed
	for _, depID := range template.Dependencies {
		found := false
		for _, existing := range inst.svc.installations[tenantID] {
			if existing.ItemID == depID && existing.Status == "active" {
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("template %s requires connector %s to be installed first", templateID, depID)
		}
	}

	// Gap 4 fix: Validate template signature before installation
	if inst.svc.sigVerifier != nil {
		result := inst.svc.sigVerifier.ValidateTemplate(template)
		if !result.Valid && !template.IsBuiltIn {
			inst.svc.logger.Printf("⚠️ Signature validation failed for template %s: %s", templateID, result.Reason)
			return nil, fmt.Errorf("signature validation failed: %s", result.Reason)
		}
	}

	installation := &Installation{
		ID:          fmt.Sprintf("inst-%s-%s-%d", tenantID, templateID, time.Now().UnixNano()),
		TenantID:    tenantID,
		ItemType:    ItemTypeTemplate,
		ItemID:      templateID,
		ItemName:    template.Name,
		Config:      config,
		Status:      "active",
		InstalledAt: time.Now(),
	}

	inst.svc.installations[tenantID] = append(inst.svc.installations[tenantID], installation)
	template.InstallCount++

	// Gap 2 fix: Record revenue for paid templates
	if inst.svc.revenueMgr != nil && template.OneTimeCredits > 0 {
		go inst.svc.revenueMgr.RecordInstallRevenue(tenantID, ItemTypeTemplate, templateID)
	}

	// Gap 7 fix: Deploy template as activity
	if inst.svc.activityBridge != nil {
		go func() {
			if _, err := inst.svc.activityBridge.DeployTemplate(tenantID, template, installation.ID, config); err != nil {
				inst.svc.logger.Printf("⚠️ Activity deployment failed for template %s: %v", templateID, err)
			}
		}()
	}

	inst.svc.logger.Printf("Installed template %s for tenant %s", template.Name, tenantID)
	return installation, nil
}

// Uninstall removes an installation for a tenant.
func (inst *Installer) Uninstall(tenantID, installationID string) error {
	inst.svc.mu.Lock()
	defer inst.svc.mu.Unlock()

	for _, installation := range inst.svc.installations[tenantID] {
		if installation.ID == installationID {
			installation.Status = "uninstalled"
			inst.svc.logger.Printf("Uninstalled %s for tenant %s", installation.ItemName, tenantID)
			return nil
		}
	}

	return fmt.Errorf("installation %s not found for tenant %s", installationID, tenantID)
}

// PauseInstallation pauses an active installation.
func (inst *Installer) PauseInstallation(tenantID, installationID string) error {
	inst.svc.mu.Lock()
	defer inst.svc.mu.Unlock()

	for _, installation := range inst.svc.installations[tenantID] {
		if installation.ID == installationID && installation.Status == "active" {
			installation.Status = "paused"
			inst.svc.logger.Printf("Paused %s for tenant %s", installation.ItemName, tenantID)
			return nil
		}
	}

	return fmt.Errorf("active installation %s not found for tenant %s", installationID, tenantID)
}

// ResumeInstallation resumes a paused installation.
func (inst *Installer) ResumeInstallation(tenantID, installationID string) error {
	inst.svc.mu.Lock()
	defer inst.svc.mu.Unlock()

	for _, installation := range inst.svc.installations[tenantID] {
		if installation.ID == installationID && installation.Status == "paused" {
			installation.Status = "active"
			inst.svc.logger.Printf("Resumed %s for tenant %s", installation.ItemName, tenantID)
			return nil
		}
	}

	return fmt.Errorf("paused installation %s not found for tenant %s", installationID, tenantID)
}

// UpdateConfig updates the config for an existing installation.
func (inst *Installer) UpdateConfig(tenantID, installationID string, config map[string]interface{}) error {
	inst.svc.mu.Lock()
	defer inst.svc.mu.Unlock()

	for _, installation := range inst.svc.installations[tenantID] {
		if installation.ID == installationID && installation.Status != "uninstalled" {
			installation.Config = config
			inst.svc.logger.Printf("Updated config for %s (tenant %s)", installation.ItemName, tenantID)
			return nil
		}
	}

	return fmt.Errorf("installation %s not found for tenant %s", installationID, tenantID)
}
