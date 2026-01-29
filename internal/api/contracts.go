package api

import (
	"encoding/json"
	"net/http"

	"github.com/ocx/backend/internal/multitenancy"
	"github.com/ocx/backend/internal/service"
)

type ContractHandler struct {
	Service *service.ContractService
}

func NewContractHandler(svc *service.ContractService) *ContractHandler {
	return &ContractHandler{Service: svc}
}

// POST /contracts/deploy
func (h *ContractHandler) Deploy(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name       string `json:"name"`
		EBCLSource string `json:"ebcl_source"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid Payload", http.StatusBadRequest)
		return
	}

	tenantID, _ := multitenancy.GetTenantID(r.Context())
	id, err := h.Service.DeployContract(r.Context(), tenantID, req.Name, req.EBCLSource)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]string{"contract_id": id, "status": "DEPLOYED"})
}

// POST /contracts/validate-interaction
func (h *ContractHandler) ValidateInteraction(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UseCaseKey string `json:"use_case_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid Payload", http.StatusBadRequest)
		return
	}

	tenantID, _ := multitenancy.GetTenantID(r.Context())
	valid, err := h.Service.ValidateInteraction(r.Context(), tenantID, req.UseCaseKey)
	if err != nil {
		// Valid check logic but invalid outcome
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}

	json.NewEncoder(w).Encode(map[string]bool{"valid": valid})
}
