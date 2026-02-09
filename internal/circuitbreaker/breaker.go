// Package circuitbreaker implements the circuit breaker pattern for AOCS resilience.
// Provides protection against cascading failures in agent/service communication.
package circuitbreaker

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"
)

// State represents the circuit breaker state
type State int

const (
	StateClosed   State = iota // Normal operation, requests pass through
	StateOpen                  // Failure threshold exceeded, requests blocked
	StateHalfOpen              // Testing if service recovered
)

func (s State) String() string {
	switch s {
	case StateClosed:
		return "CLOSED"
	case StateOpen:
		return "OPEN"
	case StateHalfOpen:
		return "HALF_OPEN"
	default:
		return "UNKNOWN"
	}
}

// Common errors
var (
	ErrCircuitOpen     = errors.New("circuit breaker is open")
	ErrTooManyRequests = errors.New("too many requests in half-open state")
)

// ============================================================================
// CONFIGURATION
// ============================================================================

// Config holds circuit breaker configuration
type Config struct {
	// Name identifies this circuit breaker
	Name string

	// MaxRequests is the maximum number of requests allowed in half-open state
	MaxRequests uint32

	// Interval is the cyclic period in closed state for clearing counts
	Interval time.Duration

	// Timeout is the period of open state before switching to half-open
	Timeout time.Duration

	// ReadyToTrip is called with a copy of Counts whenever a request fails in closed state
	// If it returns true, the circuit breaker trips to open state
	ReadyToTrip func(counts Counts) bool

	// OnStateChange is called whenever the circuit state changes
	OnStateChange func(name string, from State, to State)
}

// DefaultConfig returns a reasonable default configuration
func DefaultConfig(name string) *Config {
	return &Config{
		Name:        name,
		MaxRequests: 3,
		Interval:    60 * time.Second,
		Timeout:     30 * time.Second,
		ReadyToTrip: func(counts Counts) bool {
			// Trip if failure rate > 50% with at least 5 requests
			return counts.Requests >= 5 && counts.FailureRatio() > 0.5
		},
		OnStateChange: func(name string, from State, to State) {
			log.Printf("[CircuitBreaker:%s] State change: %s -> %s", name, from, to)
		},
	}
}

// ============================================================================
// COUNTS
// ============================================================================

// Counts holds request/response counts
type Counts struct {
	Requests             uint32
	TotalSuccesses       uint32
	TotalFailures        uint32
	ConsecutiveSuccesses uint32
	ConsecutiveFailures  uint32
}

// FailureRatio returns the failure ratio
func (c Counts) FailureRatio() float64 {
	if c.Requests == 0 {
		return 0.0
	}
	return float64(c.TotalFailures) / float64(c.Requests)
}

// Clear resets all counts
func (c *Counts) Clear() {
	c.Requests = 0
	c.TotalSuccesses = 0
	c.TotalFailures = 0
	c.ConsecutiveSuccesses = 0
	c.ConsecutiveFailures = 0
}

// OnSuccess records a successful request
func (c *Counts) OnSuccess() {
	c.Requests++
	c.TotalSuccesses++
	c.ConsecutiveSuccesses++
	c.ConsecutiveFailures = 0
}

// OnFailure records a failed request
func (c *Counts) OnFailure() {
	c.Requests++
	c.TotalFailures++
	c.ConsecutiveFailures++
	c.ConsecutiveSuccesses = 0
}

// ============================================================================
// CIRCUIT BREAKER
// ============================================================================

// CircuitBreaker implements the circuit breaker pattern
type CircuitBreaker struct {
	cfg *Config

	mu            sync.Mutex
	state         State
	generation    uint64
	counts        Counts
	expiry        time.Time
	lastStateTime time.Time
}

// New creates a new circuit breaker
func New(cfg *Config) *CircuitBreaker {
	if cfg == nil {
		cfg = DefaultConfig("default")
	}

	cb := &CircuitBreaker{
		cfg:           cfg,
		state:         StateClosed,
		lastStateTime: time.Now(),
	}

	return cb
}

// Name returns the circuit breaker name
func (cb *CircuitBreaker) Name() string {
	return cb.cfg.Name
}

// State returns the current state
func (cb *CircuitBreaker) State() State {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	now := time.Now()
	state, _ := cb.currentState(now)
	return state
}

// Counts returns the current counts
func (cb *CircuitBreaker) Counts() Counts {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.counts
}

// Execute runs the given function if the circuit breaker allows
func (cb *CircuitBreaker) Execute(req func() (interface{}, error)) (interface{}, error) {
	generation, err := cb.beforeRequest()
	if err != nil {
		return nil, err
	}

	defer func() {
		if r := recover(); r != nil {
			cb.afterRequest(generation, false)
			panic(r)
		}
	}()

	result, err := req()
	cb.afterRequest(generation, err == nil)
	return result, err
}

// ExecuteContext runs the given function with context if the circuit breaker allows
func (cb *CircuitBreaker) ExecuteContext(
	ctx context.Context,
	req func(context.Context) (interface{}, error),
) (interface{}, error) {
	generation, err := cb.beforeRequest()
	if err != nil {
		return nil, err
	}

	defer func() {
		if r := recover(); r != nil {
			cb.afterRequest(generation, false)
			panic(r)
		}
	}()

	result, err := req(ctx)
	cb.afterRequest(generation, err == nil)
	return result, err
}

// Allow checks if a request is allowed (doesn't execute anything)
func (cb *CircuitBreaker) Allow() error {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	now := time.Now()
	state, _ := cb.currentState(now)

	if state == StateOpen {
		return ErrCircuitOpen
	}

	if state == StateHalfOpen && cb.counts.Requests >= cb.cfg.MaxRequests {
		return ErrTooManyRequests
	}

	return nil
}

// beforeRequest checks if request is allowed and returns generation
func (cb *CircuitBreaker) beforeRequest() (uint64, error) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	now := time.Now()
	state, generation := cb.currentState(now)

	if state == StateOpen {
		return generation, ErrCircuitOpen
	}

	if state == StateHalfOpen && cb.counts.Requests >= cb.cfg.MaxRequests {
		return generation, ErrTooManyRequests
	}

	cb.counts.Requests++
	return generation, nil
}

// afterRequest records the result
func (cb *CircuitBreaker) afterRequest(generation uint64, success bool) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	now := time.Now()
	state, currentGeneration := cb.currentState(now)

	// Ignore stale results
	if generation != currentGeneration {
		return
	}

	if success {
		cb.onSuccess(state, now)
	} else {
		cb.onFailure(state, now)
	}
}

// onSuccess handles a successful request
func (cb *CircuitBreaker) onSuccess(state State, now time.Time) {
	switch state {
	case StateClosed:
		cb.counts.OnSuccess()
	case StateHalfOpen:
		cb.counts.OnSuccess()
		if cb.counts.ConsecutiveSuccesses >= cb.cfg.MaxRequests {
			cb.setState(StateClosed, now)
		}
	}
}

// onFailure handles a failed request
func (cb *CircuitBreaker) onFailure(state State, now time.Time) {
	switch state {
	case StateClosed:
		cb.counts.OnFailure()
		if cb.cfg.ReadyToTrip(cb.counts) {
			cb.setState(StateOpen, now)
		}
	case StateHalfOpen:
		cb.setState(StateOpen, now)
	}
}

// currentState returns the current state and possibly updates it
func (cb *CircuitBreaker) currentState(now time.Time) (State, uint64) {
	switch cb.state {
	case StateClosed:
		if !cb.expiry.IsZero() && cb.expiry.Before(now) {
			cb.toNewGeneration(now)
		}
	case StateOpen:
		if cb.expiry.Before(now) {
			cb.setState(StateHalfOpen, now)
		}
	}
	return cb.state, cb.generation
}

// setState changes the circuit breaker state
func (cb *CircuitBreaker) setState(state State, now time.Time) {
	if cb.state == state {
		return
	}

	prevState := cb.state
	cb.state = state
	cb.lastStateTime = now

	cb.toNewGeneration(now)

	if cb.cfg.OnStateChange != nil {
		cb.cfg.OnStateChange(cb.cfg.Name, prevState, state)
	}
}

// toNewGeneration starts a new generation
func (cb *CircuitBreaker) toNewGeneration(now time.Time) {
	cb.generation++
	cb.counts.Clear()

	var expiry time.Time
	switch cb.state {
	case StateClosed:
		if cb.cfg.Interval > 0 {
			expiry = now.Add(cb.cfg.Interval)
		}
	case StateOpen:
		expiry = now.Add(cb.cfg.Timeout)
	}
	cb.expiry = expiry
}

// ============================================================================
// CIRCUIT BREAKER MANAGER
// ============================================================================

// Manager manages multiple circuit breakers
type Manager struct {
	mu       sync.RWMutex
	breakers map[string]*CircuitBreaker
	cfg      *Config // Default config for new breakers
}

// NewManager creates a new circuit breaker manager
func NewManager(defaultCfg *Config) *Manager {
	if defaultCfg == nil {
		defaultCfg = DefaultConfig("")
	}

	return &Manager{
		breakers: make(map[string]*CircuitBreaker),
		cfg:      defaultCfg,
	}
}

// Get returns a circuit breaker by name, creating if necessary
func (m *Manager) Get(name string) *CircuitBreaker {
	m.mu.RLock()
	cb, exists := m.breakers[name]
	m.mu.RUnlock()

	if exists {
		return cb
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Double-check after acquiring write lock
	if cb, exists = m.breakers[name]; exists {
		return cb
	}

	// Create new circuit breaker with default config
	cfg := *m.cfg
	cfg.Name = name
	cb = New(&cfg)
	m.breakers[name] = cb

	return cb
}

// GetOrCreate returns an existing circuit breaker or creates one with custom config
func (m *Manager) GetOrCreate(name string, cfg *Config) *CircuitBreaker {
	m.mu.RLock()
	cb, exists := m.breakers[name]
	m.mu.RUnlock()

	if exists {
		return cb
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Double-check
	if cb, exists = m.breakers[name]; exists {
		return cb
	}

	if cfg == nil {
		cfg = m.cfg
	}
	cfg.Name = name
	cb = New(cfg)
	m.breakers[name] = cb

	return cb
}

// Remove removes a circuit breaker
func (m *Manager) Remove(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.breakers, name)
}

// List returns all circuit breaker names
func (m *Manager) List() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, 0, len(m.breakers))
	for name := range m.breakers {
		names = append(names, name)
	}
	return names
}

// Stats returns statistics for all circuit breakers
func (m *Manager) Stats() map[string]CircuitBreakerStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := make(map[string]CircuitBreakerStats, len(m.breakers))
	for name, cb := range m.breakers {
		stats[name] = CircuitBreakerStats{
			Name:   name,
			State:  cb.State(),
			Counts: cb.Counts(),
		}
	}
	return stats
}

// CircuitBreakerStats contains stats for a single circuit breaker
type CircuitBreakerStats struct {
	Name   string
	State  State
	Counts Counts
}

// ============================================================================
// AOCS-SPECIFIC CIRCUIT BREAKERS
// ============================================================================

// AOCSCircuitBreakers provides pre-configured circuit breakers for AOCS services
type AOCSCircuitBreakers struct {
	manager *Manager

	// Pre-configured breakers for common services
	Jury          *CircuitBreaker
	Entropy       *CircuitBreaker
	Cognitive     *CircuitBreaker
	TriFactorGate *CircuitBreaker
	Federation    *CircuitBreaker
	Escrow        *CircuitBreaker
	TrustRegistry *CircuitBreaker
}

// NewAOCSCircuitBreakers creates AOCS-specific circuit breakers
func NewAOCSCircuitBreakers() *AOCSCircuitBreakers {
	manager := NewManager(nil)

	// Jury: 3 consecutive failures trip, 30s timeout
	juryConfig := &Config{
		Name:        "jury",
		MaxRequests: 3,
		Interval:    60 * time.Second,
		Timeout:     30 * time.Second,
		ReadyToTrip: func(c Counts) bool {
			return c.ConsecutiveFailures >= 3
		},
	}

	// Entropy: 5 failures in 60s trip, 20s timeout (less critical)
	entropyConfig := &Config{
		Name:        "entropy",
		MaxRequests: 5,
		Interval:    60 * time.Second,
		Timeout:     20 * time.Second,
		ReadyToTrip: func(c Counts) bool {
			return c.TotalFailures >= 5
		},
	}

	// Cognitive: More conservative - 2 failures trip
	cognitiveConfig := &Config{
		Name:        "cognitive",
		MaxRequests: 2,
		Interval:    30 * time.Second,
		Timeout:     45 * time.Second,
		ReadyToTrip: func(c Counts) bool {
			return c.ConsecutiveFailures >= 2
		},
	}

	// Tri-Factor Gate: Critical path - very conservative
	triFactorConfig := &Config{
		Name:        "tri-factor-gate",
		MaxRequests: 2,
		Interval:    30 * time.Second,
		Timeout:     60 * time.Second, // Longer timeout for critical path
		ReadyToTrip: func(c Counts) bool {
			return c.ConsecutiveFailures >= 2
		},
	}

	// Federation: Inter-OCX communication
	federationConfig := &Config{
		Name:        "federation",
		MaxRequests: 3,
		Interval:    120 * time.Second,
		Timeout:     60 * time.Second,
		ReadyToTrip: func(c Counts) bool {
			return c.FailureRatio() > 0.4 && c.Requests >= 3
		},
	}

	return &AOCSCircuitBreakers{
		manager:       manager,
		Jury:          manager.GetOrCreate("jury", juryConfig),
		Entropy:       manager.GetOrCreate("entropy", entropyConfig),
		Cognitive:     manager.GetOrCreate("cognitive", cognitiveConfig),
		TriFactorGate: manager.GetOrCreate("tri-factor-gate", triFactorConfig),
		Federation:    manager.GetOrCreate("federation", federationConfig),
		Escrow:        manager.Get("escrow"),
		TrustRegistry: manager.Get("trust-registry"),
	}
}

// ExecuteWithFallback runs a request with circuit breaker and fallback
func ExecuteWithFallback[T any](
	cb *CircuitBreaker,
	request func() (T, error),
	fallback func(error) (T, error),
) (T, error) {
	result, err := cb.Execute(func() (interface{}, error) {
		return request()
	})

	if err != nil {
		if errors.Is(err, ErrCircuitOpen) || errors.Is(err, ErrTooManyRequests) {
			return fallback(err)
		}
		return fallback(err)
	}

	return result.(T), nil
}

// HealthStatus returns overall health based on circuit breaker states
func (a *AOCSCircuitBreakers) HealthStatus() (string, map[string]string) {
	stats := a.manager.Stats()

	statuses := make(map[string]string)
	healthy := true

	for name, stat := range stats {
		statuses[name] = stat.State.String()
		if stat.State == StateOpen {
			healthy = false
		}
	}

	if healthy {
		return "HEALTHY", statuses
	}
	return "DEGRADED", statuses
}

// String implements fmt.Stringer for CircuitBreaker
func (cb *CircuitBreaker) String() string {
	state := cb.State()
	counts := cb.Counts()
	return fmt.Sprintf("CircuitBreaker[%s: state=%s, requests=%d, failures=%d]",
		cb.cfg.Name, state, counts.Requests, counts.TotalFailures)
}
