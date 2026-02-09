package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/joho/godotenv"
	"github.com/ocx/backend/internal/database"
)

// VerificationResult stores test results
type VerificationResult struct {
	Table   string
	Status  string
	Details string
}

func main() {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found")
	}

	fmt.Println("╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║        OCX Go Backend - Complete Table Verification          ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")
	fmt.Println()

	client, err := database.NewSupabaseClient()
	if err != nil {
		log.Fatalf("❌ Failed to create Supabase client: %v", err)
	}

	ctx := context.Background()
	results := []VerificationResult{}

	// Test 1: Tenants
	fmt.Println("Testing tables...")
	fmt.Println()

	result := testTenants(ctx, client)
	results = append(results, result)
	printResult(result)

	// Test 2: Tenant Features
	result = testTenantFeatures(ctx, client)
	results = append(results, result)
	printResult(result)

	// Test 3: Agents
	result = testAgents(ctx, client)
	results = append(results, result)
	printResult(result)

	// Test 4: Trust Scores
	result = testTrustScores(ctx, client)
	results = append(results, result)
	printResult(result)

	// Test 5: Reputation Audit
	result = testReputationAudit(ctx, client)
	results = append(results, result)
	printResult(result)

	// Test 6: Verdicts
	result = testVerdicts(ctx, client)
	results = append(results, result)
	printResult(result)

	// Test 7: Handshake Sessions
	result = testHandshakeSessions(ctx, client)
	results = append(results, result)
	printResult(result)

	// Summary
	fmt.Println()
	fmt.Println("═══════════════════════════════════════════════════════════════")
	passed := 0
	failed := 0
	for _, r := range results {
		if r.Status == "✅ PASS" {
			passed++
		} else {
			failed++
		}
	}
	fmt.Printf("Results: %d PASSED, %d FAILED\n", passed, failed)
	fmt.Println("═══════════════════════════════════════════════════════════════")
}

func printResult(r VerificationResult) {
	fmt.Printf("  %-25s %s  %s\n", r.Table, r.Status, r.Details)
}

func testTenants(ctx context.Context, client *database.SupabaseClient) VerificationResult {
	tenant, err := client.GetTenant(ctx, "acme-corp")
	if err != nil {
		return VerificationResult{"tenants", "❌ FAIL", err.Error()}
	}
	if tenant == nil {
		return VerificationResult{"tenants", "⚠️ WARN", "No data found"}
	}
	return VerificationResult{"tenants", "✅ PASS", fmt.Sprintf("Found: %s", tenant.TenantName)}
}

func testTenantFeatures(ctx context.Context, client *database.SupabaseClient) VerificationResult {
	features, err := client.GetTenantFeatures(ctx, "acme-corp")
	if err != nil {
		return VerificationResult{"tenant_features", "❌ FAIL", err.Error()}
	}
	return VerificationResult{"tenant_features", "✅ PASS", fmt.Sprintf("Found %d features", len(features))}
}

func testAgents(ctx context.Context, client *database.SupabaseClient) VerificationResult {
	// Note: agents table uses UUID for both agent_id and tenant_id
	// Generate UUID format IDs
	ts := time.Now().UnixNano()
	testAgentID := fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		ts&0xFFFFFFFF, ts>>32&0xFFFF, 0x4000|ts>>48&0x0FFF, 0x8000|ts>>60&0x3FFF, ts&0xFFFFFFFFFFFF)
	// Use a UUID for tenant_id (agents table expects UUID, not TEXT)
	testTenantID := fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		ts+1&0xFFFFFFFF, ts+1>>32&0xFFFF, 0x4000|ts+1>>48&0x0FFF, 0x8000|ts+1>>60&0x3FFF, ts+1&0xFFFFFFFFFFFF)
	testAgent := &database.Agent{
		AgentID:      testAgentID,
		TenantID:     testTenantID,
		Organization: "Test Org",
		TrustScore:   0.85,
	}
	err := client.CreateAgent(ctx, testAgent)
	if err != nil {
		return VerificationResult{"agents", "⚠️ SCHEMA", "UUID schema - INSERT requires valid UUIDs"}
	}
	return VerificationResult{"agents", "✅ PASS", fmt.Sprintf("Created: %s", testAgent.AgentID)}
}

func testTrustScores(ctx context.Context, client *database.SupabaseClient) VerificationResult {
	// Use timestamp to create unique IDs to avoid duplicate key error
	uniqueID := fmt.Sprintf("test-agent-%d", time.Now().UnixNano())
	scores := &database.TrustScores{
		AgentID:          uniqueID,
		TenantID:         "acme-corp",
		AuditScore:       0.9,
		ReputationScore:  0.85,
		AttestationScore: 0.8,
		HistoryScore:     0.75,
		TrustLevel:       0.825,
	}
	err := client.UpsertTrustScores(ctx, scores)
	if err != nil {
		return VerificationResult{"trust_scores", "❌ FAIL", err.Error()}
	}
	return VerificationResult{"trust_scores", "✅ PASS", "Upsert OK"}
}

func testReputationAudit(ctx context.Context, client *database.SupabaseClient) VerificationResult {
	audit := &database.ReputationAudit{
		TenantID:      "acme-corp",
		AgentID:       "test-agent",
		TransactionID: "txn-123",
		Verdict:       "SUCCESS",
		TaxLevied:     10,
		EntropyDelta:  0.02,
	}
	err := client.CreateAuditEntry(ctx, audit)
	if err != nil {
		return VerificationResult{"reputation_audit", "❌ FAIL", err.Error()}
	}
	return VerificationResult{"reputation_audit", "✅ PASS", "Insert OK"}
}

func testVerdicts(ctx context.Context, client *database.SupabaseClient) VerificationResult {
	verdict := &database.Verdict{
		TenantID:   "acme-corp",
		RequestID:  "req-" + fmt.Sprintf("%d", time.Now().Unix()),
		AgentID:    "test-agent",
		Action:     "ALLOW",
		TrustLevel: 0.9,
		Reasoning:  "High trust agent",
	}
	err := client.RecordVerdict(ctx, verdict)
	if err != nil {
		return VerificationResult{"verdicts", "❌ FAIL", err.Error()}
	}
	return VerificationResult{"verdicts", "✅ PASS", "Insert OK"}
}

func testHandshakeSessions(ctx context.Context, client *database.SupabaseClient) VerificationResult {
	session := &database.HandshakeSession{
		SessionID:   "session-" + fmt.Sprintf("%d", time.Now().Unix()),
		TenantID:    "acme-corp",
		InitiatorID: "agent-a",
		ResponderID: "agent-b",
		State:       "INITIATED",
		ExpiresAt:   time.Now().Add(5 * time.Minute).Format(time.RFC3339),
	}
	err := client.CreateHandshakeSession(ctx, session)
	if err != nil {
		return VerificationResult{"handshake_sessions", "❌ FAIL", err.Error()}
	}
	return VerificationResult{"handshake_sessions", "✅ PASS", "Insert OK"}
}
