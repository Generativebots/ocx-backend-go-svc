// Package protocol provides session management for AOCS persistent conversations.
package protocol

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// SessionState represents the current state of a session
type SessionState string

const (
	SessionStateNew         SessionState = "NEW"
	SessionStateActive      SessionState = "ACTIVE"
	SessionStateSuspended   SessionState = "SUSPENDED"
	SessionStateTerminating SessionState = "TERMINATING"
	SessionStateTerminated  SessionState = "TERMINATED"
)

// ============================================================================
// SESSION
// ============================================================================

// Session represents a persistent AOCS conversation
type Session struct {
	// Core identification
	ID       [16]byte // 128-bit session ID
	TenantID uint32
	AgentID  uint32
	State    SessionState

	// Addressing
	LocalAddr  [16]byte
	RemoteAddr [16]byte

	// Timing
	CreatedAt   time.Time
	LastActive  time.Time
	ExpiresAt   time.Time
	IdleTimeout time.Duration

	// State tracking
	SequenceNum uint16
	AckNum      uint16
	WindowSize  uint16

	// Security
	TrustLevel      float64
	Entitlements    uint64 // Bitmask
	GovernanceHash  uint32
	EncryptionKeyID string

	// Metrics
	MessagesIn  int64
	MessagesOut int64
	BytesIn     int64
	BytesOut    int64
	ErrorCount  int64
	LastError   string

	// Conversation history (for multi-turn agents)
	TurnCount   int32
	ContextHash [32]byte // Hash of conversation context

	mu sync.RWMutex
}

// SessionConfig holds session creation parameters
type SessionConfig struct {
	TenantID     uint32
	AgentID      uint32
	LocalAddr    [16]byte
	RemoteAddr   [16]byte
	TrustLevel   float64
	Entitlements uint64
	IdleTimeout  time.Duration
	TTL          time.Duration
}

// NewSession creates a new AOCS session
func NewSession(cfg SessionConfig) (*Session, error) {
	// Generate random session ID
	var sessionID [16]byte
	if _, err := rand.Read(sessionID[:]); err != nil {
		return nil, fmt.Errorf("failed to generate session ID: %w", err)
	}

	now := time.Now()

	return &Session{
		ID:           sessionID,
		TenantID:     cfg.TenantID,
		AgentID:      cfg.AgentID,
		State:        SessionStateNew,
		LocalAddr:    cfg.LocalAddr,
		RemoteAddr:   cfg.RemoteAddr,
		CreatedAt:    now,
		LastActive:   now,
		ExpiresAt:    now.Add(cfg.TTL),
		IdleTimeout:  cfg.IdleTimeout,
		SequenceNum:  0,
		AckNum:       0,
		WindowSize:   1024,
		TrustLevel:   cfg.TrustLevel,
		Entitlements: cfg.Entitlements,
	}, nil
}

// IDString returns the session ID as a hex string
func (s *Session) IDString() string {
	return hex.EncodeToString(s.ID[:])
}

// Activate transitions session to active state
func (s *Session) Activate() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.State != SessionStateNew {
		return fmt.Errorf("cannot activate session in state %s", s.State)
	}

	s.State = SessionStateActive
	s.LastActive = time.Now()
	return nil
}

// Touch updates the last active timestamp
func (s *Session) Touch() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.LastActive = time.Now()
}

// NextSequence returns and increments the sequence number
func (s *Session) NextSequence() uint16 {
	s.mu.Lock()
	defer s.mu.Unlock()

	seq := s.SequenceNum
	s.SequenceNum++
	return seq
}

// IsExpired checks if the session has expired
func (s *Session) IsExpired() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	now := time.Now()

	// Check TTL expiration
	if now.After(s.ExpiresAt) {
		return true
	}

	// Check idle timeout
	if s.IdleTimeout > 0 && now.Sub(s.LastActive) > s.IdleTimeout {
		return true
	}

	return false
}

// RecordMessage records a message sent/received
func (s *Session) RecordMessage(isOutgoing bool, size int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.LastActive = time.Now()
	if isOutgoing {
		s.MessagesOut++
		s.BytesOut += int64(size)
	} else {
		s.MessagesIn++
		s.BytesIn += int64(size)
	}
}

// RecordError records an error on the session
func (s *Session) RecordError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.ErrorCount++
	s.LastError = err.Error()
}

// Suspend pauses the session
func (s *Session) Suspend() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.State != SessionStateActive {
		return fmt.Errorf("cannot suspend session in state %s", s.State)
	}

	s.State = SessionStateSuspended
	return nil
}

// Resume resumes a suspended session
func (s *Session) Resume() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.State != SessionStateSuspended {
		return fmt.Errorf("cannot resume session in state %s", s.State)
	}

	s.State = SessionStateActive
	s.LastActive = time.Now()
	return nil
}

// Terminate ends the session
func (s *Session) Terminate() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.State == SessionStateTerminated {
		return nil // Already terminated
	}

	s.State = SessionStateTerminated
	return nil
}

// AdvanceTurn increments the turn counter (for multi-turn conversations)
func (s *Session) AdvanceTurn() int32 {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.TurnCount++
	return s.TurnCount
}

// SetContextHash sets the conversation context hash
func (s *Session) SetContextHash(hash [32]byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ContextHash = hash
}

// ============================================================================
// SESSION MANAGER
// ============================================================================

// SessionManager manages active sessions
type SessionManager struct {
	mu       sync.RWMutex
	sessions map[[16]byte]*Session

	// Index by tenant for efficient lookups
	byTenant map[uint32][]*Session

	// Limits
	maxSessionsPerTenant int
	maxTotalSessions     int

	// Cleanup
	cleanupInterval time.Duration
	stopCleanup     chan struct{}
}

// SessionManagerConfig holds configuration for the session manager
type SessionManagerConfig struct {
	MaxSessionsPerTenant int
	MaxTotalSessions     int
	CleanupInterval      time.Duration
}

// NewSessionManager creates a new session manager
func NewSessionManager(cfg SessionManagerConfig) *SessionManager {
	sm := &SessionManager{
		sessions:             make(map[[16]byte]*Session),
		byTenant:             make(map[uint32][]*Session),
		maxSessionsPerTenant: cfg.MaxSessionsPerTenant,
		maxTotalSessions:     cfg.MaxTotalSessions,
		cleanupInterval:      cfg.CleanupInterval,
		stopCleanup:          make(chan struct{}),
	}

	// Start cleanup goroutine
	if cfg.CleanupInterval > 0 {
		go sm.cleanupLoop()
	}

	return sm
}

// Create creates and registers a new session
func (sm *SessionManager) Create(ctx context.Context, cfg SessionConfig) (*Session, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Check limits
	if len(sm.sessions) >= sm.maxTotalSessions {
		return nil, fmt.Errorf("maximum total sessions reached (%d)", sm.maxTotalSessions)
	}

	tenantSessions := sm.byTenant[cfg.TenantID]
	if len(tenantSessions) >= sm.maxSessionsPerTenant {
		return nil, fmt.Errorf("maximum sessions per tenant reached (%d)", sm.maxSessionsPerTenant)
	}

	// Create session
	session, err := NewSession(cfg)
	if err != nil {
		return nil, err
	}

	// Register
	sm.sessions[session.ID] = session
	sm.byTenant[cfg.TenantID] = append(sm.byTenant[cfg.TenantID], session)

	return session, nil
}

// Get retrieves a session by ID
func (sm *SessionManager) Get(id [16]byte) (*Session, error) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	session, exists := sm.sessions[id]
	if !exists {
		return nil, fmt.Errorf("session not found: %s", hex.EncodeToString(id[:]))
	}

	if session.IsExpired() {
		return nil, fmt.Errorf("session expired: %s", hex.EncodeToString(id[:]))
	}

	return session, nil
}

// GetByTenant returns all sessions for a tenant
func (sm *SessionManager) GetByTenant(tenantID uint32) []*Session {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	sessions := sm.byTenant[tenantID]
	active := make([]*Session, 0, len(sessions))

	for _, s := range sessions {
		if !s.IsExpired() && s.State != SessionStateTerminated {
			active = append(active, s)
		}
	}

	return active
}

// Remove removes a session
func (sm *SessionManager) Remove(id [16]byte) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	session, exists := sm.sessions[id]
	if !exists {
		return fmt.Errorf("session not found: %s", hex.EncodeToString(id[:]))
	}

	delete(sm.sessions, id)

	// Remove from tenant index
	tenantSessions := sm.byTenant[session.TenantID]
	for i, s := range tenantSessions {
		if s.ID == id {
			sm.byTenant[session.TenantID] = append(tenantSessions[:i], tenantSessions[i+1:]...)
			break
		}
	}

	return nil
}

// Cleanup removes expired sessions
func (sm *SessionManager) Cleanup() int {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	var removed int
	for id, session := range sm.sessions {
		if session.IsExpired() || session.State == SessionStateTerminated {
			delete(sm.sessions, id)

			// Remove from tenant index
			tenantSessions := sm.byTenant[session.TenantID]
			for i, s := range tenantSessions {
				if s.ID == id {
					sm.byTenant[session.TenantID] = append(tenantSessions[:i], tenantSessions[i+1:]...)
					break
				}
			}

			removed++
		}
	}

	return removed
}

func (sm *SessionManager) cleanupLoop() {
	ticker := time.NewTicker(sm.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			removed := sm.Cleanup()
			if removed > 0 {
				// Log cleanup
				fmt.Printf("[SessionManager] Cleaned up %d expired sessions\n", removed)
			}
		case <-sm.stopCleanup:
			return
		}
	}
}

// Stop stops the session manager
func (sm *SessionManager) Stop() {
	close(sm.stopCleanup)
}

// Stats returns session manager statistics
func (sm *SessionManager) Stats() SessionStats {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	stats := SessionStats{
		TotalSessions: len(sm.sessions),
		TenantCount:   len(sm.byTenant),
		ByState:       make(map[SessionState]int),
	}

	for _, s := range sm.sessions {
		stats.ByState[s.State]++
	}

	return stats
}

// SessionStats contains session manager statistics
type SessionStats struct {
	TotalSessions int
	TenantCount   int
	ByState       map[SessionState]int
}

// ============================================================================
// SESSION PERSISTENCE (for durable sessions)
// ============================================================================

// SessionStore interface for persisting sessions
type SessionStore interface {
	Save(ctx context.Context, session *Session) error
	Load(ctx context.Context, id [16]byte) (*Session, error)
	Delete(ctx context.Context, id [16]byte) error
	ListByTenant(ctx context.Context, tenantID uint32) ([]*Session, error)
}

// InMemorySessionStore provides in-memory session storage (for testing)
type InMemorySessionStore struct {
	mu       sync.RWMutex
	sessions map[[16]byte]*Session
}

// NewInMemorySessionStore creates a new in-memory session store
func NewInMemorySessionStore() *InMemorySessionStore {
	return &InMemorySessionStore{
		sessions: make(map[[16]byte]*Session),
	}
}

// Save saves a session
func (s *InMemorySessionStore) Save(ctx context.Context, session *Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[session.ID] = session
	return nil
}

// Load loads a session
func (s *InMemorySessionStore) Load(ctx context.Context, id [16]byte) (*Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	session, exists := s.sessions[id]
	if !exists {
		return nil, fmt.Errorf("session not found")
	}
	return session, nil
}

// Delete deletes a session
func (s *InMemorySessionStore) Delete(ctx context.Context, id [16]byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, id)
	return nil
}

// ListByTenant lists sessions by tenant
func (s *InMemorySessionStore) ListByTenant(ctx context.Context, tenantID uint32) ([]*Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var sessions []*Session
	for _, session := range s.sessions {
		if session.TenantID == tenantID {
			sessions = append(sessions, session)
		}
	}
	return sessions, nil
}
