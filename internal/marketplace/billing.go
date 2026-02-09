// Package marketplace — BillingIntegration (Phase B.5)
//
// Tracks credit spend for marketplace items per tenant and provides
// billing summaries for the monetization engine.
package marketplace

import (
	"fmt"
	"sync"
	"time"
)

// BillingIntegration tracks per-tenant marketplace billing.
type BillingIntegration struct {
	svc    *Service
	mu     sync.Mutex
	ledger map[string][]*BillingEntry // tenantID -> entries
}

// BillingEntry records a single charge event.
type BillingEntry struct {
	ID          string    `json:"id"`
	TenantID    string    `json:"tenant_id"`
	ItemID      string    `json:"item_id"`
	ItemName    string    `json:"item_name"`
	ItemType    ItemType  `json:"item_type"`
	Credits     int       `json:"credits"`
	Description string    `json:"description"`
	Timestamp   time.Time `json:"timestamp"`
}

// TenantBillingSummary is the aggregate billing info for a tenant.
type TenantBillingSummary struct {
	TenantID       string `json:"tenant_id"`
	TotalCredits   int    `json:"total_credits"`
	ConnectorSpend int    `json:"connector_spend"`
	TemplateSpend  int    `json:"template_spend"`
	ActiveInstalls int    `json:"active_installs"`
	EntryCount     int    `json:"entry_count"`
}

// NewBillingIntegration creates a new billing integration.
func NewBillingIntegration(svc *Service) *BillingIntegration {
	return &BillingIntegration{
		svc:    svc,
		ledger: make(map[string][]*BillingEntry),
	}
}

// ChargeOnInstall records a credit charge when an item is installed.
func (bi *BillingIntegration) ChargeOnInstall(tenantID string, itemType ItemType, itemID, itemName string, credits int) error {
	bi.mu.Lock()
	defer bi.mu.Unlock()

	entry := &BillingEntry{
		ID:          fmt.Sprintf("bill-%s-%d", tenantID, time.Now().UnixNano()),
		TenantID:    tenantID,
		ItemID:      itemID,
		ItemName:    itemName,
		ItemType:    itemType,
		Credits:     credits,
		Description: fmt.Sprintf("Installation: %s", itemName),
		Timestamp:   time.Now(),
	}

	bi.ledger[tenantID] = append(bi.ledger[tenantID], entry)
	bi.svc.logger.Printf("Charged %d credits to tenant %s for %s", credits, tenantID, itemName)
	return nil
}

// ChargeMonthly records a monthly recurring charge for active connectors.
func (bi *BillingIntegration) ChargeMonthly(tenantID string) (int, error) {
	bi.svc.mu.RLock()
	installations := bi.svc.installations[tenantID]
	bi.svc.mu.RUnlock()

	bi.mu.Lock()
	defer bi.mu.Unlock()

	totalCharged := 0

	for _, inst := range installations {
		if inst.Status != "active" || inst.ItemType != ItemTypeConnector {
			continue
		}

		bi.svc.mu.RLock()
		connector, ok := bi.svc.connectors[inst.ItemID]
		bi.svc.mu.RUnlock()

		if !ok || connector.MonthlyCredits <= 0 {
			continue
		}

		entry := &BillingEntry{
			ID:          fmt.Sprintf("bill-%s-%d", tenantID, time.Now().UnixNano()),
			TenantID:    tenantID,
			ItemID:      inst.ItemID,
			ItemName:    inst.ItemName,
			ItemType:    ItemTypeConnector,
			Credits:     connector.MonthlyCredits,
			Description: fmt.Sprintf("Monthly: %s", inst.ItemName),
			Timestamp:   time.Now(),
		}

		bi.ledger[tenantID] = append(bi.ledger[tenantID], entry)
		totalCharged += connector.MonthlyCredits
	}

	return totalCharged, nil
}

// GetSummary returns the billing summary for a tenant.
func (bi *BillingIntegration) GetSummary(tenantID string) *TenantBillingSummary {
	bi.mu.Lock()
	defer bi.mu.Unlock()

	summary := &TenantBillingSummary{TenantID: tenantID}

	for _, entry := range bi.ledger[tenantID] {
		summary.TotalCredits += entry.Credits
		if entry.ItemType == ItemTypeConnector {
			summary.ConnectorSpend += entry.Credits
		} else {
			summary.TemplateSpend += entry.Credits
		}
		summary.EntryCount++
	}

	bi.svc.mu.RLock()
	for _, inst := range bi.svc.installations[tenantID] {
		if inst.Status == "active" {
			summary.ActiveInstalls++
		}
	}
	bi.svc.mu.RUnlock()

	return summary
}
