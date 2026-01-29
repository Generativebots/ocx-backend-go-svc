package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/ocx/backend/internal/core"
)

// AgentProxy acts as the "Governance Proxy" (The Nervous System).
// It intercepts requests, consults the Brain (Python), and enforces the verdict.
func (h *Handler) AgentProxy(w http.ResponseWriter, r *http.Request) {
	// 1. CAPTURE: Extract Payload
	var req core.TokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request payload", http.StatusBadRequest)
		return
	}

	// 2. CONSULT: Call the Jury (Python Trust Registry)
	// We reuse `EvaluateIntent` which calls the Python service.
	verdict := h.Engine.EvaluateIntent(r.Context(), req)

	// 3. ENFORCE: The Kill-Switch & Governance Logic
	// Logic is strictly driven by the AI's verdict.

	// Check for "Shadow Mode" header
	shadowMode := r.Header.Get("X-Gov-Mode") == "Shadow"

	if !verdict.Allowed {
		if shadowMode {
			// Shadow Mode: Log violation but ALLOW (Learning)
			fmt.Printf("👻 [Shadow Mode] Violation Detected but ALLOWED: %s\n", verdict.Reason)
			w.Header().Set("X-Gov-Status", "Shadow-Allow")
		} else {
			// Check for Hand-off (Human Approval)
			if verdict.Status == "NEEDS_APPROVAL" {
				fmt.Printf("✋ [Governance] HUMAN APPROVAL REQUIRED: Score %.2f\n", verdict.Score)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusAccepted) // 202 Accepted = "Processing, but requires action"
				json.NewEncoder(w).Encode(map[string]interface{}{
					"status":           "NEEDS_APPROVAL",
					"message":          "Transaction paused for human review.",
					"governance_trace": verdict,
				})
				return
			}

			// Enforce Block
			fmt.Printf("🛡️ [Governance] BLOCKED: %s | Score: %.2f\n", req.AgentID, verdict.Score)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(verdict) // Return the AI reasoning
			return
		}
	}

	// 4. EXECUTE: Forward to Target Agent
	// In a real system, this would reverse-proxy to the actual agent service.
	// Here we mock the successful execution.
	fmt.Printf("🚀 [Governance] ALLOWED: Forwarding %s for %s\n", req.Action, req.AgentID)

	response := map[string]interface{}{
		"status": "success",
		"data":   "Agent action executed successfully.",
		"governance_trace": map[string]interface{}{
			"trust_score": verdict.Score,
			"token":       h.Engine.GenerateToken(req.AgentID, verdict.Score),
			"grounding":   "SOP Clause 4.2 Verified", // Static for demo
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
