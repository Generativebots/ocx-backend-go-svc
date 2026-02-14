package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/ocx/backend/internal/database"
)

// ============================================================================
// SESSION AUDIT HANDLERS — Security Forensics
// ============================================================================

// HandleSessionAuditLogs returns paginated, filtered session audit entries.
// GET /api/v1/sessions/audit?agent_id=&tenant_id=&event_type=&ip=&since=&until=&limit=&offset=
func HandleSessionAuditLogs(db *database.SupabaseClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		limit, _ := strconv.Atoi(q.Get("limit"))
		offset, _ := strconv.Atoi(q.Get("offset"))
		if limit <= 0 {
			limit = 50
		}

		logs, err := db.QueryAuditLogs(
			q.Get("agent_id"),
			q.Get("tenant_id"),
			q.Get("event_type"),
			q.Get("ip"),
			q.Get("since"),
			q.Get("until"),
			limit, offset,
		)
		if err != nil {
			http.Error(w, `{"error":"failed to query audit logs"}`, http.StatusInternalServerError)
			return
		}
		if logs == nil {
			logs = []map[string]interface{}{}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"entries":       logs,
			"total_entries": len(logs),
			"limit":         limit,
			"offset":        offset,
		})
	}
}
