package federation

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// ============================================================================
// HANDSHAKE STATE MACHINE
// ============================================================================

// HandshakeState represents the current state of a handshake
type HandshakeState int

const (
	StateInit HandshakeState = iota
	StateHelloSent
	StateHelloReceived
	StateChallengeSent
	StateChallengeReceived
	StateProofSent
	StateProofReceived
	StateVerified
	StateAttestationSent
	StateAttestationReceived
	StateAccepted
	StateRejected
	StateTimeout
	StateError
)

// String returns the string representation of a state
func (s HandshakeState) String() string {
	switch s {
	case StateInit:
		return "INIT"
	case StateHelloSent:
		return "HELLO_SENT"
	case StateHelloReceived:
		return "HELLO_RECEIVED"
	case StateChallengeSent:
		return "CHALLENGE_SENT"
	case StateChallengeReceived:
		return "CHALLENGE_RECEIVED"
	case StateProofSent:
		return "PROOF_SENT"
	case StateProofReceived:
		return "PROOF_RECEIVED"
	case StateVerified:
		return "VERIFIED"
	case StateAttestationSent:
		return "ATTESTATION_SENT"
	case StateAttestationReceived:
		return "ATTESTATION_RECEIVED"
	case StateAccepted:
		return "ACCEPTED"
	case StateRejected:
		return "REJECTED"
	case StateTimeout:
		return "TIMEOUT"
	case StateError:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}

// IsTerminal returns true if the state is a terminal state
func (s HandshakeState) IsTerminal() bool {
	return s == StateAccepted || s == StateRejected || s == StateTimeout || s == StateError
}

// HandshakeStateMachine manages state transitions for a handshake
type HandshakeStateMachine struct {
	mu sync.RWMutex

	// Current state
	currentState HandshakeState

	// Session ID
	sessionID string

	// Timestamps
	startedAt     time.Time
	lastUpdatedAt time.Time
	timeoutAt     time.Time

	// Timeout configuration
	stepTimeout  time.Duration // Timeout for each step
	totalTimeout time.Duration // Total handshake timeout

	// State history for debugging
	stateHistory []StateTransition

	// Error tracking
	lastError error

	// Context for cancellation
	ctx    context.Context
	cancel context.CancelFunc
}

// StateTransition represents a state transition event
type StateTransition struct {
	FromState HandshakeState
	ToState   HandshakeState
	Timestamp time.Time
	Error     error
}

// NewHandshakeStateMachine creates a new state machine
func NewHandshakeStateMachine(sessionID string) *HandshakeStateMachine {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)

	return &HandshakeStateMachine{
		currentState:  StateInit,
		sessionID:     sessionID,
		startedAt:     time.Now(),
		lastUpdatedAt: time.Now(),
		timeoutAt:     time.Now().Add(3 * time.Minute),
		stepTimeout:   30 * time.Second,
		totalTimeout:  3 * time.Minute,
		stateHistory:  make([]StateTransition, 0),
		ctx:           ctx,
		cancel:        cancel,
	}
}

// Transition attempts to transition from one state to another
func (sm *HandshakeStateMachine) Transition(from, to HandshakeState) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Check if we're in the expected state
	if sm.currentState != from {
		err := fmt.Errorf("invalid state transition: expected %s, got %s", from, sm.currentState)
		sm.lastError = err
		return err
	}

	// Check if transition is valid
	if !sm.isValidTransition(from, to) {
		err := fmt.Errorf("invalid state transition: %s -> %s", from, to)
		sm.lastError = err
		return err
	}

	// Check for timeout
	if time.Now().After(sm.timeoutAt) {
		sm.currentState = StateTimeout
		sm.lastError = errors.New("handshake timeout")
		return sm.lastError
	}

	// Record transition
	transition := StateTransition{
		FromState: from,
		ToState:   to,
		Timestamp: time.Now(),
	}
	sm.stateHistory = append(sm.stateHistory, transition)

	// Update state
	sm.currentState = to
	sm.lastUpdatedAt = time.Now()

	return nil
}

// isValidTransition checks if a state transition is valid
func (sm *HandshakeStateMachine) isValidTransition(from, to HandshakeState) bool {
	// Define valid transitions
	validTransitions := map[HandshakeState][]HandshakeState{
		StateInit:                {StateHelloSent, StateHelloReceived},
		StateHelloSent:           {StateChallengeReceived, StateError, StateTimeout},
		StateHelloReceived:       {StateChallengeSent, StateError, StateTimeout},
		StateChallengeSent:       {StateProofReceived, StateError, StateTimeout},
		StateChallengeReceived:   {StateProofSent, StateError, StateTimeout},
		StateProofSent:           {StateVerified, StateError, StateTimeout},
		StateProofReceived:       {StateVerified, StateError, StateTimeout},
		StateVerified:            {StateAttestationSent, StateAttestationReceived, StateRejected, StateError, StateTimeout},
		StateAttestationSent:     {StateAccepted, StateRejected, StateError, StateTimeout},
		StateAttestationReceived: {StateAccepted, StateRejected, StateError, StateTimeout},
	}

	allowedStates, ok := validTransitions[from]
	if !ok {
		return false
	}

	for _, allowed := range allowedStates {
		if allowed == to {
			return true
		}
	}

	return false
}

// GetCurrentState returns the current state (thread-safe)
func (sm *HandshakeStateMachine) GetCurrentState() HandshakeState {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.currentState
}

// IsTerminal returns true if the handshake is in a terminal state
func (sm *HandshakeStateMachine) IsTerminal() bool {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.currentState.IsTerminal()
}

// GetTimeout returns the timeout for the current state
func (sm *HandshakeStateMachine) GetTimeout() time.Duration {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	// Calculate remaining time
	remaining := time.Until(sm.timeoutAt)
	if remaining < 0 {
		return 0
	}

	// Return the minimum of step timeout and remaining total timeout
	if remaining < sm.stepTimeout {
		return remaining
	}
	return sm.stepTimeout
}

// GetElapsedTime returns the time elapsed since the handshake started
func (sm *HandshakeStateMachine) GetElapsedTime() time.Duration {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return time.Since(sm.startedAt)
}

// GetStateHistory returns the history of state transitions
func (sm *HandshakeStateMachine) GetStateHistory() []StateTransition {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	// Return a copy to prevent external modification
	history := make([]StateTransition, len(sm.stateHistory))
	copy(history, sm.stateHistory)
	return history
}

// GetLastError returns the last error that occurred
func (sm *HandshakeStateMachine) GetLastError() error {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.lastError
}

// SetError sets an error and transitions to error state
func (sm *HandshakeStateMachine) SetError(err error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.lastError = err
	sm.currentState = StateError

	transition := StateTransition{
		FromState: sm.currentState,
		ToState:   StateError,
		Timestamp: time.Now(),
		Error:     err,
	}
	sm.stateHistory = append(sm.stateHistory, transition)
}

// CheckTimeout checks if the handshake has timed out
func (sm *HandshakeStateMachine) CheckTimeout() bool {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if time.Now().After(sm.timeoutAt) && !sm.currentState.IsTerminal() {
		sm.currentState = StateTimeout
		sm.lastError = errors.New("handshake timeout")

		transition := StateTransition{
			FromState: sm.currentState,
			ToState:   StateTimeout,
			Timestamp: time.Now(),
			Error:     sm.lastError,
		}
		sm.stateHistory = append(sm.stateHistory, transition)

		return true
	}

	return false
}

// Cancel cancels the handshake
func (sm *HandshakeStateMachine) Cancel() {
	sm.cancel()
	sm.SetError(errors.New("handshake cancelled"))
}

// Context returns the context for this handshake
func (sm *HandshakeStateMachine) Context() context.Context {
	return sm.ctx
}

// GetSessionID returns the session ID
func (sm *HandshakeStateMachine) GetSessionID() string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.sessionID
}

// GetStartTime returns when the handshake started
func (sm *HandshakeStateMachine) GetStartTime() time.Time {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.startedAt
}

// GetLastUpdateTime returns when the state was last updated
func (sm *HandshakeStateMachine) GetLastUpdateTime() time.Time {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.lastUpdatedAt
}

// ToProto converts the state machine to a protobuf message
// Note: This requires the generated protobuf code
/*
func (sm *HandshakeStateMachine) ToProto() *pb.HandshakeState {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	return &pb.HandshakeState{
		SessionId:        sm.sessionID,
		CurrentState:     pb.HandshakeState_State(sm.currentState),
		StartedAt:        sm.startedAt.Unix(),
		LastUpdatedAt:    sm.lastUpdatedAt.Unix(),
		TimeoutAt:        sm.timeoutAt.Unix(),
		Error:            sm.lastError.Error(),
	}
}
*/
