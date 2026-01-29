package service

import (
	"context"
	"fmt"
	"time"

	"github.com/ocx/backend/internal/config"
	"github.com/ocx/backend/internal/database"
)

type SimulationService struct {
	configManager *config.Manager
	db            *database.SupabaseClient
}

func NewSimulationService(cm *config.Manager, db *database.SupabaseClient) *SimulationService {
	return &SimulationService{configManager: cm, db: db}
}

// RunBatchSimulation executes a scenario multiple times
func (s *SimulationService) RunBatchSimulation(ctx context.Context, tenantID, scenarioID string, batchSize int) (string, error) {
	cfg := s.configManager.Get(tenantID)

	// Enforce Limits
	limit := cfg.Simulation.MaxBatchSize
	if batchSize > limit {
		return "", fmt.Errorf("batch size %d exceeds tenant limit of %d", batchSize, limit)
	}

	// Production: Spawn background workers or call Python simulation engine
	// Mock: Generate Run ID
	runID := fmt.Sprintf("run-%s-%d", scenarioID, time.Now().Unix())

	// fmt.Printf("Running batch of %d for scenario %s (Tenant: %s)\n", batchSize, scenarioID, tenantID)

	return runID, nil
}
