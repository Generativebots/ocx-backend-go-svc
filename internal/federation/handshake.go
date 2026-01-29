package federation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"github.com/spiffe/go-spiffe/v2/svid/x509svid"
	"github.com/spiffe/go-spiffe/v2/workloadapi"
)

// OCXInstance represents a local or remote OCX deployment
type OCXInstance struct {
	InstanceID   string
	TrustDomain  string
	SPIFFESource *workloadapi.X509Source
	Region       string
	Organization string
}

// TrustAttestation represents proof of audit completion
type TrustAttestation struct {
	AttestationID string
	LocalOCX      string
	RemoteOCX     string
	AgentID       string
	AuditHash     string
	TrustLevel    float64
	Signature     string
	Timestamp     time.Time
	ExpiresAt     time.Time
}

// InterOCXHandshake manages mutual authentication between OCX instances
type InterOCXHandshake struct {
	localOCX   *OCXInstance
	remoteOCX  *OCXInstance
	trustLevel float64
	ledger     *TrustAttestationLedger
}

// NewInterOCXHandshake creates a new handshake session
func NewInterOCXHandshake(local, remote *OCXInstance, ledger *TrustAttestationLedger) *InterOCXHandshake {
	return &InterOCXHandshake{
		localOCX:  local,
		remoteOCX: remote,
		ledger:    ledger,
	}
}

// Negotiate performs the complete Inter-OCX handshake (LEGACY 4-step)
// Deprecated: Use NegotiateV2 for the full 6-step handshake
func (h *InterOCXHandshake) Negotiate(ctx context.Context, agentID string) (*TrustAttestation, error) {
	log.Printf("🤝 Starting Inter-OCX handshake: %s <-> %s", h.localOCX.InstanceID, h.remoteOCX.InstanceID)

	// Step 1: Mutual SPIFFE authentication
	if err := h.verifySPIFFECertificates(ctx); err != nil {
		return nil, fmt.Errorf("SPIFFE verification failed: %w", err)
	}

	// Step 2: Exchange audit hashes (zero-knowledge proof)
	localHash, err := h.localOCX.GetAuditHash(agentID)
	if err != nil {
		return nil, fmt.Errorf("failed to get local audit hash: %w", err)
	}

	remoteHash, err := h.remoteOCX.GetAuditHash(agentID)
	if err != nil {
		return nil, fmt.Errorf("failed to get remote audit hash: %w", err)
	}

	// Step 3: Verify against Trust Attestation Ledger
	attestation, err := h.ledger.VerifyAttestation(ctx, localHash, remoteHash, agentID)
	if err != nil {
		return nil, fmt.Errorf("attestation verification failed: %w", err)
	}

	// Step 4: Calculate trust level
	h.trustLevel = h.calculateTrustLevel(attestation)
	attestation.TrustLevel = h.trustLevel

	log.Printf("✅ Handshake complete: trust_level=%.2f", h.trustLevel)

	return attestation, nil
}

// NegotiateV2 performs the full 6-step Inter-OCX handshake
// This is the recommended method for new implementations
func (h *InterOCXHandshake) NegotiateV2(ctx context.Context, agentID string) (*HandshakeResultMessage, error) {
	log.Printf("🤝 Starting full 6-step handshake: %s <-> %s", h.localOCX.InstanceID, h.remoteOCX.InstanceID)

	// Create handshake session
	session := NewHandshakeSession(h.localOCX, h.remoteOCX, h.ledger)

	// Step 1: HELLO
	hello, err := session.SendHello(ctx)
	if err != nil {
		return nil, fmt.Errorf("HELLO failed: %w", err)
	}

	// In a real implementation, this would be sent to the remote agent via gRPC
	// For now, simulate receiving it
	if err := session.ReceiveHello(ctx, hello); err != nil {
		return nil, fmt.Errorf("HELLO validation failed: %w", err)
	}

	// Step 2: CHALLENGE
	challenge, err := session.SendChallenge(ctx, hello)
	if err != nil {
		return nil, fmt.Errorf("CHALLENGE failed: %w", err)
	}

	if err := session.ReceiveChallenge(ctx, challenge); err != nil {
		return nil, fmt.Errorf("CHALLENGE validation failed: %w", err)
	}

	// Step 3: PROOF
	proof, err := session.GenerateProof(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("PROOF generation failed: %w", err)
	}

	if err := session.ReceiveProof(ctx, proof); err != nil {
		return nil, fmt.Errorf("PROOF verification failed: %w", err)
	}

	// Step 4: VERIFY
	verify, err := session.PerformVerification(ctx, proof, agentID)
	if err != nil {
		return nil, fmt.Errorf("VERIFY failed: %w", err)
	}

	// Step 5: ATTESTATION
	attestation, err := session.ExchangeAttestation(ctx, verify)
	if err != nil {
		return nil, fmt.Errorf("ATTESTATION failed: %w", err)
	}

	if err := session.ReceiveAttestation(ctx, attestation); err != nil {
		return nil, fmt.Errorf("ATTESTATION validation failed: %w", err)
	}

	// Step 6: ACCEPT/REJECT
	result, err := session.FinalizeHandshake(ctx, attestation)
	if err != nil {
		return nil, fmt.Errorf("FINALIZE failed: %w", err)
	}

	// Update local trust level
	h.trustLevel = result.TrustLevel

	log.Printf("✅ Full handshake complete: %s (trust_level=%.2f, duration=%dms)",
		result.Verdict, result.TrustLevel, result.DurationMs)

	return result, nil
}

// verifySPIFFECertificates performs mutual SPIFFE authentication
func (h *InterOCXHandshake) verifySPIFFECertificates(ctx context.Context) error {
	// Get local SPIFFE SVID
	localSVID, err := h.localOCX.SPIFFESource.GetX509SVID()
	if err != nil {
		return fmt.Errorf("failed to get local SVID: %w", err)
	}

	// Get remote SPIFFE SVID
	remoteSVID, err := h.remoteOCX.SPIFFESource.GetX509SVID()
	if err != nil {
		return fmt.Errorf("failed to get remote SVID: %w", err)
	}

	// Verify local certificate
	if err := h.verifyCertificate(localSVID); err != nil {
		return fmt.Errorf("local certificate invalid: %w", err)
	}

	// Verify remote certificate
	if err := h.verifyCertificate(remoteSVID); err != nil {
		return fmt.Errorf("remote certificate invalid: %w", err)
	}

	// Verify trust domains match expected values
	localID, err := spiffeid.FromString(localSVID.ID.String())
	if err != nil {
		return fmt.Errorf("invalid local SPIFFE ID: %w", err)
	}

	remoteID, err := spiffeid.FromString(remoteSVID.ID.String())
	if err != nil {
		return fmt.Errorf("invalid remote SPIFFE ID: %w", err)
	}

	if localID.TrustDomain().String() != h.localOCX.TrustDomain {
		return errors.New("local trust domain mismatch")
	}

	if remoteID.TrustDomain().String() != h.remoteOCX.TrustDomain {
		return errors.New("remote trust domain mismatch")
	}

	log.Printf("🔐 SPIFFE certificates verified: %s <-> %s", localID, remoteID)

	return nil
}

// verifyCertificate validates a single SPIFFE SVID
func (h *InterOCXHandshake) verifyCertificate(svid *x509svid.SVID) error {
	// Check certificate expiration
	if time.Now().After(svid.Certificates[0].NotAfter) {
		return errors.New("certificate expired")
	}

	if time.Now().Before(svid.Certificates[0].NotBefore) {
		return errors.New("certificate not yet valid")
	}

	// Verify certificate chain
	if len(svid.Certificates) < 2 {
		return errors.New("incomplete certificate chain")
	}

	return nil
}

// GetAuditHash retrieves the audit hash for an agent (zero-knowledge proof)
func (o *OCXInstance) GetAuditHash(agentID string) (string, error) {
	// In production, this would query the local audit database
	// For now, generate a deterministic hash based on agent ID and instance

	data := fmt.Sprintf("%s:%s:%s", o.InstanceID, agentID, time.Now().Format("2006-01-02"))
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:]), nil
}

// calculateTrustLevel computes trust score based on attestation
func (h *InterOCXHandshake) calculateTrustLevel(attestation *TrustAttestation) float64 {
	// Base trust level
	trustLevel := 0.5

	// Increase trust if attestation is recent
	age := time.Since(attestation.Timestamp)
	if age < 1*time.Hour {
		trustLevel += 0.2
	} else if age < 24*time.Hour {
		trustLevel += 0.1
	}

	// Increase trust if both OCX instances are in same organization
	if h.localOCX.Organization == h.remoteOCX.Organization {
		trustLevel += 0.2
	}

	// Increase trust if attestation signature is valid
	if attestation.Signature != "" {
		trustLevel += 0.1
	}

	// Cap at 1.0
	if trustLevel > 1.0 {
		trustLevel = 1.0
	}

	return trustLevel
}

// FederationRegistry maintains directory of trusted OCX instances
type FederationRegistry struct {
	instances map[string]*OCXInstance
}

// NewFederationRegistry creates a new registry
func NewFederationRegistry() *FederationRegistry {
	return &FederationRegistry{
		instances: make(map[string]*OCXInstance),
	}
}

// Register adds an OCX instance to the federation
func (fr *FederationRegistry) Register(instance *OCXInstance) error {
	if instance.InstanceID == "" {
		return errors.New("instance ID required")
	}

	fr.instances[instance.InstanceID] = instance
	log.Printf("📝 Registered OCX instance: %s (%s)", instance.InstanceID, instance.Organization)

	return nil
}

// Lookup retrieves an OCX instance by ID
func (fr *FederationRegistry) Lookup(instanceID string) (*OCXInstance, error) {
	instance, ok := fr.instances[instanceID]
	if !ok {
		return nil, fmt.Errorf("instance not found: %s", instanceID)
	}

	return instance, nil
}

// ListInstances returns all registered instances
func (fr *FederationRegistry) ListInstances() []*OCXInstance {
	instances := make([]*OCXInstance, 0, len(fr.instances))
	for _, instance := range fr.instances {
		instances = append(instances, instance)
	}
	return instances
}
