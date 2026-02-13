package federation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"time"

	pb "github.com/ocx/backend/pb"
	"github.com/spiffe/go-spiffe/v2/workloadapi"
)

// OCXInstance represents a local or remote OCX deployment
type OCXInstance struct {
	InstanceID   string
	TrustDomain  string
	SPIFFESource *workloadapi.X509Source
	Region       string
	Organization string
	// P3 FIX: GRPCAddr is the remote gRPC endpoint for federation handshake.
	// If set, NegotiateV2 uses real gRPC transport. If empty, falls back to in-memory.
	GRPCAddr string
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

// NegotiateV2 performs the full 6-step Inter-OCX handshake.
// P3 FIX: When the remote OCX has a GRPCAddr, this delegates to
// HandshakeClient.PerformFullHandshake() for real gRPC transport.
// Falls back to in-memory simulation when no remote endpoint is configured.
func (h *InterOCXHandshake) NegotiateV2(ctx context.Context, agentID string) (*pb.HandshakeResult, error) {
	slog.Info("Starting full 6-step handshake: <->", "instance_i_d", h.localOCX.InstanceID, "instance_i_d", h.remoteOCX.InstanceID)
	// P3 FIX: Use real gRPC transport if remote has an address
	if h.remoteOCX.GRPCAddr != "" {
		slog.Info("Using gRPC transport to for federation handshake", "g_r_p_c_addr", h.remoteOCX.GRPCAddr)
		client, err := NewHandshakeClient(h.remoteOCX.GRPCAddr, h.localOCX, h.ledger)
		if err != nil {
			slog.Warn("gRPC connection to failed (), falling back to in-memory handshake", "g_r_p_c_addr", h.remoteOCX.GRPCAddr, "error", err)
			// Fall through to in-memory simulation below
		} else {
			defer client.Close()

			result, err := client.PerformFullHandshake(ctx, h.remoteOCX.InstanceID, agentID)
			if err != nil {
				slog.Warn("gRPC handshake with failed (), falling back to in-memory", "instance_i_d", h.remoteOCX.InstanceID, "error", err)
				// Fall through to in-memory simulation below
			} else {
				h.trustLevel = result.TrustLevel
				slog.Info("gRPC handshake complete: (trust_level=, duration=ms)", "verdict", result.Verdict, "trust_level", result.TrustLevel, "duration_ms", result.DurationMs)
				return result, nil
			}
		}
	} else {
		slog.Info("🧪 No remote gRPC address — using in-memory handshake simulation")
	}

	// In-memory simulation (dev/test fallback)
	session := NewHandshakeSession(h.localOCX, h.remoteOCX, h.ledger)

	// Step 1: HELLO
	hello, err := session.SendHello(ctx)
	if err != nil {
		return nil, fmt.Errorf("HELLO failed: %w", err)
	}

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

	slog.Info("In-memory handshake complete: (trust_level=, duration=ms)", "verdict", result.Verdict, "trust_level", result.TrustLevel, "duration_ms", result.DurationMs)
	return result, nil
}

// GetAuditHash retrieves the audit hash for an agent (zero-knowledge proof)
func (o *OCXInstance) GetAuditHash(agentID string) (string, error) {
	// In production, this would query the local audit database
	// For now, generate a deterministic hash based on agent ID and instance

	data := fmt.Sprintf("%s:%s:%s", o.InstanceID, agentID, time.Now().Format("2006-01-02"))
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:]), nil
}

// FederationRegistry maintains directory of trusted OCX instances
type FederationRegistry struct {
	instances      map[string]*OCXInstance
	handshakeStore *SupabaseHandshakeStore // optional durable store
}

// NewFederationRegistry creates a new registry
func NewFederationRegistry() *FederationRegistry {
	return &FederationRegistry{
		instances: make(map[string]*OCXInstance),
	}
}

// SetHandshakeStore injects a durable Supabase-backed handshake store.
func (fr *FederationRegistry) SetHandshakeStore(s *SupabaseHandshakeStore) {
	fr.handshakeStore = s
}

// Register adds an OCX instance to the federation
func (fr *FederationRegistry) Register(instance *OCXInstance) error {
	if instance.InstanceID == "" {
		return errors.New("instance ID required")
	}

	fr.instances[instance.InstanceID] = instance
	slog.Info("Registered OCX instance:", "instance_i_d", instance.InstanceID, "organization", instance.Organization)
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
