// Package marketplace — Revenue Share Manager (Gap 2 Fix)
//
// Tracks publisher revenue from marketplace item installs and subscriptions
// with a configurable commission split (default 70/30 publisher/platform).
package marketplace

import (
	"fmt"
	"sync"
	"time"
)

// CommissionRate is the OCX platform's default revenue share (30%).
const CommissionRate = 0.30

// RevenueEntry represents a single revenue transaction for a publisher.
type RevenueEntry struct {
	ID              string    `json:"id"`
	PublisherID     string    `json:"publisher_id"`
	ItemType        string    `json:"item_type"`
	ItemID          string    `json:"item_id"`
	ItemName        string    `json:"item_name"`
	TransactionType string    `json:"transaction_type"` // install, subscription, usage
	BuyerTenantID   string    `json:"buyer_tenant_id"`
	GrossCredits    int       `json:"gross_credits"`
	OCXCommission   int       `json:"ocx_commission"`
	PublisherPayout int       `json:"publisher_payout"`
	Rate            float64   `json:"commission_rate"`
	CreatedAt       time.Time `json:"created_at"`
}

// PublisherSummary provides an aggregate view of publisher earnings.
type PublisherSummary struct {
	PublisherID     string         `json:"publisher_id"`
	TotalGross      int            `json:"total_gross"`
	TotalCommission int            `json:"total_commission"`
	TotalPayout     int            `json:"total_payout"`
	TotalInstalls   int            `json:"total_installs"`
	ItemBreakdown   []ItemRevenue  `json:"item_breakdown"`
	RecentEntries   []RevenueEntry `json:"recent_entries"`
}

// ItemRevenue is per-item revenue breakdown.
type ItemRevenue struct {
	ItemID       string `json:"item_id"`
	ItemName     string `json:"item_name"`
	ItemType     string `json:"item_type"`
	Installs     int    `json:"installs"`
	GrossCredits int    `json:"gross_credits"`
	Payout       int    `json:"payout"`
}

// PublisherAnalytics is the analytics dashboard data for publishers.
type PublisherAnalytics struct {
	PublisherID   string        `json:"publisher_id"`
	TotalItems    int           `json:"total_items"`
	TotalInstalls int           `json:"total_installs"`
	TotalRevenue  int           `json:"total_revenue"`
	AvgRating     float64       `json:"avg_rating"`
	MonthlySeries []MonthlyData `json:"monthly_series"`
	TopItems      []ItemRevenue `json:"top_items"`
}

// MonthlyData provides month-over-month revenue data.
type MonthlyData struct {
	Month    string `json:"month"`
	Installs int    `json:"installs"`
	Revenue  int    `json:"revenue"`
}

// RevenueShareManager manages publisher revenue share calculations.
type RevenueShareManager struct {
	mu      sync.RWMutex
	entries []RevenueEntry
	svc     *Service
	nextID  int
}

// NewRevenueShareManager creates a new revenue share manager.
func NewRevenueShareManager(svc *Service) *RevenueShareManager {
	return &RevenueShareManager{
		entries: make([]RevenueEntry, 0),
		svc:     svc,
	}
}

// RecordInstallRevenue records revenue when a tenant installs a marketplace item.
func (rsm *RevenueShareManager) RecordInstallRevenue(buyerTenantID string, itemType ItemType, itemID string) {
	rsm.mu.Lock()
	defer rsm.mu.Unlock()

	rsm.svc.mu.RLock()
	var grossCredits int
	var publisherID, itemName string

	switch itemType {
	case ItemTypeConnector:
		conn, ok := rsm.svc.connectors[itemID]
		if !ok {
			rsm.svc.mu.RUnlock()
			return
		}
		grossCredits = conn.MonthlyCredits
		publisherID = conn.PublisherID
		itemName = conn.Name
	case ItemTypeTemplate:
		tmpl, ok := rsm.svc.templates[itemID]
		if !ok {
			rsm.svc.mu.RUnlock()
			return
		}
		grossCredits = tmpl.OneTimeCredits
		publisherID = tmpl.PublisherID
		itemName = tmpl.Name
	default:
		rsm.svc.mu.RUnlock()
		return
	}
	rsm.svc.mu.RUnlock()

	if grossCredits == 0 {
		return // Free items generate no revenue
	}

	commission := int(float64(grossCredits) * CommissionRate)
	payout := grossCredits - commission

	rsm.nextID++
	entry := RevenueEntry{
		ID:              fmt.Sprintf("rev-%d", rsm.nextID),
		PublisherID:     publisherID,
		ItemType:        string(itemType),
		ItemID:          itemID,
		ItemName:        itemName,
		TransactionType: "install",
		BuyerTenantID:   buyerTenantID,
		GrossCredits:    grossCredits,
		OCXCommission:   commission,
		PublisherPayout: payout,
		Rate:            CommissionRate,
		CreatedAt:       time.Now(),
	}

	rsm.entries = append(rsm.entries, entry)
}

// GetPublisherSummary returns aggregate revenue data for a publisher.
func (rsm *RevenueShareManager) GetPublisherSummary(publisherID string) *PublisherSummary {
	rsm.mu.RLock()
	defer rsm.mu.RUnlock()

	summary := &PublisherSummary{
		PublisherID:   publisherID,
		ItemBreakdown: make([]ItemRevenue, 0),
		RecentEntries: make([]RevenueEntry, 0),
	}

	itemMap := make(map[string]*ItemRevenue)

	for _, entry := range rsm.entries {
		if entry.PublisherID != publisherID {
			continue
		}
		summary.TotalGross += entry.GrossCredits
		summary.TotalCommission += entry.OCXCommission
		summary.TotalPayout += entry.PublisherPayout
		summary.TotalInstalls++

		key := entry.ItemID
		if _, exists := itemMap[key]; !exists {
			itemMap[key] = &ItemRevenue{
				ItemID:   entry.ItemID,
				ItemName: entry.ItemName,
				ItemType: entry.ItemType,
			}
		}
		itemMap[key].Installs++
		itemMap[key].GrossCredits += entry.GrossCredits
		itemMap[key].Payout += entry.PublisherPayout

		summary.RecentEntries = append(summary.RecentEntries, entry)
	}

	for _, item := range itemMap {
		summary.ItemBreakdown = append(summary.ItemBreakdown, *item)
	}

	// Limit recent entries to last 20
	if len(summary.RecentEntries) > 20 {
		summary.RecentEntries = summary.RecentEntries[len(summary.RecentEntries)-20:]
	}

	return summary
}

// GetPublisherAnalytics returns dashboard analytics for a publisher.
func (rsm *RevenueShareManager) GetPublisherAnalytics(publisherID string) *PublisherAnalytics {
	rsm.mu.RLock()
	defer rsm.mu.RUnlock()

	analytics := &PublisherAnalytics{
		PublisherID:   publisherID,
		MonthlySeries: make([]MonthlyData, 0),
		TopItems:      make([]ItemRevenue, 0),
	}

	// Count publisher's items
	rsm.svc.mu.RLock()
	for _, c := range rsm.svc.connectors {
		if c.PublisherID == publisherID {
			analytics.TotalItems++
			analytics.TotalInstalls += c.InstallCount
			analytics.AvgRating += c.Rating
		}
	}
	for _, t := range rsm.svc.templates {
		if t.PublisherID == publisherID {
			analytics.TotalItems++
			analytics.TotalInstalls += t.InstallCount
			analytics.AvgRating += t.Rating
		}
	}
	rsm.svc.mu.RUnlock()

	if analytics.TotalItems > 0 {
		analytics.AvgRating /= float64(analytics.TotalItems)
	}

	// Revenue from entries
	monthlyMap := make(map[string]*MonthlyData)
	itemMap := make(map[string]*ItemRevenue)

	for _, entry := range rsm.entries {
		if entry.PublisherID != publisherID {
			continue
		}
		analytics.TotalRevenue += entry.PublisherPayout

		monthKey := entry.CreatedAt.Format("2006-01")
		if _, ok := monthlyMap[monthKey]; !ok {
			monthlyMap[monthKey] = &MonthlyData{Month: monthKey}
		}
		monthlyMap[monthKey].Installs++
		monthlyMap[monthKey].Revenue += entry.PublisherPayout

		if _, ok := itemMap[entry.ItemID]; !ok {
			itemMap[entry.ItemID] = &ItemRevenue{
				ItemID:   entry.ItemID,
				ItemName: entry.ItemName,
				ItemType: entry.ItemType,
			}
		}
		itemMap[entry.ItemID].Installs++
		itemMap[entry.ItemID].GrossCredits += entry.GrossCredits
		itemMap[entry.ItemID].Payout += entry.PublisherPayout
	}

	for _, m := range monthlyMap {
		analytics.MonthlySeries = append(analytics.MonthlySeries, *m)
	}
	for _, item := range itemMap {
		analytics.TopItems = append(analytics.TopItems, *item)
	}

	return analytics
}
