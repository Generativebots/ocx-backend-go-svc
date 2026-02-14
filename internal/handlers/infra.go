package handlers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/ocx/backend/internal/config"
	"github.com/ocx/backend/internal/events"
	"github.com/ocx/backend/internal/fabric"
)

// SpokeRegistrationRequest is the request body for spoke registration.
type SpokeRegistrationRequest struct {
	AgentID      string   `json:"agent_id"`
	Capabilities []string `json:"capabilities"`
	TrustScore   float64  `json:"trust_score"`
	Entitlements []string `json:"entitlements,omitempty"`
}

// RegisterSpoke registers a new spoke with the hub.
func RegisterSpoke(hub *fabric.Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req SpokeRegistrationRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		// Convert string capabilities to fabric.Capability
		caps := make([]fabric.Capability, len(req.Capabilities))
		for i, c := range req.Capabilities {
			caps[i] = fabric.Capability(c)
		}

		spoke, err := hub.RegisterSpoke("", req.AgentID, caps, req.TrustScore, req.Entitlements)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		slog.Info("Agent connected to hub (capabilities=, trust=)", "agent_i_d", req.AgentID, "capabilities", req.Capabilities, "trust_score", req.TrustScore)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"agent_id":  req.AgentID,
			"hub_id":    hub.ID,
			"spoke_id":  spoke.ID,
			"status":    "connected",
			"region":    hub.Region,
			"spoke_url": "/ws",
		})
	}
}

// ListSpokes returns all connected spokes.
func ListSpokes(hub *fabric.Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		spokes := hub.GetSpokes()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"spokes": spokes,
			"count":  len(spokes),
		})
	}
}

// GetHubMetrics returns hub routing and network metrics.
func GetHubMetrics(hub *fabric.Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		metrics := hub.GetMetrics()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"hub_id":           hub.ID,
			"region":           hub.Region,
			"connected_spokes": metrics.SpokesConnected.Load(),
			"total_routed":     metrics.MessagesRouted.Load(),
			"peers_connected":  metrics.PeersConnected.Load(),
		})
	}
}

// MakeCORSMiddleware returns CORS middleware using config origins.
// Properly handles multiple allowed origins by matching against the request's
// Origin header, which is the only spec-compliant approach.
// Supports wildcard patterns (e.g. "https://*.run.app") by suffix matching.
func MakeCORSMiddleware(cfg *config.Config) mux.MiddlewareFunc {
	// Separate exact origins from wildcard patterns
	exact := make(map[string]bool, len(cfg.Server.CORSAllowOrigins))
	var wildcardSuffixes []string
	allowAll := false
	for _, o := range cfg.Server.CORSAllowOrigins {
		if o == "*" {
			allowAll = true
		} else if strings.Contains(o, "*") {
			// Convert "https://*.run.app" → suffix ".run.app" with scheme "https://"
			// This handles the common *.domain pattern.
			suffix := strings.Replace(o, "*", "", 1)
			wildcardSuffixes = append(wildcardSuffixes, suffix)
		} else {
			exact[o] = true
		}
	}

	// originAllowed checks whether the request origin is permitted.
	originAllowed := func(origin string) bool {
		if exact[origin] {
			return true
		}
		for _, suffix := range wildcardSuffixes {
			// suffix is e.g. "https://.run.app"
			// Split into scheme and domain suffix
			parts := strings.SplitN(suffix, "//", 2)
			if len(parts) == 2 {
				scheme := parts[0] + "//"
				domainSuffix := parts[1]
				if strings.HasPrefix(origin, scheme) && strings.HasSuffix(origin, domainSuffix) {
					return true
				}
			} else if strings.HasSuffix(origin, suffix) {
				return true
			}
		}
		return false
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")

			// Determine what to set as Access-Control-Allow-Origin
			if allowAll {
				w.Header().Set("Access-Control-Allow-Origin", "*")
			} else if origin != "" && originAllowed(origin) {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				// Vary must be set when the response depends on the Origin header
				w.Header().Set("Vary", "Origin")
			}

			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers",
				"Content-Type, Authorization, X-Tenant-ID, X-API-Key, X-Request-ID, Accept")
			w.Header().Set("Access-Control-Max-Age", "86400")

			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// LoggingMiddleware logs each request in JSON format.
func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Call next handler
		next.ServeHTTP(w, r)

		// Log in Cloud Run compatible format (JSON)
		slog.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	})
}

// HandleSSEStream handles Server-Sent Events streaming.
func HandleSSEStream(bus *events.EventBus) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "SSE not supported", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		// Parse event type filters
		eventFilter := r.URL.Query().Get("events")
		var eventTypes []string
		if eventFilter != "" {
			eventTypes = strings.Split(eventFilter, ",")
		}

		ch := bus.Subscribe(eventTypes...)
		defer bus.Unsubscribe(ch)

		// Send initial connection event
		fmt.Fprintf(w, "event: connected\ndata: {\"status\":\"connected\"}\n\n")
		flusher.Flush()

		for {
			select {
			case event, ok := <-ch:
				if !ok {
					return
				}
				sseData, err := event.SSEFormat()
				if err != nil {
					continue
				}
				w.Write(sseData)
				flusher.Flush()

			case <-r.Context().Done():
				return
			}
		}
	}
}

// HandleAgentCard returns the service discovery card.
func HandleAgentCard() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		json.NewEncoder(w).Encode(map[string]interface{}{
			"name":    "OCX Governance Gateway",
			"version": "1.0.0",
			"description": "Universal AI Agent Governance — classify, escrow, " +
				"and audit any AI tool call across any framework.",
			"capabilities": []string{
				"govern", "classify", "escrow", "micropayment",
				"entitlement", "evidence", "reputation", "federation",
			},
			"endpoints": map[string]string{
				"govern":       "/api/v1/govern",
				"tools":        "/api/v1/tools",
				"webhooks":     "/api/v1/webhooks",
				"plugins":      "/api/v1/plugins",
				"events":       "/api/v1/events/stream",
				"reputation":   "/api/v1/reputation/{agentId}",
				"pool_stats":   "/api/v1/pool/stats",
				"escrow":       "/api/v1/escrow/items",
				"evidence":     "/api/v1/evidence/chain",
				"entitlements": "/api/v1/entitlements/active",
				"health":       "/health",
			},
			"supported_protocols": []string{
				"mcp", "openai", "a2a", "langchain", "crewai",
				"autogen", "semantic-kernel", "haystack", "rag", "generic",
			},
			"sdk": map[string]string{
				"go":     "github.com/ocx/backend/pkg/sdk",
				"python": "pip install ocx-sdk",
				"cli":    "go install github.com/ocx/backend/cmd/ocx-cli@latest",
			},
			"authentication": "Bearer token via Authorization header",
		})
	}
}
