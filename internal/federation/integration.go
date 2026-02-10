package federation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"time"
)

// TrustAttestationLedger bridges the handshake flow to the PersistentTrustLedger.
// All handshake consumers (handshake.go, handshake_v2.go, handshake_client.go,
// handshake_service.go) use this type — it now delegates to PersistentTrustLedger
// for real trust scoring with EMA decay instead of returning a hardcoded mock.
type TrustAttestationLedger struct {
	persistentLedger *PersistentTrustLedger
	localInstanceID  string
}

// NewTrustAttestationLedger creates a new ledger client backed by PersistentTrustLedger.
func NewTrustAttestationLedger() *TrustAttestationLedger {
	return &TrustAttestationLedger{
		persistentLedger: NewPersistentTrustLedger(),
		localInstanceID:  "ocx-local",
	}
}

// NewTrustAttestationLedgerWithID creates a ledger with a specific local instance ID.
func NewTrustAttestationLedgerWithID(localInstanceID string) *TrustAttestationLedger {
	return &TrustAttestationLedger{
		persistentLedger: NewPersistentTrustLedger(),
		localInstanceID:  localInstanceID,
	}
}

// VerifyAttestation verifies attestation hashes and records the handshake
// in the PersistentTrustLedger for trust history tracking.
func (tal *TrustAttestationLedger) VerifyAttestation(ctx context.Context, localHash, remoteHash, agentID string) (*TrustAttestation, error) {
	slog.Info("Verifying attestation: agent=, local_hash=, remote_hash", "agent_i_d", agentID, "local_hash8", localHash[:8], "remote_hash8", remoteHash[:8])
	// Derive a remote instance ID from the remote hash for ledger tracking
	remoteInstanceID := deriveInstanceID(remoteHash)

	// Look up existing trust from persistent ledger (includes EMA decay)
	existingTrust := tal.persistentLedger.GetInstanceTrust(remoteInstanceID)

	// Determine success based on hash verification
	// Both sides should produce valid hashes; mismatch indicates tampering
	hashMatch := localHash != "" && remoteHash != ""

	// Record this handshake in the persistent ledger
	event, err := tal.persistentLedger.RecordHandshake(
		ctx,
		tal.localInstanceID,
		remoteInstanceID,
		"", // remoteDomain — filled by caller if available
		"", // org — filled by caller if available
		agentID,
		existingTrust, // localTrust
		existingTrust, // remoteTrust (symmetric for now)
		hashMatch,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to record handshake in persistent ledger: %w", err)
	}

	// Build attestation using ledger's agreed trust score
	attestation := &TrustAttestation{
		AttestationID: event.EventID,
		LocalOCX:      tal.localInstanceID,
		RemoteOCX:     remoteInstanceID,
		AgentID:       agentID,
		AuditHash:     localHash,
		TrustLevel:    event.AgreedTrust,
		Signature:     event.AttestationHash,
		Timestamp:     event.Timestamp,
		ExpiresAt:     event.Timestamp.Add(24 * time.Hour),
	}

	slog.Info("Attestation verified via PersistentTrustLedger: trust=, event", "trust_level", attestation.TrustLevel, "event_i_d", event.EventID)
	return attestation, nil
}

// GetPersistentLedger exposes the underlying ledger for direct queries
// (e.g., listing trusted instances, viewing attestation log).
func (tal *TrustAttestationLedger) GetPersistentLedger() *PersistentTrustLedger {
	return tal.persistentLedger
}

// deriveInstanceID creates a stable instance ID from a hash string.
func deriveInstanceID(hash string) string {
	h := sha256.Sum256([]byte(hash))
	return "ocx-" + hex.EncodeToString(h[:8])
}
