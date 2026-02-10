package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/ocx/backend/internal/database"
	"github.com/ocx/backend/internal/multitenancy"
)

// ListAgents returns all agents for the authenticated tenant.
func ListAgents(client *database.SupabaseClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := multitenancy.GetTenantID(r.Context())
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		agents, err := client.ListAgents(r.Context(), tenantID, 100)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(agents)
	}
}

// GetAgent returns a single agent by ID.
func GetAgent(client *database.SupabaseClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		agentID := vars["id"]

		tenantID, err := multitenancy.GetTenantID(r.Context())
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		agent, err := client.GetAgent(r.Context(), tenantID, agentID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if agent == nil {
			http.Error(w, "Agent not found", http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(agent)
	}
}

// GetTrustScores returns trust scores for a specific agent.
func GetTrustScores(client *database.SupabaseClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		agentID := vars["id"]

		tenantID, err := multitenancy.GetTenantID(r.Context())
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		scores, err := client.GetTrustScores(r.Context(), tenantID, agentID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if scores == nil {
			http.Error(w, "Trust scores not found", http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(scores)
	}
}
