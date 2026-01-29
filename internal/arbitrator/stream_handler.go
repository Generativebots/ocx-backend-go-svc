package arbitrator

import (
	"errors"
	"github.com/ocx/backend/pb"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Mocking the Server Struct for IDE resolution
type ArbitratorServer struct {
	Verifier MockVerifier
	Jury     MockJury
}

type MockVerifier struct{}

func (v MockVerifier) Verify(t *pb.NegotiationTurn) error { return nil }

type MockJury struct{}
type Verdict struct{ Action string }

func (j MockJury) Audit(t *pb.NegotiationTurn) Verdict { return Verdict{Action: "ALLOW"} }

func (s *ArbitratorServer) KillSwitch(agentId string)     {}
func (s *ArbitratorServer) Forward(t *pb.NegotiationTurn) {}

// internal/arbitrator/stream_handler.go
// Refined A2A gRPC Handlers for High-Volume Production (Predictive Buffer Pattern)

func (s *ArbitratorServer) Negotiate(stream pb.NegotiationArbitrator_NegotiateServer) error {
	// 1. Parallel verification
	// We verify the identity on the Go CPU while the Python Brain warms up the GPU
	errChan := make(chan error, 1)

	for {
		turn, err := stream.Recv()
		if err != nil {
			return err
		}

		go func(t *pb.NegotiationTurn) {
			// Check Signature
			if err := s.Verifier.Verify(t); err != nil {
				errChan <- err
				return
			}

			// Call Sovereign Jury
			verdict := s.Jury.Audit(t)

			if verdict.Action == "BLOCK" {
				s.KillSwitch(t.AgentId)
				errChan <- errors.New("SOP_VIOLATION")
				return
			}

			// Forward to the counter-party agent
			s.Forward(t)
		}(turn)

		// Non-blocking error check
		select {
		case err := <-errChan:
			return status.Error(codes.PermissionDenied, err.Error())
		default:
			continue
		}
	}
}
