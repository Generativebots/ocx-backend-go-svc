package arbitrator

import (
	"context"
	"fmt"
	"github.com/ocx/backend/internal/governance"
	"github.com/ocx/backend/pb"
	"time"
)

// Mocking ToolProvider for this component
type ToolProvider interface {
	ShadowExecute(turn *pb.NegotiationTurn) (governance.RevertFunc, error)
	Commit(turnID string)
}

// Extended ArbitratorServer methods
// Note: This relies on the ArbitratorServer struct defined in stream_handler.go (which has Verify/Forward/etc)
// We are extending it notionally here. In a real Go package, this would be part of the same struct or a composed service.

func (s *ArbitratorServer) BroadcastKillSwitch(agentId string, reason string) {
	fmt.Printf("[KILL-SWITCH] Agent: %s, Reason: %s\n", agentId, reason)
}

type MockToolProvider struct{}

func (tp *MockToolProvider) ShadowExecute(turn *pb.NegotiationTurn) (governance.RevertFunc, error) {
	fmt.Printf("[SHADOW-EXEC] Execution started for %s\n", turn.AgentId)
	return func(ctx context.Context) error {
		fmt.Printf("[REVERT] Rolling back action for %s\n", turn.AgentId)
		return nil
	}, nil
}
func (tp *MockToolProvider) Commit(turnID string) {
	fmt.Printf("[COMMIT] Action %s finalized.\n", turnID)
}

func (s *ArbitratorServer) SpeculativeExecute(ctx context.Context, turn *pb.NegotiationTurn, toolProvider ToolProvider) {
	// 1. Dispatch background Audit (Expensive Brain Audit)
	auditChan := make(chan bool)
	go func() {
		// Evaluate logic (mocking sync call to Jury)
		verdict := s.Jury.Audit(turn)
		// Check "Action" or "Score". Mock Jury returns Verdict struct.
		// Assuming ALLOW means Score >= 0.80 for this logic
		success := (verdict.Action == "ALLOW")
		auditChan <- success
	}()

	// 2. Perform Shadow Execution (Instant Nervous System Action)
	// We send the 'Success' signal to the agent so it can keep thinking
	revertFunc, err := toolProvider.ShadowExecute(turn)
	if err != nil {
		s.BroadcastKillSwitch(turn.AgentId, "Shadow Execution Failed")
		return
	}

	// 3. The Monitor Loop
	go func() {
		select {
		case approved := <-auditChan:
			if !approved {
				// THE REVERT: The Jury found a violation 300ms later.
				// We physically undo the tool action.
				revertFunc(ctx)
				s.BroadcastKillSwitch(turn.AgentId, "Speculative Failure: SOP Violation")
			} else {
				// THE COMMIT: Logic is verified.
				// Assuming NegotiationTurn has an ID field (mocked in pb)
				toolProvider.Commit("turn-id-mock")
			}
		case <-time.After(1 * time.Second):
			// Safety timeout
			revertFunc(ctx)
			s.BroadcastKillSwitch(turn.AgentId, "Speculative Timeout")
		}
	}()
}
