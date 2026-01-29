package governance

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"

	"github.com/ocx/backend/pb"
)

type GovernanceCache struct {
	mu    sync.RWMutex
	Store map[string]float64 // Map of [Action_Hash + Agent_ID] to Trust_Score
}

func NewGovernanceCache() *GovernanceCache {
	return &GovernanceCache{
		Store: make(map[string]float64),
	}
}

func (c *GovernanceCache) Check(turn *pb.NegotiationTurn) (float64, bool) {
	// Generate a hash of the 'Identity' + 'Payload Intent'
	// This ignores variable data (like timestamps) and focuses on the LOGIC PATH
	fingerprint := GenerateIntentFingerprint(turn)

	c.mu.RLock()
	score, exists := c.Store[fingerprint]
	c.mu.RUnlock()

	return score, exists
}

func (c *GovernanceCache) Add(turn *pb.NegotiationTurn, score float64) {
	fingerprint := GenerateIntentFingerprint(turn)
	c.mu.Lock()
	c.Store[fingerprint] = score
	c.mu.Unlock()
}

func GenerateIntentFingerprint(turn *pb.NegotiationTurn) string {
	// Determine Identity + Payload
	// Production: Normalize data by stripping timestamps/nonces to ensure idempotency
	data := fmt.Sprintf("%s:%s", turn.AgentId, turn.Payload)
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])
}
