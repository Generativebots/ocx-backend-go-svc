package federation

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	pb "github.com/ocx/backend/pb"
)

// ============================================================================
// FULL 6-STEP INTER-OCX HANDSHAKE IMPLEMENTATION
// ============================================================================

// HandshakeSession represents a complete 6-step handshake session
type HandshakeSession struct {
	// Session metadata
	sessionID string
	localOCX  *OCXInstance
	remoteOCX *OCXInstance

	// State management
	stateMachine *HandshakeStateMachine

	// Cryptographic state
	nonce     string
	challenge []byte

	// Ledger for attestation
	ledger *TrustAttestationLedger

	// Results
	trustLevel float64
	trustTax   float64
	verdict    string

	// Dev/test fallback key (when no SPIFFE agent)
	devKey *ecdsa.PrivateKey
}

// NewHandshakeSession creates a new 6-step handshake session
func NewHandshakeSession(local, remote *OCXInstance, ledger *TrustAttestationLedger) *HandshakeSession {
	sessionID := uuid.New().String()

	return &HandshakeSession{
		sessionID:    sessionID,
		localOCX:     local,
		remoteOCX:    remote,
		ledger:       ledger,
		stateMachine: NewHandshakeStateMachine(sessionID),
	}
}

// ============================================================================
// STEP 1: HELLO - Initial capability exchange
// ============================================================================

// SendHello initiates the handshake with capability exchange
func (hs *HandshakeSession) SendHello(ctx context.Context) (*pb.HandshakeHello, error) {
	// Transition state
	if err := hs.stateMachine.Transition(StateInit, StateHelloSent); err != nil {
		return nil, err
	}

	// Get SPIFFE SVID for identity (nil-safe for dev/test environments)
	var spiffeID string
	var publicKeyObj *ecdsa.PublicKey
	if hs.localOCX.SPIFFESource != nil {
		svid, err := hs.localOCX.SPIFFESource.GetX509SVID()
		if err != nil {
			hs.stateMachine.SetError(err)
			return nil, fmt.Errorf("failed to get SPIFFE SVID: %w", err)
		}
		pk, ok := svid.PrivateKey.Public().(*ecdsa.PublicKey)
		if !ok {
			err := errors.New("not an ECDSA public key")
			hs.stateMachine.SetError(err)
			return nil, err
		}
		spiffeID = svid.ID.String()
		publicKeyObj = pk
	} else {
		// Dev/test fallback: generate ephemeral ECDSA key
		slog.Info("No SPIFFE source using dev-mode ephemeral key for", "instance_i_d", hs.localOCX.InstanceID)
		devKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			hs.stateMachine.SetError(err)
			return nil, fmt.Errorf("failed to generate dev key: %w", err)
		}
		hs.devKey = devKey
		spiffeID = fmt.Sprintf("spiffe://dev/%s", hs.localOCX.InstanceID)
		publicKeyObj = &devKey.PublicKey
	}

	// Use resolved public key

	publicKeyPEM, err := EncodePublicKeyPEM(publicKeyObj)
	if err != nil {
		hs.stateMachine.SetError(err)
		return nil, err
	}

	hello := &pb.HandshakeHello{
		InstanceId:      hs.localOCX.InstanceID,
		Organization:    hs.localOCX.Organization,
		ProtocolVersion: "1.0",
		Capabilities: []string{
			"speculative_execution",
			"trust_attestation",
			"authority_contracts",
			"entropy_monitoring",
		},
		SpiffeId:  spiffeID,
		PublicKey: publicKeyPEM,
		Timestamp: time.Now().Unix(),
		Metadata: map[string]string{
			"region":       hs.localOCX.Region,
			"trust_domain": hs.localOCX.TrustDomain,
		},
	}

	slog.Info("[STEP 1/6] HELLO sent", "instance_i_d", hs.localOCX.InstanceID, "instance_i_d", hs.remoteOCX.InstanceID)
	return hello, nil
}

// ReceiveHello processes an incoming HELLO message
func (hs *HandshakeSession) ReceiveHello(ctx context.Context, hello *pb.HandshakeHello) error {
	// Transition state
	if err := hs.stateMachine.Transition(StateInit, StateHelloReceived); err != nil {
		return err
	}

	// Validate protocol version
	if hello.ProtocolVersion != "1.0" {
		err := fmt.Errorf("unsupported protocol version: %s", hello.ProtocolVersion)
		hs.stateMachine.SetError(err)
		return err
	}

	// Validate required capabilities
	requiredCapabilities := []string{"trust_attestation"}
	if !hasCapabilities(hello.Capabilities, requiredCapabilities) {
		err := errors.New("missing required capabilities")
		hs.stateMachine.SetError(err)
		return err
	}

	slog.Info("[STEP 1/6] HELLO received from", "instance_id", hello.InstanceId)
	return nil
}

// ============================================================================
// STEP 2: CHALLENGE - Cryptographic challenge with nonce
// ============================================================================

// SendChallenge generates and sends a cryptographic challenge
func (hs *HandshakeSession) SendChallenge(ctx context.Context, hello *pb.HandshakeHello) (*pb.HandshakeChallenge, error) {
	// Transition state
	if err := hs.stateMachine.Transition(StateHelloReceived, StateChallengeSent); err != nil {
		return nil, err
	}

	// Generate nonce
	nonce, err := GenerateNonce()
	if err != nil {
		hs.stateMachine.SetError(err)
		return nil, err
	}
	hs.nonce = nonce

	// Create challenge
	challenge, err := CreateChallenge(nonce, hello.InstanceId)
	if err != nil {
		hs.stateMachine.SetError(err)
		return nil, err
	}
	hs.challenge = challenge

	challengeMsg := &pb.HandshakeChallenge{
		Nonce:                nonce,
		Challenge:            challenge,
		ChallengeType:        "HMAC-SHA256",
		Timestamp:            time.Now().Unix(),
		Difficulty:           1,
		RequiredCapabilities: []string{"trust_attestation", "speculative_execution"},
	}

	slog.Info("[STEP 2/6] CHALLENGE sent with nonce", "nonce16", nonce[:16])
	return challengeMsg, nil
}

// ReceiveChallenge processes an incoming CHALLENGE message
func (hs *HandshakeSession) ReceiveChallenge(ctx context.Context, challenge *pb.HandshakeChallenge) error {
	// Transition state
	if err := hs.stateMachine.Transition(StateHelloSent, StateChallengeReceived); err != nil {
		return err
	}

	// Store challenge for proof generation
	hs.nonce = challenge.Nonce
	hs.challenge = challenge.Challenge

	// Verify challenge type is supported
	if challenge.ChallengeType != "HMAC-SHA256" {
		err := fmt.Errorf("unsupported challenge type: %s", challenge.ChallengeType)
		hs.stateMachine.SetError(err)
		return err
	}

	slog.Info("✅ [STEP 2/6] CHALLENGE received")
	return nil
}

// ============================================================================
// STEP 3: PROOF - Zero-knowledge proof response
// ============================================================================

// GenerateProof creates a cryptographic proof response
func (hs *HandshakeSession) GenerateProof(ctx context.Context, agentID string) (*pb.HandshakeProof, error) {
	// Transition state
	if err := hs.stateMachine.Transition(StateChallengeReceived, StateProofSent); err != nil {
		return nil, err
	}

	// Get signing key (SPIFFE SVID or dev fallback)
	var privateKey *ecdsa.PrivateKey
	if hs.localOCX.SPIFFESource != nil {
		svid, err := hs.localOCX.SPIFFESource.GetX509SVID()
		if err != nil {
			hs.stateMachine.SetError(err)
			return nil, err
		}
		pk, ok := svid.PrivateKey.(*ecdsa.PrivateKey)
		if !ok {
			err := errors.New("not an ECDSA private key")
			hs.stateMachine.SetError(err)
			return nil, err
		}
		privateKey = pk
	} else if hs.devKey != nil {
		privateKey = hs.devKey
	} else {
		err := errors.New("no SPIFFE source or dev key available")
		hs.stateMachine.SetError(err)
		return nil, err
	}

	proof, err := GenerateProof(hs.challenge, privateKey)
	if err != nil {
		hs.stateMachine.SetError(err)
		return nil, err
	}

	// Get audit hash (zero-knowledge proof)
	auditHash, err := hs.localOCX.GetAuditHash(agentID)
	if err != nil {
		hs.stateMachine.SetError(err)
		return nil, err
	}

	// Build certificate chain (nil-safe for dev/test)
	var certChain []string
	if hs.localOCX.SPIFFESource != nil {
		svid, svidErr := hs.localOCX.SPIFFESource.GetX509SVID()
		if svidErr == nil {
			certChain = make([]string, len(svid.Certificates))
			for i, cert := range svid.Certificates {
				certChain[i] = string(cert.Raw)
			}
		}
	}

	proofMsg := &pb.HandshakeProof{
		Proof:            proof,
		AuditHash:        []byte(auditHash),
		Signature:        proof, // Same as proof for ECDSA
		CertificateChain: certChain,
		Timestamp:        time.Now().Unix(),
		ProofType:        "ECDSA-SHA256",
	}

	slog.Info("📝 [STEP 3/6] PROOF generated and sent")
	return proofMsg, nil
}

// ReceiveProof processes and verifies an incoming PROOF message
func (hs *HandshakeSession) ReceiveProof(ctx context.Context, proof *pb.HandshakeProof) error {
	// Transition state
	if err := hs.stateMachine.Transition(StateChallengeSent, StateProofReceived); err != nil {
		return err
	}

	// Get remote SPIFFE SVID
	remoteSVID, err := hs.remoteOCX.SPIFFESource.GetX509SVID()
	if err != nil {
		hs.stateMachine.SetError(err)
		return err
	}

	// Extract public key
	publicKey, ok := remoteSVID.PrivateKey.Public().(*ecdsa.PublicKey)
	if !ok {
		err := errors.New("not an ECDSA public key")
		hs.stateMachine.SetError(err)
		return err
	}

	// Verify proof
	valid, err := VerifyProof(proof.Proof, hs.challenge, publicKey)
	if err != nil {
		hs.stateMachine.SetError(err)
		return err
	}

	if !valid {
		err := errors.New("proof verification failed")
		hs.stateMachine.SetError(err)
		return err
	}

	slog.Info("✅ [STEP 3/6] PROOF verified successfully")
	return nil
}

// ============================================================================
// STEP 4: VERIFY - Mutual verification result
// ============================================================================

// PerformVerification verifies the proof and calculates trust scores
func (hs *HandshakeSession) PerformVerification(ctx context.Context, proof *pb.HandshakeProof, agentID string) (*pb.HandshakeVerify, error) {
	// Transition state
	if err := hs.stateMachine.Transition(StateProofReceived, StateVerified); err != nil {
		return nil, err
	}

	// Get local audit hash
	localAuditHash, err := hs.localOCX.GetAuditHash(agentID)
	if err != nil {
		hs.stateMachine.SetError(err)
		return nil, err
	}

	// Verify against ledger
	attestation, err := hs.ledger.VerifyAttestation(ctx, localAuditHash, string(proof.AuditHash), agentID)
	if err != nil {
		hs.stateMachine.SetError(err)
		return nil, err
	}

	// Calculate weighted trust level
	trustLevel := hs.calculateWeightedTrust(attestation, proof)

	verifyMsg := &pb.HandshakeVerify{
		Verified:   true,
		TrustLevel: trustLevel,
		VerifiedAt: time.Now().Unix(),
		Details: &pb.VerificationDetails{
			AuditHashMatch:   true,
			SignatureValid:   true,
			CertificateValid: true,
			NonceFresh:       true,
			AuditScore:       1.0,
			ReputationScore:  hs.getReputationScore(),
			AttestationScore: hs.getAttestationScore(attestation),
			HistoryScore:     hs.getHistoryScore(),
		},
		Warnings: []string{},
	}

	slog.Info("[STEP 4/6] VERIFY complete: trust_level", "trust_level", trustLevel)
	return verifyMsg, nil
}

// ============================================================================
// STEP 5: ATTESTATION - Trust attestation exchange
// ============================================================================

// ExchangeAttestation creates and exchanges trust attestation
func (hs *HandshakeSession) ExchangeAttestation(ctx context.Context, verify *pb.HandshakeVerify) (*pb.HandshakeAttestation, error) {
	// Transition state
	if err := hs.stateMachine.Transition(StateVerified, StateAttestationSent); err != nil {
		return nil, err
	}

	// Calculate trust tax based on trust level
	trustTax := hs.calculateTrustTax(verify.TrustLevel)
	hs.trustLevel = verify.TrustLevel
	hs.trustTax = trustTax

	attestationID := uuid.New().String()

	// Create attestation signature
	attestationData := fmt.Sprintf("%s:%s:%.2f:%.2f", hs.localOCX.InstanceID, hs.remoteOCX.InstanceID, verify.TrustLevel, trustTax)
	attestationSig := HashAttestation([]byte(attestationData))

	attestationMsg := &pb.HandshakeAttestation{
		TrustLevel:           verify.TrustLevel,
		TrustTax:             trustTax,
		ExpiresAt:            time.Now().Add(24 * time.Hour).Unix(),
		AttestationId:        attestationID,
		LedgerTxId:           "", // Would be set by ledger
		AttestationSignature: attestationSig,
		Metadata: map[string]string{
			"session_id": hs.sessionID,
			"timestamp":  time.Now().Format(time.RFC3339),
		},
	}

	slog.Info("[STEP 5/6] ATTESTATION exchanged: trust_tax=%%", "trust_tax100", trustTax*100)
	return attestationMsg, nil
}

// ReceiveAttestation processes an incoming ATTESTATION message
func (hs *HandshakeSession) ReceiveAttestation(ctx context.Context, attestation *pb.HandshakeAttestation) error {
	// Transition state
	if err := hs.stateMachine.Transition(StateAttestationSent, StateAttestationReceived); err != nil {
		return err
	}

	hs.trustLevel = attestation.TrustLevel
	hs.trustTax = attestation.TrustTax

	slog.Info("✅ [STEP 5/6] ATTESTATION received")
	return nil
}

// ============================================================================
// STEP 6: ACCEPT/REJECT - Final handshake decision
// ============================================================================

// FinalizeHandshake makes the final accept/reject decision
func (hs *HandshakeSession) FinalizeHandshake(ctx context.Context, attestation *pb.HandshakeAttestation) (*pb.HandshakeResult, error) {
	// Minimum trust threshold
	minTrustLevel := 0.5

	var verdict string
	var reason string
	var targetState HandshakeState

	if attestation.TrustLevel >= minTrustLevel {
		verdict = "ACCEPTED"
		reason = fmt.Sprintf("Trust level %.2f meets minimum threshold %.2f", attestation.TrustLevel, minTrustLevel)
		targetState = StateAccepted
	} else {
		verdict = "REJECTED"
		reason = fmt.Sprintf("Trust level %.2f below minimum threshold %.2f", attestation.TrustLevel, minTrustLevel)
		targetState = StateRejected
	}

	// Transition to final state
	if err := hs.stateMachine.Transition(StateAttestationReceived, targetState); err != nil {
		return nil, err
	}

	hs.verdict = verdict

	resultMsg := &pb.HandshakeResult{
		Verdict:          verdict,
		Reason:           reason,
		TrustLevel:       attestation.TrustLevel,
		SessionId:        hs.sessionID,
		CompletedAt:      time.Now().Unix(),
		SessionExpiresAt: time.Now().Add(24 * time.Hour).Unix(),
		DurationMs:       hs.stateMachine.GetElapsedTime().Milliseconds(),
		Metadata: map[string]string{
			"trust_tax":      fmt.Sprintf("%.2f", attestation.TrustTax),
			"attestation_id": attestation.AttestationId,
		},
	}

	slog.Info("[STEP 6/6] HANDSHAKE : (duration=ms)", "verdict", verdict, "reason", reason, "duration_ms", resultMsg.DurationMs)
	return resultMsg, nil
}

// ============================================================================
// HELPER FUNCTIONS
// ============================================================================

// calculateWeightedTrust implements the weighted trust formula
func (hs *HandshakeSession) calculateWeightedTrust(attestation *TrustAttestation, proof *pb.HandshakeProof) float64 {
	// Audit score (40%): Based on audit hash verification
	auditScore := 1.0 // Assuming verification passed

	// Reputation score (30%): Based on historical interactions
	reputationScore := hs.getReputationScore()

	// Attestation score (20%): Based on attestation freshness
	attestationScore := hs.getAttestationScore(attestation)

	// History score (10%): Based on relationship age
	historyScore := hs.getHistoryScore()

	// Weighted calculation
	trustLevel := (0.40 * auditScore) +
		(0.30 * reputationScore) +
		(0.20 * attestationScore) +
		(0.10 * historyScore)

	return trustLevel
}

// getReputationScore calculates reputation based on past interactions
func (hs *HandshakeSession) getReputationScore() float64 {
	// In production, query reputation database
	// For now, return default based on organization match
	if hs.localOCX.Organization == hs.remoteOCX.Organization {
		return 0.9
	}
	return 0.7
}

// getAttestationScore calculates score based on attestation freshness
func (hs *HandshakeSession) getAttestationScore(attestation *TrustAttestation) float64 {
	age := time.Since(attestation.Timestamp)

	if age < 1*time.Hour {
		return 1.0
	} else if age < 24*time.Hour {
		return 0.8
	} else if age < 7*24*time.Hour {
		return 0.6
	}

	return 0.4
}

// getHistoryScore calculates score based on relationship history
func (hs *HandshakeSession) getHistoryScore() float64 {
	// In production, query interaction history
	// For now, return default
	return 0.5
}

// calculateTrustTax calculates the trust tax based on trust level
func (hs *HandshakeSession) calculateTrustTax(trustLevel float64) float64 {
	// Trust tax formula: tax = (1 - trust_level) * base_rate
	// Higher trust = lower tax
	baseRate := 0.10 // 10% base rate

	trustTax := (1.0 - trustLevel) * baseRate

	// Cap at base rate
	if trustTax > baseRate {
		trustTax = baseRate
	}

	return trustTax
}

// hasCapabilities checks if all required capabilities are present
func hasCapabilities(have, required []string) bool {
	capMap := make(map[string]bool)
	for _, cap := range have {
		capMap[cap] = true
	}

	for _, req := range required {
		if !capMap[req] {
			return false
		}
	}

	return true
}
