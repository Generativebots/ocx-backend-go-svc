package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/ocx/backend/internal/database"
	"github.com/ocx/backend/internal/multitenancy"
)

// cryptoAlgorithmRequest is the request body for updating the crypto algorithm.
type cryptoAlgorithmRequest struct {
	Algorithm string `json:"algorithm"`
}

// validCryptoAlgorithms lists the accepted values.
var validCryptoAlgorithms = map[string]bool{
	"ed25519":    true,
	"ecdsa-p256": true,
}

// HandleGetTenantSettings returns the current tenant settings from the database.
// GET /api/v1/tenant/settings
func HandleGetTenantSettings(client *database.SupabaseClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := multitenancy.GetTenantID(r.Context())
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		tenant, err := client.GetTenant(r.Context(), tenantID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if tenant == nil {
			http.Error(w, "Tenant not found", http.StatusNotFound)
			return
		}

		// Return the settings map (includes federation_crypto_algorithm if set)
		settings := tenant.Settings
		if settings == nil {
			settings = make(map[string]interface{})
		}

		// Ensure federation_crypto_algorithm has a default
		if _, ok := settings["federation_crypto_algorithm"]; !ok {
			settings["federation_crypto_algorithm"] = "ed25519"
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(settings)
	}
}

// HandleUpdateTenantCryptoAlgorithm sets the federation crypto algorithm.
// PUT /api/v1/tenant/settings/crypto
// Body: {"algorithm": "ed25519"} or {"algorithm": "ecdsa-p256"}
func HandleUpdateTenantCryptoAlgorithm(client *database.SupabaseClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := multitenancy.GetTenantID(r.Context())
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Parse request body
		var req cryptoAlgorithmRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		// Validate algorithm
		if !validCryptoAlgorithms[req.Algorithm] {
			http.Error(w, "Invalid algorithm. Must be 'ed25519' or 'ecdsa-p256'", http.StatusBadRequest)
			return
		}

		// Get current tenant to merge settings
		tenant, err := client.GetTenant(r.Context(), tenantID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if tenant == nil {
			http.Error(w, "Tenant not found", http.StatusNotFound)
			return
		}

		// Merge the crypto algorithm into existing settings
		settings := tenant.Settings
		if settings == nil {
			settings = make(map[string]interface{})
		}
		settings["federation_crypto_algorithm"] = req.Algorithm

		// Persist to DB
		if err := client.UpdateTenantSettings(r.Context(), tenantID, settings); err != nil {
			http.Error(w, "Failed to update settings: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// Return updated settings
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(settings)
	}
}
