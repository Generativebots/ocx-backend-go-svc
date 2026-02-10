package federation

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	pb "github.com/ocx/backend/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// ============================================================================
// gRPC CLIENT - Initiates handshake with any remote agent
// ============================================================================

// HandshakeClient wraps the gRPC client for handshake operations
type HandshakeClient struct {
	conn   *grpc.ClientConn
	client pb.InterOCXHandshakeServiceClient

	localAgent *OCXInstance
	ledger     *TrustAttestationLedger
}

// NewHandshakeClient creates a new handshake client
func NewHandshakeClient(serverAddr string, localAgent *OCXInstance, ledger *TrustAttestationLedger) (*HandshakeClient, error) {
	// Connect to remote agent
	conn, err := grpc.Dial(serverAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to %s: %w", serverAddr, err)
	}

	return &HandshakeClient{
		conn:       conn,
		client:     pb.NewInterOCXHandshakeServiceClient(conn),
		localAgent: localAgent,
		ledger:     ledger,
	}, nil
}

// Close closes the gRPC connection
func (c *HandshakeClient) Close() error {
	return c.conn.Close()
}

// PerformFullHandshake executes the complete 6-step handshake with a remote agent
func (c *HandshakeClient) PerformFullHandshake(ctx context.Context, remoteAgentID string, agentID string) (*pb.HandshakeResult, error) {
	slog.Info("Starting full handshake with", "remote_agent_i_d", remoteAgentID)
	// Create remote agent placeholder
	remoteAgent := &OCXInstance{
		InstanceID: remoteAgentID,
	}

	// Create handshake session
	session := NewHandshakeSession(c.localAgent, remoteAgent, c.ledger)

	// ========================================================================
	// STEP 1: Send HELLO
	// ========================================================================
	hello, err := session.SendHello(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create HELLO: %w", err)
	}

	slog.Info("[1/6] Sending HELLO to", "remote_agent_i_d", remoteAgentID)
	// Call remote agent
	challenge, err := c.client.InitiateHandshake(ctx, hello)
	if err != nil {
		return nil, fmt.Errorf("HELLO failed: %w", err)
	}

	slog.Info("📥 [2/6] Received CHALLENGE")
	// ========================================================================
	// STEP 2: Process CHALLENGE
	// ========================================================================
	if err := session.ReceiveChallenge(ctx, challenge); err != nil {
		return nil, fmt.Errorf("failed to process CHALLENGE: %w", err)
	}

	// ========================================================================
	// STEP 3: Generate and send PROOF
	// ========================================================================
	proof, err := session.GenerateProof(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("failed to generate PROOF: %w", err)
	}

	slog.Info("📤 [3/6] Sending PROOF")
	verify, err := c.client.RespondToChallenge(ctx, proof)
	if err != nil {
		return nil, fmt.Errorf("PROOF failed: %w", err)
	}

	slog.Info("[4/6] Received VERIFY (trust_level=)", "trust_level", verify.TrustLevel)
	// ========================================================================
	// STEP 4: Process VERIFY (already done by server)
	// ========================================================================

	// ========================================================================
	// STEP 5: Generate and send ATTESTATION
	// ========================================================================
	attestation, err := session.ExchangeAttestation(ctx, verify)
	if err != nil {
		return nil, fmt.Errorf("failed to generate ATTESTATION: %w", err)
	}

	slog.Info("📤 [5/6] Sending ATTESTATION")
	result, err := c.client.ExchangeAttestation(ctx, attestation)
	if err != nil {
		return nil, fmt.Errorf("ATTESTATION failed: %w", err)
	}

	slog.Info("[6/6] Received RESULT", "verdict", result.Verdict)
	// ========================================================================
	// STEP 6: Process RESULT
	// ========================================================================
	if result.Verdict == "ACCEPTED" {
		slog.Info("Handshake ACCEPTED: trust_level=, session= (duration=ms)", "trust_level", result.TrustLevel, "session_id", result.SessionId, "duration_ms", result.DurationMs)
	} else {
		slog.Info("Handshake REJECTED", "reason", result.Reason)
	}

	return result, nil
}

// PerformStreamingHandshake uses bidirectional streaming for the handshake
func (c *HandshakeClient) PerformStreamingHandshake(ctx context.Context, remoteAgentID string, agentID string) (*pb.HandshakeResult, error) {
	slog.Info("Starting streaming handshake with", "remote_agent_i_d", remoteAgentID)
	// Create stream
	stream, err := c.client.PerformHandshake(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create stream: %w", err)
	}

	// Create session
	remoteAgent := &OCXInstance{InstanceID: remoteAgentID}
	session := NewHandshakeSession(c.localAgent, remoteAgent, c.ledger)

	// Step 1: Send HELLO
	hello, err := session.SendHello(ctx)
	if err != nil {
		return nil, err
	}

	if err := stream.Send(&pb.HandshakeMessage{
		Message: &pb.HandshakeMessage_Hello{Hello: hello},
	}); err != nil {
		return nil, err
	}

	// Step 2: Receive CHALLENGE
	msg, err := stream.Recv()
	if err != nil {
		return nil, err
	}
	challengeMsg, ok := msg.Message.(*pb.HandshakeMessage_Challenge)
	if !ok {
		return nil, fmt.Errorf("expected CHALLENGE, got %T", msg.Message)
	}

	if err := session.ReceiveChallenge(ctx, challengeMsg.Challenge); err != nil {
		return nil, err
	}

	// Step 3: Send PROOF
	proof, err := session.GenerateProof(ctx, agentID)
	if err != nil {
		return nil, err
	}

	if err := stream.Send(&pb.HandshakeMessage{
		Message: &pb.HandshakeMessage_Proof{Proof: proof},
	}); err != nil {
		return nil, err
	}

	// Step 4: Receive VERIFY
	msg, err = stream.Recv()
	if err != nil {
		return nil, err
	}
	verifyMsg, ok := msg.Message.(*pb.HandshakeMessage_Verify)
	if !ok {
		return nil, fmt.Errorf("expected VERIFY, got %T", msg.Message)
	}

	// Step 5: Send ATTESTATION
	attestation, err := session.ExchangeAttestation(ctx, verifyMsg.Verify)
	if err != nil {
		return nil, err
	}

	if err := stream.Send(&pb.HandshakeMessage{
		Message: &pb.HandshakeMessage_Attestation{Attestation: attestation},
	}); err != nil {
		return nil, err
	}

	// Step 6: Receive RESULT
	msg, err = stream.Recv()
	if err != nil {
		return nil, err
	}
	resultMsg, ok := msg.Message.(*pb.HandshakeMessage_Result)
	if !ok {
		return nil, fmt.Errorf("expected RESULT, got %T", msg.Message)
	}

	slog.Info("Streaming handshake complete", "verdict", resultMsg.Result.Verdict)
	return resultMsg.Result, nil
}

// GetSessionStatus queries the status of a handshake session
func (c *HandshakeClient) GetSessionStatus(ctx context.Context, sessionID string) (*pb.HandshakeState, error) {
	req := &pb.HandshakeStatusRequest{
		SessionId: sessionID,
	}

	state, err := c.client.GetHandshakeStatus(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to get session status: %w", err)
	}

	return state, nil
}

// ============================================================================
// CONVENIENCE FUNCTIONS
// ============================================================================

// QuickHandshake performs a handshake with default settings
func QuickHandshake(localAgent *OCXInstance, remoteAddr string, remoteAgentID string) (*pb.HandshakeResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	client, err := NewHandshakeClient(remoteAddr, localAgent, nil)
	if err != nil {
		return nil, err
	}
	defer client.Close()

	return client.PerformFullHandshake(ctx, remoteAgentID, "default-agent")
}
