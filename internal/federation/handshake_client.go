package federation

import (
	"context"
	"fmt"
	"log"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// ============================================================================
// gRPC CLIENT - Initiates handshake with any remote agent
// ============================================================================

// HandshakeClient wraps the gRPC client for handshake operations
type HandshakeClient struct {
	conn   *grpc.ClientConn
	client HandshakeServiceClient

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
		client:     NewHandshakeServiceClient(conn),
		localAgent: localAgent,
		ledger:     ledger,
	}, nil
}

// Close closes the gRPC connection
func (c *HandshakeClient) Close() error {
	return c.conn.Close()
}

// PerformFullHandshake executes the complete 6-step handshake with a remote agent
func (c *HandshakeClient) PerformFullHandshake(ctx context.Context, remoteAgentID string, agentID string) (*HandshakeResultMessage, error) {
	log.Printf("🚀 Starting full handshake with %s", remoteAgentID)

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

	log.Printf("📤 [1/6] Sending HELLO to %s", remoteAgentID)

	// Call remote agent
	challenge, err := c.client.InitiateHandshake(ctx, hello)
	if err != nil {
		return nil, fmt.Errorf("HELLO failed: %w", err)
	}

	log.Printf("📥 [2/6] Received CHALLENGE")

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

	log.Printf("📤 [3/6] Sending PROOF")

	verify, err := c.client.RespondToChallenge(ctx, proof)
	if err != nil {
		return nil, fmt.Errorf("PROOF failed: %w", err)
	}

	log.Printf("📥 [4/6] Received VERIFY (trust_level=%.2f)", verify.TrustLevel)

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

	log.Printf("📤 [5/6] Sending ATTESTATION")

	result, err := c.client.ExchangeAttestation(ctx, attestation)
	if err != nil {
		return nil, fmt.Errorf("ATTESTATION failed: %w", err)
	}

	log.Printf("📥 [6/6] Received RESULT: %s", result.Verdict)

	// ========================================================================
	// STEP 6: Process RESULT
	// ========================================================================
	if result.Verdict == "ACCEPTED" {
		log.Printf("✅ Handshake ACCEPTED: trust_level=%.2f, session=%s (duration=%dms)",
			result.TrustLevel, result.SessionID, result.DurationMs)
	} else {
		log.Printf("❌ Handshake REJECTED: %s", result.Reason)
	}

	return result, nil
}

// PerformStreamingHandshake uses bidirectional streaming for the handshake
func (c *HandshakeClient) PerformStreamingHandshake(ctx context.Context, remoteAgentID string, agentID string) (*HandshakeResultMessage, error) {
	log.Printf("🔄 Starting streaming handshake with %s", remoteAgentID)

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

	if err := stream.Send(&HandshakeMessageWrapper{
		Message: &HandshakeMessageWrapper_Hello{Hello: hello},
	}); err != nil {
		return nil, err
	}

	// Step 2: Receive CHALLENGE
	msg, err := stream.Recv()
	if err != nil {
		return nil, err
	}
	challengeMsg, ok := msg.Message.(*HandshakeMessageWrapper_Challenge)
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

	if err := stream.Send(&HandshakeMessageWrapper{
		Message: &HandshakeMessageWrapper_Proof{Proof: proof},
	}); err != nil {
		return nil, err
	}

	// Step 4: Receive VERIFY
	msg, err = stream.Recv()
	if err != nil {
		return nil, err
	}
	verifyMsg, ok := msg.Message.(*HandshakeMessageWrapper_Verify)
	if !ok {
		return nil, fmt.Errorf("expected VERIFY, got %T", msg.Message)
	}

	// Step 5: Send ATTESTATION
	attestation, err := session.ExchangeAttestation(ctx, verifyMsg.Verify)
	if err != nil {
		return nil, err
	}

	if err := stream.Send(&HandshakeMessageWrapper{
		Message: &HandshakeMessageWrapper_Attestation{Attestation: attestation},
	}); err != nil {
		return nil, err
	}

	// Step 6: Receive RESULT
	msg, err = stream.Recv()
	if err != nil {
		return nil, err
	}
	resultMsg, ok := msg.Message.(*HandshakeMessageWrapper_Result)
	if !ok {
		return nil, fmt.Errorf("expected RESULT, got %T", msg.Message)
	}

	log.Printf("✅ Streaming handshake complete: %s", resultMsg.Result.Verdict)

	return resultMsg.Result, nil
}

// GetSessionStatus queries the status of a handshake session
func (c *HandshakeClient) GetSessionStatus(ctx context.Context, sessionID string) (*HandshakeStateMessage, error) {
	req := &HandshakeStatusRequest{
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
func QuickHandshake(localAgent *OCXInstance, remoteAddr string, remoteAgentID string) (*HandshakeResultMessage, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	client, err := NewHandshakeClient(remoteAddr, localAgent, nil)
	if err != nil {
		return nil, err
	}
	defer client.Close()

	return client.PerformFullHandshake(ctx, remoteAgentID, "default-agent")
}

// ============================================================================
// PLACEHOLDER TYPES (until protobuf is compiled)
// ============================================================================

type HandshakeServiceClient interface {
	InitiateHandshake(ctx context.Context, in *HandshakeHelloMessage, opts ...grpc.CallOption) (*HandshakeChallengeMessage, error)
	RespondToChallenge(ctx context.Context, in *HandshakeProofMessage, opts ...grpc.CallOption) (*HandshakeVerifyMessage, error)
	ExchangeAttestation(ctx context.Context, in *HandshakeAttestationMessage, opts ...grpc.CallOption) (*HandshakeResultMessage, error)
	PerformHandshake(ctx context.Context, opts ...grpc.CallOption) (HandshakeService_PerformHandshakeClient, error)
	GetHandshakeStatus(ctx context.Context, in *HandshakeStatusRequest, opts ...grpc.CallOption) (*HandshakeStateMessage, error)
}

type HandshakeService_PerformHandshakeClient interface {
	Send(*HandshakeMessageWrapper) error
	Recv() (*HandshakeMessageWrapper, error)
	grpc.ClientStream
}

func NewHandshakeServiceClient(conn *grpc.ClientConn) HandshakeServiceClient {
	// This will be generated by protoc
	return nil
}
