package federation

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	pb "github.com/ocx/backend/pb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ============================================================================
// gRPC SERVICE IMPLEMENTATION - Works for ANY 2 agents
// ============================================================================

// HandshakeServiceServer implements the InterOCXHandshakeService
type HandshakeServiceServer struct {
	pb.UnimplementedInterOCXHandshakeServiceServer

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
func (s *HandshakeServiceServer) InitiateHandshake(ctx context.Context, hello *pb.HandshakeHello) (*pb.HandshakeChallenge, error) {
	slog.Info("Received HELLO from", "instance_id", hello.InstanceId, "organization", hello.Organization)
	// Create remote agent from HELLO message
	remoteAgent := &OCXInstance{
		InstanceID:   hello.InstanceId,
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

	slog.Info("Sent CHALLENGE to (session=)", "instance_id", hello.InstanceId, "session_i_d", session.sessionID)
	return challenge, nil
}

// RespondToChallenge handles Step 3 (PROOF) and responds with Step 4 (VERIFY)
func (s *HandshakeServiceServer) RespondToChallenge(ctx context.Context, proof *pb.HandshakeProof) (*pb.HandshakeVerify, error) {
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

	slog.Info("Received PROOF (session=)", "session_i_d", session.sessionID)
	// Verify proof
	if err := session.ReceiveProof(ctx, proof); err != nil {
		return nil, status.Errorf(codes.Unauthenticated, "proof verification failed: %v", err)
	}

	// Perform verification and calculate trust
	verify, err := session.PerformVerification(ctx, proof, "default-agent")
	if err != nil {
		return nil, status.Errorf(codes.Internal, "verification failed: %v", err)
	}

	slog.Info("Sent VERIFY with trust_level= (session=)", "trust_level", verify.TrustLevel, "session_i_d", session.sessionID)
	return verify, nil
}

// ExchangeAttestation handles Step 5 (ATTESTATION) and responds with Step 6 (RESULT)
func (s *HandshakeServiceServer) ExchangeAttestation(ctx context.Context, attestation *pb.HandshakeAttestation) (*pb.HandshakeResult, error) {
	// Find session
	sessionID := attestation.Metadata["session_id"]
	s.mu.RLock()
	session, ok := s.sessions[sessionID]
	s.mu.RUnlock()

	if !ok {
		return nil, status.Errorf(codes.NotFound, "session not found: %s", sessionID)
	}

	slog.Info("Received ATTESTATION (session=)", "session_i_d", sessionID)
	// Receive attestation
	if err := session.ReceiveAttestation(ctx, attestation); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "attestation invalid: %v", err)
	}

	// Finalize handshake
	result, err := session.FinalizeHandshake(ctx, attestation)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to finalize handshake: %v", err)
	}

	slog.Info("Sent RESULT: (session=)", "verdict", result.Verdict, "session_i_d", sessionID)
	// Clean up session after some time
	go s.cleanupSession(sessionID, 5*time.Minute)

	return result, nil
}

// PerformHandshake handles the full bidirectional streaming handshake
func (s *HandshakeServiceServer) PerformHandshake(stream pb.InterOCXHandshakeService_PerformHandshakeServer) error {
	ctx := stream.Context()
	sessionID := uuid.New().String()

	slog.Info("Starting streaming handshake (session=)", "session_i_d", sessionID)
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
		case *pb.HandshakeMessage_Hello:
			// Respond with challenge
			challenge, err := s.InitiateHandshake(ctx, m.Hello)
			if err != nil {
				return err
			}
			if err := stream.Send(&pb.HandshakeMessage{
				Message: &pb.HandshakeMessage_Challenge{Challenge: challenge},
			}); err != nil {
				return err
			}

		case *pb.HandshakeMessage_Proof:
			// Respond with verify
			verify, err := s.RespondToChallenge(ctx, m.Proof)
			if err != nil {
				return err
			}
			if err := stream.Send(&pb.HandshakeMessage{
				Message: &pb.HandshakeMessage_Verify{Verify: verify},
			}); err != nil {
				return err
			}

		case *pb.HandshakeMessage_Attestation:
			// Respond with result
			result, err := s.ExchangeAttestation(ctx, m.Attestation)
			if err != nil {
				return err
			}
			if err := stream.Send(&pb.HandshakeMessage{
				Message: &pb.HandshakeMessage_Result{Result: result},
			}); err != nil {
				return err
			}

			// Handshake complete
			slog.Info("Streaming handshake complete: (session=)", "verdict", result.Verdict, "session_i_d", sessionID)
			return nil
		}
	}
}

// GetHandshakeStatus returns the status of a handshake session
func (s *HandshakeServiceServer) GetHandshakeStatus(ctx context.Context, req *pb.HandshakeStatusRequest) (*pb.HandshakeState, error) {
	s.mu.RLock()
	session, ok := s.sessions[req.SessionId]
	s.mu.RUnlock()

	if !ok {
		return nil, status.Errorf(codes.NotFound, "session not found: %s", req.SessionId)
	}

	state := &pb.HandshakeState{
		SessionId:        session.sessionID,
		LocalInstanceId:  session.localOCX.InstanceID,
		RemoteInstanceId: session.remoteOCX.InstanceID,
		CurrentState:     pb.HandshakeState_State(session.stateMachine.GetCurrentState()),
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
	slog.Info("Cleaned up session", "session_i_d", sessionID)
}
