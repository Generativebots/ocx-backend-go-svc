package reputation

import (
	"log"
	"sync"
	"time"
)

// TrustScoreDecayScheduler periodically decays trust scores for inactive agents.
// This implements the patent's "Trust Score Decay" mechanism — agents that
// don't interact with the system gradually lose trust, preventing stale
// high-trust scores from persisting indefinitely.
//
// The decay scheduler runs as a background goroutine and applies a configurable
// decay factor on each tick for agents that haven't been seen recently.
type TrustScoreDecayScheduler struct {
	mu     sync.Mutex
	rm     *ReputationManager
	config DecayConfig
	stopCh chan struct{}
	logger *log.Logger
}

// DecayConfig holds the configuration for the decay scheduler.
type DecayConfig struct {
	// Interval between decay sweeps
	Interval time.Duration

	// InactivityThreshold: agents inactive longer than this get decayed
	InactivityThreshold time.Duration

	// DecayRate: multiplied against current score per sweep (e.g. 0.99 = 1% decay)
	DecayRate float64

	// FloorScore: scores won't decay below this value
	FloorScore float64
}

// DefaultDecayConfig returns sensible defaults for the decay scheduler.
func DefaultDecayConfig() DecayConfig {
	return DecayConfig{
		Interval:            1 * time.Hour,
		InactivityThreshold: 7 * 24 * time.Hour, // 1 week
		DecayRate:           0.99,               // 1% per sweep
		FloorScore:          0.1,                // Never below 0.1
	}
}

// NewTrustScoreDecayScheduler creates and starts a new decay scheduler.
func NewTrustScoreDecayScheduler(rm *ReputationManager, cfg DecayConfig) *TrustScoreDecayScheduler {
	ds := &TrustScoreDecayScheduler{
		rm:     rm,
		config: cfg,
		stopCh: make(chan struct{}),
		logger: log.New(log.Writer(), "[DECAY-SCHED] ", log.LstdFlags),
	}

	go ds.run()
	return ds
}

// Stop gracefully stops the decay scheduler.
func (ds *TrustScoreDecayScheduler) Stop() {
	close(ds.stopCh)
}

// run is the main loop that periodically applies decay.
func (ds *TrustScoreDecayScheduler) run() {
	ticker := time.NewTicker(ds.config.Interval)
	defer ticker.Stop()

	ds.logger.Printf("Started trust score decay scheduler (interval=%s, decay=%.4f, inactivity=%s)",
		ds.config.Interval, ds.config.DecayRate, ds.config.InactivityThreshold)

	for {
		select {
		case <-ticker.C:
			ds.sweep()
		case <-ds.stopCh:
			ds.logger.Println("Decay scheduler stopped")
			return
		}
	}
}

// sweep applies decay to all inactive agents.
func (ds *TrustScoreDecayScheduler) sweep() {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	ds.rm.mu.Lock()
	defer ds.rm.mu.Unlock()

	now := time.Now()
	decayed := 0

	for key, rep := range ds.rm.reputations {
		// Skip blacklisted agents
		if rep.Blacklisted {
			continue
		}

		// Check if inactive
		if now.Sub(rep.LastUpdated) < ds.config.InactivityThreshold {
			continue
		}

		// Apply decay
		oldScore := rep.ReputationScore
		newScore := oldScore * ds.config.DecayRate

		// Enforce floor
		if newScore < ds.config.FloorScore {
			newScore = ds.config.FloorScore
		}

		if newScore != oldScore {
			rep.ReputationScore = newScore
			rep.LastUpdated = now
			decayed++
			ds.logger.Printf("Decayed %s: %.4f → %.4f", key, oldScore, newScore)
		}
	}

	if decayed > 0 {
		ds.logger.Printf("Sweep complete: %d agents decayed", decayed)
	}
}
