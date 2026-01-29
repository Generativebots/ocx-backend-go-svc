package service

import (
	"context"

	"github.com/ocx/backend/internal/config"
	"github.com/ocx/backend/internal/database"
)

type BillingService struct {
	configManager *config.Manager
	db            *database.SupabaseClient
}

func NewBillingService(cm *config.Manager, db *database.SupabaseClient) *BillingService {
	return &BillingService{configManager: cm, db: db}
}

// LogTransaction calculates tax and logs the event
func (s *BillingService) LogTransaction(ctx context.Context, tenantID, requestID string, trustScore float64) (float64, error) {
	// 1. Calculate Trust Tax
	// Formula: (1.0 - TrustScore) * Value
	// Higher trust = Lower tax.
	// For MVP, we assume a standard "Value" of 1.0 Coin/Credit per interaction.
	transactionValue := 1.0

	// Ensure score is 0-1 range
	normalizedScore := trustScore
	if normalizedScore > 1.0 {
		normalizedScore = normalizedScore / 100.0
	}

	trustTax := (1.0 - normalizedScore) * transactionValue
	if trustTax < 0 {
		trustTax = 0
	}

	// 2. Log to Database
	// Production: This should be asynchronous/buffered
	// s.db.Insert("billing_transactions", ...)

	// fmt.Printf("💰 Billing [Tenant: %s]: Score %.2f -> Tax %.4f\n", tenantID, normalizedScore, trustTax)

	return trustTax, nil
}
