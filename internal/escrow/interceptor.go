package escrow

import (
	"context"
	"fmt"

	"google.golang.org/grpc"
)

// EscrowInterceptor is a gRPC unary server interceptor that enforces the Economic Barrier
func EscrowInterceptor(gate *EscrowGate) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (interface{}, error) {
		// 1. Execute the tool (Speculative Output)
		resp, err := handler(ctx, req)
		if err != nil {
			return nil, err
		}

		// 2. Extract Transaction Metadata from context
		txID := extractTxID(ctx)
		agentID := extractAgentID(ctx)

		// Skip escrow for non-tool calls (e.g., health checks, metadata)
		if shouldSkipEscrow(info.FullMethod) {
			return resp, nil
		}

		// 3. Sequester the response
		payload := serializeResponse(resp)
		gate.Sequester(txID, agentID, payload)

		// 4. Enter Barrier Synchronization Phase
		// This blocks until Tri-Factor validation completes
		// Wait for Jury/Entropy verdict (Standard 2-Argument call)
		_, err = gate.AwaitRelease(ctx, txID)
		if err != nil {
			// Economic Barrier violation - data was shredded
			return nil, fmt.Errorf("economic barrier: %w", err)
		}

		// 5. Return the validated response
		return deserializeResponse(resp), nil
	}
}

// StreamEscrowInterceptor handles streaming RPCs
func StreamEscrowInterceptor(gate *EscrowGate) grpc.StreamServerInterceptor {
	return func(
		srv interface{},
		ss grpc.ServerStream,
		info *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) error {
		// For streaming, we wrap the ServerStream to intercept messages
		wrappedStream := &escrowServerStream{
			ServerStream: ss,
			gate:         gate,
			ctx:          ss.Context(),
		}

		return handler(srv, wrappedStream)
	}
}

// escrowServerStream wraps grpc.ServerStream to intercept messages
type escrowServerStream struct {
	grpc.ServerStream
	gate *EscrowGate
	ctx  context.Context
}

func (s *escrowServerStream) SendMsg(m interface{}) error {
	// Intercept outgoing messages
	txID := extractTxID(s.ctx)
	agentID := extractAgentID(s.ctx)

	payload := serializeResponse(m)
	s.gate.Sequester(txID, agentID, payload)

	// Wait for verdict
	_, err := s.gate.AwaitRelease(s.ctx, txID)
	if err != nil {
		return fmt.Errorf("economic barrier: %w", err)
	}

	validated := deserializeResponse(m)
	return s.ServerStream.SendMsg(validated)
}

// Helper functions

func extractTxID(ctx context.Context) string {
	// Extract from gRPC metadata
	// In production, this would use metadata.FromIncomingContext
	return fmt.Sprintf("tx-%d", ctx.Value("request_id"))
}

func extractAgentID(ctx context.Context) string {
	// Extract from auth token or metadata
	if agentID, ok := ctx.Value("agent_id").(string); ok {
		return agentID
	}
	return "unknown-agent"
}

func shouldSkipEscrow(method string) bool {
	// Skip health checks and metadata endpoints
	skipMethods := []string{
		"/grpc.health.v1.Health/Check",
		"/grpc.reflection.v1alpha.ServerReflection/ServerReflectionInfo",
	}

	for _, skip := range skipMethods {
		if method == skip {
			return true
		}
	}

	return false
}

func serializeResponse(resp interface{}) []byte {
	// In production, use protobuf marshaling
	// For now, simple string conversion
	return []byte(fmt.Sprintf("%v", resp))
}

func deserializeResponse(original interface{}) interface{} {
	// In production, unmarshal back to the original type
	// For now, return the original (since we're just demonstrating)
	return original
}
