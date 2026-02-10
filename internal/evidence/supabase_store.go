package evidence

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/ocx/backend/internal/database"
)

// SupabaseEvidenceStore persists evidence records to Supabase (PostgreSQL).
// Falls back gracefully if Supabase is unreachable — records are still held
// in the in-memory chain.
type SupabaseEvidenceStore struct {
	client *database.SupabaseClient
	logger *log.Logger
}

// NewSupabaseEvidenceStore creates a persistent evidence store backed by Supabase.
func NewSupabaseEvidenceStore(client *database.SupabaseClient) *SupabaseEvidenceStore {
	return &SupabaseEvidenceStore{
		client: client,
		logger: log.New(log.Writer(), "[EvidenceStore:Supabase] ", log.LstdFlags),
	}
}

// evidenceRow is the database row shape for the evidence_records table.
type evidenceRow struct {
	ID            string  `json:"id"`
	Type          string  `json:"type"`
	TransactionID string  `json:"transaction_id"`
	TenantID      string  `json:"tenant_id"`
	AgentID       string  `json:"agent_id"`
	ToolID        string  `json:"tool_id"`
	ActionClass   string  `json:"action_class"`
	Verdict       string  `json:"verdict"`
	TrustScore    float64 `json:"trust_score"`
	Reasoning     string  `json:"reasoning"`
	Hash          string  `json:"hash"`
	PreviousHash  string  `json:"previous_hash"`
	Payload       string  `json:"payload"`
	Timestamp     string  `json:"timestamp"`
	ProcessedAt   string  `json:"processed_at"`
}

// SaveRecord persists an evidence record to the evidence_records table.
func (s *SupabaseEvidenceStore) SaveRecord(_ context.Context, record *EvidenceRecord) error {
	payload, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshal evidence record: %w", err)
	}

	row := evidenceRow{
		ID:            record.ID,
		Type:          string(record.Type),
		TransactionID: record.TransactionID,
		TenantID:      record.TenantID,
		AgentID:       record.AgentID,
		ToolID:        record.ToolID,
		ActionClass:   record.ActionClass,
		Verdict:       string(record.Verdict),
		TrustScore:    record.TrustScore,
		Reasoning:     record.Reasoning,
		Hash:          record.Hash,
		PreviousHash:  record.PreviousHash,
		Payload:       string(payload),
		Timestamp:     record.Timestamp.Format(time.RFC3339),
		ProcessedAt:   record.ProcessedAt.Format(time.RFC3339),
	}

	err = s.client.InsertRow("evidence_records", row)
	if err != nil {
		s.logger.Printf("Failed to persist evidence %s: %v", record.ID, err)
		return fmt.Errorf("save evidence record: %w", err)
	}

	s.logger.Printf("Persisted evidence record %s (type=%s)", record.ID, record.Type)
	return nil
}

// LoadRecord retrieves a single evidence record by ID.
func (s *SupabaseEvidenceStore) LoadRecord(_ context.Context, id string) (*EvidenceRecord, error) {
	var rows []evidenceRow
	err := s.client.QueryRows("evidence_records", "payload", "id", id, &rows)
	if err != nil || len(rows) == 0 {
		return nil, fmt.Errorf("load evidence record %s: %w", id, err)
	}

	var record EvidenceRecord
	if err := json.Unmarshal([]byte(rows[0].Payload), &record); err != nil {
		return nil, fmt.Errorf("unmarshal evidence record: %w", err)
	}
	return &record, nil
}

// LoadChain retrieves all evidence records for a tenant, ordered by timestamp.
func (s *SupabaseEvidenceStore) LoadChain(_ context.Context, tenantID string) ([]*EvidenceRecord, error) {
	var rows []evidenceRow
	err := s.client.QueryRows("evidence_records", "payload", "tenant_id", tenantID, &rows)
	if err != nil {
		return nil, fmt.Errorf("load evidence chain: %w", err)
	}

	records := make([]*EvidenceRecord, 0, len(rows))
	for _, row := range rows {
		var record EvidenceRecord
		if err := json.Unmarshal([]byte(row.Payload), &record); err != nil {
			s.logger.Printf("Skipping corrupt record: %v", err)
			continue
		}
		records = append(records, &record)
	}
	return records, nil
}

// QueryRecords queries evidence records with filters.
func (s *SupabaseEvidenceStore) QueryRecords(_ context.Context, query RecordQuery) ([]*EvidenceRecord, error) {
	// For queries with specific filters, use the primary filter
	filterCol := "tenant_id"
	filterVal := query.TenantID

	if query.AgentID != "" {
		filterCol = "agent_id"
		filterVal = query.AgentID
	}
	if query.TransactionID != "" {
		filterCol = "transaction_id"
		filterVal = query.TransactionID
	}

	var rows []evidenceRow
	err := s.client.QueryRows("evidence_records", "payload", filterCol, filterVal, &rows)
	if err != nil {
		return nil, fmt.Errorf("query evidence records: %w", err)
	}

	records := make([]*EvidenceRecord, 0, len(rows))
	for _, row := range rows {
		var record EvidenceRecord
		if err := json.Unmarshal([]byte(row.Payload), &record); err != nil {
			continue
		}
		records = append(records, &record)
	}

	// Apply limit
	if query.Limit > 0 && len(records) > query.Limit {
		records = records[:query.Limit]
	}

	return records, nil
}
