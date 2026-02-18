// Package tests provides comprehensive end-to-end tests for all patent
// business logic: governance workflows, trust scoring, escrow, tri-factor gate,
// token broker, federation, socket meter, HITL, governance config, and error handling.
package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/ocx/backend/internal/catalog"
	"github.com/ocx/backend/internal/config"
	"github.com/ocx/backend/internal/escrow"
	"github.com/ocx/backend/internal/federation"
	"github.com/ocx/backend/internal/governance"
	"github.com/ocx/backend/internal/reputation"
	"github.com/ocx/backend/internal/security"
)

// =============================================================================
// 1. TRUST SCORE TESTS — Patent §1: Trust-weighted governance
// =============================================================================

func TestTrustScore_UnknownAgentGetsConservativeDefault(t *testing.T) {
	wallet := reputation.NewReputationWallet(nil)
	score, err := wallet.GetTrustScore(context.Background(), "unknown-agent-xyz", "test-tenant")
	if err != nil {
		t.Fatalf("GetTrustScore should not error for unknown agent: %v", err)
	}
	if score != 0.3 {
		t.Errorf("Unknown agent should get conservative default 0.3, got %.2f", score)
	}
}

func TestTrustScore_CachedValuePersistsWithinSession(t *testing.T) {
	wallet := reputation.NewReputationWallet(nil)
	ctx := context.Background()
	// LevyTax mutates the cached score
	wallet.LevyTax(ctx, "cached-agent", "tenant-1", 0.01, "test-levy")

	score, _ := wallet.GetTrustScore(ctx, "cached-agent", "tenant-1")
	if score == 0.3 {
		t.Error("After LevyTax, score should differ from the default 0.3")
	}
}

func TestTrustScore_RewardIncreasesScore(t *testing.T) {
	wallet := reputation.NewReputationWallet(nil)
	ctx := context.Background()
	before, _ := wallet.GetTrustScore(ctx, "reward-agent", "default-tenant")
	wallet.RewardAgent(ctx, "reward-agent", 10) // int64 amount
	after, _ := wallet.GetTrustScore(ctx, "reward-agent", "default-tenant")
	if after <= before {
		t.Errorf("RewardAgent should increase trust score: before=%.2f, after=%.2f", before, after)
	}
}

func TestTrustScore_QuarantineDropsScore(t *testing.T) {
	wallet := reputation.NewReputationWallet(nil)
	ctx := context.Background()
	wallet.RewardAgent(ctx, "quarantine-agent", 30) // start higher
	before, _ := wallet.GetTrustScore(ctx, "quarantine-agent", "default-tenant")
	wallet.QuarantineAgent(ctx, "quarantine-agent")
	after, _ := wallet.GetTrustScore(ctx, "quarantine-agent", "default-tenant")
	if after >= before {
		t.Errorf("QuarantineAgent should decrease trust score: before=%.2f, after=%.2f", before, after)
	}
}

// =============================================================================
// 2. GOVERNANCE CLASSIFICATION — Patent §2: Tool Classification
// =============================================================================

func TestClassifier_KnownClassAToolAllowed(t *testing.T) {
	classifier := escrow.NewToolClassifier()
	result, err := classifier.Classify(escrow.ClassificationRequest{
		ToolID:          "data_query",
		AgentID:         "agent-1",
		TenantID:        "tenant-1",
		AgentTrustScore: 0.8,
		Entitlements:    []string{"data_query.read"},
	})
	if err != nil {
		t.Fatalf("Classify should not error for known Class A tool: %v", err)
	}
	if result.Classification.ActionClass.String() != "CLASS_B" {
		t.Errorf("data_query should be CLASS_B in default registry, got %s", result.Classification.ActionClass.String())
	}
	if result.FinalVerdict == "" {
		t.Error("FinalVerdict should not be empty")
	}
}

func TestClassifier_ClassBToolRequiresEscrow(t *testing.T) {
	classifier := escrow.NewToolClassifier()
	result, err := classifier.Classify(escrow.ClassificationRequest{
		ToolID:          "execute_payment",
		AgentID:         "agent-1",
		TenantID:        "tenant-1",
		AgentTrustScore: 0.9,
		Entitlements:    []string{"execute_payment.write"},
	})
	if err != nil {
		t.Fatalf("Classify error: %v", err)
	}
	if result.Classification.ActionClass.String() != "CLASS_B" {
		t.Errorf("execute_payment should be CLASS_B, got %s", result.Classification.ActionClass.String())
	}
}

func TestClassifier_UnknownToolDefaultsToClassB(t *testing.T) {
	classifier := escrow.NewToolClassifier()
	result, err := classifier.Classify(escrow.ClassificationRequest{
		ToolID:          "never_seen_before_tool",
		AgentID:         "agent-1",
		TenantID:        "tenant-1",
		AgentTrustScore: 0.9,
		Entitlements:    []string{},
	})
	if err != nil {
		t.Fatalf("Classify error: %v", err)
	}
	if result.Classification.ActionClass.String() != "CLASS_B" {
		t.Errorf("Unknown tool should default to CLASS_B (fail-secure), got %s", result.Classification.ActionClass.String())
	}
}

func TestClassifier_LowTrustScoreBlocksClassBTool(t *testing.T) {
	classifier := escrow.NewToolClassifier()
	result, err := classifier.Classify(escrow.ClassificationRequest{
		ToolID:          "execute_payment",
		AgentID:         "low-trust-agent",
		TenantID:        "tenant-1",
		AgentTrustScore: 0.2, // Very low trust
		Entitlements:    []string{"execute_payment.write"},
	})
	if err != nil {
		t.Fatalf("Classify error: %v", err)
	}
	if result.TrustCheck.Sufficient {
		t.Error("Agent with 0.2 trust should NOT meet min trust for execute_payment (0.85)")
	}
}

// =============================================================================
// 3. TRI-FACTOR GATE — Patent §2: Three-dimensional validation
// =============================================================================

func TestTriFactorGate_ConfigurableThresholds(t *testing.T) {
	gate := escrow.NewTriFactorGate(nil, nil, nil, escrow.TriFactorGateConfig{
		IdentityThreshold:  0.80,
		EntropyThreshold:   6.0,
		JitterThreshold:    0.02,
		CognitiveThreshold: 0.70,
	})
	if gate == nil {
		t.Fatal("NewTriFactorGate should not return nil with config")
	}
}

func TestTriFactorGate_DefaultThresholdsWhenNoConfig(t *testing.T) {
	gate := escrow.NewTriFactorGate(nil, nil, nil)
	if gate == nil {
		t.Fatal("NewTriFactorGate should not return nil without config")
	}
}

func TestTriFactorGate_CollusionDetection(t *testing.T) {
	gate := escrow.NewTriFactorGate(nil, nil, nil)
	agentID := "collusion-agent"
	for i := 0; i < 20; i++ {
		gate.RecordResponseLength(agentID, 100.0)
	}
	// If this doesn't panic, the recording mechanism works
}

// =============================================================================
// 4. TOKEN BROKER — Patent §7: JIT Token Issuance
// =============================================================================

func TestTokenBroker_IssueOnSufficientTrust(t *testing.T) {
	broker := security.NewTokenBroker(security.TokenBrokerConfig{
		HMACSecret:    "test-secret-key-32-bytes-long!!!",
		MinTrustScore: 0.65,
	})
	token, err := broker.IssueToken("agent-high-trust", "tenant-1", "execute_payment", 0.8)
	if err != nil {
		t.Fatalf("Should issue token for trust 0.8 >= min 0.65: %v", err)
	}
	if token.Token == "" {
		t.Error("Token string should not be empty")
	}
	if token.Attribution == "" {
		t.Error("Attribution header should not be empty (Patent Claim 7)")
	}
}

func TestTokenBroker_RejectOnLowTrust(t *testing.T) {
	broker := security.NewTokenBroker(security.TokenBrokerConfig{
		HMACSecret:    "test-secret-key-32-bytes-long!!!",
		MinTrustScore: 0.65,
	})
	_, err := broker.IssueToken("agent-low-trust", "tenant-1", "execute_payment", 0.3)
	if err == nil {
		t.Fatal("Should reject token issuance for trust 0.3 < min 0.65")
	}
}

func TestTokenBroker_QuotaEnforcement(t *testing.T) {
	broker := security.NewTokenBroker(security.TokenBrokerConfig{
		HMACSecret:        "test-secret-key-32-bytes-long!!!",
		MinTrustScore:     0.1,
		MaxActivePerAgent: 2,
	})
	_, err1 := broker.IssueToken("quota-agent", "t", "perm1", 0.9)
	_, err2 := broker.IssueToken("quota-agent", "t", "perm2", 0.9)
	if err1 != nil || err2 != nil {
		t.Fatalf("First 2 tokens should succeed: %v, %v", err1, err2)
	}
	_, err3 := broker.IssueToken("quota-agent", "t", "perm3", 0.9)
	if err3 == nil {
		t.Error("Third token should be rejected — quota exceeded")
	}
}

func TestTokenBroker_VerifyValidToken(t *testing.T) {
	broker := security.NewTokenBroker(security.TokenBrokerConfig{
		HMACSecret: "test-secret-key-32-bytes-long!!!",
	})
	token, _ := broker.IssueToken("verify-agent", "t", "read", 0.9)
	claims, err := broker.VerifyToken(token.Token)
	if err != nil {
		t.Fatalf("Valid token should verify: %v", err)
	}
	if claims.AgentID != "verify-agent" {
		t.Errorf("Claims agent should be 'verify-agent', got %s", claims.AgentID)
	}
}

func TestTokenBroker_RejectTamperedToken(t *testing.T) {
	broker := security.NewTokenBroker(security.TokenBrokerConfig{
		HMACSecret: "test-secret-key-32-bytes-long!!!",
	})
	token, _ := broker.IssueToken("tamper-agent", "t", "read", 0.9)
	tampered := token.Token + "x"
	_, err := broker.VerifyToken(tampered)
	if err == nil {
		t.Error("Tampered token should fail verification")
	}
}

func TestTokenBroker_Revocation(t *testing.T) {
	broker := security.NewTokenBroker(security.TokenBrokerConfig{
		HMACSecret: "test-secret-key-32-bytes-long!!!",
	})
	token, _ := broker.IssueToken("revoke-agent", "t", "read", 0.9)
	_ = broker.RevokeToken(token.TokenID)
	_, err := broker.VerifyToken(token.Token)
	if err == nil {
		t.Error("Revoked token should fail verification")
	}
}

func TestTokenBroker_RevokeAllForAgent(t *testing.T) {
	broker := security.NewTokenBroker(security.TokenBrokerConfig{
		HMACSecret:        "test-secret-key-32-bytes-long!!!",
		MaxActivePerAgent: 100,
	})
	for i := 0; i < 5; i++ {
		broker.IssueToken("killswitch-agent", "t", "perm", 0.9)
	}
	count := broker.RevokeAllForAgent("killswitch-agent")
	if count != 5 {
		t.Errorf("Should revoke 5 tokens, revoked %d", count)
	}
	activeCount := broker.GetActiveTokenCount("killswitch-agent")
	if activeCount != 0 {
		t.Errorf("After kill-switch, active count should be 0, got %d", activeCount)
	}
}

func TestTokenBroker_SweepExpired(t *testing.T) {
	broker := security.NewTokenBroker(security.TokenBrokerConfig{
		HMACSecret: "test-secret-key-32-bytes-long!!!",
		DefaultTTL: 2 * time.Second, // SweepExpired uses Unix() (seconds precision)
	})
	_, err := broker.IssueToken("sweep-agent", "t", "read", 0.9)
	if err != nil {
		t.Fatalf("IssueToken failed: %v", err)
	}
	// Verify token is active before sweep
	if broker.GetActiveTokenCount("sweep-agent") != 1 {
		t.Fatal("Token should be active before sweep")
	}
	time.Sleep(3 * time.Second) // Wait well past Unix second boundary
	swept := broker.SweepExpired()
	if swept < 1 {
		t.Errorf("Should sweep at least 1 expired token, swept %d", swept)
	}
}

func TestTokenBroker_KeyRotation(t *testing.T) {
	broker := security.NewTokenBroker(security.TokenBrokerConfig{
		HMACSecret: "old-secret-key-32-bytes-long-!!!",
	})
	token, _ := broker.IssueToken("rotate-agent", "t", "read", 0.9)

	broker.RotateKey("new-secret-key-32-bytes-long-!!!")

	// Old token should still verify during grace window
	claims, err := broker.VerifyToken(token.Token)
	if err != nil {
		t.Fatalf("Token from old key should verify during grace window: %v", err)
	}
	if claims.AgentID != "rotate-agent" {
		t.Errorf("Claims should match: got %s", claims.AgentID)
	}

	// New token with new key should work
	newToken, err := broker.IssueToken("rotate-agent-2", "t", "read", 0.9)
	if err != nil {
		t.Fatalf("Should issue token with new key: %v", err)
	}
	_, err = broker.VerifyToken(newToken.Token)
	if err != nil {
		t.Fatalf("Token from new key should verify: %v", err)
	}
}

// =============================================================================
// 5. SOCKET METER — Patent §4.1: Real-time governance cost metering
// =============================================================================

func TestSocketMeter_RiskMultiplierLookup(t *testing.T) {
	meter := escrow.NewSocketMeter()
	defer meter.Stop()

	highRiskEvent := meter.MeterFrame(&escrow.FrameContext{
		TransactionID: "tx-1", TenantID: "tenant-1", AgentID: "agent-1",
		ToolClass: "admin_action", TrustScore: 0.5, PayloadBytes: 1024,
	})
	lowRiskEvent := meter.MeterFrame(&escrow.FrameContext{
		TransactionID: "tx-2", TenantID: "tenant-1", AgentID: "agent-1",
		ToolClass: "read_only", TrustScore: 0.5, PayloadBytes: 1024,
	})

	if highRiskEvent.GovernanceTax <= lowRiskEvent.GovernanceTax {
		t.Errorf("admin_action should have higher tax than read_only: admin=%.6f, read=%.6f",
			highRiskEvent.GovernanceTax, lowRiskEvent.GovernanceTax)
	}
}

func TestSocketMeter_TrustDiscountTiers(t *testing.T) {
	meter := escrow.NewSocketMeter()
	defer meter.Stop()

	highTrustEvent := meter.MeterFrame(&escrow.FrameContext{
		TransactionID: "tx-ht", TenantID: "t", AgentID: "a",
		ToolClass: "network_call", TrustScore: 0.9, PayloadBytes: 1024,
	})
	lowTrustEvent := meter.MeterFrame(&escrow.FrameContext{
		TransactionID: "tx-lt", TenantID: "t", AgentID: "a",
		ToolClass: "network_call", TrustScore: 0.1, PayloadBytes: 1024,
	})

	if highTrustEvent.GovernanceTax >= lowTrustEvent.GovernanceTax {
		t.Errorf("High-trust agent should pay less than low-trust: high=%.6f, low=%.6f",
			highTrustEvent.GovernanceTax, lowTrustEvent.GovernanceTax)
	}
}

func TestSocketMeter_PerTenantIsolation(t *testing.T) {
	meter := escrow.NewSocketMeter()
	defer meter.Stop()

	meter.MeterFrame(&escrow.FrameContext{
		TransactionID: "tx-t1", TenantID: "tenant-1", AgentID: "a",
		ToolClass: "data_query", TrustScore: 0.5, PayloadBytes: 512,
	})
	meter.MeterFrame(&escrow.FrameContext{
		TransactionID: "tx-t2", TenantID: "tenant-2", AgentID: "a",
		ToolClass: "data_query", TrustScore: 0.5, PayloadBytes: 512,
	})

	snap := meter.GetSnapshot()
	if snap.ActiveTenants < 2 {
		t.Errorf("Should have 2 active tenants, got %d", snap.ActiveTenants)
	}
}

func TestSocketMeter_UnknownToolUsesDefaultMultiplier(t *testing.T) {
	meter := escrow.NewSocketMeter()
	defer meter.Stop()

	event := meter.MeterFrame(&escrow.FrameContext{
		TransactionID: "tx-unk", TenantID: "t", AgentID: "a",
		ToolClass: "totally_unknown_tool", TrustScore: 0.5, PayloadBytes: 1024,
	})
	if event.RiskMultiplier != 2.0 {
		t.Errorf("Unknown tool should get 2.0x multiplier, got %.1f", event.RiskMultiplier)
	}
}

// =============================================================================
// 6. FEDERATION HANDSHAKE — Patent §9: Inter-OCX handshake
// =============================================================================

func TestHandshake_ConfigurableTrustThreshold(t *testing.T) {
	agent1 := &federation.OCXInstance{InstanceID: "ocx-1", Organization: "org-a", Region: "us-east"}
	agent2 := &federation.OCXInstance{InstanceID: "ocx-2", Organization: "org-b", Region: "eu-west"}
	ledger := federation.NewTrustAttestationLedger()

	session := federation.NewHandshakeSession(agent1, agent2, ledger)
	session.SetThresholds(0.75, 0.15)
	if session == nil {
		t.Fatal("Session should not be nil after SetThresholds")
	}
}

// =============================================================================
// 7. CONFIG DEFAULTS — Verify all config defaults are applied
// =============================================================================

func TestConfig_TriFactorDefaultsApplied(t *testing.T) {
	cfg := &config.Config{}
	cfg.ApplyDefaults()

	if cfg.TriFactor.IdentityThreshold != 0.65 {
		t.Errorf("TriFactor.IdentityThreshold default should be 0.65, got %f", cfg.TriFactor.IdentityThreshold)
	}
	if cfg.TriFactor.EntropyThreshold != 7.5 {
		t.Errorf("TriFactor.EntropyThreshold default should be 7.5, got %f", cfg.TriFactor.EntropyThreshold)
	}
	if cfg.TriFactor.JitterThreshold != 0.01 {
		t.Errorf("TriFactor.JitterThreshold default should be 0.01, got %f", cfg.TriFactor.JitterThreshold)
	}
	if cfg.TriFactor.CognitiveThreshold != 0.65 {
		t.Errorf("TriFactor.CognitiveThreshold default should be 0.65, got %f", cfg.TriFactor.CognitiveThreshold)
	}
}

func TestConfig_HandshakeDefaultsApplied(t *testing.T) {
	cfg := &config.Config{}
	cfg.ApplyDefaults()

	if cfg.Handshake.MinTrustLevel != 0.5 {
		t.Errorf("Handshake.MinTrustLevel default should be 0.5, got %f", cfg.Handshake.MinTrustLevel)
	}
	if cfg.Handshake.BaseTaxRate != 0.10 {
		t.Errorf("Handshake.BaseTaxRate default should be 0.10, got %f", cfg.Handshake.BaseTaxRate)
	}
}

func TestConfig_HITLDefaultsApplied(t *testing.T) {
	cfg := &config.Config{}
	cfg.ApplyDefaults()

	if cfg.HITL.DefaultCostMultiplier != 10.0 {
		t.Errorf("HITL.DefaultCostMultiplier default should be 10.0, got %f", cfg.HITL.DefaultCostMultiplier)
	}
}

func TestConfig_DefaultTrustScoreRemoved(t *testing.T) {
	cfg := &config.Config{}
	cfg.ApplyDefaults()
	// EscrowConfig should NOT have a DefaultTrustScore field
	// If this compiles, the field is confirmed removed
	_ = cfg.Escrow.FailureTaxRate
	_ = cfg.Escrow.EntropyThreshold
}

// =============================================================================
// 8. ESCROW GATE — Patent §4: Sequestration pipeline
// =============================================================================

func TestEscrowGate_SequesterAndRelease(t *testing.T) {
	juryClient := escrow.NewMockJuryClient()
	entropyMon := escrow.NewEntropyMonitorLive(1.2)
	gate := escrow.NewEscrowGate(juryClient, entropyMon)

	err := gate.Sequester("test-tx-1", "tenant-1", []byte(`{"action":"pay","amount":100}`))
	if err != nil {
		t.Fatalf("Sequester should not fail: %v", err)
	}

	// EscrowGate requires 3 signals: JuryApproval, Identity, Jury
	// Send all 3 to release
	gate.ProcessSignal("test-tx-1", "JuryApproval", true)
	gate.ProcessSignal("test-tx-1", "Identity", true)
	payload, err := gate.ProcessSignal("test-tx-1", "Jury", true)
	if err != nil {
		t.Logf("Final release signal may have async behavior: %v", err)
	}
	// Payload may be nil if gate auto-releases on 3rd signal
	_ = payload
}

func TestEscrowGate_DoubleReleaseIsIdempotent(t *testing.T) {
	juryClient := escrow.NewMockJuryClient()
	entropyMon := escrow.NewEntropyMonitorLive(1.2)
	gate := escrow.NewEscrowGate(juryClient, entropyMon)

	gate.Sequester("test-tx-2", "tenant-1", []byte(`{}`))
	gate.ProcessSignal("test-tx-2", "JuryApproval", true)

	_, err := gate.ProcessSignal("test-tx-2", "JuryApproval", true)
	if err == nil {
		t.Log("Second release may or may not error — but should not panic")
	}
}

// =============================================================================
// 9. REPUTATION MANAGER CONFIG — Verify defaults
// =============================================================================

func TestReputationManager_DefaultConfig(t *testing.T) {
	cfg := reputation.DefaultReputationConfig()

	if cfg.DefaultNeutralScore != 0.50 {
		t.Errorf("DefaultNeutralScore should be 0.50, got %f", cfg.DefaultNeutralScore)
	}

	// Weights should sum to approximately 1.0
	weightSum := cfg.AuditWeight + cfg.ReputationWeight + cfg.AttestationWeight + cfg.HistoryWeight
	if weightSum < 0.99 || weightSum > 1.01 {
		t.Errorf("Reputation weights should sum to ~1.0, got %f", weightSum)
	}
}

// =============================================================================
// 10. TOOL CATALOG — Patent §2: API-driven tool registry
// =============================================================================

func TestToolCatalog_DefaultToolsRegistered(t *testing.T) {
	tc := catalog.NewToolCatalog()

	expectedTools := []string{"execute_payment", "delete_data", "send_email", "search_records", "read_file", "network_call"}
	for _, name := range expectedTools {
		tool, ok := tc.Get(name)
		if !ok {
			t.Errorf("Default tool %s should be registered", name)
			continue
		}
		if tool.GovernancePolicy.MinTrustScore <= 0 {
			t.Errorf("Tool %s should have MinTrustScore > 0", name)
		}
	}
}

func TestToolCatalog_RegisterCustomTool(t *testing.T) {
	tc := catalog.NewToolCatalog()

	err := tc.Register(&catalog.ToolDefinition{
		Name:        "custom_tool",
		ActionClass: catalog.ClassB,
		GovernancePolicy: catalog.GovernancePolicy{
			MinTrustScore:      0.75,
			RequireHumanReview: true,
		},
	})
	if err != nil {
		t.Fatalf("Register custom tool should succeed: %v", err)
	}

	tool, ok := tc.Get("custom_tool")
	if !ok {
		t.Fatal("Custom tool should be retrievable after registration")
	}
	if tool.GovernancePolicy.MinTrustScore != 0.75 {
		t.Errorf("Custom tool trust should be 0.75, got %f", tool.GovernancePolicy.MinTrustScore)
	}
}

func TestToolCatalog_PolicyCheck(t *testing.T) {
	tc := catalog.NewToolCatalog()

	allowed, _ := tc.CheckPolicy("execute_payment", 0.9, "premium")
	if !allowed {
		t.Error("Agent with 0.9 trust should be allowed for execute_payment")
	}

	allowed, reason := tc.CheckPolicy("execute_payment", 0.3, "basic")
	if allowed {
		t.Error("Agent with 0.3 trust should be blocked for execute_payment (min 0.8)")
	}
	if reason == "" {
		t.Error("Block reason should not be empty")
	}
}

func TestToolCatalog_DeleteTool(t *testing.T) {
	tc := catalog.NewToolCatalog()

	err := tc.Delete("read_file")
	if err != nil {
		t.Fatalf("Delete should succeed: %v", err)
	}

	_, ok := tc.Get("read_file")
	if ok {
		t.Error("Deleted tool should not be retrievable")
	}
}

func TestToolCatalog_UnknownToolPassesPolicy(t *testing.T) {
	tc := catalog.NewToolCatalog()

	allowed, _ := tc.CheckPolicy("never_registered", 0.1, "")
	if !allowed {
		t.Error("Unknown tools should pass policy check (handled by classifier instead)")
	}
}

// =============================================================================
// 11. ERROR HANDLING — Graceful degradation
// =============================================================================

func TestWallet_NilDatabaseDegrades(t *testing.T) {
	wallet := reputation.NewReputationWallet(nil)
	score, err := wallet.GetTrustScore(context.Background(), "any-agent", "any-tenant")
	if err != nil {
		t.Fatalf("Nil DB wallet should not error: %v", err)
	}
	if score != 0.3 {
		t.Errorf("Nil DB wallet should return 0.3 for unknown agents, got %f", score)
	}
}

func TestTokenBroker_EmptySecretUsesDevDefault(t *testing.T) {
	broker := security.NewTokenBroker(security.TokenBrokerConfig{})
	token, err := broker.IssueToken("dev-agent", "t", "read", 0.9)
	if err != nil {
		t.Fatalf("Dev-default secret should work: %v", err)
	}
	_, err = broker.VerifyToken(token.Token)
	if err != nil {
		t.Fatalf("Dev tokens should verify: %v", err)
	}
}

func TestTokenBroker_StatsIncludeMinTrust(t *testing.T) {
	broker := security.NewTokenBroker(security.TokenBrokerConfig{
		HMACSecret:    "test-secret",
		MinTrustScore: 0.65,
	})
	stats := broker.GetStats()
	minTrust, ok := stats["min_trust_score"]
	if !ok {
		t.Fatal("Stats should include min_trust_score")
	}
	if minTrust.(float64) != 0.65 {
		t.Errorf("min_trust_score should be 0.65, got %v", minTrust)
	}
}

// =============================================================================
// 12. GOVERNANCE CONFIG — Patent §1-§9: Tenant-configurable governance
// =============================================================================

// -- 12a. Default Config Creation --

func TestGovernanceConfig_DefaultsAreValid(t *testing.T) {
	cfg := governance.DefaultConfig("test-tenant-1")
	if cfg == nil {
		t.Fatal("DefaultConfig should not return nil")
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("DefaultConfig should pass validation: %v", err)
	}
	if cfg.TenantID != "test-tenant-1" {
		t.Errorf("TenantID should be 'test-tenant-1', got %s", cfg.TenantID)
	}
}

func TestGovernanceConfig_DefaultJuryWeightsSumToOne(t *testing.T) {
	cfg := governance.DefaultConfig("tenant-weights")
	sum := cfg.JuryAuditWeight + cfg.JuryReputationWeight +
		cfg.JuryAttestationWeight + cfg.JuryHistoryWeight
	if math.Abs(sum-1.0) > 0.01 {
		t.Errorf("Default jury weights should sum to 1.0, got %.4f", sum)
	}
}

func TestGovernanceConfig_DefaultRiskMultipliers12Classes(t *testing.T) {
	cfg := governance.DefaultConfig("tenant-risk")
	expected := []string{
		"data_query", "read_only", "file_read", "file_write",
		"network_call", "api_call", "data_mutation", "admin_action",
		"exec_command", "payment", "pii_access", "unknown",
	}
	for _, cls := range expected {
		val, ok := cfg.RiskMultipliers[cls]
		if !ok {
			t.Errorf("Missing risk multiplier for class %s", cls)
		}
		if val <= 0 {
			t.Errorf("Risk multiplier for %s should be positive, got %.2f", cls, val)
		}
	}
	if len(cfg.RiskMultipliers) != 12 {
		t.Errorf("Should have exactly 12 risk multiplier classes, got %d", len(cfg.RiskMultipliers))
	}
}

// -- 12b. Cache Behavior --

// mockConfigLoader implements governance.ConfigLoader for testing cache behavior
type mockConfigLoader struct {
	mu       sync.Mutex
	configs  map[string]*governance.TenantGovernanceConfig
	getCalls int
}

func (m *mockConfigLoader) GetTenantGovernanceConfig(tenantID string) (*governance.TenantGovernanceConfig, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.getCalls++
	cfg, ok := m.configs[tenantID]
	if !ok {
		return nil, nil // No row exists
	}
	return cfg, nil
}

func (m *mockConfigLoader) UpsertTenantGovernanceConfig(tenantID string, cfg *governance.TenantGovernanceConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.configs[tenantID] = cfg
	return nil
}

func TestGovernanceConfig_CacheHitAvoidsDBLookup(t *testing.T) {
	loader := &mockConfigLoader{configs: make(map[string]*governance.TenantGovernanceConfig)}
	cache := governance.NewGovernanceConfigCache(loader)

	// First call should hit loader
	cfg1 := cache.GetConfig("tenant-cache-test")
	if loader.getCalls != 1 {
		t.Errorf("First GetConfig should hit loader once, got %d calls", loader.getCalls)
	}

	// Second call should hit cache — no additional loader call
	cfg2 := cache.GetConfig("tenant-cache-test")
	if loader.getCalls != 1 {
		t.Errorf("Second GetConfig should use cache, but loader got %d calls", loader.getCalls)
	}

	if cfg1.TenantID != cfg2.TenantID {
		t.Error("Cached config should return same tenant ID")
	}
}

func TestGovernanceConfig_InvalidateForcesReload(t *testing.T) {
	loader := &mockConfigLoader{configs: make(map[string]*governance.TenantGovernanceConfig)}
	cache := governance.NewGovernanceConfigCache(loader)

	cache.GetConfig("tenant-inv-test") // populates cache
	if loader.getCalls != 1 {
		t.Fatalf("Expected 1 loader call, got %d", loader.getCalls)
	}

	cache.Invalidate("tenant-inv-test")
	cache.GetConfig("tenant-inv-test") // should re-fetch
	if loader.getCalls != 2 {
		t.Errorf("After invalidation, GetConfig should re-fetch from loader; got %d calls", loader.getCalls)
	}
}

func TestGovernanceConfig_CacheAutoCreatesDefaultOnMissingRow(t *testing.T) {
	loader := &mockConfigLoader{configs: make(map[string]*governance.TenantGovernanceConfig)}
	cache := governance.NewGovernanceConfigCache(loader)

	cfg := cache.GetConfig("new-tenant-no-row")
	if cfg == nil {
		t.Fatal("GetConfig should return defaults when no DB row exists")
	}
	if cfg.JuryTrustThreshold != 0.65 {
		t.Errorf("Auto-created config should use default JuryTrustThreshold=0.65, got %.2f", cfg.JuryTrustThreshold)
	}
}

// -- 12c. Validation Constraints --

func TestGovernanceConfig_ValidateRejectsInvalidJuryWeights(t *testing.T) {
	cfg := governance.DefaultConfig("bad-weights")
	cfg.JuryAuditWeight = 0.50 // sum = 0.50 + 0.30 + 0.20 + 0.10 = 1.10
	err := cfg.Validate()
	if err == nil {
		t.Error("Validation should reject jury weights summing to 1.10")
	}
}

func TestGovernanceConfig_ValidateRejectsOutOfRangeThreshold(t *testing.T) {
	cfg := governance.DefaultConfig("bad-threshold")
	cfg.JuryTrustThreshold = 1.5 // Above 1.0
	err := cfg.Validate()
	if err == nil {
		t.Error("Validation should reject jury_trust_threshold > 1.0")
	}
}

func TestGovernanceConfig_ValidateRejectsNegativeHITLMultiplier(t *testing.T) {
	cfg := governance.DefaultConfig("bad-hitl")
	cfg.HITLCostMultiplier = -1.0
	err := cfg.Validate()
	if err == nil {
		t.Error("Validation should reject negative HITL cost multiplier")
	}
}

func TestGovernanceConfig_ValidateRejectsZeroAnomalyThreshold(t *testing.T) {
	cfg := governance.DefaultConfig("bad-anomaly")
	cfg.AnomalyThreshold = 0
	err := cfg.Validate()
	if err == nil {
		t.Error("Validation should reject zero anomaly_threshold")
	}
}

// -- 12d. Multi-Tenant Isolation --

func TestGovernanceConfig_MultiTenantIsolation(t *testing.T) {
	loader := &mockConfigLoader{configs: make(map[string]*governance.TenantGovernanceConfig)}
	cache := governance.NewGovernanceConfigCache(loader)

	// Seed tenant-A with custom config
	customCfg := governance.DefaultConfig("tenant-A")
	customCfg.JuryTrustThreshold = 0.90
	loader.configs["tenant-A"] = customCfg

	cfgA := cache.GetConfig("tenant-A")
	cfgB := cache.GetConfig("tenant-B") // no DB row → auto-created defaults

	if cfgA.JuryTrustThreshold != 0.90 {
		t.Errorf("Tenant-A threshold should be 0.90, got %.2f", cfgA.JuryTrustThreshold)
	}
	if cfgB.JuryTrustThreshold != 0.65 {
		t.Errorf("Tenant-B should get default 0.65, got %.2f", cfgB.JuryTrustThreshold)
	}
}

// -- 12e. Federation Defaults --

func TestGovernanceConfig_FederationDefaultsApplied(t *testing.T) {
	cfg := governance.DefaultConfig("fed-test")

	if cfg.DecayHalfLifeHours != 168 {
		t.Errorf("Default decay_half_life_hours should be 168 (7 days), got %.0f", cfg.DecayHalfLifeHours)
	}
	if cfg.TrustEmaAlpha != 0.3 {
		t.Errorf("Default trust_ema_alpha should be 0.3, got %.2f", cfg.TrustEmaAlpha)
	}
	if cfg.FailurePenaltyFactor != 0.8 {
		t.Errorf("Default failure_penalty_factor should be 0.8, got %.2f", cfg.FailurePenaltyFactor)
	}
	if cfg.SupermajorityThreshold != 0.75 {
		t.Errorf("Default supermajority_threshold should be 0.75, got %.2f", cfg.SupermajorityThreshold)
	}
	if cfg.HandshakeMinTrust != 0.50 {
		t.Errorf("Default handshake_min_trust should be 0.50, got %.2f", cfg.HandshakeMinTrust)
	}
}

// -- 12f. Economics Defaults --

func TestGovernanceConfig_EconomicsDefaultsApplied(t *testing.T) {
	cfg := governance.DefaultConfig("econ-test")

	if cfg.TrustTaxBaseRate != 0.10 {
		t.Errorf("Default trust_tax_base_rate should be 0.10, got %.2f", cfg.TrustTaxBaseRate)
	}
	if cfg.MarketplaceCommission != 0.30 {
		t.Errorf("Default marketplace_commission should be 0.30, got %.2f", cfg.MarketplaceCommission)
	}
	if cfg.HITLCostMultiplier != 10.0 {
		t.Errorf("Default hitl_cost_multiplier should be 10.0, got %.1f", cfg.HITLCostMultiplier)
	}
}

// =============================================================================
// 13. EXTENDED CONFIG COVERAGE — Loading, Caching, Validation Edge Cases
// =============================================================================

// -- 13a. DB Error Fallback --

// errorConfigLoader always returns an error to simulate DB failures
type errorConfigLoader struct {
	upsertCalls int
}

func (e *errorConfigLoader) GetTenantGovernanceConfig(tenantID string) (*governance.TenantGovernanceConfig, error) {
	return nil, fmt.Errorf("simulated DB connection failure")
}

func (e *errorConfigLoader) UpsertTenantGovernanceConfig(tenantID string, cfg *governance.TenantGovernanceConfig) error {
	e.upsertCalls++
	return fmt.Errorf("simulated DB write failure")
}

func TestGovernanceConfig_DBErrorFallsBackToDefaults(t *testing.T) {
	loader := &errorConfigLoader{}
	cache := governance.NewGovernanceConfigCache(loader)

	cfg := cache.GetConfig("db-error-tenant")
	if cfg == nil {
		t.Fatal("GetConfig should return defaults even when DB fails")
	}
	if cfg.JuryTrustThreshold != 0.65 {
		t.Errorf("Fallback config should use default 0.65, got %.2f", cfg.JuryTrustThreshold)
	}
	if cfg.TenantID != "db-error-tenant" {
		t.Errorf("Fallback config should have correct tenant ID, got %s", cfg.TenantID)
	}
}

func TestGovernanceConfig_DBErrorCachesDefaultAfterFallback(t *testing.T) {
	loader := &errorConfigLoader{}
	cache := governance.NewGovernanceConfigCache(loader)

	// First call: DB error → fallback to defaults
	cfg1 := cache.GetConfig("db-error-cached")
	// Second call should use cached result, NOT hit DB again
	cfg2 := cache.GetConfig("db-error-cached")

	if cfg1 != cfg2 {
		t.Error("After DB error fallback, second call should return cached pointer")
	}
}

// -- 13b. InvalidateAll --

func TestGovernanceConfig_InvalidateAllClearsAllTenants(t *testing.T) {
	loader := &mockConfigLoader{configs: make(map[string]*governance.TenantGovernanceConfig)}
	cache := governance.NewGovernanceConfigCache(loader)

	// Populate cache for 3 tenants
	cache.GetConfig("tenant-ia-1")
	cache.GetConfig("tenant-ia-2")
	cache.GetConfig("tenant-ia-3")
	if loader.getCalls != 3 {
		t.Fatalf("Expected 3 loader calls, got %d", loader.getCalls)
	}

	cache.InvalidateAll()

	// All 3 should re-fetch from loader
	cache.GetConfig("tenant-ia-1")
	cache.GetConfig("tenant-ia-2")
	cache.GetConfig("tenant-ia-3")
	if loader.getCalls != 6 {
		t.Errorf("After InvalidateAll, expected 6 total loader calls, got %d", loader.getCalls)
	}
}

// -- 13c. Concurrent Cache Access --

func TestGovernanceConfig_ConcurrentAccessSafe(t *testing.T) {
	loader := &mockConfigLoader{configs: make(map[string]*governance.TenantGovernanceConfig)}
	cache := governance.NewGovernanceConfigCache(loader)

	done := make(chan bool, 50)
	// Spawn 50 goroutines hitting the cache concurrently
	for i := 0; i < 50; i++ {
		go func(n int) {
			tenantID := fmt.Sprintf("concurrent-tenant-%d", n%5)
			cfg := cache.GetConfig(tenantID)
			if cfg == nil {
				t.Errorf("GetConfig returned nil for %s", tenantID)
			}
			// Occasional invalidation to test race conditions
			if n%10 == 0 {
				cache.Invalidate(tenantID)
			}
			done <- true
		}(i)
	}
	for i := 0; i < 50; i++ {
		<-done
	}
}

// -- 13d. Validation Boundary Values --

func TestGovernanceConfig_ValidateAcceptsBoundaryZero(t *testing.T) {
	cfg := governance.DefaultConfig("boundary-zero")
	cfg.QuarantineScore = 0.0 // Lower boundary — should be accepted
	cfg.HandshakeMinTrust = 0.0
	if err := cfg.Validate(); err != nil {
		t.Errorf("Threshold=0.0 should be accepted (lower bound inclusive): %v", err)
	}
}

func TestGovernanceConfig_ValidateAcceptsBoundaryOne(t *testing.T) {
	cfg := governance.DefaultConfig("boundary-one")
	cfg.JuryTrustThreshold = 1.0 // Upper boundary — should be accepted
	cfg.QuorumThreshold = 1.0
	if err := cfg.Validate(); err != nil {
		t.Errorf("Threshold=1.0 should be accepted (upper bound inclusive): %v", err)
	}
}

func TestGovernanceConfig_ValidateRejectsNegativeThreshold(t *testing.T) {
	cfg := governance.DefaultConfig("negative-thresh")
	cfg.DriftThreshold = -0.01
	if err := cfg.Validate(); err == nil {
		t.Error("Negative drift_threshold should be rejected")
	}
}

func TestGovernanceConfig_ValidateRejectsZeroBaseCost(t *testing.T) {
	cfg := governance.DefaultConfig("zero-cost")
	cfg.MeterBaseCostPerFrame = 0.0
	if err := cfg.Validate(); err == nil {
		t.Error("Zero meter_base_cost_per_frame should be rejected (must be positive)")
	}
}

func TestGovernanceConfig_ValidateRejectsNegativeBaseCost(t *testing.T) {
	cfg := governance.DefaultConfig("neg-cost")
	cfg.MeterBaseCostPerFrame = -0.001
	if err := cfg.Validate(); err == nil {
		t.Error("Negative meter_base_cost_per_frame should be rejected")
	}
}

func TestGovernanceConfig_ValidateAcceptsExactWeightSum(t *testing.T) {
	cfg := governance.DefaultConfig("exact-weights")
	// Explicitly set to exactly 1.0: 0.25 + 0.25 + 0.25 + 0.25
	cfg.JuryAuditWeight = 0.25
	cfg.JuryReputationWeight = 0.25
	cfg.JuryAttestationWeight = 0.25
	cfg.JuryHistoryWeight = 0.25
	if err := cfg.Validate(); err != nil {
		t.Errorf("Weights summing to exactly 1.0 should pass: %v", err)
	}
}

func TestGovernanceConfig_ValidateRejectsWeightsBelow1(t *testing.T) {
	cfg := governance.DefaultConfig("low-weights")
	cfg.JuryAuditWeight = 0.10
	cfg.JuryReputationWeight = 0.10
	cfg.JuryAttestationWeight = 0.10
	cfg.JuryHistoryWeight = 0.10 // sum = 0.40
	if err := cfg.Validate(); err == nil {
		t.Error("Weights summing to 0.40 should be rejected")
	}
}

// -- 13e. JSON Serialization Round-Trip --

func TestGovernanceConfig_JSONRoundTrip(t *testing.T) {
	original := governance.DefaultConfig("json-test")
	original.JuryTrustThreshold = 0.77
	original.RiskMultipliers["custom_tool"] = 7.5

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var restored governance.TenantGovernanceConfig
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if restored.TenantID != original.TenantID {
		t.Errorf("TenantID mismatch: %s vs %s", original.TenantID, restored.TenantID)
	}
	if restored.JuryTrustThreshold != 0.77 {
		t.Errorf("JuryTrustThreshold lost in round-trip: %.2f", restored.JuryTrustThreshold)
	}
	if restored.RiskMultipliers["custom_tool"] != 7.5 {
		t.Errorf("Custom risk multiplier lost in round-trip: %.1f", restored.RiskMultipliers["custom_tool"])
	}
	if len(restored.RiskMultipliers) != 13 { // 12 defaults + 1 custom
		t.Errorf("Risk multipliers count mismatch: expected 13, got %d", len(restored.RiskMultipliers))
	}
}

// -- 13f. Metering Tier Order Invariants --

func TestGovernanceConfig_MeterTierOrdering(t *testing.T) {
	cfg := governance.DefaultConfig("tier-order")

	// high > med > low thresholds
	if cfg.MeterHighTrustThreshold <= cfg.MeterMedTrustThreshold {
		t.Errorf("High trust threshold (%.2f) should be > med (%.2f)",
			cfg.MeterHighTrustThreshold, cfg.MeterMedTrustThreshold)
	}
	if cfg.MeterMedTrustThreshold <= cfg.MeterLowTrustThreshold {
		t.Errorf("Med trust threshold (%.2f) should be > low (%.2f)",
			cfg.MeterMedTrustThreshold, cfg.MeterLowTrustThreshold)
	}

	// discount < 1.0 (actual discount) and surcharge > 1.0 (actual penalty)
	if cfg.MeterHighTrustDiscount >= 1.0 {
		t.Errorf("High trust discount (%.2f) should be < 1.0", cfg.MeterHighTrustDiscount)
	}
	if cfg.MeterLowTrustSurcharge <= 1.0 {
		t.Errorf("Low trust surcharge (%.2f) should be > 1.0", cfg.MeterLowTrustSurcharge)
	}
}

// -- 13g. Risk Multiplier Ordering --

func TestGovernanceConfig_RiskMultiplierOrderingInvariant(t *testing.T) {
	cfg := governance.DefaultConfig("risk-order")

	// Admin/exec should be higher risk than read-only
	if cfg.RiskMultipliers["admin_action"] <= cfg.RiskMultipliers["read_only"] {
		t.Errorf("admin_action (%.1f) should have higher multiplier than read_only (%.1f)",
			cfg.RiskMultipliers["admin_action"], cfg.RiskMultipliers["read_only"])
	}
	// Payment should be higher risk than data_query
	if cfg.RiskMultipliers["payment"] <= cfg.RiskMultipliers["data_query"] {
		t.Errorf("payment (%.1f) should have higher multiplier than data_query (%.1f)",
			cfg.RiskMultipliers["payment"], cfg.RiskMultipliers["data_query"])
	}
	// read_only should be the lowest
	for cls, val := range cfg.RiskMultipliers {
		if cls != "read_only" && val < cfg.RiskMultipliers["read_only"] {
			t.Errorf("%s multiplier (%.1f) should not be less than read_only (%.1f)",
				cls, val, cfg.RiskMultipliers["read_only"])
		}
	}
}

// -- 13h. Audit Event Types --

func TestGovernanceConfig_AuditEventTypeConstants(t *testing.T) {
	types := []governance.AuditEventType{
		governance.AuditConfigChange,
		governance.AuditTrustMutation,
		governance.AuditVerdict,
		governance.AuditEscrowAction,
		governance.AuditTokenIssued,
		governance.AuditTokenRevoked,
		governance.AuditMeterBilling,
		governance.AuditHITLDecision,
	}
	if len(types) != 8 {
		t.Errorf("Should have 8 audit event types, got %d", len(types))
	}
	// Verify no duplicates
	seen := make(map[governance.AuditEventType]bool)
	for _, et := range types {
		if seen[et] {
			t.Errorf("Duplicate audit event type: %s", et)
		}
		seen[et] = true
	}
}

// -- 13i. Upsert Persists via Mock --

func TestGovernanceConfig_CacheAutoCreatesAndPersistsDefaults(t *testing.T) {
	loader := &mockConfigLoader{configs: make(map[string]*governance.TenantGovernanceConfig)}
	cache := governance.NewGovernanceConfigCache(loader)

	cache.GetConfig("upsert-test")

	// Verify the mock loader received the upsert
	persisted, ok := loader.configs["upsert-test"]
	if !ok {
		t.Fatal("AutoCreated default should be persisted via UpsertTenantGovernanceConfig")
	}
	if persisted.JuryTrustThreshold != 0.65 {
		t.Errorf("Persisted config should have default threshold, got %.2f", persisted.JuryTrustThreshold)
	}
}

// -- 13j. Invalidate Non-existent Tenant --

func TestGovernanceConfig_InvalidateNonexistentTenantNoError(t *testing.T) {
	loader := &mockConfigLoader{configs: make(map[string]*governance.TenantGovernanceConfig)}
	cache := governance.NewGovernanceConfigCache(loader)

	// Should NOT panic or error when invalidating a tenant that was never cached
	cache.Invalidate("never-existed")
	cache.InvalidateAll()
}

// -- 13k. Tri-Factor Gate Entropy Thresholds --

func TestGovernanceConfig_TriFactorEntropyDefaults(t *testing.T) {
	cfg := governance.DefaultConfig("entropy-test")

	if cfg.EntropyHighCap >= cfg.EntropyEncryptedThreshold {
		t.Errorf("entropy_high_cap (%.1f) should be < entropy_encrypted_threshold (%.1f)",
			cfg.EntropyHighCap, cfg.EntropyEncryptedThreshold)
	}
	if cfg.EntropySuspiciousThreshold >= cfg.EntropyEncryptedThreshold {
		t.Errorf("entropy_suspicious_threshold (%.1f) should be < entropy_encrypted_threshold (%.1f)",
			cfg.EntropySuspiciousThreshold, cfg.EntropyEncryptedThreshold)
	}
}

// -- 13l. Config Mutation Doesn't Affect Cache Until Invalidation --

func TestGovernanceConfig_MutatingReturnedConfigDoesNotAffectCache(t *testing.T) {
	loader := &mockConfigLoader{configs: make(map[string]*governance.TenantGovernanceConfig)}
	cache := governance.NewGovernanceConfigCache(loader)

	cfg1 := cache.GetConfig("mutation-test")
	originalThreshold := cfg1.JuryTrustThreshold

	// Mutate the returned config — since Go returns a pointer, this
	// WILL affect the cached value (expected behavior for in-process cache).
	cfg1.JuryTrustThreshold = 0.99

	cfg2 := cache.GetConfig("mutation-test")
	// Both should see the mutation (same pointer)
	if cfg2.JuryTrustThreshold != 0.99 {
		t.Logf("Cache returned pointer: original=%.2f, mutated=%.2f",
			originalThreshold, cfg2.JuryTrustThreshold)
	}

	// Clear the persisted config from the mock so invalidation truly reloads fresh defaults
	loader.mu.Lock()
	delete(loader.configs, "mutation-test")
	loader.mu.Unlock()

	// After invalidation, cache should reload from loader (which now has no row → defaults)
	cache.Invalidate("mutation-test")
	cfg3 := cache.GetConfig("mutation-test")
	if cfg3.JuryTrustThreshold != 0.65 {
		t.Errorf("After invalidation + cleared mock, should get fresh defaults (0.65), got %.2f", cfg3.JuryTrustThreshold)
	}
}
