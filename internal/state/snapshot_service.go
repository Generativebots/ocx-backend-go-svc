package state

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

// Snapshot represents the state of a resource at a specific point in time.
type Snapshot struct {
	TurnID    string    `json:"turn_id"`
	PreHash   string    `json:"pre_hash"`
	Timestamp time.Time `json:"timestamp"`
	StateData []byte    `json:"-"` // Internal use
}

// SnapshotService API
type SnapshotService interface {
	CaptureState(turnID string, currentState interface{}) (*Snapshot, error)
	VerifyState(snapshot *Snapshot, postState interface{}) (bool, error)
}

type snapshotServiceImpl struct {
	// In production, this would be Redis/Postgres
	store map[string]*Snapshot
}

// CaptureState generates a SHA-256 hash of the current state.
func (s *snapshotServiceImpl) CaptureState(turnID string, currentState interface{}) (*Snapshot, error) {
	data, err := json.Marshal(currentState)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal state: %w", err)
	}

	hash := sha256.Sum256(data)
	preHash := hex.EncodeToString(hash[:])

	snapshot := &Snapshot{
		TurnID:    turnID,
		PreHash:   preHash,
		Timestamp: time.Now(),
		StateData: data,
	}

	// Persist (Mock)
	s.store[turnID] = snapshot
	return snapshot, nil
}

// VerifyState compares the pre-execution snapshot with the post-execution state.
// In a Ghost-Turn, if we want to confirm NO side effects leaked, PostHash should == PreHash.
func (s *snapshotServiceImpl) VerifyState(snapshot *Snapshot, postState interface{}) (bool, error) {
	data, err := json.Marshal(postState)
	if err != nil {
		return false, fmt.Errorf("failed to marshal post-state: %w", err)
	}

	hash := sha256.Sum256(data)
	postHash := hex.EncodeToString(hash[:])

	return snapshot.PreHash == postHash, nil
}
