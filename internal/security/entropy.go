package security

import (
	"math"
	"time"
)

type EntropyAuditor struct {
	Threshold     float64
	LastTurnTime  time.Time
	JitterHistory []float64
}

// NewEntropyAuditor creates a new auditor with a default threshold
func NewEntropyAuditor() *EntropyAuditor {
	return &EntropyAuditor{
		Threshold:     5.5,
		JitterHistory: make([]float64, 0),
	}
}

// CalculateShannonEntropy measures the randomness of the payload.
// Standard business text has an entropy of ~3.5 to 4.5.
// Encrypted/Steganographic payloads often spike toward 7.0+.
func CalculateShannonEntropy(data string) float64 {
	if len(data) == 0 {
		return 0
	}

	charCounts := make(map[rune]int)
	for _, char := range data {
		charCounts[char]++
	}

	var entropy float64
	for _, count := range charCounts {
		p := float64(count) / float64(len(data))
		entropy -= p * math.Log2(p)
	}

	return entropy
}

// DetectTemporalSteganography checks for data encoded in the timing of messages.
func (a *EntropyAuditor) DetectTemporalSteganography(now time.Time) float64 {
	if a.LastTurnTime.IsZero() {
		a.LastTurnTime = now
		return 0
	}

	delta := now.Sub(a.LastTurnTime).Seconds()
	a.LastTurnTime = now

	// Calculate Variance in timing (Jitter)
	// High-entropy jitter often indicates a 'Timing Channel' attack.
	a.JitterHistory = append(a.JitterHistory, delta)
	if len(a.JitterHistory) > 10 {
		a.JitterHistory = a.JitterHistory[1:]
	}

	return calculateVariance(a.JitterHistory)
}

func calculateVariance(data []float64) float64 {
	if len(data) == 0 {
		return 0
	}
	// Standard statistical variance logic
	var sum float64
	for _, v := range data {
		sum += v
	}
	mean := sum / float64(len(data))

	var variance float64
	for _, v := range data {
		variance += math.Pow(v-mean, 2)
	}

	return variance / float64(len(data))
}
