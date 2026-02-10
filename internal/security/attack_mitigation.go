package security

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"
)

// ============================================================================
// ATTACK MITIGATION - Sybil & Replay Attack Prevention
// ============================================================================

// NonceStore manages nonces for replay attack prevention
type NonceStore struct {
	mu          sync.RWMutex
	nonces      map[string]*NonceEntry
	ttl         time.Duration
	stopCleanup chan struct{} // L4 FIX: stop channel for graceful shutdown
}

type NonceEntry struct {
	Nonce     string
	UsedAt    time.Time
	ExpiresAt time.Time
	AgentID   string
}

func NewNonceStore(ttl time.Duration) *NonceStore {
	store := &NonceStore{
		nonces:      make(map[string]*NonceEntry),
		ttl:         ttl,
		stopCleanup: make(chan struct{}),
	}

	// Start cleanup goroutine
	go store.cleanupLoop()

	return store
}

// ValidateNonce checks if a nonce is valid and marks it as used
func (ns *NonceStore) ValidateNonce(nonce, agentID string) error {
	ns.mu.Lock()
	defer ns.mu.Unlock()

	// Check if nonce exists
	entry, exists := ns.nonces[nonce]
	if !exists {
		return errors.New("invalid nonce: not found")
	}

	// Check expiration
	if time.Now().After(entry.ExpiresAt) {
		delete(ns.nonces, nonce)
		return errors.New("invalid nonce: expired")
	}

	// Check if already used (replay attack)
	if !entry.UsedAt.IsZero() {
		return fmt.Errorf("replay attack detected: nonce already used at %s", entry.UsedAt)
	}

	// Check agent ID match
	if entry.AgentID != agentID {
		return errors.New("invalid nonce: agent ID mismatch")
	}

	// Mark as used
	entry.UsedAt = time.Now()

	return nil
}

// StoreNonce stores a new nonce
func (ns *NonceStore) StoreNonce(nonce, agentID string) {
	ns.mu.Lock()
	defer ns.mu.Unlock()

	ns.nonces[nonce] = &NonceEntry{
		Nonce:     nonce,
		AgentID:   agentID,
		ExpiresAt: time.Now().Add(ns.ttl),
	}
}

func (ns *NonceStore) cleanupLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			ns.cleanup()
		case <-ns.stopCleanup:
			return
		}
	}
}

// Stop signals the background cleanup goroutine to exit.
func (ns *NonceStore) Stop() {
	close(ns.stopCleanup)
}

func (ns *NonceStore) cleanup() {
	ns.mu.Lock()
	defer ns.mu.Unlock()

	now := time.Now()
	expired := 0

	for nonce, entry := range ns.nonces {
		if now.After(entry.ExpiresAt) {
			delete(ns.nonces, nonce)
			expired++
		}
	}
}

// ============================================================================
// SYBIL ATTACK PREVENTION
// ============================================================================

// SybilDetector detects and prevents Sybil attacks
type SybilDetector struct {
	mu                 sync.RWMutex
	agentRegistrations map[string]*AgentRegistration
	ipRegistrations    map[string][]string // IP -> Agent IDs
	maxAgentsPerIP     int
	minTrustForNew     float64
	reputationStore    ReputationStore
}

type AgentRegistration struct {
	AgentID      string
	IPAddress    string
	RegisteredAt time.Time
	LastSeenAt   time.Time
	TrustLevel   float64
	Interactions int64
	Verified     bool
	VerifiedBy   []string // Other agents that verified this one
}

type ReputationStore interface {
	GetReputation(agentID string) (float64, error)
	RecordInteraction(agent1, agent2 string, success bool) error
}

func NewSybilDetector(maxAgentsPerIP int, minTrustForNew float64, repStore ReputationStore) *SybilDetector {
	return &SybilDetector{
		agentRegistrations: make(map[string]*AgentRegistration),
		ipRegistrations:    make(map[string][]string),
		maxAgentsPerIP:     maxAgentsPerIP,
		minTrustForNew:     minTrustForNew,
		reputationStore:    repStore,
	}
}

// ValidateAgent checks if an agent is legitimate or a Sybil
func (sd *SybilDetector) ValidateAgent(ctx context.Context, agentID, ipAddress string) error {
	sd.mu.Lock()
	defer sd.mu.Unlock()

	// Check if agent already registered
	if reg, exists := sd.agentRegistrations[agentID]; exists {
		// Update last seen
		reg.LastSeenAt = time.Now()

		// Check if IP changed (suspicious)
		if reg.IPAddress != ipAddress {
			return fmt.Errorf("sybil attack suspected: agent %s changed IP from %s to %s",
				agentID, reg.IPAddress, ipAddress)
		}

		return nil
	}

	// New agent - check IP reputation
	agentsFromIP := sd.ipRegistrations[ipAddress]

	// Check if too many agents from same IP
	if len(agentsFromIP) >= sd.maxAgentsPerIP {
		return fmt.Errorf("sybil attack suspected: too many agents (%d) from IP %s",
			len(agentsFromIP), ipAddress)
	}

	// For new agents, require minimum trust level
	// (would be obtained from external verification)
	trustLevel := 0.0
	if sd.reputationStore != nil {
		trust, err := sd.reputationStore.GetReputation(agentID)
		if err == nil {
			trustLevel = trust
		}
	}

	if trustLevel < sd.minTrustForNew {
		return fmt.Errorf("sybil attack suspected: new agent %s has insufficient trust (%.2f < %.2f)",
			agentID, trustLevel, sd.minTrustForNew)
	}

	// Register new agent
	sd.agentRegistrations[agentID] = &AgentRegistration{
		AgentID:      agentID,
		IPAddress:    ipAddress,
		RegisteredAt: time.Now(),
		LastSeenAt:   time.Now(),
		TrustLevel:   trustLevel,
		Verified:     false,
	}

	sd.ipRegistrations[ipAddress] = append(agentsFromIP, agentID)

	return nil
}

// VerifyAgent marks an agent as verified by another agent
func (sd *SybilDetector) VerifyAgent(agentID, verifiedBy string) error {
	sd.mu.Lock()
	defer sd.mu.Unlock()

	reg, exists := sd.agentRegistrations[agentID]
	if !exists {
		return fmt.Errorf("agent not found: %s", agentID)
	}

	// Add to verified list
	reg.VerifiedBy = append(reg.VerifiedBy, verifiedBy)

	// Mark as verified if enough verifications
	if len(reg.VerifiedBy) >= 3 {
		reg.Verified = true
	}

	return nil
}

// RecordInteraction records a successful interaction
func (sd *SybilDetector) RecordInteraction(agent1, agent2 string, success bool) error {
	sd.mu.Lock()
	defer sd.mu.Unlock()

	// Update interaction counts
	if reg, exists := sd.agentRegistrations[agent1]; exists {
		reg.Interactions++
		if success {
			reg.TrustLevel = min(1.0, reg.TrustLevel+0.01) // Slowly increase trust
		}
	}

	if reg, exists := sd.agentRegistrations[agent2]; exists {
		reg.Interactions++
		if success {
			reg.TrustLevel = min(1.0, reg.TrustLevel+0.01)
		}
	}

	// Record in reputation store
	if sd.reputationStore != nil {
		return sd.reputationStore.RecordInteraction(agent1, agent2, success)
	}

	return nil
}

// GetAgentTrust returns the trust level for an agent
func (sd *SybilDetector) GetAgentTrust(agentID string) (float64, error) {
	sd.mu.RLock()
	defer sd.mu.RUnlock()

	reg, exists := sd.agentRegistrations[agentID]
	if !exists {
		return 0.0, fmt.Errorf("agent not found: %s", agentID)
	}

	return reg.TrustLevel, nil
}

// ============================================================================
// CHALLENGE-RESPONSE VERIFICATION (Enhanced)
// ============================================================================

// ChallengeVerifier handles cryptographic challenge-response
type ChallengeVerifier struct {
	mu         sync.RWMutex
	challenges map[string]*Challenge
	secret     []byte
}

type Challenge struct {
	ChallengeID string
	Nonce       string
	AgentID     string
	CreatedAt   time.Time
	ExpiresAt   time.Time
	HMAC        []byte
	Verified    bool
}

func NewChallengeVerifier(secret []byte) *ChallengeVerifier {
	return &ChallengeVerifier{
		challenges: make(map[string]*Challenge),
		secret:     secret,
	}
}

// CreateChallenge creates a new challenge for an agent
func (cv *ChallengeVerifier) CreateChallenge(agentID, nonce string) (*Challenge, error) {
	cv.mu.Lock()
	defer cv.mu.Unlock()

	// Create HMAC of nonce + timestamp + agentID
	timestamp := time.Now().Unix()
	data := fmt.Sprintf("%s:%d:%s", nonce, timestamp, agentID)

	h := hmac.New(sha256.New, cv.secret)
	h.Write([]byte(data))
	challengeHMAC := h.Sum(nil)

	challengeID := hex.EncodeToString(challengeHMAC[:16])

	challenge := &Challenge{
		ChallengeID: challengeID,
		Nonce:       nonce,
		AgentID:     agentID,
		CreatedAt:   time.Now(),
		ExpiresAt:   time.Now().Add(5 * time.Minute),
		HMAC:        challengeHMAC,
		Verified:    false,
	}

	cv.challenges[challengeID] = challenge

	return challenge, nil
}

// VerifyResponse verifies a challenge response
func (cv *ChallengeVerifier) VerifyResponse(challengeID string, response []byte) error {
	cv.mu.Lock()
	defer cv.mu.Unlock()

	challenge, exists := cv.challenges[challengeID]
	if !exists {
		return errors.New("challenge not found")
	}

	// Check expiration
	if time.Now().After(challenge.ExpiresAt) {
		delete(cv.challenges, challengeID)
		return errors.New("challenge expired")
	}

	// Check if already verified (replay attack)
	if challenge.Verified {
		return errors.New("challenge already verified (replay attack)")
	}

	// Verify HMAC
	if !hmac.Equal(challenge.HMAC, response) {
		return errors.New("invalid response: HMAC mismatch")
	}

	// Mark as verified
	challenge.Verified = true

	return nil
}

// ============================================================================
// RATE LIMITING (DDoS Prevention)
// ============================================================================

type RateLimiter struct {
	mu       sync.RWMutex
	requests map[string]*RateLimitEntry
	limit    int
	window   time.Duration
}

type RateLimitEntry struct {
	Count       int
	WindowStart time.Time
}

func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		requests: make(map[string]*RateLimitEntry),
		limit:    limit,
		window:   window,
	}
}

// CheckLimit checks if a request is within rate limits
func (rl *RateLimiter) CheckLimit(identifier string) error {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()

	entry, exists := rl.requests[identifier]
	if !exists {
		rl.requests[identifier] = &RateLimitEntry{
			Count:       1,
			WindowStart: now,
		}
		return nil
	}

	// Check if window expired
	if now.Sub(entry.WindowStart) > rl.window {
		entry.Count = 1
		entry.WindowStart = now
		return nil
	}

	// Check limit
	if entry.Count >= rl.limit {
		return fmt.Errorf("rate limit exceeded: %d requests in %s", entry.Count, rl.window)
	}

	entry.Count++

	return nil
}

// ============================================================================
// INTEGRATED SECURITY MANAGER
// ============================================================================

type SecurityManager struct {
	nonceStore        *NonceStore
	sybilDetector     *SybilDetector
	challengeVerifier *ChallengeVerifier
	rateLimiter       *RateLimiter
}

// ValidateHandshake performs complete security validation
func (sm *SecurityManager) ValidateHandshake(ctx context.Context, agentID, ipAddress, nonce string) error {
	// 1. Rate limiting
	if err := sm.rateLimiter.CheckLimit(ipAddress); err != nil {
		return fmt.Errorf("rate limit: %w", err)
	}

	// 2. Sybil detection
	if err := sm.sybilDetector.ValidateAgent(ctx, agentID, ipAddress); err != nil {
		return fmt.Errorf("sybil detection: %w", err)
	}

	// 3. Nonce validation (replay prevention)
	if err := sm.nonceStore.ValidateNonce(nonce, agentID); err != nil {
		return fmt.Errorf("nonce validation: %w", err)
	}

	return nil
}
