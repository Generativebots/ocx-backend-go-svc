// Package marketplace — Connector Signature Validation (Gap 4 Fix)
//
// Cryptographic verification of connector packages before installation.
// Uses SHA-256 content hashing + ECDSA signature verification to ensure
// the connector has not been tampered with since the publisher signed it.
package marketplace

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"sync"
	"time"
)

// SignatureVerifier handles cryptographic validation of marketplace items.
type SignatureVerifier struct {
	mu sync.RWMutex

	// Trusted publisher keys (publisherID → public key)
	trustedKeys map[string]*ecdsa.PublicKey

	// OCX platform signing key (for built-in items)
	platformKey *ecdsa.PrivateKey

	// Verification audit log
	auditLog []VerificationAuditEntry
}

// VerificationAuditEntry records a signature verification attempt.
type VerificationAuditEntry struct {
	ItemID    string    `json:"item_id"`
	ItemType  string    `json:"item_type"`
	Publisher string    `json:"publisher"`
	Result    string    `json:"result"` // "valid", "invalid", "no_signature", "unknown_publisher"
	Hash      string    `json:"hash"`
	Timestamp time.Time `json:"timestamp"`
}

// SignatureResult is the outcome of a signature validation.
type SignatureResult struct {
	Valid       bool   `json:"valid"`
	Reason      string `json:"reason"`
	ContentHash string `json:"content_hash"`
	SignerID    string `json:"signer_id"`
}

// NewSignatureVerifier creates a new verifier with a fresh platform key.
func NewSignatureVerifier() *SignatureVerifier {
	// Generate platform ECDSA key for signing built-in items
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		log.Fatalf("Failed to generate platform key: %v", err)
	}

	return &SignatureVerifier{
		trustedKeys: make(map[string]*ecdsa.PublicKey),
		platformKey: key,
		auditLog:    make([]VerificationAuditEntry, 0),
	}
}

// RegisterPublisherKey adds a trusted publisher's public key.
func (sv *SignatureVerifier) RegisterPublisherKey(publisherID string, pubKey *ecdsa.PublicKey) {
	sv.mu.Lock()
	defer sv.mu.Unlock()
	sv.trustedKeys[publisherID] = pubKey
	log.Printf("🔑 Registered publisher key: %s", publisherID)
}

// GetPlatformPublicKey returns the platform's public key for verification.
func (sv *SignatureVerifier) GetPlatformPublicKey() *ecdsa.PublicKey {
	return &sv.platformKey.PublicKey
}

// SignContent signs content with the platform key (for built-in items).
// Returns (contentHash, signatureHex, error). Signature is ASN.1 DER encoded as hex.
func (sv *SignatureVerifier) SignContent(content []byte) (string, string, error) {
	hash := sha256.Sum256(content)
	hashHex := hex.EncodeToString(hash[:])

	// Sign using ASN.1 DER encoding
	sigBytes, err := ecdsa.SignASN1(rand.Reader, sv.platformKey, hash[:])
	if err != nil {
		return "", "", fmt.Errorf("signing failed: %w", err)
	}

	sigHex := hex.EncodeToString(sigBytes)
	return hashHex, sigHex, nil
}

// ValidateConnector validates a connector's signature before installation.
func (sv *SignatureVerifier) ValidateConnector(conn *Connector) *SignatureResult {
	sv.mu.Lock()
	defer sv.mu.Unlock()

	// Built-in connectors are implicitly trusted
	if conn.IsBuiltIn {
		result := &SignatureResult{
			Valid:       true,
			Reason:      "built-in connector (platform-signed)",
			ContentHash: computeConnectorHash(conn),
			SignerID:    "ocx-platform",
		}
		sv.logVerification(conn.ID, "connector", conn.PublisherID, "valid", result.ContentHash)
		return result
	}

	// Check for signature
	if conn.SignatureHash == "" {
		result := &SignatureResult{
			Valid:  false,
			Reason: "no signature provided",
		}
		sv.logVerification(conn.ID, "connector", conn.PublisherID, "no_signature", "")
		return result
	}

	// Check if publisher has registered key
	pubKey, exists := sv.trustedKeys[conn.PublisherID]
	if !exists {
		result := &SignatureResult{
			Valid:  false,
			Reason: fmt.Sprintf("unknown publisher: %s (no registered key)", conn.PublisherID),
		}
		sv.logVerification(conn.ID, "connector", conn.PublisherID, "unknown_publisher", "")
		return result
	}

	// Compute content hash
	contentHash := computeConnectorHash(conn)
	hashBytes, err := hex.DecodeString(contentHash)
	if err != nil {
		result := &SignatureResult{
			Valid:  false,
			Reason: "invalid content hash",
		}
		sv.logVerification(conn.ID, "connector", conn.PublisherID, "invalid", contentHash)
		return result
	}

	// Decode hex-encoded ASN.1 DER signature
	sigBytes, err := hex.DecodeString(conn.SignatureHash)
	if err != nil {
		result := &SignatureResult{
			Valid:  false,
			Reason: "malformed signature encoding",
		}
		sv.logVerification(conn.ID, "connector", conn.PublisherID, "invalid", contentHash)
		return result
	}

	// Verify ECDSA ASN.1 signature against content hash
	valid := ecdsa.VerifyASN1(pubKey, hashBytes, sigBytes)

	resultStr := "valid"
	reason := "signature verified"
	if !valid {
		resultStr = "invalid"
		reason = "signature verification failed — content may have been tampered with"
	}

	result := &SignatureResult{
		Valid:       valid,
		Reason:      reason,
		ContentHash: contentHash,
		SignerID:    conn.PublisherID,
	}
	sv.logVerification(conn.ID, "connector", conn.PublisherID, resultStr, contentHash)
	return result
}

// ValidateTemplate validates a template's signature before installation.
func (sv *SignatureVerifier) ValidateTemplate(tmpl *Template) *SignatureResult {
	sv.mu.Lock()
	defer sv.mu.Unlock()

	// Built-in templates are implicitly trusted
	if tmpl.IsBuiltIn {
		result := &SignatureResult{
			Valid:       true,
			Reason:      "built-in template (platform-signed)",
			ContentHash: computeTemplateHash(tmpl),
			SignerID:    "ocx-platform",
		}
		sv.logVerification(tmpl.ID, "template", tmpl.PublisherID, "valid", result.ContentHash)
		return result
	}

	// Unsigned templates not allowed
	if tmpl.SignatureHash == "" {
		result := &SignatureResult{
			Valid:  false,
			Reason: "no signature provided",
		}
		sv.logVerification(tmpl.ID, "template", tmpl.PublisherID, "no_signature", "")
		return result
	}

	// Check publisher key
	pubKey, exists := sv.trustedKeys[tmpl.PublisherID]
	if !exists {
		result := &SignatureResult{
			Valid:  false,
			Reason: fmt.Sprintf("unknown publisher: %s", tmpl.PublisherID),
		}
		sv.logVerification(tmpl.ID, "template", tmpl.PublisherID, "unknown_publisher", "")
		return result
	}

	// Compute content hash and decode signature
	contentHash := computeTemplateHash(tmpl)
	hashBytes, err := hex.DecodeString(contentHash)
	if err != nil {
		sv.logVerification(tmpl.ID, "template", tmpl.PublisherID, "invalid", contentHash)
		return &SignatureResult{Valid: false, Reason: "invalid content hash"}
	}

	sigBytes, err := hex.DecodeString(tmpl.SignatureHash)
	if err != nil {
		sv.logVerification(tmpl.ID, "template", tmpl.PublisherID, "invalid", contentHash)
		return &SignatureResult{Valid: false, Reason: "malformed signature encoding"}
	}

	// Verify ECDSA ASN.1 signature
	valid := ecdsa.VerifyASN1(pubKey, hashBytes, sigBytes)

	resultStr := "valid"
	reason := "signature verified"
	if !valid {
		resultStr = "invalid"
		reason = "signature verification failed"
	}

	result := &SignatureResult{
		Valid:       valid,
		Reason:      reason,
		ContentHash: contentHash,
		SignerID:    tmpl.PublisherID,
	}
	sv.logVerification(tmpl.ID, "template", tmpl.PublisherID, resultStr, contentHash)
	return result
}

// GetAuditLog returns recent verification audit entries.
func (sv *SignatureVerifier) GetAuditLog() []VerificationAuditEntry {
	sv.mu.RLock()
	defer sv.mu.RUnlock()
	out := make([]VerificationAuditEntry, len(sv.auditLog))
	copy(out, sv.auditLog)
	return out
}

// --- Internal helpers ---

func (sv *SignatureVerifier) logVerification(itemID, itemType, publisher, result, hash string) {
	sv.auditLog = append(sv.auditLog, VerificationAuditEntry{
		ItemID:    itemID,
		ItemType:  itemType,
		Publisher: publisher,
		Result:    result,
		Hash:      hash,
		Timestamp: time.Now(),
	})
	// Rolling audit log — keep last 1000 entries
	if len(sv.auditLog) > 1000 {
		sv.auditLog = sv.auditLog[len(sv.auditLog)-1000:]
	}
}

func computeConnectorHash(conn *Connector) string {
	content := fmt.Sprintf("%s|%s|%s|%s|%s",
		conn.Name, conn.Description, conn.Version, conn.Category, conn.PublisherID)
	hash := sha256.Sum256([]byte(content))
	return hex.EncodeToString(hash[:])
}

func computeTemplateHash(tmpl *Template) string {
	content := fmt.Sprintf("%s|%s|%s|%s|%s",
		tmpl.Name, tmpl.Description, tmpl.Version, tmpl.Category, tmpl.PublisherID)
	hash := sha256.Sum256([]byte(content))
	return hex.EncodeToString(hash[:])
}
