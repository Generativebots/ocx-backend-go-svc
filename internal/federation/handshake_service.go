package federation

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ============================================================================
// gRPC SERVICE IMPLEMENTATION - Works for ANY 2 agents
// ============================================================================

// HandshakeServiceServer implements the InterOCXHandshakeService
type HandshakeServiceServer struct {
	// UnimplementedInterOCXHandshakeServiceServer

	// Active handshake sessions
	sessions map[string]*HandshakeSession
	mu       sync.RWMutex

	// Local agent/OCX instance
	localAgent *OCXInstance

	// Ledger for attestation
	ledger *TrustAttestationLedger

	// Configuration
	minTrustLevel float64
	sessionTTL    time.Duration
}

// NewHandshakeServiceServer creates a new handshake service server
func NewHandshakeServiceServer(localAgent *OCXInstance, ledger *TrustAttestationLedger) *HandshakeServiceServer {
	return &HandshakeServiceServer{
		sessions:      make(map[string]*HandshakeSession),
		localAgent:    localAgent,
		ledger:        ledger,
		minTrustLevel: 0.5,
		sessionTTL:    24 * time.Hour,
	}
}

// InitiateHandshake handles Step 1 (HELLO) and responds with Step 2 (CHALLENGE)
func (s *HandshakeServiceServer) InitiateHandshake(ctx context.Context, hello *HandshakeHelloMessage) (*HandshakeChallengeMessage, error) {
	log.Printf("📥 Received HELLO from %s (%s)", hello.InstanceID, hello.Organization)

	// Create remote agent from HELLO message
	remoteAgent := &OCXInstance{
		InstanceID:   hello.InstanceID,
		Organization: hello.Organization,
		TrustDomain:  hello.Metadata["trust_domain"],
		Region:       hello.Metadata["region"],
	}

	// Create new handshake session
	session := NewHandshakeSession(s.localAgent, remoteAgent, s.ledger)

	// Store session
	s.mu.Lock()
	s.sessions[session.sessionID] = session
	s.mu.Unlock()

	// Process HELLO
	if err := session.ReceiveHello(ctx, hello); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "HELLO validation failed: %v", err)
	}

	// Generate and send CHALLENGE
	challenge, err := session.SendChallenge(ctx, hello)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to generate challenge: %v", err)
	}

	log.Printf("📤 Sent CHALLENGE to %s (session=%s)", hello.InstanceID, session.sessionID)

	return challenge, nil
}

// RespondToChallenge handles Step 3 (PROOF) and responds with Step 4 (VERIFY)
func (s *HandshakeServiceServer) RespondToChallenge(ctx context.Context, proof *HandshakeProofMessage) (*HandshakeVerifyMessage, error) {
	// Find session by proof metadata (would need to add session_id to proof)
	// For now, find the most recent session
	s.mu.RLock()
	var session *HandshakeSession
	for _, sess := range s.sessions {
		if sess.stateMachine.GetCurrentState() == StateChallengeSent {
			session = sess
			break
		}
	}
	s.mu.RUnlock()

	if session == nil {
		return nil, status.Errorf(codes.NotFound, "no active handshake session found")
	}

	log.Printf("📥 Received PROOF (session=%s)", session.sessionID)

	// Verify proof
	if err := session.ReceiveProof(ctx, proof); err != nil {
		return nil, status.Errorf(codes.Unauthenticated, "proof verification failed: %v", err)
	}

	// Perform verification and calculate trust
	verify, err := session.PerformVerification(ctx, proof, "default-agent")
	if err != nil {
		return nil, status.Errorf(codes.Internal, "verification failed: %v", err)
	}

	log.Printf("📤 Sent VERIFY with trust_level=%.2f (session=%s)", verify.TrustLevel, session.sessionID)

	return verify, nil
}

// ExchangeAttestation handles Step 5 (ATTESTATION) and responds with Step 6 (RESULT)
func (s *HandshakeServiceServer) ExchangeAttestation(ctx context.Context, attestation *HandshakeAttestationMessage) (*HandshakeResultMessage, error) {
	// Find session
	sessionID := attestation.Metadata["session_id"]
	s.mu.RLock()
	session, ok := s.sessions[sessionID]
	s.mu.RUnlock()

	if !ok {
		return nil, status.Errorf(codes.NotFound, "session not found: %s", sessionID)
	}

	log.Printf("📥 Received ATTESTATION (session=%s)", sessionID)

	// Receive attestation
	if err := session.ReceiveAttestation(ctx, attestation); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "attestation invalid: %v", err)
	}

	// Finalize handshake
	result, err := session.FinalizeHandshake(ctx, attestation)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to finalize handshake: %v", err)
	}

	log.Printf("📤 Sent RESULT: %s (session=%s)", result.Verdict, sessionID)

	// Clean up session after some time
	go s.cleanupSession(sessionID, 5*time.Minute)

	return result, nil
}

// PerformHandshake handles the full bidirectional streaming handshake
func (s *HandshakeServiceServer) PerformHandshake(stream HandshakeService_PerformHandshakeServer) error {
	ctx := stream.Context()
	sessionID := uuid.New().String()

	log.Printf("🔄 Starting streaming handshake (session=%s)", sessionID)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Receive message
		msg, err := stream.Recv()
		if err != nil {
			return err
		}

		// Process based on message type
		switch m := msg.Message.(type) {
		case *HandshakeMessageWrapper_Hello:
			// Respond with challenge
			challenge, err := s.InitiateHandshake(ctx, m.Hello)
			if err != nil {
				return err
			}
			if err := stream.Send(&HandshakeMessageWrapper{
				Message: &HandshakeMessageWrapper_Challenge{Challenge: challenge},
			}); err != nil {
				return err
			}

		case *HandshakeMessageWrapper_Proof:
			// Respond with verify
			verify, err := s.RespondToChallenge(ctx, m.Proof)
			if err != nil {
				return err
			}
			if err := stream.Send(&HandshakeMessageWrapper{
				Message: &HandshakeMessageWrapper_Verify{Verify: verify},
			}); err != nil {
				return err
			}

		case *HandshakeMessageWrapper_Attestation:
			// Respond with result
			result, err := s.ExchangeAttestation(ctx, m.Attestation)
			if err != nil {
				return err
			}
			if err := stream.Send(&HandshakeMessageWrapper{
				Message: &HandshakeMessageWrapper_Result{Result: result},
			}); err != nil {
				return err
			}

			// Handshake complete
			log.Printf("✅ Streaming handshake complete: %s (session=%s)", result.Verdict, sessionID)
			return nil
		}
	}
}

// GetHandshakeStatus returns the status of a handshake session
func (s *HandshakeServiceServer) GetHandshakeStatus(ctx context.Context, req *HandshakeStatusRequest) (*HandshakeStateMessage, error) {
	s.mu.RLock()
	session, ok := s.sessions[req.SessionId]
	s.mu.RUnlock()

	if !ok {
		return nil, status.Errorf(codes.NotFound, "session not found: %s", req.SessionId)
	}

	state := &HandshakeStateMessage{
		SessionId:        session.sessionID,
		LocalInstanceId:  session.localOCX.InstanceID,
		RemoteInstanceId: session.remoteOCX.InstanceID,
		CurrentState:     int32(session.stateMachine.GetCurrentState()),
		StartedAt:        session.stateMachine.GetStartTime().Unix(),
		LastUpdatedAt:    session.stateMachine.GetLastUpdateTime().Unix(),
		TimeoutAt:        session.stateMachine.GetStartTime().Add(3 * time.Minute).Unix(),
	}

	if err := session.stateMachine.GetLastError(); err != nil {
		state.Error = err.Error()
	}

	return state, nil
}

// cleanupSession removes a session after a delay
func (s *HandshakeServiceServer) cleanupSession(sessionID string, delay time.Duration) {
	time.Sleep(delay)
	s.mu.Lock()
	delete(s.sessions, sessionID)
	s.mu.Unlock()
	log.Printf("🗑️  Cleaned up session: %s", sessionID)
}

// ============================================================================
// PLACEHOLDER TYPES (until protobuf is compiled)
// ============================================================================

type HandshakeService_PerformHandshakeServer interface {
	Send(*HandshakeMessageWrapper) error
	Recv() (*HandshakeMessageWrapper, error)
	Context() context.Context
}

type HandshakeMessageWrapper struct {
	Message interface{}
}

type HandshakeMessageWrapper_Hello struct {
	Hello *HandshakeHelloMessage
}

type HandshakeMessageWrapper_Challenge struct {
	Challenge *HandshakeChallengeMessage
}

type HandshakeMessageWrapper_Proof struct {
	Proof *HandshakeProofMessage
}

type HandshakeMessageWrapper_Verify struct {
	Verify *HandshakeVerifyMessage
}

type HandshakeMessageWrapper_Attestation struct {
	Attestation *HandshakeAttestationMessage
}

type HandshakeMessageWrapper_Result struct {
	Result *HandshakeResultMessage
}

type HandshakeStatusRequest struct {
	SessionId string
}

type HandshakeStateMessage struct {
	SessionId        string
	LocalInstanceId  string
	RemoteInstanceId string
	CurrentState     int32
	StartedAt        int64
	LastUpdatedAt    int64
	TimeoutAt        int64
	Nonce            string
	Challenge        []byte
	Error            string
}
