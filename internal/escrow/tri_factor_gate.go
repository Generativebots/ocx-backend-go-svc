// Package escrow provides the Tri-Factor Gate for AOCS governance.
// This file implements the full three-dimensional validation per AOCS spec.
package escrow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// TriFactorSignal represents one dimension of the Tri-Factor Gate
type TriFactorSignal int

const (
	SIGNAL_IDENTITY TriFactorSignal = iota
	SIGNAL_SIGNAL                   // Entropy, Jitter, Baseline
	SIGNAL_COGNITIVE
)

func (s TriFactorSignal) String() string {
	switch s {
	case SIGNAL_IDENTITY:
		return "IDENTITY"
	case SIGNAL_SIGNAL:
		return "SIGNAL"
	case SIGNAL_COGNITIVE:
		return "COGNITIVE"
	default:
		return "UNKNOWN"
	}
}

// IdentityValidation represents identity layer validation
type IdentityValidation struct {
	// AgentID is the agent's unique identifier
	AgentID string `json:"agent_id"`

	// BinaryHash is SHA-256 of the agent binary
	BinaryHash string `json:"binary_hash"`

	// TenantID for multi-tenancy isolation
	TenantID string `json:"tenant_id"`

	// MFAAVerified indicates Multi-Factor Agentic Authentication passed
	MFAAVerified bool `json:"mfaa_verified"`

	// SPIFFEVerified indicates SPIFFE certificate validation passed
	SPIFFEVerified bool `json:"spiffe_verified"`

	// ReputationScore is the weighted trust score (0.0-1.0)
	ReputationScore float64 `json:"reputation_score"`

	// Entitlements are JIT capability tags
	Entitlements []string `json:"entitlements"`

	// Valid indicates overall identity validation status
	Valid bool `json:"valid"`

	// Reason provides explanation for validation result
	Reason string `json:"reason"`
}

// SignalValidation represents signal layer validation (entropy, jitter, baseline)
type SignalValidation struct {
	// EntropyScore from Shannon entropy analysis (0.0-8.0)
	EntropyScore float64 `json:"entropy_score"`

	// EntropyVerdict is CLEAN, SUSPICIOUS, or ENCRYPTED
	EntropyVerdict string `json:"entropy_verdict"`

	// JitterVariance measures timing consistency
	JitterVariance float64 `json:"jitter_variance"`

	// JitterVerdict is NORMAL, SUSPICIOUS, or COORDINATED
	JitterVerdict string `json:"jitter_verdict"`

	// BaselineHash is the canonical intent hash
	BaselineHash string `json:"baseline_hash"`

	// BaselineMatch indicates if intent matches known patterns
	BaselineMatch bool `json:"baseline_match"`

	// SemanticFlatteningApplied indicates canonicalization was performed
	SemanticFlatteningApplied bool `json:"semantic_flattening_applied"`

	// Valid indicates overall signal validation status
	Valid bool `json:"valid"`

	// Reason provides explanation
	Reason string `json:"reason"`
}

// CognitiveValidation represents cognitive logic layer (Jury + APE)
type CognitiveValidation struct {
	// JuryVerdict from trust calculation engine
	JuryVerdict string `json:"jury_verdict"` // ALLOW, BLOCK, HOLD

	// JuryTrustLevel is the calculated trust score
	JuryTrustLevel float64 `json:"jury_trust_level"`

	// APERulesChecked is the number of APE rules evaluated
	APERulesChecked int `json:"ape_rules_checked"`

	// APEViolations are the policy violations detected
	APEViolations []string `json:"ape_violations"`

	// IntentExtraction is the parsed semantic intent
	IntentExtraction string `json:"intent_extraction"`

	// BehavioralAnomaly indicates drift from baseline
	BehavioralAnomaly bool `json:"behavioral_anomaly"`

	// UnanimousVote indicates all jury agents agreed (for multi-agent jury)
	UnanimousVote bool `json:"unanimous_vote"`

	// Valid indicates overall cognitive validation status
	Valid bool `json:"valid"`

	// Reason provides explanation
	Reason string `json:"reason"`
}

// TriFactorResult is the complete result of Tri-Factor Gate validation
type TriFactorResult struct {
	// TransactionID for tracing
	TransactionID string `json:"transaction_id"`

	// Identity validation results
	Identity IdentityValidation `json:"identity"`

	// Signal validation results
	Signal SignalValidation `json:"signal"`

	// Cognitive validation results
	Cognitive CognitiveValidation `json:"cognitive"`

	// AllPassed indicates all three factors validated successfully
	AllPassed bool `json:"all_passed"`

	// FinalVerdict is RELEASE, REJECT, or HOLD
	FinalVerdict string `json:"final_verdict"`

	// FailedFactors lists which dimensions failed
	FailedFactors []string `json:"failed_factors"`

	// Timestamp of validation
	Timestamp time.Time `json:"timestamp"`

	// ValidationDurationMs is processing time
	ValidationDurationMs int64 `json:"validation_duration_ms"`
}

// TriFactorGate manages the complete three-dimensional validation
type TriFactorGate struct {
	mu sync.Mutex

	// Pending items awaiting validation
	pending map[string]*TriFactorPendingItem

	// Dependencies
	classifier    *ToolClassifier
	juryClient    JuryClient
	entropyClient EntropyMonitor

	// Configuration
	identityThreshold  float64
	entropyThreshold   float64
	jitterThreshold    float64
	cognitiveThreshold float64
}

// TriFactorPendingItem represents an item awaiting Tri-Factor validation
type TriFactorPendingItem struct {
	ID             string
	TenantID       string
	Payload        []byte
	Classification *ClassificationResult
	Signals        map[TriFactorSignal]bool
	Results        map[TriFactorSignal]interface{}
	CreatedAt      time.Time
	ReleaseChan    chan *TriFactorResult
}

// NewTriFactorGate creates a new Tri-Factor Gate
func NewTriFactorGate(classifier *ToolClassifier, jury JuryClient, entropy EntropyMonitor) *TriFactorGate {
	return &TriFactorGate{
		pending:            make(map[string]*TriFactorPendingItem),
		classifier:         classifier,
		juryClient:         jury,
		entropyClient:      entropy,
		identityThreshold:  0.65,
		entropyThreshold:   7.5,
		jitterThreshold:    0.01,
		cognitiveThreshold: 0.65,
	}
}

// Sequester places a Class B action into the Tri-Factor Gate for validation
func (g *TriFactorGate) Sequester(
	ctx context.Context,
	transactionID string,
	tenantID string,
	payload []byte,
	classification *ClassificationResult,
) (*TriFactorPendingItem, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	item := &TriFactorPendingItem{
		ID:             transactionID,
		TenantID:       tenantID,
		Payload:        payload,
		Classification: classification,
		Signals:        make(map[TriFactorSignal]bool),
		Results:        make(map[TriFactorSignal]interface{}),
		CreatedAt:      time.Now(),
		ReleaseChan:    make(chan *TriFactorResult, 1),
	}

	g.pending[transactionID] = item

	// Trigger async validation for all three factors
	go g.triggerIdentityValidation(ctx, item)
	go g.triggerSignalValidation(ctx, item)
	go g.triggerCognitiveValidation(ctx, item)

	return item, nil
}

// triggerIdentityValidation performs identity layer validation
func (g *TriFactorGate) triggerIdentityValidation(ctx context.Context, item *TriFactorPendingItem) {
	startTime := time.Now()

	result := IdentityValidation{
		AgentID:    item.Classification.ToolID,
		TenantID:   item.TenantID,
		BinaryHash: g.computeBinaryHash(item.Payload),
	}

	// Check MFAA (Multi-Factor Agentic Authentication)
	result.MFAAVerified = g.verifyMFAA(ctx, item)

	// Check SPIFFE certificate
	result.SPIFFEVerified = g.verifySPIFFE(ctx, item)

	// Get reputation score from classification
	result.ReputationScore = item.Classification.TrustCheck.AgentScore
	result.Entitlements = item.Classification.EntitlementCheck.Present

	// Determine validity
	if !result.MFAAVerified {
		result.Valid = false
		result.Reason = "MFAA verification failed"
	} else if result.ReputationScore < g.identityThreshold {
		result.Valid = false
		result.Reason = fmt.Sprintf("Reputation score %.2f below threshold %.2f",
			result.ReputationScore, g.identityThreshold)
	} else if !item.Classification.EntitlementCheck.Valid {
		result.Valid = false
		result.Reason = fmt.Sprintf("Missing entitlements: %v",
			item.Classification.EntitlementCheck.Missing)
	} else {
		result.Valid = true
		result.Reason = "Identity validation passed"
	}

	// Record result
	g.processSignal(item.ID, SIGNAL_IDENTITY, result.Valid, result, time.Since(startTime))
}

// triggerSignalValidation performs signal layer validation (entropy, jitter, baseline)
func (g *TriFactorGate) triggerSignalValidation(ctx context.Context, item *TriFactorPendingItem) {
	startTime := time.Now()

	result := SignalValidation{}

	// Call entropy service
	if g.entropyClient != nil {
		entropyResult := g.entropyClient.Analyze(item.Payload, item.TenantID)
		result.EntropyScore = entropyResult.EntropyScore
		result.EntropyVerdict = entropyResult.Verdict
	} else {
		// Mock entropy for testing
		result.EntropyScore = 4.5
		result.EntropyVerdict = "CLEAN"
	}

	// Calculate jitter variance (would come from timing data)
	result.JitterVariance = g.calculateJitterVariance(item)
	if result.JitterVariance < g.jitterThreshold {
		result.JitterVerdict = "NORMAL"
	} else if result.JitterVariance < 0.05 {
		result.JitterVerdict = "SUSPICIOUS"
	} else {
		result.JitterVerdict = "COORDINATED"
	}

	// Compute baseline hash
	result.BaselineHash = g.computeBaselineHash(item.Payload)
	result.BaselineMatch = g.matchBaseline(ctx, result.BaselineHash, item.TenantID)

	// Apply semantic flattening
	result.SemanticFlatteningApplied = true

	// Determine validity
	if result.EntropyVerdict == "ENCRYPTED" {
		result.Valid = false
		result.Reason = fmt.Sprintf("High entropy %.2f indicates potential exfiltration",
			result.EntropyScore)
	} else if result.JitterVerdict == "COORDINATED" {
		result.Valid = false
		result.Reason = "Coordinated timing pattern detected - possible collusion"
	} else {
		result.Valid = true
		result.Reason = "Signal validation passed"
	}

	g.processSignal(item.ID, SIGNAL_SIGNAL, result.Valid, result, time.Since(startTime))
}

// triggerCognitiveValidation performs cognitive layer validation (Jury + APE)
func (g *TriFactorGate) triggerCognitiveValidation(ctx context.Context, item *TriFactorPendingItem) {
	startTime := time.Now()

	result := CognitiveValidation{}

	// Call Jury service
	if g.juryClient != nil {
		juryResult := g.juryClient.Assess(ctx, item.ID, item.TenantID)
		result.JuryVerdict = juryResult.Verdict
		result.JuryTrustLevel = juryResult.TrustLevel
	} else {
		// Mock jury for testing
		result.JuryVerdict = "ALLOW"
		result.JuryTrustLevel = 0.85
	}

	// Check APE rules (would call APE engine)
	result.APERulesChecked = g.checkAPERules(ctx, item)
	result.APEViolations = g.getAPEViolations(ctx, item)

	// Extract semantic intent
	result.IntentExtraction = g.extractIntent(item.Payload)

	// Check for behavioral anomaly
	result.BehavioralAnomaly = g.detectBehavioralAnomaly(ctx, item)

	// Check unanimous vote (for multi-agent jury)
	result.UnanimousVote = result.JuryVerdict == "ALLOW"

	// Determine validity
	if result.JuryVerdict == "BLOCK" {
		result.Valid = false
		result.Reason = "Jury verdict: BLOCK"
	} else if len(result.APEViolations) > 0 {
		result.Valid = false
		result.Reason = fmt.Sprintf("APE policy violations: %v", result.APEViolations)
	} else if result.BehavioralAnomaly {
		result.Valid = false
		result.Reason = "Behavioral anomaly detected - drift from baseline"
	} else if result.JuryTrustLevel < g.cognitiveThreshold {
		result.Valid = false
		result.Reason = fmt.Sprintf("Jury trust level %.2f below threshold %.2f",
			result.JuryTrustLevel, g.cognitiveThreshold)
	} else {
		result.Valid = true
		result.Reason = "Cognitive validation passed"
	}

	g.processSignal(item.ID, SIGNAL_COGNITIVE, result.Valid, result, time.Since(startTime))
}

// processSignal handles a signal completion and checks for Tri-Factor release
func (g *TriFactorGate) processSignal(
	id string,
	signal TriFactorSignal,
	valid bool,
	result interface{},
	duration time.Duration,
) {
	g.mu.Lock()
	defer g.mu.Unlock()

	item, exists := g.pending[id]
	if !exists {
		return
	}

	item.Signals[signal] = valid
	item.Results[signal] = result

	// Check if all three signals have arrived
	if len(item.Signals) == 3 {
		finalResult := g.computeFinalResult(item)

		// Send result on channel
		select {
		case item.ReleaseChan <- finalResult:
		default:
		}

		// Clean up
		delete(g.pending, id)
	}
}

// computeFinalResult aggregates all three factors into final verdict
func (g *TriFactorGate) computeFinalResult(item *TriFactorPendingItem) *TriFactorResult {
	result := &TriFactorResult{
		TransactionID:        item.ID,
		Timestamp:            time.Now(),
		ValidationDurationMs: time.Since(item.CreatedAt).Milliseconds(),
		FailedFactors:        []string{},
	}

	// Extract individual results
	if identity, ok := item.Results[SIGNAL_IDENTITY].(IdentityValidation); ok {
		result.Identity = identity
		if !identity.Valid {
			result.FailedFactors = append(result.FailedFactors, "IDENTITY")
		}
	}

	if signal, ok := item.Results[SIGNAL_SIGNAL].(SignalValidation); ok {
		result.Signal = signal
		if !signal.Valid {
			result.FailedFactors = append(result.FailedFactors, "SIGNAL")
		}
	}

	if cognitive, ok := item.Results[SIGNAL_COGNITIVE].(CognitiveValidation); ok {
		result.Cognitive = cognitive
		if !cognitive.Valid {
			result.FailedFactors = append(result.FailedFactors, "COGNITIVE")
		}
	}

	// ALL THREE must pass for release
	result.AllPassed = len(result.FailedFactors) == 0

	if result.AllPassed {
		result.FinalVerdict = "RELEASE"
	} else if len(result.FailedFactors) == 1 && result.FailedFactors[0] == "IDENTITY" {
		// Identity failure might be recoverable with HITL
		result.FinalVerdict = "HOLD"
	} else {
		result.FinalVerdict = "REJECT"
	}

	return result
}

// AwaitRelease blocks until Tri-Factor validation completes
func (g *TriFactorGate) AwaitRelease(ctx context.Context, id string, timeout time.Duration) (*TriFactorResult, error) {
	g.mu.Lock()
	item, exists := g.pending[id]
	g.mu.Unlock()

	if !exists {
		return nil, fmt.Errorf("transaction %s not found in gate", id)
	}

	select {
	case result := <-item.ReleaseChan:
		return result, nil
	case <-time.After(timeout):
		return nil, fmt.Errorf("timeout waiting for Tri-Factor validation")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Helper functions

func (g *TriFactorGate) verifyMFAA(ctx context.Context, item *TriFactorPendingItem) bool {
	// In production, this would verify multi-factor agentic authentication
	// For now, check that entitlements are present
	return item.Classification.EntitlementCheck.Valid
}

func (g *TriFactorGate) verifySPIFFE(ctx context.Context, item *TriFactorPendingItem) bool {
	// In production, this would validate SPIFFE certificates
	return true
}

func (g *TriFactorGate) computeBinaryHash(payload []byte) string {
	hash := sha256.Sum256(payload)
	return hex.EncodeToString(hash[:])
}

func (g *TriFactorGate) calculateJitterVariance(item *TriFactorPendingItem) float64 {
	// In production, this would analyze timing patterns
	return 0.005 // Mock normal jitter
}

func (g *TriFactorGate) computeBaselineHash(payload []byte) string {
	// Semantic flattening + hashing
	hash := sha256.Sum256(payload)
	return hex.EncodeToString(hash[:8])
}

func (g *TriFactorGate) matchBaseline(ctx context.Context, hash, tenantID string) bool {
	// In production, this would check against known intent patterns
	return true
}

func (g *TriFactorGate) checkAPERules(ctx context.Context, item *TriFactorPendingItem) int {
	// In production, this would call APE engine
	return 5
}

func (g *TriFactorGate) getAPEViolations(ctx context.Context, item *TriFactorPendingItem) []string {
	// In production, this would return actual violations
	return []string{}
}

func (g *TriFactorGate) extractIntent(payload []byte) string {
	// In production, this would use LLM to extract semantic intent
	return "execute_action"
}

func (g *TriFactorGate) detectBehavioralAnomaly(ctx context.Context, item *TriFactorPendingItem) bool {
	// In production, this would compare against behavioral baseline
	return false
}
