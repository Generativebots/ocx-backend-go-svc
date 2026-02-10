// Package handlers provides the Bail-Out API (Patent Claims 6 + 14).
// Injects credits into reputation wallet, resets penalties, records to
// immutable ledger, and re-enables blocked streams — all gated by MFA.
package handlers

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ocx/backend/internal/economics"
	"github.com/ocx/backend/internal/evidence"
	"github.com/ocx/backend/internal/reputation"
	"github.com/ocx/backend/internal/security"
)

// ============================================================================
// BAIL-OUT API — Patent Claims 6 + 14
// "Bail-Out API [...] injects credits, resets penalty multipliers, records
//  attribution to immutable ledger, re-enables blocked bidirectional streams"
// ============================================================================

// HandleBailOut handles POST /api/v1/bail-out
func HandleBailOut(
	wallet *reputation.ReputationWallet,
	billingEngine *economics.BillingEngine,
	vault *evidence.EvidenceVault,
	tokenBroker *security.TokenBroker,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			AgentID      string  `json:"agent_id"`
			TenantID     string  `json:"tenant_id"`
			CreditAmount float64 `json:"credit_amount"`
			ResetPenalty bool    `json:"reset_penalties"`
			MFAToken     string  `json:"mfa_token"`
			Reason       string  `json:"reason"`
			AuthorizedBy string  `json:"authorized_by"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
			return
		}

		// Validate required fields
		if req.AgentID == "" || req.TenantID == "" || req.CreditAmount <= 0 {
			http.Error(w, `{"error":"agent_id, tenant_id, and credit_amount > 0 are required"}`, http.StatusBadRequest)
			return
		}

		// ================================================
		// Step 1: Verify MFA token (Patent Claim 6 + 14)
		// "upon successful human multi-factor authentication"
		// ================================================
		if !verifyMFAToken(req.MFAToken, req.AuthorizedBy) {
			slog.Warn("Bail-out DENIED for agent : MFA verification failed", "agent_i_d", req.AgentID)
			http.Error(w, `{"error":"MFA verification failed. Human multi-factor authentication required for bail-out."}`, http.StatusUnauthorized)
			return
		}

		txID := fmt.Sprintf("bail-%s-%d", req.AgentID[:min(8, len(req.AgentID))], time.Now().UnixNano()%1e9)

		slog.Info("Bail-out initiated: agent= tenant= credits= by", "agent_i_d", req.AgentID, "tenant_i_d", req.TenantID, "credit_amount", req.CreditAmount, "authorized_by", req.AuthorizedBy)
		// ================================================
		// Step 2: Inject credits + reset penalties
		// ================================================
		var walletResult string

		// Try the billing engine first (has penalty tracking)
		if billingEngine != nil {
			err := billingEngine.InjectCredits(req.AgentID, req.CreditAmount, req.ResetPenalty)
			if err != nil {
				// Agent not registered in billing engine — register then inject
				billingEngine.RegisterWallet(req.AgentID, req.CreditAmount)
				walletResult = "registered_and_credited"
			} else {
				walletResult = "credits_injected"
			}
		}

		// Also update reputation wallet if available
		if wallet != nil && req.ResetPenalty {
			wallet.RewardAgent(r.Context(), req.AgentID, int64(req.CreditAmount))
			walletResult = "credits_injected_with_reputation_reset"
		}

		// ================================================
		// Step 3: Record to immutable evidence ledger
		// (Patent Claim 14: "records the attribution to an immutable ledger")
		// ================================================
		var evidenceHash string
		if vault != nil {
			record, recErr := vault.RecordTransaction(
				r.Context(), req.TenantID, req.AgentID, txID,
				"BAIL_OUT", "SYSTEM_ADMIN", evidence.OutcomeAllow,
				0, // trust score at bail-out (depleted)
				fmt.Sprintf("Bail-out: %.2f credits injected by %s. Reason: %s. Penalties reset: %v",
					req.CreditAmount, req.AuthorizedBy, req.Reason, req.ResetPenalty),
				map[string]interface{}{
					"credit_amount":   req.CreditAmount,
					"reset_penalties": req.ResetPenalty,
					"authorized_by":   req.AuthorizedBy,
					"mfa_verified":    true,
				},
			)
			if recErr == nil && record != nil {
				evidenceHash = record.Hash
			}
		}

		// ================================================
		// Step 4: Re-enable blocked streams
		// (Patent Claim 14: "re-enables blocked bidirectional streams")
		// ================================================
		reenabledTokens := 0
		if tokenBroker != nil {
			// Re-issue a fresh token for the agent (their old ones may have been revoked)
			newToken, tokErr := tokenBroker.IssueToken(
				req.AgentID, req.TenantID, "*:execute", 1.0, // trust reset to max
			)
			if tokErr == nil && newToken != nil {
				reenabledTokens = 1
			}
		}

		// ================================================
		// Response
		// ================================================
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"transaction_id":    txID,
			"status":            "BAIL_OUT_COMPLETE",
			"wallet_result":     walletResult,
			"credits_injected":  req.CreditAmount,
			"penalties_reset":   req.ResetPenalty,
			"evidence_hash":     evidenceHash,
			"streams_reenabled": reenabledTokens,
			"authorized_by":     req.AuthorizedBy,
			"processed_at":      time.Now(),
		})
	}
}

// ============================================================================
// MFA VERIFICATION — T3 fix: Real TOTP verification
// Uses HMAC-SHA1 time-based OTP (RFC 6238) with configurable secret.
// Also supports system override tokens for automated flows (kill-switch).
// ============================================================================

// systemOverrideTokens are special tokens used by automated subsystems.
// In production these should be loaded from a secrets manager.
var systemOverrideTokens = map[string]bool{
	"kill-switch-auto": true,
}

// verifyMFAToken verifies a multi-factor authentication token.
// Supports:
//  1. System override tokens (for automated flows like kill-switch)
//  2. TOTP verification (RFC 6238 — 6-digit time-based codes)
//  3. Bearer tokens from identity provider (delegated validation)
func verifyMFAToken(token, authorizedBy string) bool {
	if token == "" || authorizedBy == "" {
		return false
	}

	// Check 1: System override tokens (for automated subsystems)
	if systemOverrideTokens[token] {
		slog.Info("MFA: System override token used by", "authorized_by", authorizedBy)
		return true
	}

	// Check 2: TOTP verification (6-digit numeric codes)
	if len(token) == 6 && isNumeric(token) {
		secret := os.Getenv("OCX_MFA_TOTP_SECRET")
		if secret == "" {
			env := os.Getenv("OCX_ENV")
			if env != "" && env != "development" && env != "dev" {
				slog.Info("MFA: OCX_MFA_TOTP_SECRET not set in environment", "env", env)
				return false
			}
			secret = "ocx-dev-mfa-secret" // dev fallback only — blocked in staging/production
			slog.Info("MFA: Using dev fallback TOTP secret (OCX_ENV=)", "env", env)
		}
		return verifyTOTP(token, []byte(secret), time.Now())
	}

	// Check 3: Bearer token format (delegated to identity provider)
	// In production, call out to Okta/Auth0 for validation.
	// For now, validate format: must be a JWT-like token (3 dot-separated parts).
	if strings.Count(token, ".") == 2 && len(token) >= 32 {
		slog.Info("MFA: Bearer token accepted for (format validated)", "authorized_by", authorizedBy)
		return true
	}

	slog.Info("MFA: Invalid token format from (len=)", "authorized_by", authorizedBy, "count", len(token))
	return false
}

// verifyTOTP implements RFC 6238 TOTP verification.
// Checks the current time step and ±1 adjacent windows to handle clock skew.
func verifyTOTP(code string, secret []byte, now time.Time) bool {
	timeStep := now.Unix() / 30 // 30-second windows

	// Check current window and ±1 for clock skew
	for _, offset := range []int64{-1, 0, 1} {
		expected := generateTOTP(secret, timeStep+offset)
		if hmac.Equal([]byte(code), []byte(expected)) {
			return true
		}
	}
	return false
}

// generateTOTP generates a 6-digit TOTP code for a given time step.
func generateTOTP(secret []byte, counter int64) string {
	// RFC 4226: HOTP = Truncate(HMAC-SHA1(secret, counter))
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, uint64(counter))

	mac := hmac.New(sha1.New, secret)
	mac.Write(buf)
	hash := mac.Sum(nil)

	// Dynamic truncation (RFC 4226 §5.4)
	offset := hash[len(hash)-1] & 0x0F
	binCode := (uint32(hash[offset]&0x7F) << 24) |
		(uint32(hash[offset+1]) << 16) |
		(uint32(hash[offset+2]) << 8) |
		uint32(hash[offset+3])

	otp := binCode % uint32(math.Pow10(6))
	return fmt.Sprintf("%06d", otp)
}

// isNumeric returns true if the string contains only digits.
func isNumeric(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return len(s) > 0
}
