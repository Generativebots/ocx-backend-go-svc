package api

import (
	"encoding/json"
	"net/http"

	"github.com/ocx/backend/internal/multitenancy"
	"github.com/ocx/backend/internal/service"
)

type SimulationHandler struct {
	Service *service.SimulationService
}

func NewSimulationHandler(svc *service.SimulationService) *SimulationHandler {
	return &SimulationHandler{Service: svc}
}

// POST /simulation/batch
func (h *SimulationHandler) RunBatch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ScenarioID string `json:"scenario_id"`
		BatchSize  int    `json:"batch_size"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid Payload", http.StatusBadRequest)
		return
	}

	tenantID, _ := multitenancy.GetTenantID(r.Context())
	runID, err := h.Service.RunBatchSimulation(r.Context(), tenantID, req.ScenarioID, req.BatchSize)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	json.NewEncoder(w).Encode(map[string]string{"run_id": runID, "status": "QUEUED"})
}
