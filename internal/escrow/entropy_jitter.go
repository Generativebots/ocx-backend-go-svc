// Package escrow — Temporal Jitter Injection (Patent §3.3)
//
// Implements: "Temporal jitter injection to break steganography."
//
// Adds random micro-delays to outbound response timing, making it
// impossible for agents to encode information in request/response
// cadence (a steganographic timing channel).
package escrow

import (
	"crypto/rand"
	"fmt"
	"log"
	"math/big"
	"sync"
	"time"
)

// TemporalJitterInjector adds random delays to break steganographic
// timing channels in agent handshakes.
type TemporalJitterInjector struct {
	mu sync.RWMutex

	// Configuration
	minJitterMs int64 // Minimum jitter in milliseconds
	maxJitterMs int64 // Maximum jitter in milliseconds

	// Per-agent tracking for jitter-resistance analysis
	agentTimings map[string]*timingProfile
	logger       *log.Logger
}

// timingProfile stores recent timing samples for an agent.
type timingProfile struct {
	Samples      []timingSample
	MaxSamples   int
	SuspectScore float64 // 0.0 (clean) → 1.0 (steganographic)
}

type timingSample struct {
	RequestedAt time.Time
	JitterMs    int64
	RespondedAt time.Time
}

// JitterAnalysis is the result of analyzing an agent's jitter resistance.
type JitterAnalysis struct {
	AgentID        string
	SampleCount    int
	SuspectScore   float64 // 0.0–1.0
	MeanIntervalMs float64
	StdDevMs       float64
	Verdict        string // "CLEAN", "SUSPICIOUS", "STEGANOGRAPHIC"
}

// NewTemporalJitterInjector creates a new jitter injector.
// Default jitter range is 50–500ms.
func NewTemporalJitterInjector(minMs, maxMs int64) *TemporalJitterInjector {
	if minMs <= 0 {
		minMs = 50
	}
	if maxMs <= minMs {
		maxMs = 500
	}
	return &TemporalJitterInjector{
		minJitterMs:  minMs,
		maxJitterMs:  maxMs,
		agentTimings: make(map[string]*timingProfile),
		logger:       log.New(log.Writer(), "[TemporalJitter] ", log.LstdFlags),
	}
}

// InjectJitter adds a cryptographically random delay before responding.
// This breaks any timing-based steganographic channel an agent could use.
// Returns the jitter duration applied.
func (tj *TemporalJitterInjector) InjectJitter(agentID string) time.Duration {
	requestedAt := time.Now()

	// Generate cryptographically random jitter
	rangeMs := tj.maxJitterMs - tj.minJitterMs
	bigN, err := rand.Int(rand.Reader, big.NewInt(rangeMs))
	if err != nil {
		// Fallback to minimum jitter on crypto failure
		tj.logger.Printf("⚠️  Crypto random failed, using min jitter: %v", err)
		bigN = big.NewInt(0)
	}
	jitterMs := tj.minJitterMs + bigN.Int64()
	jitterDuration := time.Duration(jitterMs) * time.Millisecond

	// Apply the delay
	time.Sleep(jitterDuration)

	respondedAt := time.Now()

	// Record for analysis
	tj.recordSample(agentID, requestedAt, jitterMs, respondedAt)

	return jitterDuration
}

// recordSample stores a timing sample for later steganography analysis.
func (tj *TemporalJitterInjector) recordSample(agentID string, requestedAt time.Time, jitterMs int64, respondedAt time.Time) {
	tj.mu.Lock()
	defer tj.mu.Unlock()

	profile, exists := tj.agentTimings[agentID]
	if !exists {
		profile = &timingProfile{
			Samples:    make([]timingSample, 0, 100),
			MaxSamples: 100,
		}
		tj.agentTimings[agentID] = profile
	}

	sample := timingSample{
		RequestedAt: requestedAt,
		JitterMs:    jitterMs,
		RespondedAt: respondedAt,
	}

	profile.Samples = append(profile.Samples, sample)
	if len(profile.Samples) > profile.MaxSamples {
		profile.Samples = profile.Samples[len(profile.Samples)-profile.MaxSamples:]
	}
}

// AnalyzeJitterResistance checks if an agent's request patterns show
// signs of timing-channel injection (steganography).
//
// A legitimate agent will have random-looking inter-request intervals.
// An agent using a timing channel will show suspiciously periodic patterns
// despite the injected jitter.
func (tj *TemporalJitterInjector) AnalyzeJitterResistance(agentID string) (*JitterAnalysis, error) {
	tj.mu.RLock()
	defer tj.mu.RUnlock()

	profile, exists := tj.agentTimings[agentID]
	if !exists {
		return nil, fmt.Errorf("no timing data for agent %s", agentID)
	}

	if len(profile.Samples) < 10 {
		return &JitterAnalysis{
			AgentID:     agentID,
			SampleCount: len(profile.Samples),
			Verdict:     "INSUFFICIENT_DATA",
		}, nil
	}

	// Calculate inter-request intervals (time between consecutive requests)
	intervals := make([]float64, 0, len(profile.Samples)-1)
	for i := 1; i < len(profile.Samples); i++ {
		delta := profile.Samples[i].RequestedAt.Sub(profile.Samples[i-1].RequestedAt)
		intervals = append(intervals, float64(delta.Milliseconds()))
	}

	// Calculate statistics
	meanMs := mean(intervals)
	stdDev := stddev(intervals, meanMs)

	// Coefficient of variation: low CV with many samples = periodic = suspicious
	cv := 0.0
	if meanMs > 0 {
		cv = stdDev / meanMs
	}

	// Calculate suspect score based on coefficient of variation
	// Real users: CV > 0.5 (high variability)
	// Steganographic bots: CV < 0.2 (very regular)
	suspectScore := 0.0
	if cv < 0.15 {
		suspectScore = 0.9 // Very periodic → highly suspect
	} else if cv < 0.25 {
		suspectScore = 0.6 // Somewhat periodic
	} else if cv < 0.4 {
		suspectScore = 0.3 // Slightly periodic
	} else {
		suspectScore = 0.1 // Normal variability
	}

	verdict := "CLEAN"
	if suspectScore > 0.7 {
		verdict = "STEGANOGRAPHIC"
		tj.logger.Printf("🚨 Agent %s shows steganographic timing pattern (CV=%.3f, score=%.2f)",
			agentID, cv, suspectScore)
	} else if suspectScore > 0.4 {
		verdict = "SUSPICIOUS"
		tj.logger.Printf("⚠️  Agent %s has suspicious timing pattern (CV=%.3f, score=%.2f)",
			agentID, cv, suspectScore)
	}

	profile.SuspectScore = suspectScore

	return &JitterAnalysis{
		AgentID:        agentID,
		SampleCount:    len(profile.Samples),
		SuspectScore:   suspectScore,
		MeanIntervalMs: meanMs,
		StdDevMs:       stdDev,
		Verdict:        verdict,
	}, nil
}

// GetSuspectScore returns the current suspect score for an agent (0–1).
func (tj *TemporalJitterInjector) GetSuspectScore(agentID string) float64 {
	tj.mu.RLock()
	defer tj.mu.RUnlock()

	if profile, ok := tj.agentTimings[agentID]; ok {
		return profile.SuspectScore
	}
	return 0
}

// -- helpers --

func mean(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range vals {
		sum += v
	}
	return sum / float64(len(vals))
}

func stddev(vals []float64, m float64) float64 {
	if len(vals) < 2 {
		return 0
	}
	sumSq := 0.0
	for _, v := range vals {
		d := v - m
		sumSq += d * d
	}
	variance := sumSq / float64(len(vals)-1)
	// Manual sqrt via Newton's method to avoid importing math for one call
	if variance <= 0 {
		return 0
	}
	x := variance
	for i := 0; i < 20; i++ {
		x = (x + variance/x) / 2
	}
	return x
}
