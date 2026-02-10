package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/ocx/backend/internal/evidence"
)

// HandleEvidenceChain queries the evidence vault (§6).
func HandleEvidenceChain(vault *evidence.EvidenceVault) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		txID := r.URL.Query().Get("tx_id")

		var response interface{}

		if txID != "" {
			// Get specific transaction history
			records, err := vault.GetTransactionHistory(r.Context(), txID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			response = map[string]interface{}{
				"tx_id":   txID,
				"records": records,
			}
		} else {
			// Return vault stats
			response = vault.Stats()
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}
}
