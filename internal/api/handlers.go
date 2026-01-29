package api

import (
	"encoding/json"
	"net/http"

	"github.com/ocx/backend/internal/core"
	"github.com/ocx/backend/internal/service"
)

type Handler struct {
	Engine *service.TrustEngine
}

func NewHandler(engine *service.TrustEngine) *Handler {
	return &Handler{Engine: engine}
}

func (h *Handler) CheckIn(w http.ResponseWriter, r *http.Request) {
	// Check-in logic (mock)
	response := map[string]string{"status": "registered", "agent_id": "agent-new-001"}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (h *Handler) VerifyIntent(w http.ResponseWriter, r *http.Request) {
	// CORS headers for frontend
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	var req core.TokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request payload", http.StatusBadRequest)
		return
	}

	result := h.Engine.EvaluateIntent(r.Context(), req)

	var token string
	if result.Allowed {
		token = h.Engine.GenerateToken(req.AgentID, result.Score)
	}

	resp := core.TokenResponse{
		Token:      token,
		Authorized: result.Allowed,
		Score:      result.Score,
		Reasoning:  result.Reason,
		Breakdown:  result.Breakdown,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// SIMULATION API (Interface Layer): Generative Intent / Ghost Nodes
func (h *Handler) SimulateIntent(w http.ResponseWriter, r *http.Request) {
	// CORS
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == "OPTIONS" {
		return
	}

	// Mock response representing "Ghost Nodes"
	// In production, this would call arbitrator.SpeculativeExecute dry-run
	response := map[string]interface{}{
		"probability": 0.85,
		"nodes": []map[string]string{
			{"type": "security", "status": "pass", "label": "Policy Check"},
			{"type": "performance", "status": "pass", "label": "Latency < 20ms"},
			{"type": "risk", "status": "warn", "label": "Compliance Verification"},
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// CONFLICT API (Orchestration Layer): Jury Sliders
func (h *Handler) ResolveConflict(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == "OPTIONS" {
		return
	}

	var payload struct {
		ConflictID string `json:"conflict_id"`
		Weight     int    `json:"weight"` // 0-100 (Compliance vs Speed)
	}

	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "Invalid Payload", http.StatusBadRequest)
		return
	}

	// Logic: Update the Weighted Trust Vector implementation mentioned in user requirements
	// For now, return success
	response := map[string]interface{}{
		"status": "resolved",
		"new_policy_vector": map[string]float64{
			"compliance": float64(payload.Weight) / 100.0,
			"velocity":   1.0 - (float64(payload.Weight) / 100.0),
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
