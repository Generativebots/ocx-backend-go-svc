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

// recalculateRoot rebuilds the Merkle tree from all leaves.
// This is an O(N) full rebuild on every append, which guarantees correctness
// by re-hashing all leaf pairs bottom-up. This is the canonical approach for
// immutable audit ledgers where proof integrity is paramount.
func (l *Ledger) recalculateRoot() {
	if len(l.Leaves) == 0 {
		return
	}

	// If only one leaf, it IS the root
	if len(l.Leaves) == 1 {
		l.Root = l.Leaves[0]
		return
	}

	l.fullRebuild()
}

// fullRebuild does a complete tree rebuild — used when tree needs rebalancing.
func (l *Ledger) fullRebuild() {
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

// MerkleProof contains the sibling hashes needed to verify inclusion.
type MerkleProof struct {
	LeafHash string
	Siblings []ProofSibling
	RootHash string
}

// ProofSibling is a sibling hash and its position (left or right).
type ProofSibling struct {
	Hash   string
	IsLeft bool // true if sibling is on the left
}

// VerifyInclusion checks if a hash exists in the tree by generating a
// Merkle proof and verifying it against the current root.
// Patent Claim 14: "records the attribution to an immutable ledger"
func (l *Ledger) VerifyInclusion(hash string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.Root == nil || len(l.Leaves) == 0 {
		return false
	}

	// Find the leaf index that matches the hash
	leafIdx := -1
	for i, leaf := range l.Leaves {
		if leaf.Hash == hash {
			leafIdx = i
			break
		}
	}
	if leafIdx == -1 {
		return false
	}

	// Generate proof and verify against root
	proof := l.generateProofUnlocked(leafIdx)
	if proof == nil {
		return false
	}
	return verifyProof(proof, l.Root.Hash)
}

// GenerateProof creates a Merkle inclusion proof for a given leaf hash.
func (l *Ledger) GenerateProof(leafHash string) *MerkleProof {
	l.mu.Lock()
	defer l.mu.Unlock()

	leafIdx := -1
	for i, leaf := range l.Leaves {
		if leaf.Hash == leafHash {
			leafIdx = i
			break
		}
	}
	if leafIdx == -1 {
		return nil
	}
	return l.generateProofUnlocked(leafIdx)
}

// generateProofUnlocked creates a proof without holding the lock (caller must lock).
func (l *Ledger) generateProofUnlocked(leafIdx int) *MerkleProof {
	if leafIdx < 0 || leafIdx >= len(l.Leaves) {
		return nil
	}

	proof := &MerkleProof{
		LeafHash: l.Leaves[leafIdx].Hash,
		Siblings: make([]ProofSibling, 0),
	}
	if l.Root != nil {
		proof.RootHash = l.Root.Hash
	}

	// Walk up the tree layer by layer, collecting siblings
	nodes := make([]*MerkleNode, len(l.Leaves))
	copy(nodes, l.Leaves)
	idx := leafIdx

	for len(nodes) > 1 {
		var nextLevel []*MerkleNode
		newIdx := idx / 2

		for i := 0; i < len(nodes); i += 2 {
			left := nodes[i]
			var right *MerkleNode
			if i+1 < len(nodes) {
				right = nodes[i+1]
			} else {
				right = left // duplicated for odd count
			}

			// If this pair contains our target, record the sibling
			if i == idx || i+1 == idx {
				if i == idx {
					// Our node is on the left → sibling is right
					proof.Siblings = append(proof.Siblings, ProofSibling{
						Hash:   right.Hash,
						IsLeft: false,
					})
				} else {
					// Our node is on the right → sibling is left
					proof.Siblings = append(proof.Siblings, ProofSibling{
						Hash:   left.Hash,
						IsLeft: true,
					})
				}
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
		idx = newIdx
	}

	return proof
}

// VerifyProof verifies a Merkle proof against an expected root hash.
// This is the public entry point for external proof validation.
func VerifyProof(proof *MerkleProof, expectedRoot string) bool {
	return verifyProof(proof, expectedRoot)
}

// verifyProof recomputes the root hash from a leaf hash and its sibling path.
func verifyProof(proof *MerkleProof, expectedRoot string) bool {
	if proof == nil {
		return false
	}

	current := proof.LeafHash
	for _, sibling := range proof.Siblings {
		if sibling.IsLeft {
			current = hashData(sibling.Hash + current)
		} else {
			current = hashData(current + sibling.Hash)
		}
	}

	return current == expectedRoot
}
