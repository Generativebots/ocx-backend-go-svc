package reputation

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// ============================================================================
// REPUTATION SYSTEM - Powers the weighted trust calculation
// ============================================================================

// ReputationManager manages reputation scores for agents with multi-tenant support
type ReputationManager struct {
	mu sync.RWMutex

	// Agent reputations: "tenantID:agentID" -> reputation
	reputations map[string]*AgentReputation

	// Interaction history: "tenantID:interactionID" -> interaction
	interactions map[string]*InteractionRecord

	// Audit scores: "tenantID:agentID" -> audit score
	auditScores map[string]*AuditScore

	// Attestation freshness: "tenantID:agentID" -> attestation
	attestations map[string]*AttestationRecord
}

// AgentReputation represents the reputation of an agent
// (AgentReputation struct moved to interfaces.go)

// InteractionRecord tracks a single interaction
type InteractionRecord struct {
	InteractionID string
	TenantID      string // Multi-tenant isolation
	Agent1ID      string
	Agent2ID      string
	Timestamp     time.Time
	Success       bool
	TrustLevel    float64
	ValueCreated  float64
}

// AuditScore represents the audit verification score
type AuditScore struct {
	AgentID    string
	AuditHash  string
	Verified   bool
	VerifiedAt time.Time
	Score      float64 // 0.0 - 1.0
}

// AttestationRecord tracks attestation freshness
type AttestationRecord struct {
	AgentID       string
	AttestationID string
	CreatedAt     time.Time
	ExpiresAt     time.Time
	TrustLevel    float64
}

// NewReputationManager creates a new reputation manager
func NewReputationManager() *ReputationManager {
	return &ReputationManager{
		reputations:  make(map[string]*AgentReputation),
		interactions: make(map[string]*InteractionRecord),
		auditScores:  make(map[string]*AuditScore),
		attestations: make(map[string]*AttestationRecord),
	}
}

// RecordInteraction records an interaction and updates reputation (tenant-scoped)
func (rm *ReputationManager) RecordInteraction(ctx context.Context, record *InteractionRecord) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	if record.TenantID == "" {
		return fmt.Errorf("tenantID is required")
	}

	// Store interaction with tenant-scoped key
	key := fmt.Sprintf("%s:%s", record.TenantID, record.InteractionID)
	rm.interactions[key] = record

	// Update reputations for both agents within this tenant
	rm.updateReputation(record.TenantID, record.Agent1ID, record.Success)
	rm.updateReputation(record.TenantID, record.Agent2ID, record.Success)

	return nil
}

// updateReputation updates an agent's reputation based on interaction (tenant-scoped)
func (rm *ReputationManager) updateReputation(tenantID, agentID string, success bool) {
	key := fmt.Sprintf("%s:%s", tenantID, agentID)
	rep, exists := rm.reputations[key]
	if !exists {
		rep = &AgentReputation{
			AgentID:     agentID,
			FirstSeen:   time.Now(),
			LastUpdated: time.Now(),
		}
		rm.reputations[key] = rep
	}

	rep.TotalInteractions++
	if success {
		rep.SuccessfulInteractions++
	} else {
		rep.FailedInteractions++
	}

	// Calculate reputation score
	// Formula: (successful / total) with decay for old interactions
	successRate := float64(rep.SuccessfulInteractions) / float64(rep.TotalInteractions)

	// Apply time decay (older reputations decay slightly)
	age := time.Since(rep.FirstSeen)
	decayFactor := 1.0
	if age > 365*24*time.Hour { // > 1 year
		decayFactor = 0.95
	} else if age > 90*24*time.Hour { // > 3 months
		decayFactor = 0.98
	}

	rep.ReputationScore = successRate * decayFactor
	rep.LastUpdated = time.Now()
}

// GetReputationScore returns the reputation score for an agent in a specific tenant
func (rm *ReputationManager) GetReputationScore(tenantID, agentID string) float64 {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	key := fmt.Sprintf("%s:%s", tenantID, agentID)
	rep, exists := rm.reputations[key]
	if !exists {
		return 0.5 // Default neutral reputation
	}

	if rep.Blacklisted {
		return 0.0
	}

	return rep.ReputationScore
}

// RecordAuditScore records an audit verification score (tenant-scoped)
func (rm *ReputationManager) RecordAuditScore(ctx context.Context, tenantID string, score *AuditScore) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	if tenantID == "" {
		return fmt.Errorf("tenantID is required")
	}

	key := fmt.Sprintf("%s:%s", tenantID, score.AgentID)
	rm.auditScores[key] = score
	return nil
}

// GetAuditScore returns the audit score for an agent in a specific tenant
func (rm *ReputationManager) GetAuditScore(tenantID, agentID string) float64 {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	key := fmt.Sprintf("%s:%s", tenantID, agentID)
	score, exists := rm.auditScores[key]
	if !exists {
		return 0.0
	}

	if !score.Verified {
		return 0.0
	}

	return score.Score
}

// RecordAttestation records a trust attestation (tenant-scoped)
func (rm *ReputationManager) RecordAttestation(ctx context.Context, tenantID string, attestation *AttestationRecord) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	if tenantID == "" {
		return fmt.Errorf("tenantID is required")
	}

	key := fmt.Sprintf("%s:%s", tenantID, attestation.AgentID)
	rm.attestations[key] = attestation
	return nil
}

// GetAttestationScore returns the attestation freshness score for an agent in a specific tenant
func (rm *ReputationManager) GetAttestationScore(tenantID, agentID string) float64 {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	key := fmt.Sprintf("%s:%s", tenantID, agentID)
	attestation, exists := rm.attestations[key]
	if !exists {
		return 0.0
	}

	// Check if expired
	if time.Now().After(attestation.ExpiresAt) {
		return 0.0
	}

	// Calculate freshness score based on age
	age := time.Since(attestation.CreatedAt)

	if age < 1*time.Hour {
		return 1.0
	} else if age < 24*time.Hour {
		return 0.8
	} else if age < 7*24*time.Hour {
		return 0.6
	}

	return 0.4
}

// GetHistoryScore returns the relationship history score for an agent in a specific tenant
func (rm *ReputationManager) GetHistoryScore(tenantID, agentID string) float64 {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	key := fmt.Sprintf("%s:%s", tenantID, agentID)
	rep, exists := rm.reputations[key]
	if !exists {
		return 0.0 // First interaction
	}

	// Score based on relationship age and interaction count
	age := time.Since(rep.FirstSeen)
	interactionCount := rep.TotalInteractions

	ageScore := 0.0
	if age > 365*24*time.Hour { // > 1 year
		ageScore = 1.0
	} else if age > 90*24*time.Hour { // > 3 months
		ageScore = 0.8
	} else if age > 30*24*time.Hour { // > 1 month
		ageScore = 0.6
	} else if age > 7*24*time.Hour { // > 1 week
		ageScore = 0.4
	} else {
		ageScore = 0.2
	}

	// Interaction count bonus
	interactionBonus := 0.0
	if interactionCount > 1000 {
		interactionBonus = 0.2
	} else if interactionCount > 100 {
		interactionBonus = 0.1
	} else if interactionCount > 10 {
		interactionBonus = 0.05
	}

	score := ageScore + interactionBonus
	if score > 1.0 {
		score = 1.0
	}

	return score
}

// CalculateWeightedTrust calculates the weighted trust level for an agent in a specific tenant
// This is the CORE FORMULA for trust calculation
func (rm *ReputationManager) CalculateWeightedTrust(tenantID, agentID string) float64 {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	// Get all four components (tenant-scoped)
	auditScore := rm.getAuditScoreUnsafe(tenantID, agentID)
	reputationScore := rm.getReputationScoreUnsafe(tenantID, agentID)
	attestationScore := rm.getAttestationScoreUnsafe(tenantID, agentID)
	historyScore := rm.getHistoryScoreUnsafe(tenantID, agentID)

	// Weighted trust formula
	trustLevel := (0.40 * auditScore) +
		(0.30 * reputationScore) +
		(0.20 * attestationScore) +
		(0.10 * historyScore)

	return trustLevel
}

// Unsafe versions (must be called with lock held) - tenant-scoped
func (rm *ReputationManager) getAuditScoreUnsafe(tenantID, agentID string) float64 {
	key := fmt.Sprintf("%s:%s", tenantID, agentID)
	score, exists := rm.auditScores[key]
	if !exists || !score.Verified {
		return 0.0
	}
	return score.Score
}

func (rm *ReputationManager) getReputationScoreUnsafe(tenantID, agentID string) float64 {
	key := fmt.Sprintf("%s:%s", tenantID, agentID)
	rep, exists := rm.reputations[key]
	if !exists {
		return 0.5
	}
	if rep.Blacklisted {
		return 0.0
	}
	return rep.ReputationScore
}

func (rm *ReputationManager) getAttestationScoreUnsafe(tenantID, agentID string) float64 {
	key := fmt.Sprintf("%s:%s", tenantID, agentID)
	attestation, exists := rm.attestations[key]
	if !exists || time.Now().After(attestation.ExpiresAt) {
		return 0.0
	}

	age := time.Since(attestation.CreatedAt)
	if age < 1*time.Hour {
		return 1.0
	} else if age < 24*time.Hour {
		return 0.8
	} else if age < 7*24*time.Hour {
		return 0.6
	}
	return 0.4
}

func (rm *ReputationManager) getHistoryScoreUnsafe(tenantID, agentID string) float64 {
	key := fmt.Sprintf("%s:%s", tenantID, agentID)
	rep, exists := rm.reputations[key]
	if !exists {
		return 0.0
	}

	age := time.Since(rep.FirstSeen)
	interactionCount := rep.TotalInteractions

	ageScore := 0.0
	if age > 365*24*time.Hour {
		ageScore = 1.0
	} else if age > 90*24*time.Hour {
		ageScore = 0.8
	} else if age > 30*24*time.Hour {
		ageScore = 0.6
	} else if age > 7*24*time.Hour {
		ageScore = 0.4
	} else {
		ageScore = 0.2
	}

	interactionBonus := 0.0
	if interactionCount > 1000 {
		interactionBonus = 0.2
	} else if interactionCount > 100 {
		interactionBonus = 0.1
	} else if interactionCount > 10 {
		interactionBonus = 0.05
	}

	score := ageScore + interactionBonus
	if score > 1.0 {
		score = 1.0
	}

	return score
}

// BlacklistAgent blacklists an agent in a specific tenant
func (rm *ReputationManager) BlacklistAgent(tenantID, agentID string, reason string) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	if tenantID == "" {
		return fmt.Errorf("tenantID is required")
	}

	key := fmt.Sprintf("%s:%s", tenantID, agentID)
	rep, exists := rm.reputations[key]
	if !exists {
		rep = &AgentReputation{
			AgentID:     agentID,
			FirstSeen:   time.Now(),
			LastUpdated: time.Now(),
		}
		rm.reputations[key] = rep
	}

	rep.Blacklisted = true
	rep.ReputationScore = 0.0
	rep.LastUpdated = time.Now()

	return nil
}

// GetTrustBreakdown returns a detailed breakdown of trust components for an agent in a specific tenant
func (rm *ReputationManager) GetTrustBreakdown(tenantID, agentID string) map[string]float64 {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	auditScore := rm.getAuditScoreUnsafe(tenantID, agentID)
	reputationScore := rm.getReputationScoreUnsafe(tenantID, agentID)
	attestationScore := rm.getAttestationScoreUnsafe(tenantID, agentID)
	historyScore := rm.getHistoryScoreUnsafe(tenantID, agentID)

	trustLevel := (0.40 * auditScore) +
		(0.30 * reputationScore) +
		(0.20 * attestationScore) +
		(0.10 * historyScore)

	return map[string]float64{
		"audit_score":        auditScore,
		"reputation_score":   reputationScore,
		"attestation_score":  attestationScore,
		"history_score":      historyScore,
		"trust_level":        trustLevel,
		"audit_weight":       0.40 * auditScore,
		"reputation_weight":  0.30 * reputationScore,
		"attestation_weight": 0.20 * attestationScore,
		"history_weight":     0.10 * historyScore,
	}
}

// GetAgentReputation returns the full reputation record for an agent in a specific tenant
func (rm *ReputationManager) GetAgentReputation(tenantID, agentID string) (*AgentReputation, error) {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	key := fmt.Sprintf("%s:%s", tenantID, agentID)
	rep, exists := rm.reputations[key]
	if !exists {
		return nil, fmt.Errorf("reputation not found for agent %s in tenant %s", agentID, tenantID)
	}

	// Return a copy
	repCopy := *rep
	return &repCopy, nil
}
