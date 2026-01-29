package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	"github.com/ocx/backend/internal/database"
	"github.com/ocx/backend/internal/middleware"
	"github.com/ocx/backend/internal/multitenancy"
)

func main() {
	// Get port from environment (Cloud Run requirement)
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080" // Default for local development
	}

	// Initialize Supabase client
	supabaseClient, err := database.NewSupabaseClient()
	if err != nil {
		log.Fatalf("Failed to initialize Supabase client: %v", err)
	}

	// Initialize Tenant Manager
	tenantManager := multitenancy.NewTenantManager(supabaseClient)

	// Create router
	router := mux.NewRouter()

	// Health check endpoint (required for Cloud Run)
	router.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Check Supabase connectivity
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		// Use a system/admin check or default tenant for health check
		// We'll trust the connection check without tenant context for now or use "default-org"
		_, err := supabaseClient.GetTenant(ctx, "default-org")
		supabaseStatus := "connected"
		if err != nil {
			supabaseStatus = "error"
		}

		json.NewEncoder(w).Encode(map[string]string{
			"status":   "healthy",
			"service":  "ocx-api",
			"supabase": supabaseStatus,
		})
	}).Methods("GET")

	// API routes
	api := router.PathPrefix("/api/v1").Subrouter()

	// Tenant Middleware (Gorilla Mux Adapter)
	api.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Wrap next.ServeHTTP to match http.HandlerFunc signature
			middleware.TenantMiddleware(tenantManager, next.ServeHTTP)(w, r)
		})
	})

	// Agents endpoints
	api.HandleFunc("/agents", listAgents(supabaseClient)).Methods("GET")
	api.HandleFunc("/agents/{id}", getAgent(supabaseClient)).Methods("GET")
	api.HandleFunc("/agents/{id}/trust", getTrustScores(supabaseClient)).Methods("GET")

	// CORS middleware for Cloud Run
	router.Use(corsMiddleware)

	// Logging middleware
	router.Use(loggingMiddleware)

	// Create server
	server := &http.Server{
		Addr:         ":" + port,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Graceful shutdown (Cloud Run sends SIGTERM)
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("Received shutdown signal, shutting down gracefully...")

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := server.Shutdown(ctx); err != nil {
			log.Printf("Server shutdown error: %v", err)
		}
	}()

	// Start server
	log.Printf("🚀 OCX API starting on port %s", port)
	log.Printf("📊 Health check: http://localhost:%s/health", port)

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Server failed to start: %v", err)
	}

	log.Println("Server stopped")
}

// Handler functions
func listAgents(client *database.SupabaseClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := multitenancy.GetTenantID(r.Context())
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		agents, err := client.ListAgents(r.Context(), tenantID, 100)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(agents)
	}
}

func getAgent(client *database.SupabaseClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		agentID := vars["id"]

		tenantID, err := multitenancy.GetTenantID(r.Context())
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		agent, err := client.GetAgent(r.Context(), tenantID, agentID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if agent == nil {
			http.Error(w, "Agent not found", http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(agent)
	}
}

func getTrustScores(client *database.SupabaseClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		agentID := vars["id"]

		tenantID, err := multitenancy.GetTenantID(r.Context())
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		scores, err := client.GetTrustScores(r.Context(), tenantID, agentID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if scores == nil {
			http.Error(w, "Trust scores not found", http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(scores)
	}
}

// Middleware
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Tenant-ID")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Call next handler
		next.ServeHTTP(w, r)

		// Log in Cloud Run compatible format (JSON)
		log.Printf(`{"method":"%s","path":"%s","duration_ms":%d}`,
			r.Method,
			r.URL.Path,
			time.Since(start).Milliseconds(),
		)
	})
}
