package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/ocx/backend/internal/database"
	"github.com/ocx/backend/internal/multitenancy"
)

// ============================================================================
// AGENT HANDLERS — Agent Registry & Profile Management
// ============================================================================

// HandleListAgents returns all agents with enriched profile data.
// GET /api/v1/agents?limit=
// Tenant is extracted from the request context (set by TenantMiddleware).
func HandleListAgents(db *database.SupabaseClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		if limit <= 0 {
			limit = 100
		}

		// Extract tenant from context (set by TenantMiddleware)
		tenantID, err := multitenancy.GetTenantID(r.Context())
		if err != nil {
			// Fallback: try query param or header for backward compatibility
			tenantID = r.URL.Query().Get("tenant_id")
			if tenantID == "" {
				tenantID = r.Header.Get("X-Tenant-ID")
			}
		}

		var agents []database.Agent
		if tenantID != "" {
			agents, err = db.ListAgents(r.Context(), tenantID, limit)
		} else {
			agents, err = db.ListAllAgents(r.Context(), limit)
		}

		if err != nil {
			slog.Error("ListAgents failed", "error", err, "tenant_id", tenantID)
			http.Error(w, `{"error":"failed to list agents"}`, http.StatusInternalServerError)
			return
		}
		if agents == nil {
			agents = []database.Agent{}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"agents":       agents,
			"total_agents": len(agents),
		})
	}
}

// HandleGetAgent returns a single agent by ID with full profile.
// GET /api/v1/agents/{agentId}
func HandleGetAgent(db *database.SupabaseClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		agentID := mux.Vars(r)["agentId"]
		if agentID == "" {
			agentID = mux.Vars(r)["id"]
		}

		// Extract tenant from context (set by TenantMiddleware)
		tenantID, err := multitenancy.GetTenantID(r.Context())
		if err != nil {
			tenantID = r.URL.Query().Get("tenant_id")
			if tenantID == "" {
				tenantID = r.Header.Get("X-Tenant-ID")
			}
		}

		if tenantID == "" {
			http.Error(w, `{"error":"tenant context required"}`, http.StatusBadRequest)
			return
		}

		agent, err := db.GetAgent(r.Context(), tenantID, agentID)
		if err != nil {
			slog.Error("GetAgent failed", "error", err, "agent_id", agentID, "tenant_id", tenantID)
			http.Error(w, `{"error":"failed to get agent"}`, http.StatusInternalServerError)
			return
		}
		if agent == nil {
			http.Error(w, `{"error":"agent not found"}`, http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(agent)
	}
}

// HandleUpdateAgent partially updates an agent's profile.
// PUT /api/v1/agents/{agentId}
func HandleUpdateAgent(db *database.SupabaseClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		agentID := mux.Vars(r)["agentId"]
		if agentID == "" {
			agentID = mux.Vars(r)["id"]
		}

		var updates database.Agent
		if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
			http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
			return
		}

		updates.AgentID = agentID

		// Extract tenant from context
		tenantID, err := multitenancy.GetTenantID(r.Context())
		if err != nil {
			tenantID = r.Header.Get("X-Tenant-ID")
		}
		if tenantID != "" {
			updates.TenantID = tenantID
		}

		if updates.TenantID == "" {
			http.Error(w, `{"error":"tenant context required"}`, http.StatusBadRequest)
			return
		}

		if err := db.UpdateAgent(r.Context(), &updates); err != nil {
			slog.Error("UpdateAgent failed", "error", err, "agent_id", agentID)
			http.Error(w, `{"error":"failed to update agent"}`, http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":   "updated",
			"agent_id": agentID,
		})
	}
}

// ============================================================================
// COMPATIBILITY WRAPPERS — names referenced in main.go
// ============================================================================

// ListAgents is an alias for HandleListAgents.
func ListAgents(db *database.SupabaseClient) http.HandlerFunc {
	return HandleListAgents(db)
}

// GetAgent is an alias for HandleGetAgent.
func GetAgent(db *database.SupabaseClient) http.HandlerFunc {
	return HandleGetAgent(db)
}

// GetTrustScores returns trust scores for an agent by ID.
func GetTrustScores(db *database.SupabaseClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		agentID := mux.Vars(r)["id"]

		// Extract tenant from context (set by TenantMiddleware)
		tenantID, err := multitenancy.GetTenantID(r.Context())
		if err != nil {
			tenantID = r.URL.Query().Get("tenant_id")
			if tenantID == "" {
				tenantID = r.Header.Get("X-Tenant-ID")
			}
		}

		if tenantID == "" {
			http.Error(w, `{"error":"tenant context required"}`, http.StatusBadRequest)
			return
		}

		scores, err := db.GetTrustScores(r.Context(), tenantID, agentID)
		if err != nil {
			slog.Error("GetTrustScores failed", "error", err, "agent_id", agentID, "tenant_id", tenantID)
			http.Error(w, `{"error":"failed to get trust scores"}`, http.StatusInternalServerError)
			return
		}
		if scores == nil {
			http.Error(w, `{"error":"trust scores not found"}`, http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(scores)
	}
}
