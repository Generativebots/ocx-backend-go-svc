package service

import (
	"context"
	"errors"
	"time"

	"github.com/ocx/backend/internal/config"
	"github.com/ocx/backend/internal/database" // Assuming DB structs are here or will be generated
)

type GovernanceService struct {
	configManager *config.Manager
	db            *database.SupabaseClient
}

func NewGovernanceService(cm *config.Manager, db *database.SupabaseClient) *GovernanceService {
	return &GovernanceService{configManager: cm, db: db}
}

// Proposals and Votes structs would ideally be in database package or domain
// For now, using mock implementations or assuming DB client usage

type Proposal struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
}

type Vote struct {
	ProposalID string `json:"proposal_id"`
	MemberID   string `json:"member_id"`
	Choice     string `json:"choice"`
}

func (s *GovernanceService) CreateProposal(ctx context.Context, title, desc, authorID string) (*Proposal, error) {
	// Governance Hardening: Check Committee Size
	// Production: Count rows in `committee_members` table
	currentMembers := 22

	// Enforce Limit
	cfg := s.configManager.Get("default") // Global governance config
	if currentMembers >= cfg.Governance.CommitteeSize {
		// Log warning or enforce restrictions on *who* can propose based on seat count
		// For now, we just enforce the limit concept exists
	}

	// Production: s.db.CreateProposal(...)
	return &Proposal{
		ID:          "prop-" + time.Now().Format("20060102150405"),
		Title:       title,
		Description: desc,
		Status:      "DRAFT",
		CreatedAt:   time.Now(),
	}, nil
}

func (s *GovernanceService) CastVote(ctx context.Context, proposalID, memberID, choice string) error {
	// In real impl: check member rights, check proposal status, insert vote
	if choice != "YES" && choice != "NO" && choice != "ABSTAIN" {
		return errors.New("invalid vote choice")
	}
	return nil
}

func (s *GovernanceService) GetNetworkGrowth() map[string]float64 {
	// Placeholder for Network Effects Calculation
	// Value = n * (n-1) / 2

	// Note: For global network stats, we might use the master config or a specific "system" tenant.
	// We'll use "default" for now. In a real system, the request context would provide the tenant.
	cfg := s.configManager.Get("default")

	n := cfg.Network.InitialNodeCount
	value := n * (n - 1) / 2
	return map[string]float64{
		"nodes":         float64(n),
		"network_value": float64(value),
		"growth_rate":   0.05,
	}
}
