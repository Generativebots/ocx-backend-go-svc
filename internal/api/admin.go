package api

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// PolicyUpdateRequest represents a request to update an agent's policy.
type PolicyUpdateRequest struct {
	AgentID string `json:"agent_id"`
	Action  string `json:"action"` // e.g., "THROTTLE", "RESTORE"
}

// UpdatePolicy handles POST /admin/policy requests.
// This is the "Control Port" that the Python Brain uses to reconfigure the system.
func (h *Handler) UpdatePolicy(w http.ResponseWriter, r *http.Request) {
	var req PolicyUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request payload", http.StatusBadRequest)
		return
	}

	// Delegate to TrustEngine to update the in-memory policy map
	h.Engine.UpdateAgentPolicy(req.AgentID, req.Action)

	fmt.Printf("🔧 [Admin] Policy Update Received: %s -> %s\n", req.AgentID, req.Action)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "success",
		"msg":    fmt.Sprintf("Policy for %s updated to %s", req.AgentID, req.Action),
	})
}
