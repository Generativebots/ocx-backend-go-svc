package ledger

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// MerkleNode represents a node in the tree
type MerkleNode struct {
	Left  *MerkleNode
	Right *MerkleNode
	Hash  string
	Data  string // Only leaves have data
}

// Ledger maintains the tree and the current root
type Ledger struct {
	mu          sync.Mutex
	Leaves      []*MerkleNode
	Root        *MerkleNode
	TenantRoots map[string]string // Multi-tenancy: Root per tenant
}

func NewLedger() *Ledger {
	return &Ledger{
		Leaves:      make([]*MerkleNode, 0),
		TenantRoots: make(map[string]string),
	}
}

// hashData returns SHA256 hex string
func hashData(data string) string {
	h := sha256.New()
	h.Write([]byte(data))
	return hex.EncodeToString(h.Sum(nil))
}

// Append adds a new log entry and recalculates the root
func (l *Ledger) Append(tenantID, action, diff string) string {
	l.mu.Lock()
	defer l.mu.Unlock()

	timestamp := time.Now().Format(time.RFC3339)
	entry := fmt.Sprintf("[%s] %s: %s | %s", timestamp, tenantID, action, diff)

	node := &MerkleNode{
		Hash: hashData(entry),
		Data: entry,
	}

	l.Leaves = append(l.Leaves, node)
	l.recalculateRoot()

	// Update tenant-specific view (simplified for now to global root)
	l.TenantRoots[tenantID] = l.Root.Hash

	return entry
}

// recalculateRoot rebuilds the tree (Naive O(N) implementation for demo)
func (l *Ledger) recalculateRoot() {
	if len(l.Leaves) == 0 {
		return
	}

	nodes := l.Leaves

	for len(nodes) > 1 {
		var nextLevel []*MerkleNode

		for i := 0; i < len(nodes); i += 2 {
			left := nodes[i]
			var right *MerkleNode

			if i+1 < len(nodes) {
				right = nodes[i+1]
			} else {
				// Duplicate last node if odd number
				right = left
			}

			combinedHash := hashData(left.Hash + right.Hash)
			parent := &MerkleNode{
				Left:  left,
				Right: right,
				Hash:  combinedHash,
			}
			nextLevel = append(nextLevel, parent)
		}
		nodes = nextLevel
	}

	l.Root = nodes[0]
}

// VerifyInclusion checks if a hash exists in the tree (Mock for now)
func (l *Ledger) VerifyInclusion(hash string) bool {
	// Traverse tree...
	return true
}
