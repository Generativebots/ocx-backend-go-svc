package api

import (
	"encoding/json"
	"net/http"

	"github.com/ocx/backend/internal/service"
)

type GovernanceHandler struct {
	Service *service.GovernanceService
}

func NewGovernanceHandler(svc *service.GovernanceService) *GovernanceHandler {
	return &GovernanceHandler{Service: svc}
}

// POST /governance/proposals
func (h *GovernanceHandler) CreateProposal(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		AuthorID    string `json:"author_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid Payload", http.StatusBadRequest)
		return
	}

	prop, err := h.Service.CreateProposal(r.Context(), req.Title, req.Description, req.AuthorID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(prop)
}

// POST /governance/vote
func (h *GovernanceHandler) CastVote(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ProposalID string `json:"proposal_id"`
		MemberID   string `json:"member_id"`
		Choice     string `json:"choice"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid Payload", http.StatusBadRequest)
		return
	}

	if err := h.Service.CastVote(r.Context(), req.ProposalID, req.MemberID, req.Choice); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	json.NewEncoder(w).Encode(map[string]string{"status": "VOTE_CAST"})
}

// GET /network/stats
func (h *GovernanceHandler) GetNetworkStats(w http.ResponseWriter, r *http.Request) {
	stats := h.Service.GetNetworkGrowth()
	json.NewEncoder(w).Encode(stats)
}
