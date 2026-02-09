// Package marketplace — HTTP Handlers (Gap 3 Fix)
//
// REST API endpoints for the marketplace service. Registers routes
// on an http.ServeMux for integration with the OCX gateway.
package marketplace

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
)

// Handler provides HTTP endpoints for the marketplace.
type Handler struct {
	svc *Service
}

// NewHandler creates a new marketplace HTTP handler.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// RegisterRoutes registers all marketplace endpoints on the given mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	// Connectors
	mux.HandleFunc("GET /api/v1/marketplace/connectors", h.listConnectors)
	mux.HandleFunc("GET /api/v1/marketplace/connectors/{id}", h.getConnector)
	mux.HandleFunc("POST /api/v1/marketplace/connectors", h.publishConnector)

	// Templates
	mux.HandleFunc("GET /api/v1/marketplace/templates", h.listTemplates)
	mux.HandleFunc("GET /api/v1/marketplace/templates/{id}", h.getTemplate)
	mux.HandleFunc("POST /api/v1/marketplace/templates", h.publishTemplate)

	// Search
	mux.HandleFunc("GET /api/v1/marketplace/search", h.searchAll)

	// Installations (tenant-scoped)
	mux.HandleFunc("GET /api/v1/marketplace/installations", h.listInstallations)
	mux.HandleFunc("POST /api/v1/marketplace/install/connector", h.installConnector)
	mux.HandleFunc("POST /api/v1/marketplace/install/template", h.installTemplate)
	mux.HandleFunc("POST /api/v1/marketplace/uninstall", h.uninstallItem)
	mux.HandleFunc("POST /api/v1/marketplace/installations/{id}/pause", h.pauseInstallation)
	mux.HandleFunc("POST /api/v1/marketplace/installations/{id}/resume", h.resumeInstallation)

	// Billing
	mux.HandleFunc("GET /api/v1/marketplace/billing/summary", h.billingSummary)

	// Revenue (publisher-scoped)
	mux.HandleFunc("GET /api/v1/marketplace/revenue/summary", h.revenueSummary)
	mux.HandleFunc("GET /api/v1/marketplace/revenue/analytics", h.revenueAnalytics)

	log.Println("📦 Marketplace API routes registered")
}

// --- Helper ---

func extractTenantID(r *http.Request) string {
	tid := r.Header.Get("X-Tenant-ID")
	if tid == "" {
		tid = r.URL.Query().Get("tenant_id")
	}
	if tid == "" {
		tid = "default"
	}
	return tid
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("JSON encode error: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// --- Connector Endpoints ---

func (h *Handler) listConnectors(w http.ResponseWriter, r *http.Request) {
	tenantID := extractTenantID(r)
	category := r.URL.Query().Get("category")
	connectors := h.svc.ListConnectors(tenantID, category)
	writeJSON(w, http.StatusOK, connectors)
}

func (h *Handler) getConnector(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "connector id required")
		return
	}
	conn, err := h.svc.GetConnector(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, conn)
}

type publishConnectorReq struct {
	ID           string                 `json:"id"`
	Name         string                 `json:"name"`
	Slug         string                 `json:"slug"`
	Description  string                 `json:"description"`
	Category     string                 `json:"category"`
	Version      string                 `json:"version"`
	ConfigSchema map[string]interface{} `json:"config_schema"`
	Actions      []ConnectorAction      `json:"actions"`
}

func (h *Handler) publishConnector(w http.ResponseWriter, r *http.Request) {
	tenantID := extractTenantID(r)
	var req publishConnectorReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	conn := &Connector{
		ID:           req.ID,
		Name:         req.Name,
		Slug:         req.Slug,
		Description:  req.Description,
		Category:     ConnectorCategory(req.Category),
		Version:      req.Version,
		ConfigSchema: req.ConfigSchema,
		Actions:      req.Actions,
		IsPublic:     true,
	}
	if err := h.svc.connectorMgr.PublishConnector(tenantID, conn); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, conn)
}

// --- Template Endpoints ---

func (h *Handler) listTemplates(w http.ResponseWriter, r *http.Request) {
	tenantID := extractTenantID(r)
	category := r.URL.Query().Get("category")
	templates := h.svc.ListTemplates(tenantID, category)
	writeJSON(w, http.StatusOK, templates)
}

func (h *Handler) getTemplate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "template id required")
		return
	}
	tmpl, err := h.svc.GetTemplate(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, tmpl)
}

type publishTemplateReq struct {
	ID             string                 `json:"id"`
	Name           string                 `json:"name"`
	Slug           string                 `json:"slug"`
	Description    string                 `json:"description"`
	Category       string                 `json:"category"`
	Version        string                 `json:"version"`
	EBCLDefinition map[string]interface{} `json:"ebcl_definition"`
	Dependencies   []string               `json:"dependencies"`
	IndustryTags   []string               `json:"industry_tags"`
	StepCount      int                    `json:"step_count"`
}

func (h *Handler) publishTemplate(w http.ResponseWriter, r *http.Request) {
	tenantID := extractTenantID(r)
	var req publishTemplateReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	tmpl := &Template{
		ID:             req.ID,
		Name:           req.Name,
		Slug:           req.Slug,
		Description:    req.Description,
		Category:       TemplateCategory(req.Category),
		Version:        req.Version,
		EBCLDefinition: req.EBCLDefinition,
		Dependencies:   req.Dependencies,
		IndustryTags:   req.IndustryTags,
		StepCount:      req.StepCount,
		IsPublic:       true,
	}
	if err := h.svc.templateMgr.PublishTemplate(tenantID, tmpl); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, tmpl)
}

// --- Search ---

type searchResult struct {
	Connectors []*Connector `json:"connectors"`
	Templates  []*Template  `json:"templates"`
}

func (h *Handler) searchAll(w http.ResponseWriter, r *http.Request) {
	tenantID := extractTenantID(r)
	query := r.URL.Query().Get("q")
	if query == "" {
		writeError(w, http.StatusBadRequest, "query parameter 'q' required")
		return
	}
	connectors, templates := h.svc.SearchAll(tenantID, query)
	writeJSON(w, http.StatusOK, searchResult{
		Connectors: connectors,
		Templates:  templates,
	})
}

// --- Installation Endpoints ---

func (h *Handler) listInstallations(w http.ResponseWriter, r *http.Request) {
	tenantID := extractTenantID(r)
	installations := h.svc.GetInstallations(tenantID)
	writeJSON(w, http.StatusOK, installations)
}

type installReq struct {
	ItemID string                 `json:"item_id"`
	Config map[string]interface{} `json:"config"`
}

func (h *Handler) installConnector(w http.ResponseWriter, r *http.Request) {
	tenantID := extractTenantID(r)
	var req installReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	inst, err := h.svc.installer.InstallConnector(tenantID, req.ItemID, req.Config)
	if err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
		} else if strings.Contains(err.Error(), "already installed") {
			status = http.StatusConflict
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, inst)
}

func (h *Handler) installTemplate(w http.ResponseWriter, r *http.Request) {
	tenantID := extractTenantID(r)
	var req installReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	inst, err := h.svc.installer.InstallTemplate(tenantID, req.ItemID, req.Config)
	if err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
		} else if strings.Contains(err.Error(), "requires") {
			status = http.StatusConflict
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, inst)
}

type uninstallReq struct {
	InstallationID string `json:"installation_id"`
}

func (h *Handler) uninstallItem(w http.ResponseWriter, r *http.Request) {
	tenantID := extractTenantID(r)
	var req uninstallReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if err := h.svc.installer.Uninstall(tenantID, req.InstallationID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "uninstalled"})
}

func (h *Handler) pauseInstallation(w http.ResponseWriter, r *http.Request) {
	tenantID := extractTenantID(r)
	instID := r.PathValue("id")
	if err := h.svc.installer.PauseInstallation(tenantID, instID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "paused"})
}

func (h *Handler) resumeInstallation(w http.ResponseWriter, r *http.Request) {
	tenantID := extractTenantID(r)
	instID := r.PathValue("id")
	if err := h.svc.installer.ResumeInstallation(tenantID, instID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "active"})
}

// --- Billing ---

func (h *Handler) billingSummary(w http.ResponseWriter, r *http.Request) {
	tenantID := extractTenantID(r)
	summary := h.svc.billing.GetSummary(tenantID)
	writeJSON(w, http.StatusOK, summary)
}

// --- Revenue ---

func (h *Handler) revenueSummary(w http.ResponseWriter, r *http.Request) {
	publisherID := r.URL.Query().Get("publisher_id")
	if publisherID == "" {
		publisherID = extractTenantID(r)
	}
	if h.svc.revenueMgr == nil {
		writeError(w, http.StatusServiceUnavailable, "revenue tracking not available")
		return
	}
	summary := h.svc.revenueMgr.GetPublisherSummary(publisherID)
	writeJSON(w, http.StatusOK, summary)
}

func (h *Handler) revenueAnalytics(w http.ResponseWriter, r *http.Request) {
	publisherID := r.URL.Query().Get("publisher_id")
	if publisherID == "" {
		publisherID = extractTenantID(r)
	}
	if h.svc.revenueMgr == nil {
		writeError(w, http.StatusServiceUnavailable, "revenue tracking not available")
		return
	}
	analytics := h.svc.revenueMgr.GetPublisherAnalytics(publisherID)
	writeJSON(w, http.StatusOK, analytics)
}

// SetupMarketplace initializes the marketplace and registers all HTTP routes.
// Call this from your main() to wire everything together.
func SetupMarketplace(mux *http.ServeMux) *Handler {
	svc := NewService()
	handler := NewHandler(svc)
	handler.RegisterRoutes(mux)

	log.Printf("📦 Marketplace ready: %d connectors, %d templates, 17 API endpoints",
		len(svc.connectors), len(svc.templates))

	return handler
}
