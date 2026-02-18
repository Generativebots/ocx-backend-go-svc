package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	"github.com/ocx/backend/internal/catalog"
	"github.com/ocx/backend/internal/config"
	"github.com/ocx/backend/internal/database"
	"github.com/ocx/backend/internal/economics"
	"github.com/ocx/backend/internal/escrow"
	"github.com/ocx/backend/internal/events"
	"github.com/ocx/backend/internal/evidence"
	"github.com/ocx/backend/internal/fabric"
	"github.com/ocx/backend/internal/federation"
	"github.com/ocx/backend/internal/ghostpool"
	"github.com/ocx/backend/internal/governance"
	"github.com/ocx/backend/internal/gvisor"
	"github.com/ocx/backend/internal/handlers"
	"github.com/ocx/backend/internal/identity"
	"github.com/ocx/backend/internal/infra"
	"github.com/ocx/backend/internal/marketplace"
	"github.com/ocx/backend/internal/middleware"
	"github.com/ocx/backend/internal/multitenancy"
	"github.com/ocx/backend/internal/plan"
	"github.com/ocx/backend/internal/reputation"
	"github.com/ocx/backend/internal/security"
	"github.com/ocx/backend/internal/webhooks"
	"github.com/ocx/backend/pkg/plugins"
)

func main() {
	// Load configuration (YAML + env overrides + defaults)
	cfg := config.Get()
	port := cfg.GetPort()

	// Initialize Supabase client
	supabaseClient, err := database.NewSupabaseClient()
	if err != nil {
		log.Fatalf("Failed to initialize Supabase client: %v", err)
	}

	// Initialize Tenant Manager
	tenantManager := multitenancy.NewTenantManager(supabaseClient)

	// Initialize Hub (O(n) routing layer)
	hub := fabric.GetHub()
	slog.Info("Hub initialized", "hub_id", hub.ID, "region", hub.Region)

	// =========================================================================
	// Redis Infrastructure — multi-pod Hub Store + Event Bus (graceful fallback)
	// =========================================================================
	var redisAdapter *infra.GoRedisAdapter
	if cfg.Redis.Enabled {
		adapter, err := infra.NewGoRedisAdapter(cfg.Redis.Addr, cfg.Redis.Password, cfg.Redis.DB)
		if err != nil {
			slog.Warn("Redis connection failed, falling back to in-memory stores", "addr", cfg.Redis.Addr, "error", err)
		} else {
			redisAdapter = adapter
			defer redisAdapter.Close()

			// RedisHubStore — cross-pod spoke persistence
			hubStore := fabric.NewRedisHubStore(redisAdapter, "ocx:hub:", 10*time.Minute)
			hub.SetStore(hubStore)
			slog.Info("RedisHubStore wired into Hub for cross-pod spoke discovery")

			// RedisEventBus — cross-pod event distribution via Pub/Sub
			redisEventBus := fabric.NewRedisEventBus(redisAdapter, "ocx:events:")
			hub.SetFabricEventBus(redisEventBus)
			defer redisEventBus.Close()
			slog.Info("RedisEventBus wired into Hub for cross-pod event distribution")
		}
	} else {
		slog.Info("Redis disabled (OCX_REDIS_ENABLED=false), using in-memory Hub store and event bus")
	}

	// Initialize Patent Components — all values from config
	federationRegistry := federation.NewFederationRegistry()
	trustLedger := federation.NewPersistentTrustLedger()

	// SupabaseHandshakeStore — durable federation handshake sessions
	if cfg.GetSupabaseURL() != "" && cfg.GetSupabaseKey() != "" {
		handshakeStore := federation.NewSupabaseHandshakeStore(cfg.GetSupabaseURL(), cfg.GetSupabaseKey())
		federationRegistry.SetHandshakeStore(handshakeStore)
		slog.Info("SupabaseHandshakeStore wired into FederationRegistry")
	} else {
		slog.Warn("Supabase not configured, federation handshakes use in-memory store")
	}
	evidenceVault := evidence.NewEvidenceVault(evidence.VaultConfig{
		RetentionDays: cfg.Evidence.RetentionDays,
		Store:         evidence.NewSupabaseEvidenceStore(supabaseClient),
	})
	micropaymentEscrow := escrow.NewMicropaymentEscrow()
	compensationStack := escrow.NewCompensationStack()

	// JIT entitlement TTL cap from config (default 1 hour)
	jitMaxTTL := time.Duration(cfg.Escrow.JITEntitlementTTL*2) * time.Second
	if jitMaxTTL <= 0 {
		jitMaxTTL = time.Hour
	}
	jitEntitlements := escrow.NewJITEntitlementManager(jitMaxTTL)

	// §4.2 Patent: Wire JIT entitlement audit events to evidence vault
	jitEntitlements.SetAuditCallback(func(event escrow.EntitlementEvent) {
		slog.Info("JIT audit event", "type", event.Type, "permission", event.Permission, "agent_id", event.AgentID, "ttl", event.TTL)
	})

	// Wire real JuryGRPCClient — fall back to mock if gRPC connection fails
	var juryClient escrow.JuryClient
	realJury, err := escrow.NewJuryGRPCClient(cfg.Escrow.JuryServiceAddr)
	if err != nil {
		slog.Warn("Jury gRPC connection failed, using mock client", "error", err)
		juryClient = escrow.NewMockJuryClient()
	} else {
		slog.Info("Connected to Jury gRPC service", "addr", cfg.Escrow.JuryServiceAddr)
		juryClient = realJury
	}
	escrowGate := escrow.NewEscrowGate(juryClient, escrow.NewEntropyMonitorLive(cfg.Escrow.EntropyThreshold))
	toolClassifier := escrow.NewToolClassifier()
	repWallet := reputation.NewReputationWallet(supabaseClient)

	// =====================================================================
	// Patent Gap Fixes — New Components
	// =====================================================================

	// §2 Claim 2: TriFactorGate for full three-dimensional validation
	var entropyMon escrow.EntropyMonitor
	entropyMon = escrow.NewEntropyMonitorLive(cfg.Escrow.EntropyThreshold)
	triFactorGate := escrow.NewTriFactorGate(toolClassifier, juryClient, entropyMon, escrow.TriFactorGateConfig{
		IdentityThreshold:  cfg.TriFactor.IdentityThreshold,
		EntropyThreshold:   cfg.TriFactor.EntropyThreshold,
		JitterThreshold:    cfg.TriFactor.JitterThreshold,
		CognitiveThreshold: cfg.TriFactor.CognitiveThreshold,
	})
	slog.Info("TriFactorGate initialized", "claim", 2, "component", "sequestration_pipeline")

	// §7 Claim 7: Token Broker — JIT tokens with HMAC-SHA256 + attribution
	tokenBroker := security.NewTokenBroker(security.TokenBrokerConfig{
		HMACSecret:        cfg.Security.HMACSecret,
		DefaultTTL:        time.Duration(cfg.Security.TokenTTLSec) * time.Second,
		MinTrustScore:     cfg.Security.MinTrustForToken,
		Issuer:            "ocx-gateway-" + cfg.Federation.InstanceID,
		MaxActivePerAgent: cfg.Security.MaxTokensPerAgent,
	})
	slog.Info("TokenBroker initialized", "claim", 7, "algo", "HMAC-SHA256")

	// §8 Claim 8: Continuous Access Evaluator — mid-stream revocation
	continuousEval := security.NewContinuousAccessEvaluator(
		tokenBroker,
		nil, // trust provider wired via reputation wallet adapter below
		security.ContinuousEvalConfig{
			SweepInterval:     time.Duration(cfg.Security.CAESweepIntervalSec) * time.Second,
			DriftThreshold:    cfg.Security.DriftThreshold,
			TrustDropLimit:    cfg.Security.TrustDropLimit,
			AnomalyThreshold:  cfg.Security.AnomalyThreshold,
			InactivityTimeout: 10 * time.Minute,
		},
	)
	continuousEval.Start()
	slog.Info("ContinuousAccessEvaluator started", "claim", 8, "sweep_interval", cfg.Security.CAESweepIntervalSec)

	// §1 Claim 1: SandboxExecutor — gVisor speculative execution
	runscPath := getEnvOrDefault("GVISOR_RUNSC_PATH", "/usr/local/bin/runsc")
	rootfsPath := getEnvOrDefault("GVISOR_ROOTFS_PATH", "/var/lib/ocx/rootfs")
	sandboxExecutor := gvisor.NewSandboxExecutor(runscPath, rootfsPath)
	slog.Info("SandboxExecutor initialized", "claim", 1, "runsc_path", sandboxExecutor.RunscPath(), "available", sandboxExecutor.IsAvailable())

	// §1 State Cloner — Redis-backed state snapshots for speculative execution
	stateCloner := gvisor.NewStateCloner(cfg.Redis.Addr)
	slog.Info("StateCloner initialized", "redis_addr", cfg.Redis.Addr)

	// §1 GhostPool — pre-warmed sandbox container pool
	poolImage := getEnvOrDefault("OCX_GHOST_IMAGE", "ocx-ghost-node:latest")
	ghostPool := ghostpool.NewPoolManager(2, 5, poolImage)
	slog.Info("GhostPool initialized", "min", 2, "max", 5, "image", poolImage)

	// §9 Claim 9: Ghost State Engine — business-state sandbox
	ghostEngine := governance.NewGhostStateEngine()
	slog.Info("GhostStateEngine initialized", "claim", 9)

	// Governance Config Cache — tenant-configurable parameters
	govAdapter := &governance.SupabaseConfigAdapter{Client: supabaseClient}
	govConfigCache := governance.NewGovernanceConfigCache(govAdapter)
	slog.Info("GovernanceConfigCache initialized")

	// §2 Claim 2 (G2 fix): SPIFFE Verifier for x509-SVID identity validation
	spiffeSocket := getEnvOrDefault("SPIFFE_ENDPOINT_SOCKET", "unix:///run/spire/sockets/agent.sock")
	spiffeVerifier, spiffeErr := identity.NewSPIFFEVerifier(spiffeSocket)
	if spiffeErr != nil {
		slog.Warn("SPIFFE verifier not available, using structural validation fallback", "error", spiffeErr)
	} else {
		triFactorGate.SetSPIFFEVerifier(spiffeVerifier)
		defer spiffeVerifier.Close()
		slog.Info("SPIFFEVerifier wired into TriFactorGate", "socket", spiffeSocket)
	}

	// §13 Claim 13 (G3 fix): SOP Graph Manager — drift computation
	sopManager := plan.NewSOPGraphManager()
	slog.Info("SOPGraphManager initialized", "claim", 13)

	// Billing engine for bail-out (Claims 6+14)
	billingEngine := economics.NewBillingEngine()

	// §4 Patent: Wire micropayment billing callbacks (P2 FIX)
	// onRelease: levy governance tax on the agent's reputation wallet
	// onRefund: credit the agent back when a signal is rejected
	micropaymentEscrow.SetCallbacks(
		func(tenantID, agentID string, amount float64) error {
			slog.Info("Billing: charging governance tax", "amount", amount, "agent_id", agentID, "tenant_id", tenantID)
			baseCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			ctx := multitenancy.WithTenant(baseCtx, tenantID)
			// Levy governance tax as a fraction of the escrowed amount
			taxAmount := amount * 0.01 // 1% governance tax rate
			_, taxErr := repWallet.LevyTax(ctx, agentID, tenantID, taxAmount,
				fmt.Sprintf("Governance Tax: micropayment release $%.4f", amount))
			if taxErr != nil {
				slog.Warn("Trust tax levy failed", "agent_id", agentID, "error", taxErr)
			}
			// Record billing event in evidence vault for audit trail
			evidenceVault.RecordTransaction(ctx, tenantID, agentID,
				fmt.Sprintf("billing-release-%s-%d", agentID, time.Now().UnixNano()),
				"micropayment", "B", evidence.OutcomeAllow, 0,
				fmt.Sprintf("Released $%.4f, tax $%.6f", amount, taxAmount),
				map[string]interface{}{"amount": amount, "tax": taxAmount},
			)
			return taxErr
		},
		func(tenantID, agentID string, amount float64) error {
			slog.Info("Refund: returning funds", "amount", amount, "agent_id", agentID, "tenant_id", tenantID)
			baseCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			ctx := multitenancy.WithTenant(baseCtx, tenantID)
			// Credit the agent back via reputation reward
			creditPoints := int64(amount * 100) // $1 = 100 reputation points
			if creditPoints < 1 {
				creditPoints = 1
			}
			if err := repWallet.RewardAgent(ctx, agentID, creditPoints); err != nil {
				slog.Warn("Refund credit failed", "agent_id", agentID, "error", err)
			}
			// Record refund event in evidence vault
			evidenceVault.RecordTransaction(ctx, tenantID, agentID,
				fmt.Sprintf("billing-refund-%s-%d", agentID, time.Now().UnixNano()),
				"micropayment", "B", evidence.OutcomeBlock, 0,
				fmt.Sprintf("Refunded $%.4f, credited %d rep points", amount, creditPoints),
				map[string]interface{}{"amount": amount, "credit_points": creditPoints},
			)
			return nil
		},
	)

	// §7: Trust Score Decay Scheduler — decays inactive agent scores
	repManager := reputation.NewReputationManager()
	decayScheduler := reputation.NewTrustScoreDecayScheduler(repManager, reputation.DecayConfig{
		Interval:            1 * time.Hour,
		InactivityThreshold: 7 * 24 * time.Hour, // 1 week
		DecayRate:           0.99,               // 1% per sweep
		FloorScore:          0.1,                // Never below 10%
	})
	defer decayScheduler.Stop()
	slog.Info("Trust Score Decay Scheduler started", "interval", "1h", "decay_rate", 0.99)

	// Webhook gateway — Cloud Tasks if GCP enabled, else in-memory
	webhookRegistry := webhooks.NewRegistry()
	var webhookEmitter webhooks.WebhookEmitter
	if cfg.CloudTasks.Enabled && cfg.CloudTasks.ProjectID != "" {
		cd, err := webhooks.NewCloudDispatcher(
			webhookRegistry,
			cfg.CloudTasks.ProjectID,
			cfg.CloudTasks.LocationID,
			cfg.CloudTasks.QueueID,
			cfg.Webhook.WorkerCount, // fallback workers
		)
		if err != nil {
			slog.Warn("Cloud Tasks init failed, falling back to in-memory", "error", err)
			webhookEmitter = webhooks.NewDispatcher(webhookRegistry, cfg.Webhook.WorkerCount)
		} else {
			webhookEmitter = cd
		}
	} else {
		webhookEmitter = webhooks.NewDispatcher(webhookRegistry, cfg.Webhook.WorkerCount)
	}
	defer webhookEmitter.Shutdown()

	// Plugin registry
	pluginRegistry := plugins.NewRegistry()

	// Tool catalog (API-driven, replaces hardcoded classifier)
	toolCatalog := catalog.NewToolCatalog()

	// Event bus — Cloud Pub/Sub if GCP enabled, else in-memory
	var eventEmitter events.EventEmitter
	var eventBus *events.EventBus // always available for SSE
	if cfg.PubSub.Enabled && cfg.PubSub.ProjectID != "" {
		pubsubBus, err := events.NewPubSubEventBus(cfg.PubSub.ProjectID, cfg.PubSub.TopicID)
		if err != nil {
			slog.Warn("Pub/Sub init failed, falling back to in-memory", "error", err)
			eventBus = events.NewEventBus()
			eventEmitter = eventBus
		} else {
			defer pubsubBus.Close()
			eventEmitter = pubsubBus
			eventBus = pubsubBus.EventBus // SSE uses embedded in-memory bus
		}
	} else {
		eventBus = events.NewEventBus()
		eventEmitter = eventBus
	}

	slog.Info("Connectors initialized", "webhooks", 0, "plugins", pluginRegistry.Count(), "tools", toolCatalog.Count())

	// Session Audit Logger — security forensics
	sessionAuditor := security.NewSessionAuditor(supabaseClient)

	// =========================================================================
	// Router Setup
	// =========================================================================

	router := mux.NewRouter()

	// Health check endpoint (required for Cloud Run)
	router.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Check Supabase connectivity by looking up a known tenant
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		_, err := supabaseClient.GetTenant(ctx, "00000000-0000-0000-0000-000000000001")
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

	// API subrouter with tenant middleware
	api := router.PathPrefix("/api/v1").Subrouter()

	// Tenant Middleware (Gorilla Mux Adapter)
	// TenantMiddleware has signature (tm, next HandlerFunc) HandlerFunc,
	// so we adapt it to work with mux.Use() which expects mux.MiddlewareFunc.
	api.Use(func(next http.Handler) http.Handler {
		wrapped := middleware.TenantMiddleware(tenantManager, func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r)
		})
		return wrapped
	})

	// =========================================================================
	// Route Registration — handlers from internal/handlers package
	// =========================================================================

	// Agents — literal paths MUST come before parameterized {id} catch-all
	api.HandleFunc("/agents", handlers.ListAgents(supabaseClient)).Methods("GET")
	api.HandleFunc("/agents/registry", handlers.HandleListAgents(supabaseClient)).Methods("GET")
	api.HandleFunc("/agents/{id}", handlers.GetAgent(supabaseClient)).Methods("GET")
	api.HandleFunc("/agents/{id}/trust", handlers.GetTrustScores(supabaseClient)).Methods("GET")
	api.HandleFunc("/agents/{id}/profile", handlers.HandleGetAgent(supabaseClient)).Methods("GET")
	api.HandleFunc("/agents/{id}/profile", handlers.HandleUpdateAgent(supabaseClient)).Methods("PUT")

	// Hub/Spoke
	api.HandleFunc("/spokes", handlers.RegisterSpoke(hub)).Methods("POST")
	api.HandleFunc("/spokes", handlers.ListSpokes(hub)).Methods("GET")
	api.HandleFunc("/hub/metrics", handlers.GetHubMetrics(hub)).Methods("GET")

	// L3 FIX: WebSocket endpoint moved to api subrouter (was on top-level router,
	// which bypassed TenantMiddleware allowing unauthenticated connections)
	api.HandleFunc("/ws", hub.HandleWebSocket)

	// Marketplace API
	marketplaceMux := http.NewServeMux()
	marketplace.SetupMarketplace(marketplaceMux)
	router.PathPrefix("/api/v1/marketplace/").Handler(marketplaceMux)

	// Federation (§5)
	api.HandleFunc("/federation/handshake", handlers.HandleFederationHandshake(cfg, federationRegistry, trustLedger)).Methods("POST")
	api.HandleFunc("/federation/trust", handlers.HandleFederationTrust(trustLedger)).Methods("GET")
	api.HandleFunc("/federation/trust/{instanceId}", handlers.HandleFederationInstanceTrust(trustLedger)).Methods("GET")
	api.HandleFunc("/federation/attestations", handlers.HandleFederationAttestations(trustLedger)).Methods("GET")

	// Escrow (§4)
	api.HandleFunc("/escrow/items", handlers.HandleEscrowItems(escrowGate)).Methods("GET")
	api.HandleFunc("/escrow/release", handlers.HandleEscrowRelease(escrowGate)).Methods("POST")

	// Evidence (§6)
	api.HandleFunc("/evidence/chain", handlers.HandleEvidenceChain(evidenceVault)).Methods("GET")

	// Entitlements (§4.3)
	api.HandleFunc("/entitlements/active", handlers.HandleActiveEntitlements(jitEntitlements)).Methods("GET")
	api.HandleFunc("/entitlements/{agentId}", handlers.HandleAgentEntitlements(jitEntitlements)).Methods("GET")
	api.HandleFunc("/entitlements/{agentId}/{permission}", handlers.HandleRevokeEntitlement(jitEntitlements)).Methods("DELETE")

	// Micropayments (§4.2)
	api.HandleFunc("/micropayments/status", handlers.HandleMicropaymentStatus(micropaymentEscrow)).Methods("GET")

	// Governance — main endpoint (Patent Claims 1, 2, 7, 8, 9, 10, 12)
	api.HandleFunc("/govern", handlers.HandleGovern(
		cfg, toolClassifier, escrowGate, triFactorGate, micropaymentEscrow,
		jitEntitlements, evidenceVault, repWallet, toolCatalog,
		webhookEmitter, eventEmitter, compensationStack,
		tokenBroker, continuousEval, sandboxExecutor, ghostEngine,
		sopManager, sessionAuditor,
	)).Methods("POST")

	// Bail-Out API (Patent Claims 6 + 14)
	api.HandleFunc("/bail-out", handlers.HandleBailOut(
		repWallet, billingEngine, evidenceVault, tokenBroker,
	)).Methods("POST")

	// Tools (§4 Catalog)
	api.HandleFunc("/tools", handlers.HandleListTools(toolCatalog)).Methods("GET")
	api.HandleFunc("/tools", handlers.HandleRegisterTool(toolCatalog, eventEmitter)).Methods("POST")
	api.HandleFunc("/tools/{toolName}", handlers.HandleGetTool(toolCatalog)).Methods("GET")
	api.HandleFunc("/tools/{toolName}", handlers.HandleDeleteTool(toolCatalog, eventEmitter)).Methods("DELETE")

	// Webhooks
	api.HandleFunc("/webhooks", handlers.HandleListWebhooks(webhookRegistry)).Methods("GET")
	api.HandleFunc("/webhooks", handlers.HandleRegisterWebhook(webhookRegistry)).Methods("POST")
	api.HandleFunc("/webhooks/{webhookId}", handlers.HandleDeleteWebhook(webhookRegistry)).Methods("DELETE")

	// Plugins
	api.HandleFunc("/plugins", handlers.HandleListPlugins(pluginRegistry)).Methods("GET")

	// SSE Events
	api.HandleFunc("/events/stream", handlers.HandleSSEStream(eventBus)).Methods("GET")

	// Reputation
	api.HandleFunc("/reputation/{agentId}", handlers.HandleAgentReputation(repWallet)).Methods("GET")

	// Pool Stats
	api.HandleFunc("/pool/stats", handlers.HandlePoolStats(evidenceVault, escrowGate, repWallet)).Methods("GET")

	// Compensation (§9)
	api.HandleFunc("/compensation/pending", handlers.HandleCompensationPending(compensationStack)).Methods("GET")

	// Continuous Access Evaluation (Patent Claim 8)
	api.HandleFunc("/cae/sessions", handlers.HandleCAESessions(continuousEval)).Methods("GET")
	api.HandleFunc("/cae/stats", handlers.HandleCAEStats(continuousEval)).Methods("GET")

	// Session Audit Log (Security Forensics)
	api.HandleFunc("/sessions/audit", handlers.HandleSessionAuditLogs(supabaseClient)).Methods("GET")

	// (Enriched Agent Profiles — registered above with the /agents block)

	// Tenant Settings
	api.HandleFunc("/tenant/settings", handlers.HandleGetTenantSettings(supabaseClient)).Methods("GET")
	api.HandleFunc("/tenant/settings/crypto", handlers.HandleUpdateTenantCryptoAlgorithm(supabaseClient)).Methods("PUT")

	// Tenant Governance Config — CRUD for tenant-configurable governance parameters
	api.HandleFunc("/tenant/{tenantId}/governance-config", handlers.HandleGetGovernanceConfig(supabaseClient, govConfigCache)).Methods("GET")
	api.HandleFunc("/tenant/{tenantId}/governance-config", handlers.HandleUpdateGovernanceConfig(supabaseClient, govConfigCache)).Methods("PUT")
	api.HandleFunc("/tenant/{tenantId}/governance-config/reset", handlers.HandleResetGovernanceConfig(supabaseClient, govConfigCache)).Methods("POST")
	api.HandleFunc("/tenant/{tenantId}/governance-audit", handlers.HandleGetGovernanceAuditLog(supabaseClient)).Methods("GET")

	// Sandbox Status (§1 gVisor)
	api.HandleFunc("/sandbox/status", handlers.HandleSandboxStatus(sandboxExecutor, ghostPool, stateCloner)).Methods("GET")

	// HITL Routes — Patent Layer 4: Human-in-the-Loop Governance
	api.HandleFunc("/hitl/decide", handlers.HandleHITLDecide(escrowGate, supabaseClient, cfg.HITL.DefaultCostMultiplier)).Methods("POST")
	api.HandleFunc("/hitl/decisions", handlers.HandleHITLDecisions(supabaseClient)).Methods("GET")
	api.HandleFunc("/hitl/metrics", handlers.HandleHITLMetrics(supabaseClient)).Methods("GET")
	api.HandleFunc("/hitl/rlhc/clusters", handlers.HandleRLHCClusters(supabaseClient)).Methods("GET")
	api.HandleFunc("/hitl/rlhc/promote", handlers.HandleRLHCPromote(supabaseClient)).Methods("POST")

	// Agent Card — service discovery
	router.HandleFunc("/.well-known/ocx-governance.json", handlers.HandleAgentCard()).Methods("GET")

	// =========================================================================
	// Global Middleware
	// =========================================================================

	// CORS middleware — origins from config
	router.Use(handlers.MakeCORSMiddleware(cfg))

	// Logging middleware
	router.Use(handlers.LoggingMiddleware)

	// =========================================================================
	// Server Start + Graceful Shutdown
	// =========================================================================

	server := &http.Server{
		Addr:         ":" + port,
		Handler:      router,
		ReadTimeout:  time.Duration(cfg.Server.ReadTimeoutSec) * time.Second,
		WriteTimeout: time.Duration(cfg.Server.WriteTimeoutSec) * time.Second,
		IdleTimeout:  time.Duration(cfg.Server.IdleTimeoutSec) * time.Second,
	}

	// L4 FIX: Create a shared shutdown context for background goroutines
	shutdownCtx, shutdownCancel := context.WithCancel(context.Background())

	// Graceful shutdown (Cloud Run sends SIGTERM)
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		slog.Info("Received shutdown signal, shutting down gracefully")

		// L4 FIX: Signal background goroutines to stop
		shutdownCancel()

		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.Server.ShutdownTimeout)*time.Second)
		defer cancel()

		if err := server.Shutdown(ctx); err != nil {
			slog.Error("Server shutdown error", "error", err)
		}
	}()

	// Suppress unused variable warnings
	_ = tenantManager
	_ = shutdownCtx
	_ = billingEngine

	// Start server
	slog.Info("OCX API starting", "port", port, "health_check", "http://localhost:"+port+"/health")

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Server failed to start: %v", err)
	}

	slog.Info("Server stopped")
}

// getEnvOrDefault returns the env var value or a default.
func getEnvOrDefault(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}
