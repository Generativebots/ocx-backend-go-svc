// Package security provides the JIT Token Broker (Patent Claim 7).
// Issues HMAC-SHA256 signed tokens with attribution headers,
// gated by trust score threshold.
package security

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
)

// ============================================================================
// TOKEN BROKER — Patent Claim 7
// "token broker issuing JIT tokens upon sufficient trust and an attribution
//  header cryptographically bound to each token"
// ============================================================================

// TokenClaims contains the claims embedded in a JIT token.
type TokenClaims struct {
	TokenID    string  `json:"tid"`
	AgentID    string  `json:"aid"`
	TenantID   string  `json:"tnt"`
	Permission string  `json:"perm"`
	TrustScore float64 `json:"ts"`
	IssuedAt   int64   `json:"iat"`
	ExpiresAt  int64   `json:"exp"`
	Issuer     string  `json:"iss"`
}

// JITToken is a signed token issued by the broker.
type JITToken struct {
	Token       string `json:"token"`
	TokenID     string `json:"token_id"`
	Attribution string `json:"attribution"` // X-OCX-Attribution header value
	ExpiresAt   int64  `json:"expires_at"`
}

// TokenBrokerConfig configures the token broker.
type TokenBrokerConfig struct {
	HMACSecret          string
	PreviousHMACSecret  string        // Previous key for rotation grace window
	RotationGracePeriod time.Duration // How long the previous key remains valid
	DefaultTTL          time.Duration
	MinTrustScore       float64
	Issuer              string
	SweepInterval       time.Duration
	MaxActivePerAgent   int
}

// TokenBroker issues and validates HMAC-signed JIT tokens.
type TokenBroker struct {
	mu          sync.RWMutex
	secret      []byte
	prevSecret  []byte    // T4: previous key for rotation grace window
	graceUntil  time.Time // T4: when the previous key expires
	defaultTTL  time.Duration
	minTrust    float64
	issuer      string
	maxPerAgent int

	// Active tokens: tokenID → claims
	activeTokens map[string]*TokenClaims

	// Revocation set: tokenID → revocation time
	revokedTokens map[string]time.Time

	// Agent → active token count (for quota enforcement)
	agentTokens map[string]int
}

// NewTokenBroker creates a new token broker.
func NewTokenBroker(cfg TokenBrokerConfig) *TokenBroker {
	if cfg.DefaultTTL == 0 {
		cfg.DefaultTTL = 5 * time.Minute
	}
	if cfg.MinTrustScore == 0 {
		cfg.MinTrustScore = 0.65
	}
	if cfg.Issuer == "" {
		cfg.Issuer = "ocx-gateway"
	}
	if cfg.MaxActivePerAgent == 0 {
		cfg.MaxActivePerAgent = 50
	}
	if cfg.RotationGracePeriod == 0 {
		cfg.RotationGracePeriod = 24 * time.Hour // 24h grace for key rotation
	}

	secret := []byte(cfg.HMACSecret)
	if len(secret) == 0 {
		// Generate a default secret for development
		secret = []byte("ocx-dev-hmac-secret-change-in-production")
	}

	var prevSecret []byte
	var graceUntil time.Time
	if cfg.PreviousHMACSecret != "" {
		prevSecret = []byte(cfg.PreviousHMACSecret)
		graceUntil = time.Now().Add(cfg.RotationGracePeriod)
	}

	return &TokenBroker{
		secret:        secret,
		prevSecret:    prevSecret,
		graceUntil:    graceUntil,
		defaultTTL:    cfg.DefaultTTL,
		minTrust:      cfg.MinTrustScore,
		issuer:        cfg.Issuer,
		maxPerAgent:   cfg.MaxActivePerAgent,
		activeTokens:  make(map[string]*TokenClaims),
		revokedTokens: make(map[string]time.Time),
		agentTokens:   make(map[string]int),
	}
}

// IssueToken issues a JIT token if the agent's trust score meets the threshold.
// Returns token + attribution header value.
func (tb *TokenBroker) IssueToken(agentID, tenantID, permission string, trustScore float64) (*JITToken, error) {
	// Trust gate: Claim 7 — "upon sufficient trust"
	if trustScore < tb.minTrust {
		return nil, fmt.Errorf("trust score %.2f below minimum %.2f for token issuance", trustScore, tb.minTrust)
	}

	tb.mu.Lock()
	defer tb.mu.Unlock()

	// Quota check
	if tb.agentTokens[agentID] >= tb.maxPerAgent {
		return nil, fmt.Errorf("agent %s has reached max active tokens (%d)", agentID, tb.maxPerAgent)
	}

	now := time.Now()
	tokenID := fmt.Sprintf("tok_%s_%d", agentID[:min(8, len(agentID))], now.UnixNano()%1e9)

	claims := &TokenClaims{
		TokenID:    tokenID,
		AgentID:    agentID,
		TenantID:   tenantID,
		Permission: permission,
		TrustScore: trustScore,
		IssuedAt:   now.Unix(),
		ExpiresAt:  now.Add(tb.defaultTTL).Unix(),
		Issuer:     tb.issuer,
	}

	// Serialize claims
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize token claims: %w", err)
	}

	// HMAC-SHA256 signature
	sig := tb.sign(claimsJSON)

	// Token = base64(claims) + "." + base64(signature)
	tokenStr := base64.RawURLEncoding.EncodeToString(claimsJSON) +
		"." +
		base64.RawURLEncoding.EncodeToString(sig)

	// Attribution header: "agentID:tokenHash:timestamp"
	// Claim 7 — "attribution header cryptographically bound to each token"
	tokenHash := sha256.Sum256([]byte(tokenStr))
	attribution := fmt.Sprintf("%s:%s:%d",
		agentID,
		base64.RawURLEncoding.EncodeToString(tokenHash[:8]),
		now.Unix(),
	)

	// Track
	tb.activeTokens[tokenID] = claims
	tb.agentTokens[agentID]++

	return &JITToken{
		Token:       tokenStr,
		TokenID:     tokenID,
		Attribution: attribution,
		ExpiresAt:   claims.ExpiresAt,
	}, nil
}

// VerifyToken validates a token's signature, expiry, and revocation status.
// T4: Tries current key first, then previous key during rotation grace window.
func (tb *TokenBroker) VerifyToken(tokenStr string) (*TokenClaims, error) {
	// Split token
	parts := splitToken(tokenStr)
	if len(parts) != 2 {
		return nil, errors.New("invalid token format")
	}

	claimsJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("invalid token encoding: %w", err)
	}

	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("invalid signature encoding: %w", err)
	}

	// Verify HMAC — try current key first
	expectedSig := tb.sign(claimsJSON)
	valid := hmac.Equal(sig, expectedSig)

	// T4: If current key fails, try previous key during grace window
	if !valid {
		tb.mu.RLock()
		hasPrev := len(tb.prevSecret) > 0 && time.Now().Before(tb.graceUntil)
		prev := tb.prevSecret
		tb.mu.RUnlock()

		if hasPrev {
			prevMac := hmac.New(sha256.New, prev)
			prevMac.Write(claimsJSON)
			if hmac.Equal(sig, prevMac.Sum(nil)) {
				valid = true
			}
		}
	}

	if !valid {
		return nil, errors.New("invalid token signature")
	}

	// Parse claims
	var claims TokenClaims
	if err := json.Unmarshal(claimsJSON, &claims); err != nil {
		return nil, fmt.Errorf("invalid token claims: %w", err)
	}

	// Check expiry
	if time.Now().Unix() > claims.ExpiresAt {
		return nil, errors.New("token expired")
	}

	// Check revocation
	tb.mu.RLock()
	_, revoked := tb.revokedTokens[claims.TokenID]
	tb.mu.RUnlock()
	if revoked {
		return nil, errors.New("token has been revoked")
	}

	return &claims, nil
}

// RevokeToken adds a token to the revocation set.
func (tb *TokenBroker) RevokeToken(tokenID string) error {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	claims, exists := tb.activeTokens[tokenID]
	if !exists {
		// Already revoked or never existed - idempotent
		tb.revokedTokens[tokenID] = time.Now()
		return nil
	}

	// Remove from active, add to revoked
	delete(tb.activeTokens, tokenID)
	tb.revokedTokens[tokenID] = time.Now()
	if tb.agentTokens[claims.AgentID] > 0 {
		tb.agentTokens[claims.AgentID]--
	}

	return nil
}

// RevokeAllForAgent revokes all tokens for an agent (e.g., on kill-switch).
func (tb *TokenBroker) RevokeAllForAgent(agentID string) int {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	count := 0
	now := time.Now()
	for tokenID, claims := range tb.activeTokens {
		if claims.AgentID == agentID {
			delete(tb.activeTokens, tokenID)
			tb.revokedTokens[tokenID] = now
			count++
		}
	}
	tb.agentTokens[agentID] = 0
	return count
}

// GetActiveTokenCount returns the number of active tokens for an agent.
func (tb *TokenBroker) GetActiveTokenCount(agentID string) int {
	tb.mu.RLock()
	defer tb.mu.RUnlock()
	return tb.agentTokens[agentID]
}

// SweepExpired removes expired tokens from the active set.
// Called periodically by the Continuous Access Evaluator.
func (tb *TokenBroker) SweepExpired() int {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	now := time.Now().Unix()
	swept := 0
	for tokenID, claims := range tb.activeTokens {
		if now > claims.ExpiresAt {
			delete(tb.activeTokens, tokenID)
			if tb.agentTokens[claims.AgentID] > 0 {
				tb.agentTokens[claims.AgentID]--
			}
			swept++
		}
	}

	// Also sweep old revocation entries (keep for 1 hour)
	cutoff := time.Now().Add(-1 * time.Hour)
	for tokenID, revokedAt := range tb.revokedTokens {
		if revokedAt.Before(cutoff) {
			delete(tb.revokedTokens, tokenID)
		}
	}

	return swept
}

// RotateKey atomically rotates the HMAC signing secret.
// The previous key remains valid for the configured grace period (default 24h).
func (tb *TokenBroker) RotateKey(newSecret string) {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	tb.prevSecret = tb.secret
	tb.graceUntil = time.Now().Add(24 * time.Hour)
	tb.secret = []byte(newSecret)
}

// GetStats returns broker statistics.
func (tb *TokenBroker) GetStats() map[string]interface{} {
	tb.mu.RLock()
	defer tb.mu.RUnlock()
	stats := map[string]interface{}{
		"active_tokens":   len(tb.activeTokens),
		"revoked_tokens":  len(tb.revokedTokens),
		"tracked_agents":  len(tb.agentTokens),
		"min_trust_score": tb.minTrust,
		"default_ttl_sec": tb.defaultTTL.Seconds(),
	}
	if len(tb.prevSecret) > 0 {
		stats["key_rotation_active"] = time.Now().Before(tb.graceUntil)
		stats["key_rotation_grace_until"] = tb.graceUntil.Format(time.RFC3339)
	}
	return stats
}

// --- internal helpers ---

func (tb *TokenBroker) sign(data []byte) []byte {
	mac := hmac.New(sha256.New, tb.secret)
	mac.Write(data)
	return mac.Sum(nil)
}

func splitToken(token string) []string {
	for i := len(token) - 1; i >= 0; i-- {
		if token[i] == '.' {
			return []string{token[:i], token[i+1:]}
		}
	}
	return []string{token}
}
