package service

import (
	"context"
	"math/rand"

	"github.com/ocx/backend/internal/config"
	"github.com/ocx/backend/internal/database"
)

type ImpactService struct {
	configManager *config.Manager
	db            *database.SupabaseClient
}

func NewImpactService(cm *config.Manager, db *database.SupabaseClient) *ImpactService {
	return &ImpactService{configManager: cm, db: db}
}

// CalculateROI runs Monte Carlo simulation to estimate impact
func (s *ImpactService) CalculateROI(ctx context.Context, tenantID string, assumptions map[string]float64) map[string]interface{} {
	cfg := s.configManager.Get(tenantID)
	iterations := cfg.Impact.MonteCarloIterations

	// Production: Run extensive Monte Carlo logic
	// Mock Monte Carlo: Run N iterations adding random noise to base value
	baseValue := assumptions["base_value"]
	var total float64

	for i := 0; i < iterations; i++ {
		// Random variation +/- 10%
		variation := 0.9 + (rand.Float64() * 0.2)
		total += baseValue * variation
	}

	avg := total / float64(iterations)

	return map[string]interface{}{
		"iterations":    iterations,
		"estimated_roi": avg * 1.5, // Mock formula
		"risk_factor":   0.12,
	}
}
