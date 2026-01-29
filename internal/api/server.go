package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/mux"

	"github.com/ocx/backend/internal/escrow"
	"github.com/ocx/backend/internal/ghostpool"
	"github.com/ocx/backend/internal/reputation"
)

// APIServer exposes the internal microservices via REST/JSON for the React Frontend.
type APIServer struct {
	pool       *ghostpool.PoolManager
	escrow     *escrow.EscrowGate
	reputation *reputation.ReputationWallet
}

func NewAPIServer(pool *ghostpool.PoolManager, escrow *escrow.EscrowGate, rep *reputation.ReputationWallet) *APIServer {
	return &APIServer{
		pool:       pool,
		escrow:     escrow,
		reputation: rep,
	}
}

func (s *APIServer) Start(port int) error {
	r := mux.NewRouter()

	// CORS Middleware
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)
				return
			}
			next.ServeHTTP(w, r)
		})
	})

	// --- Endpoints ---

	// 1. Ghost Pool Stats
	r.HandleFunc("/api/pool/stats", s.handlePoolStats).Methods("GET")

	// 2. Escrow Items
	r.HandleFunc("/api/escrow/items", s.handleEscrowItems).Methods("GET")
	r.HandleFunc("/api/escrow/release", s.handleEscrowRelease).Methods("POST")

	// 3. Reputation
	r.HandleFunc("/api/reputation/{agent_id}", s.handleReputation).Methods("GET")

	addr := fmt.Sprintf(":%d", port)
	log.Printf("🚀 API Gateway listening on %s", addr)
	return http.ListenAndServe(addr, r)
}

// --- Handlers ---

// Helper to get tenant
func getTenantID(r *http.Request) string {
	tid := r.Header.Get("X-Tenant-ID")
	if tid == "" {
		return "default" // Fallback for dev/demo
	}
	return tid
}

func (s *APIServer) handlePoolStats(w http.ResponseWriter, r *http.Request) {
	tenantID := getTenantID(r)
	// In reality, query PoolManager for this specific tenant's stats
	// s.pool.GetStats(tenantID)

	// Stub: In reality, we'd query the PoolManager's internal state
	stats := map[string]interface{}{
		"active_containers":      12, // Placeholder
		"idle_containers":        5,
		"total_capacity":         20,
		"avg_recycle_latency_ms": 340.5,
		"tenant_id":              tenantID,
	}
	json.NewEncoder(w).Encode(stats)
}

func (s *APIServer) handleEscrowItems(w http.ResponseWriter, r *http.Request) {
	tenantID := getTenantID(r)
	// Mock items held in escrow for this tenant
	items := []map[string]interface{}{
		{
			"id":        "escrow-123",
			"agent_id":  "agent-alpha",
			"tenant_id": tenantID,
			"payload":   "Execute Payment $500",
			"status":    "HELD",
			"timestamp": time.Now(),
		},
		{
			"id":        "escrow-456",
			"agent_id":  "agent-beta",
			"tenant_id": tenantID,
			"payload":   "Delete User Record",
			"status":    "HELD",
			"timestamp": time.Now().Add(-2 * time.Minute),
		},
	}
	json.NewEncoder(w).Encode(items)
}

func (s *APIServer) handleEscrowRelease(w http.ResponseWriter, r *http.Request) {
	tenantID := getTenantID(r)
	var req struct {
		EscrowID string `json:"escrow_id"`
		Decision string `json:"decision"` // APPROVED / REJECTED
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Call internal EscrowGate with Tenant Context
	// Note: EscrowGate.ProcessSignal needs to verify tenant ownership in real impl
	// s.escrow.ProcessSignal(req.EscrowID, "JuryOverride", req.Decision == "APPROVED")

	log.Printf("Tenant %s processing Escrow %s: %s", tenantID, req.EscrowID, req.Decision)

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "processed", "tenant_id": tenantID})
}

func (s *APIServer) handleReputation(w http.ResponseWriter, r *http.Request) {
	tenantID := getTenantID(r)
	vars := mux.Vars(r)
	agentID := vars["agent_id"]

	score, _ := s.reputation.GetTrustScore(context.Background(), agentID, tenantID)

	resp := map[string]interface{}{
		"agent_id":  agentID,
		"tenant_id": tenantID,
		"score":     score,
		"tier":      "GOLD", // Logic would derive this
	}
	json.NewEncoder(w).Encode(resp)
}
