package api

import (
	"encoding/json"
	"net/http"

	"github.com/ocx/backend/internal/multitenancy"
	"github.com/ocx/backend/internal/service"
)

type ImpactHandler struct {
	Service *service.ImpactService
}

func NewImpactHandler(svc *service.ImpactService) *ImpactHandler {
	return &ImpactHandler{Service: svc}
}

// POST /impact/calculate
func (h *ImpactHandler) Calculate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Assumptions map[string]float64 `json:"assumptions"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid Payload", http.StatusBadRequest)
		return
	}

	tenantID, _ := multitenancy.GetTenantID(r.Context())
	result := h.Service.CalculateROI(r.Context(), tenantID, req.Assumptions)

	json.NewEncoder(w).Encode(result)
}
