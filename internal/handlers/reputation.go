package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/ocx/backend/internal/escrow"
	"github.com/ocx/backend/internal/evidence"
	"github.com/ocx/backend/internal/multitenancy"
	"github.com/ocx/backend/internal/reputation"
)

// HandleAgentReputation returns trust scores for a specific agent.
func HandleAgentReputation(wallet *reputation.ReputationWallet) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		agentID := vars["agentId"]

		if agentID == "" {
			http.Error(w, `{"error":"agentId is required"}`, http.StatusBadRequest)
			return
		}

		rep, err := wallet.GetAgentReputation(r.Context(), agentID)
		if err != nil {
			// Return a default score for unknown agents
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"agent_id":    agentID,
				"trust_score": 0.5,
				"level":       "UNKNOWN",
				"history":     []interface{}{},
				"message":     "Agent not found, returning default score",
			})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"agent_id":      agentID,
			"trust_score":   rep.ReputationScore,
			"status":        rep.Status,
			"total_actions": rep.TotalInteractions,
			"successful":    rep.SuccessfulInteractions,
			"failed":        rep.FailedInteractions,
			"last_updated":  rep.LastUpdated,
			"blacklisted":   rep.Blacklisted,
		})
	}
}

// HandlePoolStats returns governance health metrics for the Command Center.
func HandlePoolStats(
	vault *evidence.EvidenceVault,
	gate *escrow.EscrowGate,
	wallet *reputation.ReputationWallet,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := multitenancy.GetTenantID(r.Context())
		if err != nil || tenantID == "" {
			tenantID = r.Header.Get("X-Tenant-ID")
			if tenantID == "" {
				tenantID = "unknown"
			}
		}

		// Collect vault stats (Stats() exists on EvidenceVault)
		vaultStats := vault.Stats()

		// Get escrow held items count via ListHeld()
		heldItems := gate.ListHeld()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"tenant_id":         tenantID,
			"total_evidence":    vaultStats["total_records"],
			"chain_count":       vaultStats["chain_count"],
			"held_in_escrow":    len(heldItems),
			"verdict_breakdown": vaultStats["verdict_breakdown"],
			"timestamp":         time.Now().Format(time.RFC3339),
		})
	}
}
