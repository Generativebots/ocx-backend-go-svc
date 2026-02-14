package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/ocx/backend/internal/security"
)

// ============================================================================
// CAE HANDLERS — Patent Claim 8: Continuous Access Evaluation
// ============================================================================

// HandleCAESessions returns all active CAE-monitored sessions.
// GET /api/v1/cae/sessions
func HandleCAESessions(cae *security.ContinuousAccessEvaluator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessions := cae.GetSessions()
		if sessions == nil {
			sessions = []security.SessionSnapshot{}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"sessions":       sessions,
			"total_sessions": len(sessions),
		})
	}
}

// HandleCAEStats returns CAE evaluator configuration and statistics.
// GET /api/v1/cae/stats
func HandleCAEStats(cae *security.ContinuousAccessEvaluator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		stats := cae.GetStats()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(stats)
	}
}
