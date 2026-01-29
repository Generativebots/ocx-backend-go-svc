package service

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ocx/backend/internal/config"
)

// HandshakeState defines the step in the protocol
type HandshakeState string

const (
	StateHello       HandshakeState = "HELLO"
	StateChallenge   HandshakeState = "CHALLENGE"
	StateProof       HandshakeState = "PROOF"
	StateVerify      HandshakeState = "VERIFY"
	StateAttestation HandshakeState = "ATTESTATION"
	StateFinalize    HandshakeState = "FINALIZE"
	StateAccepted    HandshakeState = "ACCEPTED"
	StateRejected    HandshakeState = "REJECTED"
)

// Session represents a handshake in progress
type Session struct {
	ID           string
	InitiatorID  string
	TenantID     string
	Nonce        string
	State        HandshakeState
	Attestations map[string]string
	CreatedAt    time.Time
	ExpiresAt    time.Time
}

// HandshakeService manages the 6-step protocol
type HandshakeService struct {
	configManager *config.Manager
	sessions      sync.Map // Map[string]*Session
}

// NewHandshakeService creates a new service
func NewHandshakeService(cm *config.Manager) *HandshakeService {
	return &HandshakeService{configManager: cm}
}

// 1. HELLO
func (s *HandshakeService) Hello(initiatorID, version, tenantID string) (string, error) {
	// Production: Validate InitiatorID signature
	// if !verifySignature(initiatorID) { return "", fmt.Errorf("invalid signature") }

	// Resolve Config
	cfg := s.configManager.Get(tenantID)

	// Create Session
	sessionID := generateID()
	session := &Session{
		ID:          sessionID,
		InitiatorID: initiatorID,
		TenantID:    tenantID,
		State:       StateHello,
		CreatedAt:   time.Now(),
		ExpiresAt:   time.Now().Add(time.Duration(cfg.Handshake.SessionExpiryMinutes) * time.Minute),
	}
	s.sessions.Store(sessionID, session)

	// Transition to Challenge Ready (Client must now request Challenge)
	// In strict state machine, receiving Hello transitions session to Created.
	// The next expected call is Challenge.

	return sessionID, nil
}

// 2. CHALLENGE
func (s *HandshakeService) GenerateChallenge(sessionID string) (string, error) {
	session, err := s.getSession(sessionID)
	if err != nil {
		return "", err
	}

	if session.State != StateHello {
		return "", fmt.Errorf("invalid state: expected HELLO, got %s", session.State)
	}

	// Generate Nonce via crypto/rand
	nonceBytes := make([]byte, 32)
	rand.Read(nonceBytes)
	nonce := hex.EncodeToString(nonceBytes)

	session.Nonce = nonce
	session.State = StateChallenge

	return nonce, nil
}

// 3. PROOF
func (s *HandshakeService) VerifyProof(sessionID, signedNonce, pubKeyPEM string) (bool, error) {
	session, err := s.getSession(sessionID)
	if err != nil {
		return false, err
	}

	if session.State != StateChallenge {
		return false, fmt.Errorf("invalid state: expected CHALLENGE, got %s", session.State)
	}

	// Verify Signature
	// Note: In production, pubKey should probably be looked up from Registry via InitiatorID, not trusted from input.
	// For this task, we will verify the signature against the provided key.

	valid, err := verifySignature(session.Nonce, signedNonce, pubKeyPEM)
	if err != nil {
		session.State = StateRejected
		return false, fmt.Errorf("signature verification failed: %w", err)
	}
	if !valid {
		session.State = StateRejected
		return false, errors.New("invalid signature")
	}

	session.State = StateProof
	return true, nil
}

// 4. VERIFY (Registry Check)
func (s *HandshakeService) VerifyIdentity(sessionID string) (bool, error) {
	session, err := s.getSession(sessionID)
	if err != nil {
		return false, err
	}

	if session.State != StateProof {
		return false, fmt.Errorf("invalid state: expected PROOF, got %s", session.State)
	}

	// Mock Registry Check
	// In real impl, checking `session.InitiatorID` against `TrustRegistry`
	// Assuming always valid for now if they passed crypto proof

	session.State = StateVerify
	return true, nil
}

// 5. ATTESTATION
func (s *HandshakeService) ProcessAttestations(sessionID string, claims map[string]string) error {
	session, err := s.getSession(sessionID)
	if err != nil {
		return err
	}

	if session.State != StateVerify {
		return fmt.Errorf("invalid state: expected VERIFY, got %s", session.State)
	}

	session.Attestations = claims
	session.State = StateAttestation
	return nil
}

// 6. FINALIZE (DECISION)
func (s *HandshakeService) Finalize(sessionID string) (string, string, error) {
	session, err := s.getSession(sessionID)
	if err != nil {
		return "", "", err
	}

	if session.State != StateAttestation {
		return "REJECTED", "", fmt.Errorf("invalid state: expected ATTESTATION, got %s", session.State)
	}

	// Make Decision
	// check if 'role' claim exists (example policy)
	if _, ok := session.Attestations["role"]; !ok {
		session.State = StateRejected
		return "REJECTED", "", nil
	}

	session.State = StateAccepted
	token := generateID() // This would be a JWT in production

	return "ACCEPTED", token, nil
}

// Helpers

func (s *HandshakeService) getSession(id string) (*Session, error) {
	val, ok := s.sessions.Load(id)
	if !ok {
		return nil, errors.New("session not found")
	}
	session := val.(*Session)
	if time.Now().After(session.ExpiresAt) {
		s.sessions.Delete(id)
		return nil, errors.New("session expired")
	}
	return session, nil
}

func generateID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func verifySignature(data, sigHex, pubKeyPEM string) (bool, error) {
	// Decode PEM
	block, _ := pem.Decode([]byte(pubKeyPEM))
	if block == nil {
		return false, errors.New("failed to parse PEM block")
	}

	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return false, err
	}

	rsaPub, ok := pub.(*rsa.PublicKey)
	if !ok {
		return false, errors.New("not an RSA public key")
	}

	// Hash Data
	hashed := sha256.Sum256([]byte(data))

	// Decode Signature
	sigBytes, err := hex.DecodeString(sigHex)
	if err != nil {
		return false, err
	}

	// Verify
	err = rsa.VerifyPKCS1v15(rsaPub, crypto.SHA256, hashed[:], sigBytes)
	return err == nil, nil
}
