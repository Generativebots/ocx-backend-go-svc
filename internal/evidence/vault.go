// Package evidence implements the Evidence Vault for cryptographic audit trails.
// This provides tamper-evident storage and compliance reporting for AOCS transactions.
package evidence

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"
)

// ============================================================================
// EVIDENCE TYPES
// ============================================================================

// EvidenceType categorizes the kind of evidence
type EvidenceType string

const (
	EvidenceTransaction   EvidenceType = "TRANSACTION"   // Agent action
	EvidenceTriFactorGate EvidenceType = "TRI_FACTOR"    // Tri-Factor validation
	EvidenceJuryVerdict   EvidenceType = "JURY_VERDICT"  // Jury decision
	EvidenceHITL          EvidenceType = "HITL"          // Human intervention
	EvidencePolicyChange  EvidenceType = "POLICY_CHANGE" // APE rule change
	EvidenceCorrection    EvidenceType = "CORRECTION"    // Human correction
	EvidenceFederation    EvidenceType = "FEDERATION"    // Cross-OCX event
	EvidenceDispute       EvidenceType = "DISPUTE"       // Dispute record
)

// VerdictOutcome represents the outcome of a decision
type VerdictOutcome string

const (
	OutcomeAllow    VerdictOutcome = "ALLOW"
	OutcomeBlock    VerdictOutcome = "BLOCK"
	OutcomeHold     VerdictOutcome = "HOLD"
	OutcomeEscalate VerdictOutcome = "ESCALATE"
	OutcomeOverride VerdictOutcome = "OVERRIDE"
)

// ============================================================================
// EVIDENCE RECORD
// ============================================================================

// EvidenceRecord is a single piece of immutable evidence
type EvidenceRecord struct {
	// Identification
	ID            string       `json:"id"`
	Type          EvidenceType `json:"type"`
	TransactionID string       `json:"transaction_id"`

	// Context
	TenantID  string `json:"tenant_id"`
	AgentID   string `json:"agent_id"`
	SessionID string `json:"session_id,omitempty"`

	// Action details
	ToolID      string                 `json:"tool_id,omitempty"`
	ActionClass string                 `json:"action_class,omitempty"` // A or B
	Payload     map[string]interface{} `json:"payload,omitempty"`

	// Decision
	Verdict    VerdictOutcome `json:"verdict,omitempty"`
	TrustScore float64        `json:"trust_score,omitempty"`
	Reasoning  string         `json:"reasoning,omitempty"`

	// Validation results
	IdentityResult  *ValidationResult `json:"identity_result,omitempty"`
	SignalResult    *ValidationResult `json:"signal_result,omitempty"`
	CognitiveResult *ValidationResult `json:"cognitive_result,omitempty"`

	// Human involvement
	HITLReviewer string `json:"hitl_reviewer,omitempty"`
	HITLAction   string `json:"hitl_action,omitempty"`
	HITLNotes    string `json:"hitl_notes,omitempty"`

	// Timestamps
	Timestamp   time.Time `json:"timestamp"`
	ProcessedAt time.Time `json:"processed_at,omitempty"`

	// Integrity
	Hash         string `json:"hash"`
	PreviousHash string `json:"previous_hash"`
	Signature    []byte `json:"signature,omitempty"`

	// Metadata
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// ValidationResult captures a validation outcome
type ValidationResult struct {
	Passed  bool                   `json:"passed"`
	Score   float64                `json:"score,omitempty"`
	Reason  string                 `json:"reason,omitempty"`
	Details map[string]interface{} `json:"details,omitempty"`
}

// ComputeHash computes the SHA-256 hash of the record
func (e *EvidenceRecord) ComputeHash() string {
	// Create canonical representation without hash/signature
	copy := *e
	copy.Hash = ""
	copy.Signature = nil

	data, _ := json.Marshal(copy)
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// Verify verifies the record's hash integrity
func (e *EvidenceRecord) Verify() bool {
	return e.Hash == e.ComputeHash()
}

// ============================================================================
// EVIDENCE CHAIN
// ============================================================================

// EvidenceChain is a linked sequence of evidence records (blockchain-like)
type EvidenceChain struct {
	ChainID     string
	TenantID    string
	Records     []*EvidenceRecord
	LastHash    string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	RecordCount int64

	mu sync.RWMutex
}

// NewEvidenceChain creates a new evidence chain for a tenant
func NewEvidenceChain(tenantID string) *EvidenceChain {
	chainID := fmt.Sprintf("chain-%s-%d", tenantID, time.Now().UnixNano())

	// Create genesis record
	genesis := &EvidenceRecord{
		ID:            "genesis",
		Type:          EvidenceTransaction,
		TransactionID: "genesis",
		TenantID:      tenantID,
		Timestamp:     time.Now(),
		PreviousHash:  "0000000000000000000000000000000000000000000000000000000000000000",
		Metadata: map[string]interface{}{
			"genesis":  true,
			"chain_id": chainID,
		},
	}
	genesis.Hash = genesis.ComputeHash()

	return &EvidenceChain{
		ChainID:     chainID,
		TenantID:    tenantID,
		Records:     []*EvidenceRecord{genesis},
		LastHash:    genesis.Hash,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		RecordCount: 1,
	}
}

// Append adds a new record to the chain
func (ec *EvidenceChain) Append(record *EvidenceRecord) error {
	ec.mu.Lock()
	defer ec.mu.Unlock()

	// Link to previous record
	record.PreviousHash = ec.LastHash

	// Compute hash
	record.Hash = record.ComputeHash()

	// Add to chain
	ec.Records = append(ec.Records, record)
	ec.LastHash = record.Hash
	ec.RecordCount++
	ec.UpdatedAt = time.Now()

	return nil
}

// Validate validates the entire chain integrity
func (ec *EvidenceChain) Validate() (bool, int) {
	ec.mu.RLock()
	defer ec.mu.RUnlock()

	for i, record := range ec.Records {
		// Verify hash
		if !record.Verify() {
			return false, i
		}

		// Verify chain linkage (skip genesis)
		if i > 0 && record.PreviousHash != ec.Records[i-1].Hash {
			return false, i
		}
	}

	return true, -1
}

// GetRecord retrieves a record by ID
func (ec *EvidenceChain) GetRecord(id string) (*EvidenceRecord, error) {
	ec.mu.RLock()
	defer ec.mu.RUnlock()

	for _, record := range ec.Records {
		if record.ID == id {
			return record, nil
		}
	}
	return nil, fmt.Errorf("record %s not found", id)
}

// GetRecordsByTransaction retrieves all records for a transaction
func (ec *EvidenceChain) GetRecordsByTransaction(txID string) []*EvidenceRecord {
	ec.mu.RLock()
	defer ec.mu.RUnlock()

	var records []*EvidenceRecord
	for _, r := range ec.Records {
		if r.TransactionID == txID {
			records = append(records, r)
		}
	}
	return records
}

// ============================================================================
// EVIDENCE VAULT
// ============================================================================

// EvidenceVault is the central storage for all evidence
type EvidenceVault struct {
	chains map[string]*EvidenceChain // tenantID -> chain

	// Index by transaction
	txIndex map[string][]string // transactionID -> []recordID

	// Index by agent
	agentIndex map[string][]string // agentID -> []recordID

	// Retention configuration
	retentionDays int

	// Storage backend (for production)
	store EvidenceStore

	mu     sync.RWMutex
	logger *log.Logger
}

// EvidenceStore interface for persistent storage
type EvidenceStore interface {
	SaveRecord(ctx context.Context, record *EvidenceRecord) error
	LoadRecord(ctx context.Context, id string) (*EvidenceRecord, error)
	LoadChain(ctx context.Context, tenantID string) ([]*EvidenceRecord, error)
	QueryRecords(ctx context.Context, query RecordQuery) ([]*EvidenceRecord, error)
}

// RecordQuery defines a query for evidence records
type RecordQuery struct {
	TenantID      string
	AgentID       string
	TransactionID string
	Type          EvidenceType
	Verdict       VerdictOutcome
	StartTime     time.Time
	EndTime       time.Time
	Limit         int
	Offset        int
}

// VaultConfig holds vault configuration
type VaultConfig struct {
	RetentionDays int
	Store         EvidenceStore
}

// NewEvidenceVault creates a new evidence vault
func NewEvidenceVault(cfg VaultConfig) *EvidenceVault {
	return &EvidenceVault{
		chains:        make(map[string]*EvidenceChain),
		txIndex:       make(map[string][]string),
		agentIndex:    make(map[string][]string),
		retentionDays: cfg.RetentionDays,
		store:         cfg.Store,
		logger:        log.New(log.Writer(), "[EvidenceVault] ", log.LstdFlags),
	}
}

// RecordTransaction records a transaction in the vault
func (ev *EvidenceVault) RecordTransaction(
	ctx context.Context,
	tenantID, agentID, txID string,
	toolID, actionClass string,
	verdict VerdictOutcome,
	trustScore float64,
	reasoning string,
	payload map[string]interface{},
) (*EvidenceRecord, error) {
	record := &EvidenceRecord{
		ID:            fmt.Sprintf("ev-%s-%d", txID, time.Now().UnixNano()),
		Type:          EvidenceTransaction,
		TransactionID: txID,
		TenantID:      tenantID,
		AgentID:       agentID,
		ToolID:        toolID,
		ActionClass:   actionClass,
		Verdict:       verdict,
		TrustScore:    trustScore,
		Reasoning:     reasoning,
		Payload:       payload,
		Timestamp:     time.Now(),
		ProcessedAt:   time.Now(),
	}

	return ev.appendRecord(ctx, record)
}

// RecordTriFactorGate records a Tri-Factor Gate validation
func (ev *EvidenceVault) RecordTriFactorGate(
	ctx context.Context,
	tenantID, agentID, txID string,
	identityResult, signalResult, cognitiveResult *ValidationResult,
	finalVerdict VerdictOutcome,
) (*EvidenceRecord, error) {
	record := &EvidenceRecord{
		ID:              fmt.Sprintf("tfg-%s-%d", txID, time.Now().UnixNano()),
		Type:            EvidenceTriFactorGate,
		TransactionID:   txID,
		TenantID:        tenantID,
		AgentID:         agentID,
		Verdict:         finalVerdict,
		IdentityResult:  identityResult,
		SignalResult:    signalResult,
		CognitiveResult: cognitiveResult,
		Timestamp:       time.Now(),
		ProcessedAt:     time.Now(),
	}

	return ev.appendRecord(ctx, record)
}

// RecordHITL records a human-in-the-loop intervention
func (ev *EvidenceVault) RecordHITL(
	ctx context.Context,
	tenantID, agentID, txID string,
	reviewer, action, notes string,
	verdict VerdictOutcome,
) (*EvidenceRecord, error) {
	record := &EvidenceRecord{
		ID:            fmt.Sprintf("hitl-%s-%d", txID, time.Now().UnixNano()),
		Type:          EvidenceHITL,
		TransactionID: txID,
		TenantID:      tenantID,
		AgentID:       agentID,
		Verdict:       verdict,
		HITLReviewer:  reviewer,
		HITLAction:    action,
		HITLNotes:     notes,
		Timestamp:     time.Now(),
		ProcessedAt:   time.Now(),
	}

	return ev.appendRecord(ctx, record)
}

// RecordCorrection records a human correction (for RLHC)
func (ev *EvidenceVault) RecordCorrection(
	ctx context.Context,
	tenantID, agentID, txID string,
	correctionType, originalAction, correctedAction string,
	reviewer, reasoning string,
) (*EvidenceRecord, error) {
	record := &EvidenceRecord{
		ID:            fmt.Sprintf("corr-%s-%d", txID, time.Now().UnixNano()),
		Type:          EvidenceCorrection,
		TransactionID: txID,
		TenantID:      tenantID,
		AgentID:       agentID,
		HITLReviewer:  reviewer,
		Reasoning:     reasoning,
		Timestamp:     time.Now(),
		ProcessedAt:   time.Now(),
		Metadata: map[string]interface{}{
			"correction_type":  correctionType,
			"original_action":  originalAction,
			"corrected_action": correctedAction,
		},
	}

	return ev.appendRecord(ctx, record)
}

func (ev *EvidenceVault) appendRecord(ctx context.Context, record *EvidenceRecord) (*EvidenceRecord, error) {
	ev.mu.Lock()
	defer ev.mu.Unlock()

	// Get or create chain for tenant
	chain, exists := ev.chains[record.TenantID]
	if !exists {
		chain = NewEvidenceChain(record.TenantID)
		ev.chains[record.TenantID] = chain
	}

	// Append to chain
	if err := chain.Append(record); err != nil {
		return nil, err
	}

	// Update indexes
	ev.txIndex[record.TransactionID] = append(
		ev.txIndex[record.TransactionID],
		record.ID,
	)
	ev.agentIndex[record.AgentID] = append(
		ev.agentIndex[record.AgentID],
		record.ID,
	)

	// Persist to store if available
	if ev.store != nil {
		if err := ev.store.SaveRecord(ctx, record); err != nil {
			ev.logger.Printf("Failed to persist record %s: %v", record.ID, err)
		}
	}

	ev.logger.Printf("Recorded evidence: %s (type=%s, tx=%s)", record.ID, record.Type, record.TransactionID)

	return record, nil
}

// GetRecord retrieves a record by ID
func (ev *EvidenceVault) GetRecord(ctx context.Context, tenantID, recordID string) (*EvidenceRecord, error) {
	ev.mu.RLock()
	chain, exists := ev.chains[tenantID]
	ev.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("tenant %s not found", tenantID)
	}

	return chain.GetRecord(recordID)
}

// GetTransactionHistory retrieves all evidence for a transaction
func (ev *EvidenceVault) GetTransactionHistory(ctx context.Context, txID string) ([]*EvidenceRecord, error) {
	ev.mu.RLock()
	recordIDs := ev.txIndex[txID]
	ev.mu.RUnlock()

	if len(recordIDs) == 0 {
		return nil, nil
	}

	// Collect records from all chains
	var records []*EvidenceRecord
	ev.mu.RLock()
	for _, chain := range ev.chains {
		for _, r := range chain.GetRecordsByTransaction(txID) {
			records = append(records, r)
		}
	}
	ev.mu.RUnlock()

	return records, nil
}

// ValidateChain validates the integrity of a tenant's evidence chain
func (ev *EvidenceVault) ValidateChain(tenantID string) (bool, int, error) {
	ev.mu.RLock()
	chain, exists := ev.chains[tenantID]
	ev.mu.RUnlock()

	if !exists {
		return false, -1, fmt.Errorf("tenant %s not found", tenantID)
	}

	valid, failIndex := chain.Validate()
	return valid, failIndex, nil
}

// ============================================================================
// COMPLIANCE REPORTING
// ============================================================================

// ComplianceReport contains compliance audit data
type ComplianceReport struct {
	ReportID    string                `json:"report_id"`
	TenantID    string                `json:"tenant_id"`
	GeneratedAt time.Time             `json:"generated_at"`
	PeriodStart time.Time             `json:"period_start"`
	PeriodEnd   time.Time             `json:"period_end"`
	Summary     ComplianceSummary     `json:"summary"`
	ByAgent     map[string]AgentStats `json:"by_agent"`
	ByTool      map[string]ToolStats  `json:"by_tool"`
	Violations  []ViolationRecord     `json:"violations"`
	ChainValid  bool                  `json:"chain_valid"`
	RecordCount int64                 `json:"record_count"`
}

// ComplianceSummary contains high-level compliance metrics
type ComplianceSummary struct {
	TotalTransactions int64   `json:"total_transactions"`
	AllowedCount      int64   `json:"allowed_count"`
	BlockedCount      int64   `json:"blocked_count"`
	HeldCount         int64   `json:"held_count"`
	EscalatedCount    int64   `json:"escalated_count"`
	OverrideCount     int64   `json:"override_count"`
	HITLInterventions int64   `json:"hitl_interventions"`
	AvgTrustScore     float64 `json:"avg_trust_score"`
	ComplianceRate    float64 `json:"compliance_rate"`
}

// AgentStats contains per-agent statistics
type AgentStats struct {
	AgentID       string  `json:"agent_id"`
	Transactions  int64   `json:"transactions"`
	Allowed       int64   `json:"allowed"`
	Blocked       int64   `json:"blocked"`
	AvgTrustScore float64 `json:"avg_trust_score"`
	Violations    int64   `json:"violations"`
}

// ToolStats contains per-tool statistics
type ToolStats struct {
	ToolID      string `json:"tool_id"`
	Invocations int64  `json:"invocations"`
	Allowed     int64  `json:"allowed"`
	Blocked     int64  `json:"blocked"`
	ClassACount int64  `json:"class_a_count"`
	ClassBCount int64  `json:"class_b_count"`
}

// ViolationRecord contains a policy violation
type ViolationRecord struct {
	RecordID      string         `json:"record_id"`
	TransactionID string         `json:"transaction_id"`
	AgentID       string         `json:"agent_id"`
	ToolID        string         `json:"tool_id"`
	Verdict       VerdictOutcome `json:"verdict"`
	Reason        string         `json:"reason"`
	Timestamp     time.Time      `json:"timestamp"`
}

// GenerateComplianceReport generates a compliance report for a tenant
func (ev *EvidenceVault) GenerateComplianceReport(
	ctx context.Context,
	tenantID string,
	start, end time.Time,
) (*ComplianceReport, error) {
	ev.mu.RLock()
	chain, exists := ev.chains[tenantID]
	ev.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("tenant %s not found", tenantID)
	}

	report := &ComplianceReport{
		ReportID:    fmt.Sprintf("report-%s-%d", tenantID, time.Now().UnixNano()),
		TenantID:    tenantID,
		GeneratedAt: time.Now(),
		PeriodStart: start,
		PeriodEnd:   end,
		ByAgent:     make(map[string]AgentStats),
		ByTool:      make(map[string]ToolStats),
		Violations:  make([]ViolationRecord, 0),
	}

	// Validate chain integrity
	valid, _ := chain.Validate()
	report.ChainValid = valid

	// Aggregate statistics
	chain.mu.RLock()
	defer chain.mu.RUnlock()

	var totalTrust float64
	var trustCount int64

	for _, record := range chain.Records {
		// Filter by time range
		if record.Timestamp.Before(start) || record.Timestamp.After(end) {
			continue
		}

		// Skip genesis
		if record.ID == "genesis" {
			continue
		}

		report.RecordCount++

		// Only process transaction-type records for main stats
		if record.Type != EvidenceTransaction && record.Type != EvidenceTriFactorGate {
			continue
		}

		report.Summary.TotalTransactions++

		// Count by verdict
		switch record.Verdict {
		case OutcomeAllow:
			report.Summary.AllowedCount++
		case OutcomeBlock:
			report.Summary.BlockedCount++
			report.Violations = append(report.Violations, ViolationRecord{
				RecordID:      record.ID,
				TransactionID: record.TransactionID,
				AgentID:       record.AgentID,
				ToolID:        record.ToolID,
				Verdict:       record.Verdict,
				Reason:        record.Reasoning,
				Timestamp:     record.Timestamp,
			})
		case OutcomeHold:
			report.Summary.HeldCount++
		case OutcomeEscalate:
			report.Summary.EscalatedCount++
		case OutcomeOverride:
			report.Summary.OverrideCount++
		}

		// Track trust scores
		if record.TrustScore > 0 {
			totalTrust += record.TrustScore
			trustCount++
		}

		// Agent stats
		agentStats := report.ByAgent[record.AgentID]
		agentStats.AgentID = record.AgentID
		agentStats.Transactions++
		switch record.Verdict {
		case OutcomeAllow:
			agentStats.Allowed++
		case OutcomeBlock:
			agentStats.Blocked++
			agentStats.Violations++
		}
		report.ByAgent[record.AgentID] = agentStats

		// Tool stats
		if record.ToolID != "" {
			toolStats := report.ByTool[record.ToolID]
			toolStats.ToolID = record.ToolID
			toolStats.Invocations++
			switch record.Verdict {
			case OutcomeAllow:
				toolStats.Allowed++
			case OutcomeBlock:
				toolStats.Blocked++
			}
			switch record.ActionClass {
			case "A":
				toolStats.ClassACount++
			case "B":
				toolStats.ClassBCount++
			}
			report.ByTool[record.ToolID] = toolStats
		}
	}

	// Calculate averages
	if trustCount > 0 {
		report.Summary.AvgTrustScore = totalTrust / float64(trustCount)
	}

	if report.Summary.TotalTransactions > 0 {
		report.Summary.ComplianceRate = float64(report.Summary.AllowedCount) / float64(report.Summary.TotalTransactions) * 100
	}

	// Count HITL interventions
	for _, record := range chain.Records {
		if record.Type == EvidenceHITL && record.Timestamp.After(start) && record.Timestamp.Before(end) {
			report.Summary.HITLInterventions++
		}
	}

	ev.logger.Printf("Generated compliance report: %s (records=%d, transactions=%d)",
		report.ReportID, report.RecordCount, report.Summary.TotalTransactions)

	return report, nil
}

// Stats returns vault statistics
func (ev *EvidenceVault) Stats() map[string]interface{} {
	ev.mu.RLock()
	defer ev.mu.RUnlock()

	totalRecords := int64(0)
	for _, chain := range ev.chains {
		totalRecords += chain.RecordCount
	}

	return map[string]interface{}{
		"tenant_count":      len(ev.chains),
		"total_records":     totalRecords,
		"transaction_index": len(ev.txIndex),
		"agent_index":       len(ev.agentIndex),
		"retention_days":    ev.retentionDays,
	}
}

// ============================================================================
// IN-MEMORY STORE (for testing)
// ============================================================================

// InMemoryEvidenceStore provides in-memory storage
type InMemoryEvidenceStore struct {
	records map[string]*EvidenceRecord
	mu      sync.RWMutex
}

// NewInMemoryEvidenceStore creates a new in-memory store
func NewInMemoryEvidenceStore() *InMemoryEvidenceStore {
	return &InMemoryEvidenceStore{
		records: make(map[string]*EvidenceRecord),
	}
}

// SaveRecord saves a record
func (s *InMemoryEvidenceStore) SaveRecord(_ context.Context, record *EvidenceRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records[record.ID] = record
	return nil
}

// LoadRecord loads a record
func (s *InMemoryEvidenceStore) LoadRecord(_ context.Context, id string) (*EvidenceRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	record, exists := s.records[id]
	if !exists {
		return nil, errors.New("record not found")
	}
	return record, nil
}

// LoadChain loads all records for a tenant
func (s *InMemoryEvidenceStore) LoadChain(_ context.Context, tenantID string) ([]*EvidenceRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var records []*EvidenceRecord
	for _, r := range s.records {
		if r.TenantID == tenantID {
			records = append(records, r)
		}
	}
	return records, nil
}

// QueryRecords queries records
func (s *InMemoryEvidenceStore) QueryRecords(_ context.Context, query RecordQuery) ([]*EvidenceRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var results []*EvidenceRecord
	for _, r := range s.records {
		// Apply filters
		if query.TenantID != "" && r.TenantID != query.TenantID {
			continue
		}
		if query.AgentID != "" && r.AgentID != query.AgentID {
			continue
		}
		if query.TransactionID != "" && r.TransactionID != query.TransactionID {
			continue
		}
		if query.Type != "" && r.Type != query.Type {
			continue
		}
		if query.Verdict != "" && r.Verdict != query.Verdict {
			continue
		}
		if !query.StartTime.IsZero() && r.Timestamp.Before(query.StartTime) {
			continue
		}
		if !query.EndTime.IsZero() && r.Timestamp.After(query.EndTime) {
			continue
		}

		results = append(results, r)

		if query.Limit > 0 && len(results) >= query.Limit {
			break
		}
	}

	return results, nil
}
