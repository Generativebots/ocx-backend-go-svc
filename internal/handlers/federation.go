package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/ocx/backend/internal/config"
	"github.com/ocx/backend/internal/federation"
)

// HandleFederationHandshake initiates an Inter-OCX handshake (§5).
func HandleFederationHandshake(cfg *config.Config, registry *federation.FederationRegistry, ledger *federation.PersistentTrustLedger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			RemoteInstanceID string `json:"remote_instance_id"`
			AgentID          string `json:"agent_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		// Look up remote instance
		remote, err := registry.Lookup(req.RemoteInstanceID)
		if err != nil {
			http.Error(w, "Remote instance not found in registry: "+err.Error(), http.StatusNotFound)
			return
		}

		// Create local instance stub for the handshake
		local := &federation.OCXInstance{
			InstanceID:   cfg.Federation.InstanceID,
			TrustDomain:  cfg.Federation.TrustDomain,
			Region:       cfg.Federation.Region,
			Organization: cfg.Federation.Organization,
		}

		// §5.2 Fix: Use real TrustAttestationLedger backed by PersistentTrustLedger
		tal := federation.NewTrustAttestationLedgerWithID(cfg.Federation.InstanceID)
		handshake := federation.NewInterOCXHandshake(local, remote, tal)
		result, err := handshake.NegotiateV2(r.Context(), req.AgentID)
		if err != nil {
			slog.Warn("Federation handshake failed", "error", err)
			http.Error(w, "Handshake failed: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// §5.2: Record handshake outcome in persistent trust ledger
		success := result.Verdict == "ACCEPT" || result.Verdict == "TRUSTED"
		ledger.RecordHandshake(
			r.Context(),
			cfg.Federation.InstanceID, req.RemoteInstanceID,
			remote.TrustDomain, remote.Organization, req.AgentID,
			result.TrustLevel, result.TrustLevel,
			success,
		)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}
}

// HandleFederationTrust lists trusted OCX instances (§5.2).
func HandleFederationTrust(ledger *federation.PersistentTrustLedger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		instances := ledger.ListTrustedInstances(0.5)
		attestationLog := ledger.GetAttestationLog(20)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"trusted_instances": instances,
			"attestation_log":   attestationLog,
		})
	}
}

// HandleFederationInstanceTrust returns trust record for a specific remote instance.
func HandleFederationInstanceTrust(ledger *federation.PersistentTrustLedger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		instanceID := mux.Vars(r)["instanceId"]

		record := ledger.GetInstanceRecord(instanceID)
		if record == nil {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"instance_id": instanceID,
				"trust_score": 0.5,
				"status":      "UNKNOWN",
				"message":     "No trust history for this instance",
			})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(record)
	}
}

// HandleFederationAttestations lists recent attestation events.
func HandleFederationAttestations(ledger *federation.PersistentTrustLedger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		events := ledger.GetAttestationLog(50)
		if events == nil {
			events = []federation.TrustAttestationEvent{}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"attestations": events,
			"total":        len(events),
		})
	}
}
