package federation

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// ============================================================================
// SUPABASE HANDSHAKE STORE
//
// Persists handshake session state to the Supabase `federation_handshakes`
// table so sessions survive pod restarts and can be monitored via the
// master schema.
// ============================================================================

// SupabaseHandshakeStore implements HandshakeSessionStore backed by Supabase.
type SupabaseHandshakeStore struct {
	url    string       // Supabase project REST URL (e.g. https://xxx.supabase.co)
	apiKey string       // Supabase service-role key
	client *http.Client // HTTP client (reused for connection pooling)
	table  string       // Table name, default "federation_handshakes"
}

// NewSupabaseHandshakeStore creates a new Supabase-backed handshake store.
func NewSupabaseHandshakeStore(supabaseURL, serviceKey string) *SupabaseHandshakeStore {
	return &SupabaseHandshakeStore{
		url:    strings.TrimRight(supabaseURL, "/"),
		apiKey: serviceKey,
		client: &http.Client{Timeout: 10 * time.Second},
		table:  "federation_handshakes",
	}
}

// Save upserts a handshake session to Supabase.
func (s *SupabaseHandshakeStore) Save(ctx context.Context, state *HandshakeSessionState) error {
	state.LastStepAt = time.Now()

	body, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}

	url := fmt.Sprintf("%s/rest/v1/%s", s.url, s.table)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(body)))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("apikey", s.apiKey)
	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	// Upsert on session_id conflict
	req.Header.Set("Prefer", "resolution=merge-duplicates")

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("supabase request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("supabase save failed: HTTP %d", resp.StatusCode)
	}

	slog.Info("[SupabaseHandshakeStore] Saved session",
		"session_id", state.SessionID,
		"step", state.StepCompleted,
	)
	return nil
}

// Load retrieves a handshake session by ID from Supabase.
func (s *SupabaseHandshakeStore) Load(ctx context.Context, sessionID string) (*HandshakeSessionState, error) {
	url := fmt.Sprintf("%s/rest/v1/%s?session_id=eq.%s",
		s.url, s.table, sessionID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("apikey", s.apiKey)
	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("supabase request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("supabase load failed: HTTP %d", resp.StatusCode)
	}

	var results []HandshakeSessionState
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if len(results) == 0 {
		return nil, fmt.Errorf("handshake session not found: %s", sessionID)
	}

	return &results[0], nil
}

// Delete removes a completed handshake session from Supabase.
func (s *SupabaseHandshakeStore) Delete(ctx context.Context, sessionID string) error {
	url := fmt.Sprintf("%s/rest/v1/%s?session_id=eq.%s",
		s.url, s.table, sessionID)

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("apikey", s.apiKey)
	req.Header.Set("Authorization", "Bearer "+s.apiKey)

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("supabase request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("supabase delete failed: HTTP %d", resp.StatusCode)
	}

	slog.Info("[SupabaseHandshakeStore] Deleted session", "session_id", sessionID)
	return nil
}

// ListIncomplete returns all handshake sessions that haven't reached a
// terminal state (verdict is empty).
func (s *SupabaseHandshakeStore) ListIncomplete(ctx context.Context) ([]*HandshakeSessionState, error) {
	url := fmt.Sprintf("%s/rest/v1/%s?verdict=eq.&order=started_at.desc",
		s.url, s.table)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("apikey", s.apiKey)
	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("supabase request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("supabase list failed: HTTP %d", resp.StatusCode)
	}

	var results []HandshakeSessionState
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	ptrs := make([]*HandshakeSessionState, len(results))
	for i := range results {
		ptrs[i] = &results[i]
	}

	slog.Info("[SupabaseHandshakeStore] Listed incomplete sessions", "count", len(ptrs))
	return ptrs, nil
}
