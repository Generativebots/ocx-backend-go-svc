package federation

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// ============================================================================
// HANDSHAKE SESSION PERSISTENCE (P2 FIX #13)
//
// HandshakeSession is an ephemeral struct with no persistence. If the server
// restarts mid-handshake (between Step 3 and Step 5), the session is lost and
// the remote OCX gets no response. This interface allows persisting handshake
// state to Supabase/Redis after each step transition.
// ============================================================================

// HandshakeSessionState represents a serializable snapshot of a handshake session.
type HandshakeSessionState struct {
	SessionID     string    `json:"session_id"`
	LocalOCXID    string    `json:"local_ocx_id"`
	RemoteOCXID   string    `json:"remote_ocx_id"`
	AgentID       string    `json:"agent_id"`
	CurrentState  int       `json:"current_state"` // Maps to HandshakeState
	Nonce         string    `json:"nonce"`
	Challenge     string    `json:"challenge"`
	TrustLevel    float64   `json:"trust_level"`
	StartedAt     time.Time `json:"started_at"`
	LastStepAt    time.Time `json:"last_step_at"`
	StepCompleted int       `json:"step_completed"` // 0-6
	Verdict       string    `json:"verdict"`        // "", "ACCEPTED", "REJECTED"
}

// HandshakeSessionStore persists handshake session state for crash recovery.
//
// When implemented, the handshake flow should:
// 1. Save state after each step transition (SendHello, ReceiveChallenge, etc.)
// 2. On startup, load incomplete sessions and resume or expire them
// 3. Use idempotency keys so steps can be retried without side effects
//
// TODO(durability): Implement SupabaseHandshakeStore for production persistence.
type HandshakeSessionStore interface {
	// Save persists the current handshake session state.
	Save(ctx context.Context, state *HandshakeSessionState) error

	// Load retrieves a handshake session by ID.
	Load(ctx context.Context, sessionID string) (*HandshakeSessionState, error)

	// Delete removes a completed/expired handshake session.
	Delete(ctx context.Context, sessionID string) error

	// ListIncomplete returns all handshake sessions that didn't reach a terminal state.
	ListIncomplete(ctx context.Context) ([]*HandshakeSessionState, error)
}

// ============================================================================
// IN-MEMORY IMPLEMENTATION (for dev/test)
// ============================================================================

// InMemoryHandshakeStore provides an in-memory implementation for dev/test.
type InMemoryHandshakeStore struct {
	mu       sync.RWMutex
	sessions map[string]*HandshakeSessionState
}

// NewInMemoryHandshakeStore creates a new in-memory handshake store.
func NewInMemoryHandshakeStore() *InMemoryHandshakeStore {
	return &InMemoryHandshakeStore{
		sessions: make(map[string]*HandshakeSessionState),
	}
}

func (s *InMemoryHandshakeStore) Save(ctx context.Context, state *HandshakeSessionState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	state.LastStepAt = time.Now()
	s.sessions[state.SessionID] = state
	slog.Info("[HandshakeStore] Saved session (step=, state=)", "session_i_d", state.SessionID, "step_completed", state.StepCompleted, "current_state", state.CurrentState)
	return nil
}

func (s *InMemoryHandshakeStore) Load(ctx context.Context, sessionID string) (*HandshakeSessionState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	state, exists := s.sessions[sessionID]
	if !exists {
		return nil, fmt.Errorf("handshake session not found: %s", sessionID)
	}
	return state, nil
}

func (s *InMemoryHandshakeStore) Delete(ctx context.Context, sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, sessionID)
	return nil
}

func (s *InMemoryHandshakeStore) ListIncomplete(ctx context.Context) ([]*HandshakeSessionState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var incomplete []*HandshakeSessionState
	for _, state := range s.sessions {
		if state.Verdict == "" {
			incomplete = append(incomplete, state)
		}
	}
	return incomplete, nil
}

// ============================================================================
// T9 FIX: NONCE STORE — TTL-Based Replay Prevention
//
// In-memory nonce store with automatic expiration. Prevents replay attacks
// by ensuring each nonce can only be used once within its TTL window.
// For production, implement a Redis-backed NonceStore.
// ============================================================================

// NonceStore tracks used nonces to prevent replay attacks.
type NonceStore interface {
	// MarkUsed records a nonce. Returns false if already used.
	MarkUsed(nonce string) bool
	// IsUsed checks if a nonce has been used.
	IsUsed(nonce string) bool
}

// InMemoryNonceStore provides TTL-based in-memory nonce tracking.
type InMemoryNonceStore struct {
	mu     sync.Mutex
	nonces map[string]time.Time // nonce → expiry time
	maxAge time.Duration
}

// NewInMemoryNonceStore creates a nonce store with the given TTL.
// Starts a background goroutine to clean up expired nonces.
func NewInMemoryNonceStore(maxAge time.Duration) *InMemoryNonceStore {
	if maxAge == 0 {
		maxAge = 5 * time.Minute
	}
	store := &InMemoryNonceStore{
		nonces: make(map[string]time.Time),
		maxAge: maxAge,
	}
	// Background cleanup every 60 seconds
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			store.cleanup()
		}
	}()
	return store
}

// MarkUsed atomically checks and records a nonce. Returns false if already used.
func (ns *InMemoryNonceStore) MarkUsed(nonce string) bool {
	ns.mu.Lock()
	defer ns.mu.Unlock()

	if _, exists := ns.nonces[nonce]; exists {
		return false // replay detected
	}
	ns.nonces[nonce] = time.Now().Add(ns.maxAge)
	return true
}

// IsUsed checks if a nonce has been previously used (and not yet expired).
func (ns *InMemoryNonceStore) IsUsed(nonce string) bool {
	ns.mu.Lock()
	defer ns.mu.Unlock()
	expiry, exists := ns.nonces[nonce]
	if !exists {
		return false
	}
	// If expired, treat as unused (cleaned up lazily)
	if time.Now().After(expiry) {
		delete(ns.nonces, nonce)
		return false
	}
	return true
}

// cleanup removes expired nonces.
func (ns *InMemoryNonceStore) cleanup() {
	ns.mu.Lock()
	defer ns.mu.Unlock()
	now := time.Now()
	for nonce, expiry := range ns.nonces {
		if now.After(expiry) {
			delete(ns.nonces, nonce)
		}
	}
}
