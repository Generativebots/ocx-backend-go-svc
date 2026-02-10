package evidence

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"
)

// AuditLogQuery provides a queryable API for the Evidence Vault.
// Implements the patent's "Audit Log Query API" for searching and
// filtering governance events, verdicts, and transaction records.

// AuditQueryResult is the paginated response for audit queries.
type AuditQueryResult struct {
	Records    []*EvidenceRecord `json:"records"`
	Total      int               `json:"total"`
	Limit      int               `json:"limit"`
	Offset     int               `json:"offset"`
	ExecutedAt time.Time         `json:"executed_at"`
}

// RegisterAuditRoutes adds audit log query endpoints to the router.
func RegisterAuditRoutes(router *mux.Router, vault *EvidenceVault) {
	router.HandleFunc("/api/v1/audit/logs", handleQueryAuditLogs(vault)).Methods("GET")
	router.HandleFunc("/api/v1/audit/logs/{transactionID}", handleGetAuditLog(vault)).Methods("GET")
	router.HandleFunc("/api/v1/audit/stats", handleAuditStats(vault)).Methods("GET")
}

// GET /api/v1/audit/logs?agent_id=X&tenant_id=Y&verdict=BLOCK&start=...&end=...&limit=50&offset=0
func handleQueryAuditLogs(vault *EvidenceVault) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()

		limit, _ := strconv.Atoi(q.Get("limit"))
		if limit <= 0 || limit > 100 {
			limit = 50
		}
		offset, _ := strconv.Atoi(q.Get("offset"))
		if offset < 0 {
			offset = 0
		}

		query := RecordQuery{
			AgentID:  q.Get("agent_id"),
			TenantID: q.Get("tenant_id"),
			Limit:    limit,
			Offset:   offset,
		}

		// Parse verdict filter
		if v := q.Get("verdict"); v != "" {
			query.Verdict = VerdictOutcome(v)
		}

		// Parse time range
		if start := q.Get("start"); start != "" {
			if t, err := time.Parse(time.RFC3339, start); err == nil {
				query.StartTime = t
			}
		}
		if end := q.Get("end"); end != "" {
			if t, err := time.Parse(time.RFC3339, end); err == nil {
				query.EndTime = t
			}
		}

		records, err := vault.store.QueryRecords(r.Context(), query)
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"query failed: %s"}`, err.Error()), http.StatusInternalServerError)
			return
		}

		result := AuditQueryResult{
			Records:    records,
			Total:      len(records),
			Limit:      limit,
			Offset:     offset,
			ExecutedAt: time.Now(),
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}
}

// GET /api/v1/audit/logs/{transactionID}
func handleGetAuditLog(vault *EvidenceVault) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		txID := mux.Vars(r)["transactionID"]

		record, err := vault.store.LoadRecord(r.Context(), txID)
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"record not found: %s"}`, txID), http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(record)
	}
}

// GET /api/v1/audit/stats — aggregate statistics
func handleAuditStats(vault *EvidenceVault) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Query all records (no filters) — limited to most recent 1000
		records, _ := vault.store.QueryRecords(r.Context(), RecordQuery{Limit: 1000})

		verdictCounts := make(map[string]int)
		agentCounts := make(map[string]int)
		typeCounts := make(map[string]int)

		for _, record := range records {
			verdictCounts[string(record.Verdict)]++
			agentCounts[record.AgentID]++
			typeCounts[string(record.Type)]++
		}

		stats := map[string]interface{}{
			"total_records":  len(records),
			"verdict_counts": verdictCounts,
			"agent_counts":   agentCounts,
			"type_counts":    typeCounts,
			"generated_at":   time.Now(),
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(stats)
	}
}
