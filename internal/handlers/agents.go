package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/ocx/backend/internal/database"
)

// ============================================================================
// AGENT HANDLERS — Agent Registry & Profile Management
// ============================================================================

// HandleListAgents returns all agents with enriched profile data.
// GET /api/v1/agents?tenant_id=&limit=
func HandleListAgents(db *database.SupabaseClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := r.URL.Query().Get("tenant_id")
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		if limit <= 0 {
			limit = 100
		}

		var agents []database.Agent
		var err error

		if tenantID != "" {
			agents, err = db.ListAgents(context.Background(), tenantID, limit)
		} else {
			agents, err = db.ListAllAgents(context.Background(), limit)
		}

		if err != nil {
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
		tenantID := r.URL.Query().Get("tenant_id")
		if tenantID == "" {
			tenantID = "default-org"
		}

		agent, err := db.GetAgent(context.Background(), tenantID, agentID)
		if err != nil {
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

		var updates database.Agent
		if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
			http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
			return
		}

		updates.AgentID = agentID
		if updates.TenantID == "" {
			updates.TenantID = "default-org"
		}

		if err := db.UpdateAgent(context.Background(), &updates); err != nil {
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
		tenantID := r.URL.Query().Get("tenant_id")
		if tenantID == "" {
			tenantID = "default-org"
		}

		scores, err := db.GetTrustScores(context.Background(), tenantID, agentID)
		if err != nil {
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
