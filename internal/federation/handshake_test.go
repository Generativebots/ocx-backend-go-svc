package federation

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// INTEGRATION TESTS FOR 6-STEP HANDSHAKE
// ============================================================================

func TestFullHandshakeFlow(t *testing.T) {
	// Create two agents
	agent1 := &OCXInstance{
		InstanceID:   "agent-1",
		Organization: "Acme Corp",
		TrustDomain:  "acme.example.com",
		Region:       "us-east-1",
	}

	agent2 := &OCXInstance{
		InstanceID:   "agent-2",
		Organization: "Acme Corp",
		TrustDomain:  "acme.example.com",
		Region:       "us-west-1",
	}

	// Create mock ledger
	ledger := NewMockTrustAttestationLedger()

	// Create handshake session
	session := NewHandshakeSession(agent1, agent2, ledger)

	ctx := context.Background()

	// Step 1: HELLO
	hello, err := session.SendHello(ctx)
	require.NoError(t, err)
	assert.Equal(t, "agent-1", hello.InstanceID)
	assert.Equal(t, "Acme Corp", hello.Organization)
	assert.Contains(t, hello.Capabilities, "trust_attestation")

	err = session.ReceiveHello(ctx, hello)
	require.NoError(t, err)

	// Step 2: CHALLENGE
	challenge, err := session.SendChallenge(ctx, hello)
	require.NoError(t, err)
	assert.NotEmpty(t, challenge.Nonce)
	assert.Equal(t, "HMAC-SHA256", challenge.ChallengeType)

	err = session.ReceiveChallenge(ctx, challenge)
	require.NoError(t, err)

	// Step 3: PROOF
	proof, err := session.GenerateProof(ctx, "test-agent")
	require.NoError(t, err)
	assert.NotEmpty(t, proof.Proof)
	assert.NotEmpty(t, proof.AuditHash)

	// Note: ReceiveProof would need actual SPIFFE setup
	// Skipping for unit test

	// Step 4: VERIFY
	verify := &HandshakeVerifyMessage{
		Verified:   true,
		TrustLevel: 0.85,
		VerifiedAt: time.Now().Unix(),
		Details: &VerificationDetails{
			AuditHashMatch:   true,
			SignatureValid:   true,
			CertificateValid: true,
			NonceFresh:       true,
			AuditScore:       1.0,
			ReputationScore:  0.9,
			AttestationScore: 0.8,
			HistoryScore:     0.5,
		},
	}

	// Step 5: ATTESTATION
	attestation, err := session.ExchangeAttestation(ctx, verify)
	require.NoError(t, err)
	assert.Equal(t, 0.85, attestation.TrustLevel)
	assert.True(t, attestation.TrustTax < 0.10) // Should be low for high trust

	err = session.ReceiveAttestation(ctx, attestation)
	require.NoError(t, err)

	// Step 6: RESULT
	result, err := session.FinalizeHandshake(ctx, attestation)
	require.NoError(t, err)
	assert.Equal(t, "ACCEPTED", result.Verdict)
	assert.NotEmpty(t, result.SessionID)
	assert.True(t, result.DurationMs > 0)
}

func TestHandshakeRejection(t *testing.T) {
	agent1 := &OCXInstance{
		InstanceID:   "agent-1",
		Organization: "Acme Corp",
	}

	agent2 := &OCXInstance{
		InstanceID:   "agent-2",
		Organization: "Evil Corp", // Different org
	}

	ledger := NewMockTrustAttestationLedger()
	session := NewHandshakeSession(agent1, agent2, ledger)

	ctx := context.Background()

	// Low trust level
	verify := &HandshakeVerifyMessage{
		Verified:   true,
		TrustLevel: 0.3, // Below threshold
		VerifiedAt: time.Now().Unix(),
	}

	attestation, err := session.ExchangeAttestation(ctx, verify)
	require.NoError(t, err)

	result, err := session.FinalizeHandshake(ctx, attestation)
	require.NoError(t, err)
	assert.Equal(t, "REJECTED", result.Verdict)
	assert.Contains(t, result.Reason, "below minimum threshold")
}

func TestWeightedTrustCalculation(t *testing.T) {
	agent1 := &OCXInstance{
		InstanceID:   "agent-1",
		Organization: "Acme Corp",
	}

	agent2 := &OCXInstance{
		InstanceID:   "agent-2",
		Organization: "Acme Corp",
	}

	ledger := NewMockTrustAttestationLedger()
	session := NewHandshakeSession(agent1, agent2, ledger)

	attestation := &TrustAttestation{
		LocalOCX:  "agent-1",
		RemoteOCX: "agent-2",
		Timestamp: time.Now(),
	}

	proof := &HandshakeProofMessage{
		AuditHash: []byte("test-hash"),
	}

	trustLevel := session.calculateWeightedTrust(attestation, proof)

	// Trust level should be weighted average
	// 40% audit (1.0) + 30% reputation (0.9) + 20% attestation (1.0) + 10% history (0.5)
	// = 0.4 + 0.27 + 0.2 + 0.05 = 0.92
	assert.InDelta(t, 0.92, trustLevel, 0.05)
}

func TestTrustTaxCalculation(t *testing.T) {
	agent1 := &OCXInstance{InstanceID: "agent-1"}
	agent2 := &OCXInstance{InstanceID: "agent-2"}

	ledger := NewMockTrustAttestationLedger()
	session := NewHandshakeSession(agent1, agent2, ledger)

	tests := []struct {
		trustLevel float64
		maxTax     float64
	}{
		{1.0, 0.00}, // Perfect trust = 0% tax
		{0.9, 0.01}, // High trust = 1% tax
		{0.5, 0.05}, // Medium trust = 5% tax
		{0.0, 0.10}, // No trust = 10% tax
	}

	for _, tt := range tests {
		tax := session.calculateTrustTax(tt.trustLevel)
		assert.LessOrEqual(t, tax, tt.maxTax, "Trust level %.2f should have tax <= %.2f", tt.trustLevel, tt.maxTax)
	}
}

func TestStateMachineTransitions(t *testing.T) {
	sm := NewHandshakeStateMachine("test-session")

	// Valid transitions
	err := sm.Transition(StateInit, StateHelloSent)
	assert.NoError(t, err)

	err = sm.Transition(StateHelloSent, StateChallengeReceived)
	assert.NoError(t, err)

	// Invalid transition (skipping states)
	err = sm.Transition(StateChallengeReceived, StateAccepted)
	assert.Error(t, err)

	// Check state
	assert.Equal(t, StateChallengeReceived, sm.GetCurrentState())
}

func TestStateMachineTimeout(t *testing.T) {
	sm := NewHandshakeStateMachine("test-session")
	sm.timeoutAt = time.Now().Add(-1 * time.Second) // Already timed out

	err := sm.Transition(StateInit, StateHelloSent)
	assert.Error(t, err)
	assert.Equal(t, StateTimeout, sm.GetCurrentState())
}

func TestCryptographicPrimitives(t *testing.T) {
	// Test nonce generation
	nonce1, err := GenerateNonce()
	require.NoError(t, err)
	assert.Len(t, nonce1, 64) // 32 bytes hex-encoded = 64 chars

	nonce2, err := GenerateNonce()
	require.NoError(t, err)
	assert.NotEqual(t, nonce1, nonce2, "Nonces should be unique")

	// Test challenge creation
	challenge, err := CreateChallenge(nonce1, "test-instance")
	require.NoError(t, err)
	assert.NotEmpty(t, challenge)

	// Test hash attestation
	data := []byte("test attestation data")
	hash := HashAttestation(data)
	assert.Len(t, hash, 32) // SHA-256 = 32 bytes

	// Verify hash
	valid := VerifyAttestationHash(data, hash)
	assert.True(t, valid)

	// Invalid hash
	invalidHash := make([]byte, 32)
	valid = VerifyAttestationHash(data, invalidHash)
	assert.False(t, valid)
}

func TestCapabilityNegotiation(t *testing.T) {
	have := []string{"trust_attestation", "speculative_execution", "entropy_monitoring"}
	required := []string{"trust_attestation"}

	assert.True(t, hasCapabilities(have, required))

	required = []string{"trust_attestation", "missing_capability"}
	assert.False(t, hasCapabilities(have, required))
}

// ============================================================================
// MOCK LEDGER FOR TESTING
// ============================================================================

type MockTrustAttestationLedger struct{}

func NewMockTrustAttestationLedger() *TrustAttestationLedger {
	return &TrustAttestationLedger{}
}

func (m *MockTrustAttestationLedger) VerifyAttestation(ctx context.Context, localHash, remoteHash, agentID string) (*TrustAttestation, error) {
	return &TrustAttestation{
		AttestationID: "mock-attestation",
		LocalOCX:      "agent-1",
		RemoteOCX:     "agent-2",
		AgentID:       agentID,
		AuditHash:     localHash,
		TrustLevel:    0.85,
		Timestamp:     time.Now(),
		ExpiresAt:     time.Now().Add(24 * time.Hour),
	}, nil
}

// ============================================================================
// BENCHMARK TESTS
// ============================================================================

func BenchmarkFullHandshake(b *testing.B) {
	agent1 := &OCXInstance{
		InstanceID:   "agent-1",
		Organization: "Acme Corp",
	}

	agent2 := &OCXInstance{
		InstanceID:   "agent-2",
		Organization: "Acme Corp",
	}

	ledger := NewMockTrustAttestationLedger()
	ctx := context.Background()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		session := NewHandshakeSession(agent1, agent2, ledger)

		hello, _ := session.SendHello(ctx)
		session.ReceiveHello(ctx, hello)

		challenge, _ := session.SendChallenge(ctx, hello)
		session.ReceiveChallenge(ctx, challenge)

		// Skip proof generation (requires SPIFFE)

		verify := &HandshakeVerifyMessage{
			Verified:   true,
			TrustLevel: 0.85,
			VerifiedAt: time.Now().Unix(),
		}

		attestation, _ := session.ExchangeAttestation(ctx, verify)
		session.ReceiveAttestation(ctx, attestation)

		session.FinalizeHandshake(ctx, attestation)
	}
}

func BenchmarkNonceGeneration(b *testing.B) {
	for i := 0; i < b.N; i++ {
		GenerateNonce()
	}
}

func BenchmarkChallengeCreation(b *testing.B) {
	nonce, _ := GenerateNonce()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		CreateChallenge(nonce, "test-instance")
	}
}
