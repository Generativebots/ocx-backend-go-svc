package service

import (
	"context"
	"fmt"
	"time"

	"github.com/ocx/backend/internal/config"
	"github.com/ocx/backend/internal/database"
)

type ContractService struct {
	configManager *config.Manager
	db            *database.SupabaseClient
}

func NewContractService(cm *config.Manager, db *database.SupabaseClient) *ContractService {
	return &ContractService{configManager: cm, db: db}
}

// Contract represents an active EBCL contract
type ContractDeployment struct {
	ContractID string `json:"contract_id"`
	TenantID   string `json:"tenant_id"`
	Name       string `json:"name"`
	EBCLSource string `json:"ebcl_source"`
}

// ValidateInteraction checks if a contract allows an interaction
func (s *ContractService) ValidateInteraction(ctx context.Context, tenantID, useCaseKey string) (bool, error) {
	cfg := s.configManager.Get(tenantID)

	// If linking is not enforced, assume valid
	if !cfg.Contracts.EnforceUseCaseLink {
		return true, nil
	}

	// 1. Check if Use Case is Linked to a Contact
	// Production: Query `use_case_links` table
	if useCaseKey == "finance.audit.daily" {
		return true, nil
	}

	return false, fmt.Errorf("use case '%s' is not linked to any active contract", useCaseKey)
}

// DeployContract simulates deploying an EBCL contract
func (s *ContractService) DeployContract(ctx context.Context, tenantID, name, ebcl string) (string, error) {
	// Production: Insert into `contract_deployments`
	id := fmt.Sprintf("contract-%d", time.Now().Unix())
	return id, nil
}
