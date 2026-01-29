package main

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/ocx/backend/internal/snapshot"
)

func TestSnapshot_Integrity(t *testing.T) {
	// 1. Setup Data
	payload := []byte("critical_system_op")

	// 2. Generate Expected Hash
	hasher := sha256.New()
	hasher.Write(payload)
	expected := hex.EncodeToString(hasher.Sum(nil))

	// 3. Verify Alignment (Pass Case)
	aligned, err := snapshot.CompareAndVerify(expected, payload)
	if err != nil || !aligned {
		t.Errorf("CompareAndVerify failed matching hash. Err: %v", err)
	}

	// 4. Verify Mismatch (Fail Case)
	badPayload := []byte("hacked_data")
	aligned, err = snapshot.CompareAndVerify(expected, badPayload)
	if aligned || err == nil {
		t.Errorf("CompareAndVerify PASSED mismatching hash. Expected error!")
	}
}
