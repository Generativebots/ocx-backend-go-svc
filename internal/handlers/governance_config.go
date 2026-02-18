package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/ocx/backend/internal/database"
	"github.com/ocx/backend/internal/governance"
	"github.com/ocx/backend/internal/multitenancy"
)

// ============================================================================
// GOVERNANCE CONFIGURATION ENDPOINTS
// REST API for tenant-configurable governance parameters.
//
//   GET    /api/v1/tenant/{id}/governance-config       — Get config
//   PUT    /api/v1/tenant/{id}/governance-config       — Update config (+ audit)
//   POST   /api/v1/tenant/{id}/governance-config/reset — Reset to defaults
//   GET    /api/v1/tenant/{id}/governance-audit         — Paginated audit log
// ============================================================================

// HandleGetGovernanceConfig returns the governance config for a tenant.
// GET /api/v1/tenant/{tenantId}/governance-config
func HandleGetGovernanceConfig(
	client *database.SupabaseClient,
	cache *governance.GovernanceConfigCache,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := resolveTenantID(r)
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		cfg := cache.GetConfig(tenantID)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(cfg)
	}
}

// HandleUpdateGovernanceConfig updates the governance config for a tenant.
// PUT /api/v1/tenant/{tenantId}/governance-config
// Body: partial or full TenantGovernanceConfig JSON
func HandleUpdateGovernanceConfig(
	client *database.SupabaseClient,
	cache *governance.GovernanceConfigCache,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := resolveTenantID(r)
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Get current config (to compute diff for audit log)
		oldCfg := cache.GetConfig(tenantID)
		oldJSON, _ := json.Marshal(oldCfg)

		// Decode the incoming update. Start from current config so that
		// partial updates are supported (only supplied fields change).
		var updated governance.TenantGovernanceConfig
		// Deep-copy current config as base
		if err := json.Unmarshal(oldJSON, &updated); err != nil {
			http.Error(w, "Internal error", http.StatusInternalServerError)
			return
		}

		// Overlay incoming fields
		if err := json.NewDecoder(r.Body).Decode(&updated); err != nil {
			http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		updated.TenantID = tenantID

		// Validate
		if err := updated.Validate(); err != nil {
			http.Error(w, "Validation failed: "+err.Error(), http.StatusBadRequest)
			return
		}

		// Convert to DB row and persist
		row := governanceConfigToRow(&updated)
		if err := client.UpsertTenantGovernanceConfig(tenantID, row); err != nil {
			slog.Error("Failed to upsert governance config", "tenant_id", tenantID, "error", err)
			http.Error(w, "Failed to save config", http.StatusInternalServerError)
			return
		}

		// Invalidate cache so next GetConfig fetches fresh
		cache.Invalidate(tenantID)

		// Write audit log
		newJSON, _ := json.Marshal(updated)
		actorID := r.Header.Get("X-Actor-ID")
		if actorID == "" {
			actorID = "api"
		}
		auditEntry := &database.GovernanceAuditLogRow{
			TenantID:  tenantID,
			EventType: "CONFIG_CHANGE",
			ActorID:   actorID,
			Action:    "UPDATE_GOVERNANCE_CONFIG",
			OldValue:  json.RawMessage(oldJSON),
			NewValue:  json.RawMessage(newJSON),
		}
		if err := client.InsertGovernanceAuditLog(auditEntry); err != nil {
			slog.Warn("Failed to write governance audit log", "tenant_id", tenantID, "error", err)
			// Non-fatal — config was saved successfully
		}

		// Return the updated config
		freshCfg := cache.GetConfig(tenantID)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(freshCfg)
	}
}

// HandleResetGovernanceConfig resets a tenant's governance config to defaults.
// POST /api/v1/tenant/{tenantId}/governance-config/reset
func HandleResetGovernanceConfig(
	client *database.SupabaseClient,
	cache *governance.GovernanceConfigCache,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := resolveTenantID(r)
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Get current config for audit trail
		oldCfg := cache.GetConfig(tenantID)
		oldJSON, _ := json.Marshal(oldCfg)

		// Create default config
		defaults := governance.DefaultConfig(tenantID)
		row := governanceConfigToRow(defaults)
		if err := client.UpsertTenantGovernanceConfig(tenantID, row); err != nil {
			slog.Error("Failed to reset governance config", "tenant_id", tenantID, "error", err)
			http.Error(w, "Failed to reset config", http.StatusInternalServerError)
			return
		}

		// Invalidate cache
		cache.Invalidate(tenantID)

		// Write audit log
		newJSON, _ := json.Marshal(defaults)
		actorID := r.Header.Get("X-Actor-ID")
		if actorID == "" {
			actorID = "api"
		}
		auditEntry := &database.GovernanceAuditLogRow{
			TenantID:  tenantID,
			EventType: "CONFIG_CHANGE",
			ActorID:   actorID,
			Action:    "RESET_TO_DEFAULTS",
			OldValue:  json.RawMessage(oldJSON),
			NewValue:  json.RawMessage(newJSON),
		}
		if err := client.InsertGovernanceAuditLog(auditEntry); err != nil {
			slog.Warn("Failed to write governance audit log", "tenant_id", tenantID, "error", err)
		}

		// Return fresh defaults
		freshCfg := cache.GetConfig(tenantID)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(freshCfg)
	}
}

// HandleGetGovernanceAuditLog returns paginated audit log entries for a tenant.
// GET /api/v1/tenant/{tenantId}/governance-audit?limit=50
func HandleGetGovernanceAuditLog(
	client *database.SupabaseClient,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := resolveTenantID(r)
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		limit := 50
		if l := r.URL.Query().Get("limit"); l != "" {
			if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 200 {
				limit = parsed
			}
		}

		logs, err := client.GetGovernanceAuditLogs(tenantID, limit)
		if err != nil {
			slog.Error("Failed to fetch audit logs", "tenant_id", tenantID, "error", err)
			http.Error(w, "Failed to fetch audit logs", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(logs)
	}
}

// ============================================================================
// HELPERS
// ============================================================================

// resolveTenantID tries the URL {tenantId} path variable first, then falls
// back to the X-Tenant-ID header extracted by multitenancy middleware.
func resolveTenantID(r *http.Request) (string, error) {
	vars := mux.Vars(r)
	if id := vars["tenantId"]; id != "" {
		return id, nil
	}
	return multitenancy.GetTenantID(r.Context())
}

// governanceConfigToRow converts governance.TenantGovernanceConfig to the
// database row type. Both share the same JSON tags so we marshal/unmarshal.
func governanceConfigToRow(cfg *governance.TenantGovernanceConfig) *database.TenantGovernanceConfigRow {
	data, _ := json.Marshal(cfg)
	var row database.TenantGovernanceConfigRow
	json.Unmarshal(data, &row)
	return &row
}
