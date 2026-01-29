package plan

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// MatchResult defines the outcome of the speculative comparison
type MatchResult struct {
	IsAligned bool
	Diff      string
}

// MatchSpeculation performs the SHA-256 comparison
func (pm *PlanManager) MatchSpeculation(agentID string, shadowOutcome []byte) MatchResult {
	pm.mu.RLock()
	plan, exists := pm.activePlans[agentID]
	pm.mu.RUnlock()

	if !exists {
		return MatchResult{IsAligned: false, Diff: "No plan found for this agent"}
	}

	// 1. Generate hash of the Actual Speculative Outcome
	hasher := sha256.New()
	hasher.Write(shadowOutcome)
	actualHash := hex.EncodeToString(hasher.Sum(nil))

	// 2. Compare against the Plan's Expected Outcome Hash
	if actualHash == plan.ExpectedOutcomeHash {
		return MatchResult{IsAligned: true}
	}

	return MatchResult{
		IsAligned: false,
		Diff:      fmt.Sprintf("Hash Mismatch: Expected %s, Got %s", plan.ExpectedOutcomeHash, actualHash),
	}
}
