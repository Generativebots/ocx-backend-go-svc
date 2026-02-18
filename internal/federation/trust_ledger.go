// Package federation — Persistent Trust Ledger (Gap 6 Fix: §5.2)
//
// Persistent trust score exchange and storage between OCX instances.
// Replaces the mock TrustAttestationLedger stub with real trust history
// tracking per remote OCX instance.
package federation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/ocx/backend/internal/governance"
)

// PersistentTrustLedger stores and retrieves trust scores for cross-OCX
// handshakes. Implements §5.2: "Trust score exchange during handshakes"
// with persistent trust history between OCX instances.
type PersistentTrustLedger struct {
	mu sync.RWMutex

	// Per-instance trust records (remoteInstanceID → history)
	instanceTrust map[string]*InstanceTrustRecord

	// Global trust attestation log
	attestationLog []TrustAttestationEvent

	// Decay parameters
	decayHalfLifeHours float64 // Trust score decays over time
	minTrustFloor      float64 // Minimum trust floor
}

// InstanceTrustRecord holds the persistent trust data for a remote OCX instance.
type InstanceTrustRecord struct {
	RemoteInstanceID string           `json:"remote_instance_id"`
	RemoteDomain     string           `json:"remote_domain"`
	Organization     string           `json:"organization"`
	CurrentTrust     float64          `json:"current_trust"`
	HighWaterMark    float64          `json:"high_water_mark"`
	LowWaterMark     float64          `json:"low_water_mark"`
	HandshakeCount   int              `json:"handshake_count"`
	SuccessCount     int              `json:"success_count"`
	FailureCount     int              `json:"failure_count"`
	LastHandshakeAt  time.Time        `json:"last_handshake_at"`
	FirstSeenAt      time.Time        `json:"first_seen_at"`
	TrustHistory     []TrustDataPoint `json:"trust_history"`
}

// TrustDataPoint is a single trust score observation.
type TrustDataPoint struct {
	Score     float64   `json:"score"`
	Source    string    `json:"source"` // "handshake", "attestation", "decay", "penalty"
	Timestamp time.Time `json:"timestamp"`
}

// TrustAttestationEvent is an immutable record of a trust exchange.
type TrustAttestationEvent struct {
	EventID          string    `json:"event_id"`
	LocalInstanceID  string    `json:"local_instance_id"`
	RemoteInstanceID string    `json:"remote_instance_id"`
	AgentID          string    `json:"agent_id"`
	LocalTrust       float64   `json:"local_trust"`
	RemoteTrust      float64   `json:"remote_trust"`
	AgreedTrust      float64   `json:"agreed_trust"`
	AttestationHash  string    `json:"attestation_hash"`
	Outcome          string    `json:"outcome"` // "success", "rejected", "timeout"
	Timestamp        time.Time `json:"timestamp"`
}

// NewPersistentTrustLedger creates a new persistent trust ledger.
func NewPersistentTrustLedger() *PersistentTrustLedger {
	return &PersistentTrustLedger{
		instanceTrust:      make(map[string]*InstanceTrustRecord),
		attestationLog:     make([]TrustAttestationEvent, 0),
		decayHalfLifeHours: 168, // 1 week half-life
		minTrustFloor:      0.1,
	}
}

// SetGovernanceConfig loads trust decay parameters from the tenant governance config.
func (ptl *PersistentTrustLedger) SetGovernanceConfig(cache *governance.GovernanceConfigCache, tenantID string) {
	if cache == nil {
		return
	}
	cfg := cache.GetConfig(tenantID)
	ptl.decayHalfLifeHours = cfg.DecayHalfLifeHours
	ptl.minTrustFloor = cfg.HandshakeMinTrust
	slog.Info("Trust ledger configured from tenant governance",
		"tenant_id", tenantID,
		"decay_hours", ptl.decayHalfLifeHours,
		"min_trust", ptl.minTrustFloor)
}

// RecordHandshake records the outcome of a cross-OCX handshake and updates trust.
func (ptl *PersistentTrustLedger) RecordHandshake(
	ctx context.Context,
	localInstanceID, remoteInstanceID, remoteDomain, org, agentID string,
	localTrust, remoteTrust float64,
	success bool,
) (*TrustAttestationEvent, error) {
	ptl.mu.Lock()
	defer ptl.mu.Unlock()

	// Get or create instance record
	record, exists := ptl.instanceTrust[remoteInstanceID]
	if !exists {
		record = &InstanceTrustRecord{
			RemoteInstanceID: remoteInstanceID,
			RemoteDomain:     remoteDomain,
			Organization:     org,
			CurrentTrust:     0.5, // Start at neutral
			HighWaterMark:    0.5,
			LowWaterMark:     0.5,
			FirstSeenAt:      time.Now(),
			TrustHistory:     make([]TrustDataPoint, 0),
		}
		ptl.instanceTrust[remoteInstanceID] = record
	}

	// Apply time-decay to current trust before updating
	record.CurrentTrust = ptl.applyDecay(record.CurrentTrust, record.LastHandshakeAt)

	// Update counters
	record.HandshakeCount++
	record.LastHandshakeAt = time.Now()

	// Calculate agreed trust using minimum-of-claims principle
	// (conservative: trust cannot be higher than either party claims)
	agreedTrust := math.Min(localTrust, remoteTrust)

	outcome := "success"
	if success {
		record.SuccessCount++
		// Blend current trust with new observation (EMA with α=0.3)
		alpha := 0.3
		record.CurrentTrust = (1-alpha)*record.CurrentTrust + alpha*agreedTrust
	} else {
		record.FailureCount++
		outcome = "rejected"
		// Apply penalty for failed handshakes
		record.CurrentTrust *= 0.8 // 20% penalty
	}

	// Clamp to floor
	if record.CurrentTrust < ptl.minTrustFloor {
		record.CurrentTrust = ptl.minTrustFloor
	}

	// Update watermarks
	if record.CurrentTrust > record.HighWaterMark {
		record.HighWaterMark = record.CurrentTrust
	}
	if record.CurrentTrust < record.LowWaterMark {
		record.LowWaterMark = record.CurrentTrust
	}

	// Append to history (keep last 100 points)
	record.TrustHistory = append(record.TrustHistory, TrustDataPoint{
		Score:     record.CurrentTrust,
		Source:    "handshake",
		Timestamp: time.Now(),
	})
	if len(record.TrustHistory) > 100 {
		record.TrustHistory = record.TrustHistory[len(record.TrustHistory)-100:]
	}

	// Create attestation event
	attestHash := computeAttestationHash(localInstanceID, remoteInstanceID, agentID, agreedTrust)
	event := TrustAttestationEvent{
		EventID:          fmt.Sprintf("att-%d", time.Now().UnixNano()),
		LocalInstanceID:  localInstanceID,
		RemoteInstanceID: remoteInstanceID,
		AgentID:          agentID,
		LocalTrust:       localTrust,
		RemoteTrust:      remoteTrust,
		AgreedTrust:      agreedTrust,
		AttestationHash:  attestHash,
		Outcome:          outcome,
		Timestamp:        time.Now(),
	}

	// Append to attestation log (keep last 5000)
	ptl.attestationLog = append(ptl.attestationLog, event)
	if len(ptl.attestationLog) > 5000 {
		ptl.attestationLog = ptl.attestationLog[len(ptl.attestationLog)-5000:]
	}

	slog.Info("Trust ledger: trust= (handshake #, outcome=)", "remote_instance_i_d", remoteInstanceID, "current_trust", record.CurrentTrust, "handshake_count", record.HandshakeCount, "outcome", outcome)
	return &event, nil
}

// GetInstanceTrust returns the current trust score for a remote instance.
func (ptl *PersistentTrustLedger) GetInstanceTrust(remoteInstanceID string) float64 {
	ptl.mu.RLock()
	defer ptl.mu.RUnlock()

	record, exists := ptl.instanceTrust[remoteInstanceID]
	if !exists {
		return 0.5 // Unknown instance → neutral
	}

	// Apply decay before returning
	return ptl.applyDecay(record.CurrentTrust, record.LastHandshakeAt)
}

// GetInstanceRecord returns the full trust record for a remote instance.
func (ptl *PersistentTrustLedger) GetInstanceRecord(remoteInstanceID string) *InstanceTrustRecord {
	ptl.mu.RLock()
	defer ptl.mu.RUnlock()

	record, exists := ptl.instanceTrust[remoteInstanceID]
	if !exists {
		return nil
	}

	// Return a copy
	recordCopy := *record
	recordCopy.TrustHistory = make([]TrustDataPoint, len(record.TrustHistory))
	copy(recordCopy.TrustHistory, record.TrustHistory)
	return &recordCopy
}

// ListTrustedInstances returns all known instances with trust ≥ threshold.
func (ptl *PersistentTrustLedger) ListTrustedInstances(minTrust float64) []*InstanceTrustRecord {
	ptl.mu.RLock()
	defer ptl.mu.RUnlock()

	results := make([]*InstanceTrustRecord, 0)
	for _, record := range ptl.instanceTrust {
		decayedTrust := ptl.applyDecay(record.CurrentTrust, record.LastHandshakeAt)
		if decayedTrust >= minTrust {
			recordCopy := *record
			recordCopy.CurrentTrust = decayedTrust
			results = append(results, &recordCopy)
		}
	}
	return results
}

// GetAttestationLog returns recent attestation events.
func (ptl *PersistentTrustLedger) GetAttestationLog(limit int) []TrustAttestationEvent {
	ptl.mu.RLock()
	defer ptl.mu.RUnlock()

	if limit <= 0 || limit > len(ptl.attestationLog) {
		limit = len(ptl.attestationLog)
	}

	start := len(ptl.attestationLog) - limit
	out := make([]TrustAttestationEvent, limit)
	copy(out, ptl.attestationLog[start:])
	return out
}

// --- Internal helpers ---

// applyDecay computes trust decay since last handshake.
// Trust decays exponentially toward the floor.
func (ptl *PersistentTrustLedger) applyDecay(currentTrust float64, lastUpdate time.Time) float64 {
	if lastUpdate.IsZero() {
		return currentTrust
	}

	elapsed := time.Since(lastUpdate).Hours()
	if elapsed <= 0 {
		return currentTrust
	}

	// Exponential decay: trust(t) = floor + (current - floor) * 2^(-t/halflife)
	decay := math.Pow(2, -elapsed/ptl.decayHalfLifeHours)
	decayed := ptl.minTrustFloor + (currentTrust-ptl.minTrustFloor)*decay

	return math.Max(decayed, ptl.minTrustFloor)
}

func computeAttestationHash(local, remote, agent string, trust float64) string {
	content := fmt.Sprintf("%s|%s|%s|%.6f|%d", local, remote, agent, trust, time.Now().UnixNano())
	hash := sha256.Sum256([]byte(content))
	return hex.EncodeToString(hash[:])
}
