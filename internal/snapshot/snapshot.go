package snapshot

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
)

type Snapshot struct {
	ResourceID string
	StateHash  string
}

// CompareAndVerify matches the shadow execution against the Brain's intent
func CompareAndVerify(expectedHash string, ghostOutcome []byte) (bool, error) {
	if expectedHash == "" {
		// In some modes, empty hash might mean "no specific expectation", but for strict verify:
		return false, errors.New("no expected intent hash provided by orchestrator")
	}

	// 1. Hash the actual data generated in the Ghost-Turn
	hasher := sha256.New()
	hasher.Write(ghostOutcome)
	actualHash := hex.EncodeToString(hasher.Sum(nil))

	// 2. Cryptographic Comparison
	if actualHash == expectedHash {
		return true, nil
	}

	return false, fmt.Errorf("integrity violation: expected %s but ghost produced %s", expectedHash, actualHash)
}

// GenerateStateSnapshot hashes a set of files or DB rows to establish a baseline
func GenerateStateSnapshot(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}
