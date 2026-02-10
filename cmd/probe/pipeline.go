package main

import (
	"log/slog"
	"github.com/ocx/backend/internal/plan"

	socketio "github.com/googollee/go-socket.io"
)

// GovernanceNode orchestrates the Async Pipeline
type GovernanceNode struct {
	PlanManager *plan.PlanManager
	Synapse     *socketio.Server
	// Sandbox interface mock for now
	Sandbox SandboxExecutor
}

type SandboxExecutor interface {
	Execute(payload []byte) ([]byte, error)
}

type MockSandbox struct{}

func (m *MockSandbox) Execute(payload []byte) ([]byte, error) {
	return payload, nil // Echo for demo
}

// ProcessAgentTurn handles the async logic
func (gn *GovernanceNode) ProcessAgentTurn(agentID string, payload []byte) {
	go func() {
		// 1. Execute Ghost-Turn (Simulation) in a non-blocking goroutine
		shadowData, err := gn.Sandbox.Execute(payload)
		if err != nil {
			slog.Warn("Sandbox Failure for", "agent_i_d", agentID, "error", err)
			return
		}

		// 2. Perform the Plan Match
		result := gn.PlanManager.MatchSpeculation(agentID, shadowData)

		// 3. Handle Boolean Switch Logic
		plan := gn.PlanManager.GetPlan(agentID)

		if plan == nil {
			slog.Info("No plan for", "agent_i_d", agentID)
			return
		}

		if plan.ManualReviewRequired {
			// Send results to Socket.IO for human intervention
			// gn.Synapse.BroadcastToNamespace("/", "awaiting_manual_approval", result)
			slog.Info("MANUAL REVIEW: Agent waiting...", "agent_i_d", agentID)
		} else {
			// AUTO-COMMIT: If aligned, push to production kernel
			if result.IsAligned {
				// gn.releaseToKernel(agentID)
				slog.Info("AUTO-COMMIT: Agent aligned.", "agent_i_d", agentID)
			} else {
				// gn.blockAgent(agentID, result.Diff)
				slog.Info("BLOCKED: Agent mismatch", "agent_i_d", agentID, "diff", result.Diff)
			}
		}
	}()
}
