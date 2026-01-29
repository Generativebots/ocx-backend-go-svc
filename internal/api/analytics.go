package api

import (
	"encoding/json"
	"net/http"

	"github.com/ocx/backend/internal/multitenancy"
	"github.com/ocx/backend/internal/service"
)

type AnalyticsHandler struct {
	Service *service.AnalyticsService
}

func NewAnalyticsHandler(svc *service.AnalyticsService) *AnalyticsHandler {
	return &AnalyticsHandler{Service: svc}
}

// GET /analytics/dashboard
func (h *AnalyticsHandler) GetDashboard(w http.ResponseWriter, r *http.Request) {
	tenantID, _ := multitenancy.GetTenantID(r.Context())
	stats := h.Service.GetDashboardStats(r.Context(), tenantID)
	json.NewEncoder(w).Encode(stats)
}

// POST /analytics/event (Internal/Test use)
func (h *AnalyticsHandler) TrackEvent(w http.ResponseWriter, r *http.Request) {
	var req struct {
		MetricName string  `json:"metric_name"`
		Value      float64 `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid Payload", http.StatusBadRequest)
		return
	}

	tenantID, _ := multitenancy.GetTenantID(r.Context())
	err := h.Service.TrackEvent(r.Context(), tenantID, req.MetricName, req.Value)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusAccepted)
}
