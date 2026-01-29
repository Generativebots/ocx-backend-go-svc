package main

import (
	"log"
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
			log.Printf("Sandbox Failure for %s: %v", agentID, err)
			return
		}

		// 2. Perform the Plan Match
		result := gn.PlanManager.MatchSpeculation(agentID, shadowData)

		// 3. Handle Boolean Switch Logic
		plan := gn.PlanManager.GetPlan(agentID)

		if plan == nil {
			log.Printf("No plan for %s", agentID)
			return
		}

		if plan.ManualReviewRequired {
			// Send results to Socket.IO for human intervention
			// gn.Synapse.BroadcastToNamespace("/", "awaiting_manual_approval", result)
			log.Printf("MANUAL REVIEW: Agent %s waiting...", agentID)
		} else {
			// AUTO-COMMIT: If aligned, push to production kernel
			if result.IsAligned {
				// gn.releaseToKernel(agentID)
				log.Printf("AUTO-COMMIT: Agent %s aligned.", agentID)
			} else {
				// gn.blockAgent(agentID, result.Diff)
				log.Printf("BLOCKED: Agent %s mismatch: %s", agentID, result.Diff)
			}
		}
	}()
}
