package api

import (
	"encoding/json"
	"net/http"

	"github.com/ocx/backend/internal/service"
)

type HandshakeHandler struct {
	Service *service.HandshakeService
}

func NewHandshakeHandler(svc *service.HandshakeService) *HandshakeHandler {
	return &HandshakeHandler{Service: svc}
}

// 1. POST /handshake/hello
func (h *HandshakeHandler) Hello(w http.ResponseWriter, r *http.Request) {
	var req struct {
		InitiatorID string `json:"initiator_id"`
		Version     string `json:"version"`
		TenantID    string `json:"tenant_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid Payload", http.StatusBadRequest)
		return
	}

	sessionID, err := h.Service.Hello(req.InitiatorID, req.Version, req.TenantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]string{
		"session_id":         sessionID,
		"status":             "HELLO_ACK",
		"supported_versions": "1.0",
	})
}

// 2. POST /handshake/challenge
func (h *HandshakeHandler) Challenge(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid Payload", http.StatusBadRequest)
		return
	}

	nonce, err := h.Service.GenerateChallenge(req.SessionID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest) // 400 likely due to state mismatch
		return
	}

	json.NewEncoder(w).Encode(map[string]any{
		"nonce":      nonce,
		"difficulty": 0,
		"algorithm":  "SHA256-RSA",
	})
}

// 3. POST /handshake/proof
func (h *HandshakeHandler) Proof(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionID   string `json:"session_id"`
		SignedNonce string `json:"signed_nonce"`
		PublicKey   string `json:"public_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid Payload", http.StatusBadRequest)
		return
	}

	valid, err := h.Service.VerifyProof(req.SessionID, req.SignedNonce, req.PublicKey)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	status := "PROOF_ACCEPTED"
	if !valid {
		status = "PROOF_REJECTED"
	}

	json.NewEncoder(w).Encode(map[string]string{
		"status": status,
	})
}

// 4. POST /handshake/verify
func (h *HandshakeHandler) Verify(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid Payload", http.StatusBadRequest)
		return
	}

	verified, err := h.Service.VerifyIdentity(req.SessionID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]any{
		"is_verified":     verified,
		"registry_status": "ACTIVE",
	})
}

// 5. POST /handshake/attest
func (h *HandshakeHandler) Attest(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionID string            `json:"session_id"`
		Claims    map[string]string `json:"claims"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid Payload", http.StatusBadRequest)
		return
	}

	if err := h.Service.ProcessAttestations(req.SessionID, req.Claims); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	json.NewEncoder(w).Encode(map[string]string{
		"status": "CLAIMS_RECEIVED",
	})
}

// 6. POST /handshake/finalize
func (h *HandshakeHandler) Finalize(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid Payload", http.StatusBadRequest)
		return
	}

	decision, token, err := h.Service.Finalize(req.SessionID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	json.NewEncoder(w).Encode(map[string]any{
		"decision":      decision,
		"session_token": token,
		"expires_at":    0, // TODO: Add real expiry
	})
}
