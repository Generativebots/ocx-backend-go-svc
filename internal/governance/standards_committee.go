package governance

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// ============================================================================
// OCX GOVERNANCE STRUCTURE
// ============================================================================

// StandardsCommittee manages protocol evolution and dispute resolution
type StandardsCommittee struct {
	mu sync.RWMutex

	// Committee members
	members map[string]*CommitteeMember

	// Protocol versions
	versions map[string]*ProtocolVersion

	// Proposals
	proposals map[string]*Proposal

	// Voting records
	votes map[string]*VoteRecord
}

// CommitteeMember represents a member of the standards committee
type CommitteeMember struct {
	MemberID     string
	Organization string
	Role         string // "chair", "vice-chair", "member"
	JoinedAt     time.Time
	VotingPower  int // 1 for regular members, 2 for chairs
	Active       bool
}

// ProtocolVersion represents a version of the OCX protocol
type ProtocolVersion struct {
	Version         string
	Status          string // "draft", "proposed", "approved", "deprecated"
	ReleaseDate     time.Time
	DeprecationDate *time.Time
	BackwardCompat  bool
	BreakingChanges []string
	Features        []string
	ApprovedBy      []string
	VoteCount       int
	RequiredVotes   int
}

// Proposal represents a protocol change proposal
type Proposal struct {
	ProposalID     string
	Title          string
	Description    string
	ProposedBy     string
	ProposedAt     time.Time
	Type           string // "feature", "bugfix", "breaking", "deprecation"
	TargetVersion  string
	Status         string // "draft", "voting", "approved", "rejected", "implemented"
	VotingDeadline time.Time
	RequiredVotes  int
	CurrentVotes   int
	YesVotes       int
	NoVotes        int
	AbstainVotes   int
	ImplementedAt  *time.Time
}

// VoteRecord tracks voting on proposals
type VoteRecord struct {
	ProposalID string
	MemberID   string
	Vote       string // "yes", "no", "abstain"
	VotedAt    time.Time
	Comment    string
}

// NewStandardsCommittee creates a new standards committee
func NewStandardsCommittee() *StandardsCommittee {
	sc := &StandardsCommittee{
		members:   make(map[string]*CommitteeMember),
		versions:  make(map[string]*ProtocolVersion),
		proposals: make(map[string]*Proposal),
		votes:     make(map[string]*VoteRecord),
	}

	// Initialize with 22 founding members
	sc.initializeFoundingMembers()

	// Initialize version roadmap
	sc.initializeVersionRoadmap()

	return sc
}

// initializeFoundingMembers creates the 22-member committee
func (sc *StandardsCommittee) initializeFoundingMembers() {
	foundingMembers := []struct {
		id   string
		org  string
		role string
	}{
		// Chairs (2)
		{"member-001", "OCX Foundation", "chair"},
		{"member-002", "OCX Foundation", "vice-chair"},

		// Technical Working Group (8)
		{"member-003", "Google", "member"},
		{"member-004", "Microsoft", "member"},
		{"member-005", "Amazon", "member"},
		{"member-006", "Meta", "member"},
		{"member-007", "IBM", "member"},
		{"member-008", "Oracle", "member"},
		{"member-009", "Salesforce", "member"},
		{"member-010", "SAP", "member"},

		// Security & Compliance (4)
		{"member-011", "NIST", "member"},
		{"member-012", "ISO", "member"},
		{"member-013", "OWASP", "member"},
		{"member-014", "Cloud Security Alliance", "member"},

		// Industry Representatives (4)
		{"member-015", "Financial Services", "member"},
		{"member-016", "Healthcare", "member"},
		{"member-017", "Government", "member"},
		{"member-018", "Manufacturing", "member"},

		// Academic & Research (4)
		{"member-019", "MIT", "member"},
		{"member-020", "Stanford", "member"},
		{"member-021", "Berkeley", "member"},
		{"member-022", "CMU", "member"},
	}

	for _, m := range foundingMembers {
		votingPower := 1
		if m.role == "chair" || m.role == "vice-chair" {
			votingPower = 2
		}

		sc.members[m.id] = &CommitteeMember{
			MemberID:     m.id,
			Organization: m.org,
			Role:         m.role,
			JoinedAt:     time.Now(),
			VotingPower:  votingPower,
			Active:       true,
		}
	}
}

// initializeVersionRoadmap sets up the version control roadmap
func (sc *StandardsCommittee) initializeVersionRoadmap() {
	// v1.0 - Current stable version
	sc.versions["1.0"] = &ProtocolVersion{
		Version:         "1.0",
		Status:          "approved",
		ReleaseDate:     time.Now().Add(-6 * 30 * 24 * time.Hour), // 6 months ago
		BackwardCompat:  true,
		BreakingChanges: []string{},
		Features: []string{
			"6-step handshake",
			"Weighted trust calculation",
			"SPIFFE authentication",
			"Trust attestation ledger",
		},
		ApprovedBy:    []string{"member-001", "member-002"},
		VoteCount:     22,
		RequiredVotes: 15, // 2/3 majority
	}

	// v1.1 - Proposed minor update
	sc.versions["1.1"] = &ProtocolVersion{
		Version:         "1.1",
		Status:          "proposed",
		ReleaseDate:     time.Now().Add(3 * 30 * 24 * time.Hour), // 3 months from now
		BackwardCompat:  true,
		BreakingChanges: []string{},
		Features: []string{
			"Enhanced reputation scoring",
			"Multi-region support",
			"Session caching",
		},
		RequiredVotes: 15,
	}

	// v2.0 - Future major version
	sc.versions["2.0"] = &ProtocolVersion{
		Version:        "2.0",
		Status:         "draft",
		ReleaseDate:    time.Now().Add(12 * 30 * 24 * time.Hour), // 12 months from now
		BackwardCompat: false,
		BreakingChanges: []string{
			"New message format",
			"Quantum-resistant cryptography",
			"Multi-party attestation",
		},
		Features: []string{
			"Post-quantum cryptography",
			"Zero-trust architecture",
			"Decentralized governance",
		},
		RequiredVotes: 18, // Higher threshold for breaking changes
	}
}

// ProposeChange creates a new protocol change proposal
func (sc *StandardsCommittee) ProposeChange(ctx context.Context, proposal *Proposal) error {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	// Validate proposer is a committee member
	member, ok := sc.members[proposal.ProposedBy]
	if !ok || !member.Active {
		return errors.New("proposer is not an active committee member")
	}

	// Set defaults
	proposal.Status = "voting"
	proposal.ProposedAt = time.Now()
	proposal.VotingDeadline = time.Now().Add(14 * 24 * time.Hour) // 2 weeks
	proposal.RequiredVotes = 15                                   // 2/3 of 22 members

	// Higher threshold for breaking changes
	if proposal.Type == "breaking" {
		proposal.RequiredVotes = 18 // ~80% majority
	}

	sc.proposals[proposal.ProposalID] = proposal

	return nil
}

// Vote records a vote on a proposal
func (sc *StandardsCommittee) Vote(ctx context.Context, proposalID, memberID, vote, comment string) error {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	// Validate proposal exists
	proposal, ok := sc.proposals[proposalID]
	if !ok {
		return fmt.Errorf("proposal not found: %s", proposalID)
	}

	// Validate member
	member, ok := sc.members[memberID]
	if !ok || !member.Active {
		return errors.New("member is not active")
	}

	// Check voting deadline
	if time.Now().After(proposal.VotingDeadline) {
		return errors.New("voting deadline has passed")
	}

	// Check if already voted
	voteKey := fmt.Sprintf("%s:%s", proposalID, memberID)
	if _, exists := sc.votes[voteKey]; exists {
		return errors.New("member has already voted")
	}

	// Record vote
	sc.votes[voteKey] = &VoteRecord{
		ProposalID: proposalID,
		MemberID:   memberID,
		Vote:       vote,
		VotedAt:    time.Now(),
		Comment:    comment,
	}

	// Update proposal vote counts
	proposal.CurrentVotes += member.VotingPower

	switch vote {
	case "yes":
		proposal.YesVotes += member.VotingPower
	case "no":
		proposal.NoVotes += member.VotingPower
	case "abstain":
		proposal.AbstainVotes += member.VotingPower
	}

	// Check if proposal passed
	if proposal.YesVotes >= proposal.RequiredVotes {
		proposal.Status = "approved"
		sc.approveProposal(proposal)
	} else if proposal.NoVotes > (24 - proposal.RequiredVotes) { // Can't reach required votes
		proposal.Status = "rejected"
	}

	return nil
}

// approveProposal implements an approved proposal
func (sc *StandardsCommittee) approveProposal(proposal *Proposal) {
	// Update target version
	if version, ok := sc.versions[proposal.TargetVersion]; ok {
		version.Features = append(version.Features, proposal.Title)
		if proposal.Type == "breaking" {
			version.BreakingChanges = append(version.BreakingChanges, proposal.Title)
			version.BackwardCompat = false
		}
	}

	now := time.Now()
	proposal.ImplementedAt = &now
	proposal.Status = "implemented"
}

// GetVersion returns information about a protocol version
func (sc *StandardsCommittee) GetVersion(version string) (*ProtocolVersion, error) {
	sc.mu.RLock()
	defer sc.mu.RUnlock()

	v, ok := sc.versions[version]
	if !ok {
		return nil, fmt.Errorf("version not found: %s", version)
	}

	return v, nil
}

// IsBackwardCompatible checks if a version is backward compatible
func (sc *StandardsCommittee) IsBackwardCompatible(fromVersion, toVersion string) (bool, error) {
	sc.mu.RLock()
	defer sc.mu.RUnlock()

	to, ok := sc.versions[toVersion]
	if !ok {
		return false, fmt.Errorf("version not found: %s", toVersion)
	}

	return to.BackwardCompat, nil
}

// GetActiveProposals returns all proposals currently in voting
func (sc *StandardsCommittee) GetActiveProposals() []*Proposal {
	sc.mu.RLock()
	defer sc.mu.RUnlock()

	var active []*Proposal
	for _, p := range sc.proposals {
		if p.Status == "voting" && time.Now().Before(p.VotingDeadline) {
			active = append(active, p)
		}
	}

	return active
}

// GetCommitteeMembers returns all committee members
func (sc *StandardsCommittee) GetCommitteeMembers() []*CommitteeMember {
	sc.mu.RLock()
	defer sc.mu.RUnlock()

	members := make([]*CommitteeMember, 0, len(sc.members))
	for _, m := range sc.members {
		if m.Active {
			members = append(members, m)
		}
	}

	return members
}

// GetVersionRoadmap returns the version control roadmap
func (sc *StandardsCommittee) GetVersionRoadmap() []*ProtocolVersion {
	sc.mu.RLock()
	defer sc.mu.RUnlock()

	versions := make([]*ProtocolVersion, 0, len(sc.versions))
	for _, v := range sc.versions {
		versions = append(versions, v)
	}

	return versions
}
