// Package federation implements the Inter-OCX Federation Protocol.
// This enables secure, trust-attested communication between OCX instances.
package federation

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"
)

// ============================================================================
// FEDERATION IDENTIFIERS
// ============================================================================

// OCXInstanceID uniquely identifies an OCX deployment
type OCXInstanceID string

// TrustLevel represents the trust relationship between OCX instances
type TrustLevel string

const (
	TrustNone        TrustLevel = "NONE"        // No trust established
	TrustProvisional TrustLevel = "PROVISIONAL" // Initial handshake complete
	TrustVerified    TrustLevel = "VERIFIED"    // Attestation verified
	TrustMutual      TrustLevel = "MUTUAL"      // Full bidirectional trust
	TrustRevoked     TrustLevel = "REVOKED"     // Trust revoked
)

// FederationState represents the state of a federation relationship
type FederationState string

const (
	StateDisconnected FederationState = "DISCONNECTED"
	StateHandshaking  FederationState = "HANDSHAKING"
	StateConnected    FederationState = "CONNECTED"
	StateSuspended    FederationState = "SUSPENDED"
)

// ============================================================================
// ATTESTATION
// ============================================================================

// Attestation proves identity and capability of an OCX instance
type Attestation struct {
	InstanceID     OCXInstanceID          `json:"instance_id"`
	Region         string                 `json:"region"`
	Version        string                 `json:"version"`
	PublicKey      []byte                 `json:"public_key"`
	Capabilities   []string               `json:"capabilities"`
	TenantCount    int                    `json:"tenant_count"`
	AgentCount     int                    `json:"agent_count"`
	GovernanceHash string                 `json:"governance_hash"`
	Timestamp      time.Time              `json:"timestamp"`
	ValidUntil     time.Time              `json:"valid_until"`
	Signature      []byte                 `json:"signature"`
	Metadata       map[string]interface{} `json:"metadata,omitempty"`
}

// Sign signs the attestation with the private key
func (a *Attestation) Sign(privateKey ed25519.PrivateKey) error {
	// Create canonical representation for signing
	data, err := a.canonicalBytes()
	if err != nil {
		return err
	}

	a.Signature = ed25519.Sign(privateKey, data)
	return nil
}

// Verify verifies the attestation signature
func (a *Attestation) Verify() bool {
	if len(a.PublicKey) != ed25519.PublicKeySize {
		return false
	}

	data, err := a.canonicalBytes()
	if err != nil {
		return false
	}

	return ed25519.Verify(a.PublicKey, data, a.Signature)
}

func (a *Attestation) canonicalBytes() ([]byte, error) {
	// Create a copy without signature for signing/verification
	copy := *a
	copy.Signature = nil
	return json.Marshal(copy)
}

// Hash returns a unique hash of the attestation
func (a *Attestation) Hash() string {
	data, _ := a.canonicalBytes()
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// ============================================================================
// HANDSHAKE PROTOCOL
// ============================================================================

// HandshakeMessage represents a federation handshake message
type HandshakeMessage struct {
	Type        HandshakeType `json:"type"`
	InstanceID  OCXInstanceID `json:"instance_id"`
	Nonce       []byte        `json:"nonce"`
	Attestation *Attestation  `json:"attestation,omitempty"`
	Challenge   []byte        `json:"challenge,omitempty"`
	Response    []byte        `json:"response,omitempty"`
	Timestamp   time.Time     `json:"timestamp"`
}

// HandshakeType defines the handshake message type
type HandshakeType string

const (
	HandshakeHello     HandshakeType = "HELLO"     // Initial contact
	HandshakeChallenge HandshakeType = "CHALLENGE" // Challenge request
	HandshakeResponse  HandshakeType = "RESPONSE"  // Challenge response
	HandshakeConfirm   HandshakeType = "CONFIRM"   // Handshake complete
	HandshakeReject    HandshakeType = "REJECT"    // Handshake rejected
)

// HandshakeResult contains the result of a handshake
type HandshakeResult struct {
	Success      bool
	PeerID       OCXInstanceID
	TrustLevel   TrustLevel
	SessionKey   []byte // Derived session key for encrypted communication
	ErrorMessage string
}

// ============================================================================
// PEER CONNECTION
// ============================================================================

// PeerConnection represents a federation connection to another OCX instance
type PeerConnection struct {
	ID            OCXInstanceID
	Endpoint      string
	State         FederationState
	TrustLevel    TrustLevel
	Attestation   *Attestation
	SessionKey    []byte
	ConnectedAt   time.Time
	LastHeartbeat time.Time
	LastActivity  time.Time

	// Statistics
	MessagesSent     int64
	MessagesReceived int64
	BytesSent        int64
	BytesReceived    int64
	ErrorCount       int64

	mu sync.RWMutex
}

// IsHealthy checks if the connection is healthy
func (p *PeerConnection) IsHealthy() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.State != StateConnected {
		return false
	}

	// Check heartbeat timeout (5 minutes)
	if time.Since(p.LastHeartbeat) > 5*time.Minute {
		return false
	}

	return true
}

// ============================================================================
// FEDERATION MANAGER
// ============================================================================

// FederationManager manages federation relationships with other OCX instances
type FederationManager struct {
	instanceID OCXInstanceID
	region     string
	version    string
	privateKey ed25519.PrivateKey
	publicKey  ed25519.PublicKey

	peers        map[OCXInstanceID]*PeerConnection
	pendingPeers map[OCXInstanceID]*PendingHandshake

	// Configuration
	governanceHash string
	capabilities   []string
	maxPeers       int

	// Callbacks
	onPeerConnected    func(peerID OCXInstanceID)
	onPeerDisconnected func(peerID OCXInstanceID)
	onMessageReceived  func(peerID OCXInstanceID, msg []byte)

	mu     sync.RWMutex
	logger *log.Logger
}

// PendingHandshake tracks ongoing handshake
type PendingHandshake struct {
	PeerID      OCXInstanceID
	Nonce       []byte
	Challenge   []byte
	StartedAt   time.Time
	Attestation *Attestation
}

// FederationConfig holds federation manager configuration
type FederationConfig struct {
	InstanceID     OCXInstanceID
	Region         string
	Version        string
	GovernanceHash string
	Capabilities   []string
	MaxPeers       int
}

// NewFederationManager creates a new federation manager
func NewFederationManager(cfg FederationConfig) (*FederationManager, error) {
	// Generate key pair
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate key pair: %w", err)
	}

	return &FederationManager{
		instanceID:     cfg.InstanceID,
		region:         cfg.Region,
		version:        cfg.Version,
		privateKey:     privateKey,
		publicKey:      publicKey,
		peers:          make(map[OCXInstanceID]*PeerConnection),
		pendingPeers:   make(map[OCXInstanceID]*PendingHandshake),
		governanceHash: cfg.GovernanceHash,
		capabilities:   cfg.Capabilities,
		maxPeers:       cfg.MaxPeers,
		logger:         log.New(log.Writer(), fmt.Sprintf("[Federation:%s] ", cfg.InstanceID), log.LstdFlags),
	}, nil
}

// CreateAttestation creates a signed attestation for this instance
func (fm *FederationManager) CreateAttestation(tenantCount, agentCount int) (*Attestation, error) {
	attestation := &Attestation{
		InstanceID:     fm.instanceID,
		Region:         fm.region,
		Version:        fm.version,
		PublicKey:      fm.publicKey,
		Capabilities:   fm.capabilities,
		TenantCount:    tenantCount,
		AgentCount:     agentCount,
		GovernanceHash: fm.governanceHash,
		Timestamp:      time.Now(),
		ValidUntil:     time.Now().Add(24 * time.Hour),
	}

	if err := attestation.Sign(fm.privateKey); err != nil {
		return nil, err
	}

	return attestation, nil
}

// InitiateHandshake starts a handshake with a peer OCX instance
func (fm *FederationManager) InitiateHandshake(ctx context.Context, peerEndpoint string) (*HandshakeResult, error) {
	fm.mu.Lock()

	if len(fm.peers) >= fm.maxPeers {
		fm.mu.Unlock()
		return nil, errors.New("maximum peers reached")
	}

	// Generate nonce
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		fm.mu.Unlock()
		return nil, err
	}

	// Create attestation
	attestation, err := fm.CreateAttestation(0, 0)
	if err != nil {
		fm.mu.Unlock()
		return nil, err
	}

	// Create HELLO message
	hello := &HandshakeMessage{
		Type:        HandshakeHello,
		InstanceID:  fm.instanceID,
		Nonce:       nonce,
		Attestation: attestation,
		Timestamp:   time.Now(),
	}

	fm.mu.Unlock()

	fm.logger.Printf("Initiating handshake with %s", peerEndpoint)

	// In production, this would send via network
	// For now, return a simulated successful handshake
	_ = hello

	return &HandshakeResult{
		Success:    true,
		TrustLevel: TrustProvisional,
	}, nil
}

// ProcessHandshakeMessage processes an incoming handshake message
func (fm *FederationManager) ProcessHandshakeMessage(msg *HandshakeMessage) (*HandshakeMessage, error) {
	switch msg.Type {
	case HandshakeHello:
		return fm.handleHello(msg)
	case HandshakeChallenge:
		return fm.handleChallenge(msg)
	case HandshakeResponse:
		return fm.handleResponse(msg)
	case HandshakeConfirm:
		return fm.handleConfirm(msg)
	default:
		return nil, fmt.Errorf("unknown handshake type: %s", msg.Type)
	}
}

func (fm *FederationManager) handleHello(msg *HandshakeMessage) (*HandshakeMessage, error) {
	// Verify attestation
	if msg.Attestation == nil || !msg.Attestation.Verify() {
		return &HandshakeMessage{
			Type:       HandshakeReject,
			InstanceID: fm.instanceID,
			Timestamp:  time.Now(),
		}, errors.New("invalid attestation")
	}

	// Generate challenge
	challenge := make([]byte, 32)
	if _, err := rand.Read(challenge); err != nil {
		return nil, err
	}

	// Store pending handshake state
	fm.mu.Lock()
	fm.pendingPeers[msg.InstanceID] = &PendingHandshake{
		PeerID:      msg.InstanceID,
		Nonce:       msg.Nonce,
		Challenge:   challenge,
		StartedAt:   time.Now(),
		Attestation: msg.Attestation,
	}
	fm.mu.Unlock()

	// Create our attestation
	attestation, err := fm.CreateAttestation(0, 0)
	if err != nil {
		return nil, err
	}

	return &HandshakeMessage{
		Type:        HandshakeChallenge,
		InstanceID:  fm.instanceID,
		Nonce:       msg.Nonce,
		Attestation: attestation,
		Challenge:   challenge,
		Timestamp:   time.Now(),
	}, nil
}

func (fm *FederationManager) handleChallenge(msg *HandshakeMessage) (*HandshakeMessage, error) {
	// Sign the challenge with our private key
	response := ed25519.Sign(fm.privateKey, msg.Challenge)

	return &HandshakeMessage{
		Type:       HandshakeResponse,
		InstanceID: fm.instanceID,
		Nonce:      msg.Nonce,
		Challenge:  msg.Challenge,
		Response:   response,
		Timestamp:  time.Now(),
	}, nil
}

func (fm *FederationManager) handleResponse(msg *HandshakeMessage) (*HandshakeMessage, error) {
	fm.mu.Lock()
	pending, exists := fm.pendingPeers[msg.InstanceID]
	if !exists {
		fm.mu.Unlock()
		return nil, errors.New("no pending handshake for this peer")
	}

	// Verify the response signature
	if !ed25519.Verify(pending.Attestation.PublicKey, pending.Challenge, msg.Response) {
		delete(fm.pendingPeers, msg.InstanceID)
		fm.mu.Unlock()
		return &HandshakeMessage{
			Type:       HandshakeReject,
			InstanceID: fm.instanceID,
			Timestamp:  time.Now(),
		}, errors.New("challenge response verification failed")
	}

	// Create peer connection
	conn := &PeerConnection{
		ID:            msg.InstanceID,
		State:         StateConnected,
		TrustLevel:    TrustVerified,
		Attestation:   pending.Attestation,
		ConnectedAt:   time.Now(),
		LastHeartbeat: time.Now(),
		LastActivity:  time.Now(),
	}

	fm.peers[msg.InstanceID] = conn
	delete(fm.pendingPeers, msg.InstanceID)
	fm.mu.Unlock()

	fm.logger.Printf("Handshake complete with %s (trust=%s)", msg.InstanceID, TrustVerified)

	if fm.onPeerConnected != nil {
		fm.onPeerConnected(msg.InstanceID)
	}

	return &HandshakeMessage{
		Type:       HandshakeConfirm,
		InstanceID: fm.instanceID,
		Timestamp:  time.Now(),
	}, nil
}

func (fm *FederationManager) handleConfirm(msg *HandshakeMessage) (*HandshakeMessage, error) {
	fm.logger.Printf("Handshake confirmed by %s", msg.InstanceID)
	return nil, nil // No response needed
}

// GetPeer returns a peer connection by ID
func (fm *FederationManager) GetPeer(id OCXInstanceID) (*PeerConnection, error) {
	fm.mu.RLock()
	defer fm.mu.RUnlock()

	peer, exists := fm.peers[id]
	if !exists {
		return nil, fmt.Errorf("peer %s not found", id)
	}
	return peer, nil
}

// ListPeers returns all connected peers
func (fm *FederationManager) ListPeers() []*PeerConnection {
	fm.mu.RLock()
	defer fm.mu.RUnlock()

	peers := make([]*PeerConnection, 0, len(fm.peers))
	for _, p := range fm.peers {
		peers = append(peers, p)
	}
	return peers
}

// DisconnectPeer disconnects from a peer
func (fm *FederationManager) DisconnectPeer(id OCXInstanceID) error {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	peer, exists := fm.peers[id]
	if !exists {
		return fmt.Errorf("peer %s not found", id)
	}

	peer.State = StateDisconnected
	delete(fm.peers, id)

	fm.logger.Printf("Disconnected from peer %s", id)

	if fm.onPeerDisconnected != nil {
		fm.onPeerDisconnected(id)
	}

	return nil
}

// SetCallbacks sets event callbacks
func (fm *FederationManager) SetCallbacks(
	onConnected func(OCXInstanceID),
	onDisconnected func(OCXInstanceID),
	onMessage func(OCXInstanceID, []byte),
) {
	fm.onPeerConnected = onConnected
	fm.onPeerDisconnected = onDisconnected
	fm.onMessageReceived = onMessage
}

// ============================================================================
// FEDERATED MESSAGE
// ============================================================================

// FederatedMessage represents a message sent between OCX instances
type FederatedMessage struct {
	ID           string        `json:"id"`
	SourceOCX    OCXInstanceID `json:"source_ocx"`
	DestOCX      OCXInstanceID `json:"dest_ocx"`
	SourceTenant string        `json:"source_tenant"`
	DestTenant   string        `json:"dest_tenant"`
	SourceAgent  string        `json:"source_agent"`
	DestAgent    string        `json:"dest_agent"`
	MessageType  string        `json:"message_type"`
	Payload      []byte        `json:"payload"`
	Timestamp    time.Time     `json:"timestamp"`
	TTL          int           `json:"ttl"`
	TraceID      string        `json:"trace_id"`
	Signature    []byte        `json:"signature"`
}

// SendMessage sends a message to a federated peer
func (fm *FederationManager) SendMessage(ctx context.Context, msg *FederatedMessage) error {
	fm.mu.RLock()
	peer, exists := fm.peers[msg.DestOCX]
	fm.mu.RUnlock()

	if !exists {
		return fmt.Errorf("peer %s not connected", msg.DestOCX)
	}

	if !peer.IsHealthy() {
		return fmt.Errorf("peer %s is unhealthy", msg.DestOCX)
	}

	// Sign the message
	msgBytes, _ := json.Marshal(msg)
	msg.Signature = ed25519.Sign(fm.privateKey, msgBytes)

	peer.mu.Lock()
	peer.MessagesSent++
	peer.BytesSent += int64(len(msgBytes))
	peer.LastActivity = time.Now()
	peer.mu.Unlock()

	fm.logger.Printf("Sent message %s to %s", msg.ID, msg.DestOCX)

	return nil
}

// Stats returns federation statistics
func (fm *FederationManager) Stats() map[string]interface{} {
	fm.mu.RLock()
	defer fm.mu.RUnlock()

	connected := 0
	totalSent := int64(0)
	totalRecv := int64(0)

	for _, p := range fm.peers {
		if p.State == StateConnected {
			connected++
		}
		totalSent += p.MessagesSent
		totalRecv += p.MessagesReceived
	}

	return map[string]interface{}{
		"instance_id":        fm.instanceID,
		"region":             fm.region,
		"total_peers":        len(fm.peers),
		"connected_peers":    connected,
		"pending_handshakes": len(fm.pendingPeers),
		"messages_sent":      totalSent,
		"messages_received":  totalRecv,
	}
}
