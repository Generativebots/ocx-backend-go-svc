// Package escrow — Live Entropy Monitor (Patent §3: Shannon Entropy)
//
// EntropyMonitorLive provides real-time, in-process Shannon entropy analysis.
// It serves as the LOCAL FALLBACK when the Python entropy service (OCX_ENTROPY_URL)
// is unreachable.
//
// Architecture:
//
//   - PRIMARY: Python service (entropy/monitor.py) — full 6-dimension signal
//     validation: Shannon entropy, temporal jitter detection, semantic flattening,
//     baseline hashing, compression ratio analysis, and strategic jitter injection.
//     Called via HTTP POST to OCX_ENTROPY_URL/analyze by EscrowGate.triggerEntropyCheck().
//
//   - FALLBACK: This file (EntropyMonitorLive) — basic Shannon entropy only.
//     Used when the Python service returns an error or is offline.
//     See gate.go lines 190-196 for the fallback path.
package escrow

import (
	"context"
	"fmt"
	"log"
	"math"
	"sync"
	"time"
)

// EntropyMonitorLive monitors real-time handshake intervals
type EntropyMonitorLive struct {
	intervals     map[string][]time.Time // agentID -> handshake timestamps
	mu            sync.RWMutex
	baseThreshold float64
	logger        *log.Logger
	cleanupTicker *time.Ticker
	stopCleanup   chan struct{}
}

// NewEntropyMonitorLive creates a live entropy monitor
func NewEntropyMonitorLive(baseThreshold float64) *EntropyMonitorLive {
	em := &EntropyMonitorLive{
		intervals:     make(map[string][]time.Time),
		baseThreshold: baseThreshold,
		logger:        log.New(log.Writer(), "[EntropyMonitor] ", log.LstdFlags),
		cleanupTicker: time.NewTicker(5 * time.Minute),
		stopCleanup:   make(chan struct{}),
	}

	// Start background cleanup
	go em.cleanupOldIntervals()

	return em
}

// RecordHandshake records a handshake timestamp for an agent
func (em *EntropyMonitorLive) RecordHandshake(agentID string, timestamp time.Time) {
	em.mu.Lock()
	defer em.mu.Unlock()

	if _, exists := em.intervals[agentID]; !exists {
		em.intervals[agentID] = make([]time.Time, 0, 100)
	}

	em.intervals[agentID] = append(em.intervals[agentID], timestamp)

	// Keep only last 100 handshakes
	if len(em.intervals[agentID]) > 100 {
		em.intervals[agentID] = em.intervals[agentID][len(em.intervals[agentID])-100:]
	}
}

// CheckEntropy validates entropy against dynamic threshold
func (em *EntropyMonitorLive) CheckEntropy(ctx context.Context, data []byte, agentID string) (bool, error) {
	em.mu.RLock()
	timestamps, exists := em.intervals[agentID]
	em.mu.RUnlock()

	if !exists || len(timestamps) < 10 {
		// Not enough data - allow but log warning
		em.logger.Printf("⚠️ Insufficient handshake data for agent %s (%d samples)", agentID, len(timestamps))
		return true, nil
	}

	// Calculate intervals between handshakes
	intervals := make([]float64, len(timestamps)-1)
	for i := 1; i < len(timestamps); i++ {
		intervals[i-1] = timestamps[i].Sub(timestamps[i-1]).Seconds()
	}

	// Calculate Shannon Entropy
	entropy := calculateShannonEntropyFromIntervals(intervals)

	// Dynamic threshold based on agent's behavioral drift
	// In production, fetch from Spanner: agent.BehavioralDrift
	threshold := em.baseThreshold // Default: 1.2

	if entropy < threshold {
		em.logger.Printf("🚨 Low entropy detected for agent %s: %.2f < %.2f (possible collusion)", agentID, entropy, threshold)
		return false, fmt.Errorf("entropy too low: %.2f < %.2f", entropy, threshold)
	}

	if entropy > 4.8 {
		em.logger.Printf("🚨 High entropy detected for agent %s: %.2f > 4.8 (possible randomness attack)", agentID, entropy)
		return false, fmt.Errorf("entropy too high: %.2f > 4.8", entropy)
	}

	em.logger.Printf("✅ Entropy check passed for agent %s: %.2f", agentID, entropy)
	return true, nil
}

// calculateShannonEntropyFromIntervals computes entropy from time intervals
func calculateShannonEntropyFromIntervals(intervals []float64) float64 {
	if len(intervals) == 0 {
		return 0.0
	}

	// Discretize intervals into buckets (0-1s, 1-2s, 2-5s, 5-10s, 10+s)
	buckets := map[string]int{
		"0-1s":  0,
		"1-2s":  0,
		"2-5s":  0,
		"5-10s": 0,
		"10+s":  0,
	}

	for _, interval := range intervals {
		switch {
		case interval < 1.0:
			buckets["0-1s"]++
		case interval < 2.0:
			buckets["1-2s"]++
		case interval < 5.0:
			buckets["2-5s"]++
		case interval < 10.0:
			buckets["5-10s"]++
		default:
			buckets["10+s"]++
		}
	}

	// Calculate Shannon Entropy
	var entropy float64
	total := float64(len(intervals))

	for _, count := range buckets {
		if count > 0 {
			p := float64(count) / total
			entropy -= p * math.Log2(p)
		}
	}

	return entropy
}

// cleanupOldIntervals removes stale data
func (em *EntropyMonitorLive) cleanupOldIntervals() {
	for {
		select {
		case <-em.cleanupTicker.C:
			em.mu.Lock()
			cutoff := time.Now().Add(-1 * time.Hour)
			for agentID, timestamps := range em.intervals {
				// Remove timestamps older than 1 hour
				validIdx := 0
				for i, ts := range timestamps {
					if ts.After(cutoff) {
						validIdx = i
						break
					}
				}
				if validIdx > 0 {
					em.intervals[agentID] = timestamps[validIdx:]
				}
			}
			em.mu.Unlock()
		case <-em.stopCleanup:
			return
		}
	}
}

// Close stops the cleanup goroutine
func (em *EntropyMonitorLive) Close() error {
	em.cleanupTicker.Stop()
	close(em.stopCleanup)
	return nil
}

// MeasureEntropy implements the EntropyMonitor interface (calculates payload entropy)
func (em *EntropyMonitorLive) MeasureEntropy(ctx context.Context, payload []byte) (float64, error) {
	if len(payload) == 0 {
		return 0.0, nil
	}

	// Count character frequencies
	charCounts := make(map[byte]int)
	for _, b := range payload {
		charCounts[b]++
	}

	// Calculate Shannon Entropy
	var entropy float64
	totalLen := float64(len(payload))

	for _, count := range charCounts {
		p := float64(count) / totalLen
		entropy -= p * math.Log2(p)
	}

	return entropy, nil
}

// Analyze performs full signal validation for Tri-Factor Gate
// This provides AOCS-compliant entropy analysis with verdict
func (em *EntropyMonitorLive) Analyze(payload []byte, tenantID string) EntropyResult {
	if len(payload) == 0 {
		return EntropyResult{
			EntropyScore: 0.0,
			Verdict:      "CLEAN",
			Confidence:   0.9,
		}
	}

	// Count byte frequencies
	charCounts := make(map[byte]int)
	for _, b := range payload {
		charCounts[b]++
	}

	// Calculate Shannon Entropy (bits per byte, 0-8 range)
	var entropy float64
	totalLen := float64(len(payload))
	for _, count := range charCounts {
		p := float64(count) / totalLen
		entropy -= p * math.Log2(p)
	}

	// Determine verdict based on thresholds
	// English text: ~3.5-4.5, JSON/XML: ~4.5-5.5, Compressed: ~7.0-7.5, Encrypted: ~7.5-8.0
	verdict := "CLEAN"
	confidence := 0.9

	if entropy > 7.5 {
		verdict = "ENCRYPTED"
		em.logger.Printf("🚨 Tenant %s: High entropy %.2f - potential exfiltration", tenantID, entropy)
	} else if entropy > 6.0 {
		verdict = "SUSPICIOUS"
		confidence = 0.7
	}

	em.logger.Printf("✅ Tenant %s: Entropy analysis %.2f -> %s", tenantID, entropy, verdict)

	return EntropyResult{
		EntropyScore: entropy,
		Verdict:      verdict,
		Confidence:   confidence,
	}
}
