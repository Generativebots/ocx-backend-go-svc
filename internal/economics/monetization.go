// Package economics provides the full AOCS monetization engine.
// Extends the basic wallet with comprehensive billing, metering, and governance tax.
package economics

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"sync"
	"time"
)

// ============================================================================
// MONETIZATION TIERS
// ============================================================================

// PricingTier defines a subscription tier
type PricingTier string

const (
	TierStartup    PricingTier = "STARTUP"    // $499/month - 1M transactions
	TierGrowth     PricingTier = "GROWTH"     // $2,499/month - 10M transactions
	TierEnterprise PricingTier = "ENTERPRISE" // Custom pricing
	TierPayAsYouGo PricingTier = "PAYG"       // Pay per transaction
)

// TierLimits defines the limits for each tier
type TierLimits struct {
	MonthlyTransactions int64
	MaxAgentsPerTenant  int
	MaxToolsPerTenant   int
	JuryPoolSize        int
	IncludesJITRewrite  bool
	IncludesFederation  bool
	SLAUptime           float64 // e.g., 0.999 for 99.9%
	PricePerMonth       float64
	OveragePerTx        float64 // Price per tx after limit
}

// GetTierLimits returns the limits for a pricing tier
func GetTierLimits(tier PricingTier) TierLimits {
	switch tier {
	case TierStartup:
		return TierLimits{
			MonthlyTransactions: 1_000_000,
			MaxAgentsPerTenant:  50,
			MaxToolsPerTenant:   100,
			JuryPoolSize:        3,
			IncludesJITRewrite:  false,
			IncludesFederation:  false,
			SLAUptime:           0.99,
			PricePerMonth:       499.00,
			OveragePerTx:        0.0005, // $0.50 per 1K
		}
	case TierGrowth:
		return TierLimits{
			MonthlyTransactions: 10_000_000,
			MaxAgentsPerTenant:  500,
			MaxToolsPerTenant:   1000,
			JuryPoolSize:        5,
			IncludesJITRewrite:  true,
			IncludesFederation:  false,
			SLAUptime:           0.999,
			PricePerMonth:       2499.00,
			OveragePerTx:        0.0003, // $0.30 per 1K
		}
	case TierEnterprise:
		return TierLimits{
			MonthlyTransactions: -1, // Unlimited (custom)
			MaxAgentsPerTenant:  -1,
			MaxToolsPerTenant:   -1,
			JuryPoolSize:        9,
			IncludesJITRewrite:  true,
			IncludesFederation:  true,
			SLAUptime:           0.9999,
			PricePerMonth:       0, // Custom
			OveragePerTx:        0,
		}
	case TierPayAsYouGo:
		return TierLimits{
			MonthlyTransactions: -1, // No limit, pay for each
			MaxAgentsPerTenant:  10,
			MaxToolsPerTenant:   50,
			JuryPoolSize:        3,
			IncludesJITRewrite:  false,
			IncludesFederation:  false,
			SLAUptime:           0.99,
			PricePerMonth:       0,
			OveragePerTx:        0.001, // $1.00 per 1K
		}
	default:
		return GetTierLimits(TierPayAsYouGo)
	}
}

// ============================================================================
// TRANSACTION TYPES
// ============================================================================

// TransactionType categorizes billable events
type TransactionType string

const (
	TxToolCall       TransactionType = "TOOL_CALL"
	TxJuryAudit      TransactionType = "JURY_AUDIT"
	TxHITLReview     TransactionType = "HITL_REVIEW"
	TxTriFactorGate  TransactionType = "TRI_FACTOR_GATE"
	TxFederation     TransactionType = "FEDERATION"
	TxAPEExtraction  TransactionType = "APE_EXTRACTION"
	TxShadowSOPLearn TransactionType = "SHADOW_SOP_LEARN"
)

// TransactionCost defines the cost multiplier for each transaction type
var TransactionCost = map[TransactionType]float64{
	TxToolCall:       1.0,  // Base cost
	TxJuryAudit:      5.0,  // Jury audits are expensive
	TxHITLReview:     10.0, // Human review is most expensive
	TxTriFactorGate:  3.0,  // Full validation
	TxFederation:     2.0,  // Cross-OCX
	TxAPEExtraction:  8.0,  // LLM-based extraction
	TxShadowSOPLearn: 4.0,  // Pattern learning
}

// ============================================================================
// USAGE RECORD
// ============================================================================

// UsageRecord tracks a single billable event
type UsageRecord struct {
	ID              string
	TenantID        string
	AgentID         string
	TransactionType TransactionType
	ToolID          string
	ActionClass     string // "A" or "B"
	Timestamp       time.Time
	Duration        time.Duration
	BaseCost        float64
	GovTax          float64 // Governance tax based on trust
	TotalCost       float64
	TrustScore      float64
	Success         bool
	Metadata        map[string]interface{}
}

// ============================================================================
// TENANT ACCOUNT
// ============================================================================

// TenantAccount represents a tenant's billing account
type TenantAccount struct {
	mu sync.RWMutex

	TenantID string
	Tier     PricingTier
	Limits   TierLimits

	// Billing period
	BillingPeriodStart time.Time
	BillingPeriodEnd   time.Time

	// Usage counters
	TransactionsThisPeriod int64
	AmountThisPeriod       float64

	// Credits/Balance
	PrepaidCredits float64
	OverageAmount  float64

	// Agent wallets
	AgentWallets map[string]*AgentWallet

	// Usage history
	UsageRecords []UsageRecord

	// Status
	Active      bool
	Suspended   bool
	LastUpdated time.Time
}

// AgentWallet represents an agent's reputation wallet within a tenant
type AgentWallet struct {
	AgentID           string
	TrustScore        float64
	ReputationBalance float64 // Reputation credits
	TotalSpent        float64
	TotalEarned       float64
	PenaltyLevel      int
	Violations        int
	LastActivity      time.Time
}

// NewTenantAccount creates a new tenant account
func NewTenantAccount(tenantID string, tier PricingTier) *TenantAccount {
	now := time.Now()
	periodEnd := time.Date(now.Year(), now.Month()+1, 1, 0, 0, 0, 0, now.Location())

	return &TenantAccount{
		TenantID:           tenantID,
		Tier:               tier,
		Limits:             GetTierLimits(tier),
		BillingPeriodStart: now,
		BillingPeriodEnd:   periodEnd,
		AgentWallets:       make(map[string]*AgentWallet),
		UsageRecords:       make([]UsageRecord, 0),
		Active:             true,
		LastUpdated:        now,
	}
}

// GetOrCreateWallet gets or creates an agent wallet
func (ta *TenantAccount) GetOrCreateWallet(agentID string, initialCredits float64) *AgentWallet {
	ta.mu.Lock()
	defer ta.mu.Unlock()

	if wallet, exists := ta.AgentWallets[agentID]; exists {
		return wallet
	}

	wallet := &AgentWallet{
		AgentID:           agentID,
		TrustScore:        1.0,
		ReputationBalance: initialCredits,
		LastActivity:      time.Now(),
	}
	ta.AgentWallets[agentID] = wallet
	return wallet
}

// ============================================================================
// MONETIZATION ENGINE
// ============================================================================

// MonetizationEngine is the central billing engine
type MonetizationEngine struct {
	mu sync.RWMutex

	accounts map[string]*TenantAccount

	// Pricing configuration
	baseTransactionCost float64

	// Governance tax configuration
	trustThreshold      float64 // Below this, governance tax applies
	maxGovTaxMultiplier float64 // Maximum tax multiplier

	// Callbacks
	onLimitReached   func(tenantID string)
	onAccountSuspend func(tenantID string)

	logger *log.Logger
}

// MonetizationConfig holds engine configuration
type MonetizationConfig struct {
	BaseTransactionCost float64
	TrustThreshold      float64
	MaxGovTaxMultiplier float64
}

// NewMonetizationEngine creates a new monetization engine
func NewMonetizationEngine(cfg MonetizationConfig) *MonetizationEngine {
	return &MonetizationEngine{
		accounts:            make(map[string]*TenantAccount),
		baseTransactionCost: cfg.BaseTransactionCost,
		trustThreshold:      cfg.TrustThreshold,
		maxGovTaxMultiplier: cfg.MaxGovTaxMultiplier,
		logger:              log.New(log.Writer(), "[Monetization] ", log.LstdFlags),
	}
}

// DefaultMonetizationEngine creates engine with default settings
func DefaultMonetizationEngine() *MonetizationEngine {
	return NewMonetizationEngine(MonetizationConfig{
		BaseTransactionCost: 0.001, // $0.001 per base transaction
		TrustThreshold:      0.80,
		MaxGovTaxMultiplier: 10.0,
	})
}

// RegisterTenant registers a new tenant account
func (me *MonetizationEngine) RegisterTenant(tenantID string, tier PricingTier) error {
	me.mu.Lock()
	defer me.mu.Unlock()

	if _, exists := me.accounts[tenantID]; exists {
		return fmt.Errorf("tenant %s already registered", tenantID)
	}

	me.accounts[tenantID] = NewTenantAccount(tenantID, tier)
	me.logger.Printf("Registered tenant: %s (tier=%s)", tenantID, tier)
	return nil
}

// GetAccount retrieves a tenant account
func (me *MonetizationEngine) GetAccount(tenantID string) (*TenantAccount, error) {
	me.mu.RLock()
	defer me.mu.RUnlock()

	account, exists := me.accounts[tenantID]
	if !exists {
		return nil, fmt.Errorf("tenant %s not found", tenantID)
	}
	return account, nil
}

// RecordTransaction records a billable transaction
func (me *MonetizationEngine) RecordTransaction(
	ctx context.Context,
	tenantID, agentID string,
	txType TransactionType,
	toolID string,
	actionClass string,
	trustScore float64,
	success bool,
) (*UsageRecord, error) {
	me.mu.Lock()
	defer me.mu.Unlock()

	account, exists := me.accounts[tenantID]
	if !exists {
		return nil, fmt.Errorf("tenant %s not found", tenantID)
	}

	if account.Suspended {
		return nil, errors.New("account suspended")
	}

	// Calculate costs
	baseCost := me.baseTransactionCost * TransactionCost[txType]

	// Apply action class multiplier (Class B = 2x)
	if actionClass == "B" {
		baseCost *= 2.0
	}

	// Calculate governance tax
	govTax := me.calculateGovernanceTax(baseCost, trustScore)

	totalCost := baseCost + govTax

	// Create usage record
	record := UsageRecord{
		ID:              fmt.Sprintf("tx-%d-%s", time.Now().UnixNano(), agentID),
		TenantID:        tenantID,
		AgentID:         agentID,
		TransactionType: txType,
		ToolID:          toolID,
		ActionClass:     actionClass,
		Timestamp:       time.Now(),
		BaseCost:        baseCost,
		GovTax:          govTax,
		TotalCost:       totalCost,
		TrustScore:      trustScore,
		Success:         success,
	}

	// Update account
	account.mu.Lock()
	account.TransactionsThisPeriod++
	account.AmountThisPeriod += totalCost
	account.UsageRecords = append(account.UsageRecords, record)
	account.LastUpdated = time.Now()

	// Check limits
	if account.Limits.MonthlyTransactions > 0 &&
		account.TransactionsThisPeriod > account.Limits.MonthlyTransactions {
		// Calculate overage
		overage := (account.TransactionsThisPeriod - account.Limits.MonthlyTransactions) *
			int64(account.Limits.OveragePerTx*1000) / 1000
		account.OverageAmount = float64(overage) * account.Limits.OveragePerTx

		if me.onLimitReached != nil {
			me.onLimitReached(tenantID)
		}
	}

	// Update agent wallet
	wallet := account.AgentWallets[agentID]
	if wallet != nil {
		wallet.TotalSpent += totalCost
		wallet.LastActivity = time.Now()
		wallet.TrustScore = trustScore
	}

	account.mu.Unlock()

	me.logger.Printf("Transaction: tenant=%s, agent=%s, type=%s, cost=%.4f (base=%.4f, tax=%.4f)",
		tenantID, agentID, txType, totalCost, baseCost, govTax)

	return &record, nil
}

// calculateGovernanceTax applies the governance tax based on trust score
func (me *MonetizationEngine) calculateGovernanceTax(baseCost, trustScore float64) float64 {
	if trustScore >= me.trustThreshold {
		return 0 // No tax for trusted agents
	}

	// Calculate tax multiplier (inverse of trust, capped)
	// As trust approaches 0, tax approaches maxGovTaxMultiplier
	trustDeficit := me.trustThreshold - trustScore
	taxMultiplier := 1.0 + (trustDeficit/me.trustThreshold)*(me.maxGovTaxMultiplier-1.0)

	// Apply exponential scaling for very low trust
	if trustScore < 0.5 {
		taxMultiplier = math.Pow(taxMultiplier, 1.5)
	}

	// Cap at max multiplier
	if taxMultiplier > me.maxGovTaxMultiplier {
		taxMultiplier = me.maxGovTaxMultiplier
	}

	return baseCost * (taxMultiplier - 1.0)
}

// CalculateBill generates the bill for a tenant
func (me *MonetizationEngine) CalculateBill(tenantID string) (*Bill, error) {
	me.mu.RLock()
	defer me.mu.RUnlock()

	account, exists := me.accounts[tenantID]
	if !exists {
		return nil, fmt.Errorf("tenant %s not found", tenantID)
	}

	account.mu.RLock()
	defer account.mu.RUnlock()

	bill := &Bill{
		TenantID:          tenantID,
		Tier:              account.Tier,
		PeriodStart:       account.BillingPeriodStart,
		PeriodEnd:         account.BillingPeriodEnd,
		Transactions:      account.TransactionsThisPeriod,
		BaseAmount:        account.Limits.PricePerMonth,
		UsageAmount:       account.AmountThisPeriod,
		OverageAmount:     account.OverageAmount,
		CreditsApplied:    0,
		TotalAmount:       0,
		ByTransactionType: make(map[TransactionType]BillLineItem),
	}

	// Aggregate by transaction type
	for _, record := range account.UsageRecords {
		item := bill.ByTransactionType[record.TransactionType]
		item.Count++
		item.BaseCost += record.BaseCost
		item.GovTax += record.GovTax
		item.TotalCost += record.TotalCost
		bill.ByTransactionType[record.TransactionType] = item
	}

	// Calculate total
	bill.TotalAmount = bill.BaseAmount + bill.OverageAmount

	// For PAYG, add all usage
	if account.Tier == TierPayAsYouGo {
		bill.TotalAmount = bill.UsageAmount
	}

	// Apply prepaid credits
	if account.PrepaidCredits > 0 {
		if account.PrepaidCredits >= bill.TotalAmount {
			bill.CreditsApplied = bill.TotalAmount
			bill.TotalAmount = 0
		} else {
			bill.CreditsApplied = account.PrepaidCredits
			bill.TotalAmount -= account.PrepaidCredits
		}
	}

	return bill, nil
}

// Bill represents a billing statement
type Bill struct {
	TenantID          string
	Tier              PricingTier
	PeriodStart       time.Time
	PeriodEnd         time.Time
	Transactions      int64
	BaseAmount        float64 // Subscription fee
	UsageAmount       float64 // Total usage
	OverageAmount     float64 // Over limit charges
	CreditsApplied    float64
	TotalAmount       float64
	ByTransactionType map[TransactionType]BillLineItem
}

// BillLineItem represents a line item in the bill
type BillLineItem struct {
	Count     int64
	BaseCost  float64
	GovTax    float64
	TotalCost float64
}

// AddCredits adds prepaid credits to an account
func (me *MonetizationEngine) AddCredits(tenantID string, amount float64) error {
	me.mu.Lock()
	defer me.mu.Unlock()

	account, exists := me.accounts[tenantID]
	if !exists {
		return fmt.Errorf("tenant %s not found", tenantID)
	}

	account.mu.Lock()
	account.PrepaidCredits += amount
	account.mu.Unlock()

	me.logger.Printf("Added credits: tenant=%s, amount=%.2f", tenantID, amount)
	return nil
}

// UpdateTier changes a tenant's pricing tier
func (me *MonetizationEngine) UpdateTier(tenantID string, newTier PricingTier) error {
	me.mu.Lock()
	defer me.mu.Unlock()

	account, exists := me.accounts[tenantID]
	if !exists {
		return fmt.Errorf("tenant %s not found", tenantID)
	}

	account.mu.Lock()
	account.Tier = newTier
	account.Limits = GetTierLimits(newTier)
	account.mu.Unlock()

	me.logger.Printf("Updated tier: tenant=%s, newTier=%s", tenantID, newTier)
	return nil
}

// SuspendAccount suspends a tenant account
func (me *MonetizationEngine) SuspendAccount(tenantID string) error {
	me.mu.Lock()
	defer me.mu.Unlock()

	account, exists := me.accounts[tenantID]
	if !exists {
		return fmt.Errorf("tenant %s not found", tenantID)
	}

	account.mu.Lock()
	account.Suspended = true
	account.mu.Unlock()

	if me.onAccountSuspend != nil {
		me.onAccountSuspend(tenantID)
	}

	me.logger.Printf("Suspended account: tenant=%s", tenantID)
	return nil
}

// ReactivateAccount reactivates a suspended account
func (me *MonetizationEngine) ReactivateAccount(tenantID string) error {
	me.mu.Lock()
	defer me.mu.Unlock()

	account, exists := me.accounts[tenantID]
	if !exists {
		return fmt.Errorf("tenant %s not found", tenantID)
	}

	account.mu.Lock()
	account.Suspended = false
	account.mu.Unlock()

	me.logger.Printf("Reactivated account: tenant=%s", tenantID)
	return nil
}

// GetUsageStats returns usage statistics for a tenant
func (me *MonetizationEngine) GetUsageStats(tenantID string) (*UsageStats, error) {
	me.mu.RLock()
	defer me.mu.RUnlock()

	account, exists := me.accounts[tenantID]
	if !exists {
		return nil, fmt.Errorf("tenant %s not found", tenantID)
	}

	account.mu.RLock()
	defer account.mu.RUnlock()

	stats := &UsageStats{
		TenantID:         tenantID,
		Tier:             account.Tier,
		TransactionCount: account.TransactionsThisPeriod,
		TotalCost:        account.AmountThisPeriod,
		ByType:           make(map[TransactionType]int64),
		ByAgent:          make(map[string]int64),
		TransactionLimit: account.Limits.MonthlyTransactions,
		PercentUsed:      0,
	}

	for _, record := range account.UsageRecords {
		stats.ByType[record.TransactionType]++
		stats.ByAgent[record.AgentID]++
	}

	if account.Limits.MonthlyTransactions > 0 {
		stats.PercentUsed = float64(account.TransactionsThisPeriod) /
			float64(account.Limits.MonthlyTransactions) * 100
	}

	return stats, nil
}

// UsageStats contains usage statistics
type UsageStats struct {
	TenantID         string
	Tier             PricingTier
	TransactionCount int64
	TotalCost        float64
	ByType           map[TransactionType]int64
	ByAgent          map[string]int64
	TransactionLimit int64
	PercentUsed      float64
}

// SetCallbacks sets event callbacks
func (me *MonetizationEngine) SetCallbacks(
	onLimitReached func(tenantID string),
	onAccountSuspend func(tenantID string),
) {
	me.onLimitReached = onLimitReached
	me.onAccountSuspend = onAccountSuspend
}
