/*
Speculative Outbound Proxy (SOP)
Intercepts external API calls during speculation and returns mock responses
*/

package sop

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"time"

	"github.com/redis/go-redis/v9"
)

// SpeculativeMode represents the execution mode
type SpeculativeMode int

const (
	ModeReal SpeculativeMode = iota
	ModeSpeculative
)

// SpeculativeProxy handles outbound requests with sequestration
type SpeculativeProxy struct {
	cache         *redis.Client
	realProxy     *httputil.ReverseProxy
	mockGenerator *MockGenerator
	certGen       *CertGenerator
	ctx           context.Context
}

// Config for SOP
type Config struct {
	RedisAddr     string
	CertCacheDir  string
	MockSchemaDir string
}

// NewSpeculativeProxy creates a new SOP instance
func NewSpeculativeProxy(cfg Config) (*SpeculativeProxy, error) {
	// Redis client for sequestration
	rdb := redis.NewClient(&redis.Options{
		Addr: cfg.RedisAddr,
		DB:   1, // Use DB 1 for SOP
	})

	// Test connection
	ctx := context.Background()
	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis connection failed: %w", err)
	}

	// Mock generator
	mockGen, err := NewMockGenerator(cfg.MockSchemaDir)
	if err != nil {
		return nil, fmt.Errorf("failed to create mock generator: %w", err)
	}

	// Certificate generator
	certGen, err := NewCertGenerator(cfg.CertCacheDir)
	if err != nil {
		return nil, fmt.Errorf("failed to create cert generator: %w", err)
	}

	// Real-world proxy
	realProxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			// Forward to real destination
			req.URL.Scheme = "https"
			req.URL.Host = req.Host
		},
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: false,
			},
		},
	}

	return &SpeculativeProxy{
		cache:         rdb,
		realProxy:     realProxy,
		mockGenerator: mockGen,
		certGen:       certGen,
		ctx:           ctx,
	}, nil
}

// HandleOutbound is the main request handler
func (sop *SpeculativeProxy) HandleOutbound(w http.ResponseWriter, r *http.Request) {
	txID := r.Header.Get("X-OCX-Transaction-ID")
	agentID := r.Header.Get("X-OCX-Agent-ID")

	log.Printf("📡 SOP: %s %s (TxID: %s, Agent: %s)", r.Method, r.URL.String(), txID, agentID)

	// Determine mode
	mode := sop.getMode(txID)

	if mode == ModeSpeculative {
		sop.handleSpeculative(w, r, txID, agentID)
	} else {
		sop.handleReal(w, r, txID, agentID)
	}
}

// getMode checks if transaction is in speculative mode
func (sop *SpeculativeProxy) getMode(txID string) SpeculativeMode {
	if txID == "" {
		return ModeReal
	}

	// Check Redis for speculation flag
	key := fmt.Sprintf("speculation:%s", txID)
	val, err := sop.cache.Get(sop.ctx, key).Result()
	if err == redis.Nil {
		return ModeReal
	}
	if err != nil {
		log.Printf("⚠️  Redis error: %v", err)
		return ModeReal
	}

	if val == "true" {
		return ModeSpeculative
	}
	return ModeReal
}

// handleSpeculative sequester request and return mock
func (sop *SpeculativeProxy) handleSpeculative(w http.ResponseWriter, r *http.Request, txID, _ string) {
	log.Printf("🔒 SEQUESTERING: %s %s", r.Method, r.URL.String())

	// 1. Dump full request
	rawRequest, err := httputil.DumpRequest(r, true)
	if err != nil {
		http.Error(w, "Failed to dump request", http.StatusInternalServerError)
		return
	}

	// 2. Store in Redis with TTL
	key := fmt.Sprintf("req:%s:%s", txID, r.URL.Host)
	err = sop.cache.Set(sop.ctx, key, rawRequest, 5*time.Minute).Err()
	if err != nil {
		log.Printf("❌ Failed to sequester request: %v", err)
		http.Error(w, "Sequestration failed", http.StatusInternalServerError)
		return
	}

	// 3. Generate mock response
	mockResp := sop.mockGenerator.GenerateMock(r.URL.Host, r.Method, r.URL.Path)

	// 4. Return mock to agent
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-OCX-Sequestered", "true")
	w.Header().Set("X-OCX-Transaction-ID", txID)
	w.WriteHeader(http.StatusOK)
	w.Write(mockResp)

	log.Printf("✅ SEQUESTERED: %s (mock returned)", key)
}

// handleReal forward to actual external API
func (sop *SpeculativeProxy) handleReal(w http.ResponseWriter, r *http.Request, txID, _ string) {
	log.Printf("🌐 REAL EXECUTION: %s %s", r.Method, r.URL.String())

	// Check if this was previously sequestered
	if txID != "" {
		key := fmt.Sprintf("req:%s:%s", txID, r.URL.Host)
		sequestered, err := sop.cache.Get(sop.ctx, key).Result()
		if err == nil {
			// Replay sequestered request
			log.Printf("🔄 REPLAYING sequestered request: %s", key)
			sop.replayRequest(w, r, sequestered, txID)
			return
		}
	}

	// Forward to real API
	sop.realProxy.ServeHTTP(w, r)
}

// replayRequest executes a previously sequestered request
func (sop *SpeculativeProxy) replayRequest(w http.ResponseWriter, r *http.Request, rawRequest string, txID string) {
	// Parse sequestered request
	buf := bytes.NewBufferString(rawRequest)
	bufReader := bufio.NewReader(buf)
	req, err := http.ReadRequest(bufReader)
	if err != nil {
		log.Printf("❌ Failed to parse sequestered request: %v", err)
		http.Error(w, "Replay failed", http.StatusInternalServerError)
		return
	}

	// Update timestamps and nonces
	sop.updateRequestMetadata(req)

	// Execute real request
	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: false,
			},
		},
	}

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("❌ Replay request failed: %v", err)
		http.Error(w, "External API error", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Copy response to client
	for k, v := range resp.Header {
		w.Header()[k] = v
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)

	// Clean up sequestered request
	key := fmt.Sprintf("req:%s:%s", txID, r.URL.Host)
	sop.cache.Del(sop.ctx, key)

	log.Printf("✅ REPLAYED: %s (status: %d)", key, resp.StatusCode)
}

// updateRequestMetadata updates timestamps, nonces, etc.
func (sp *SpeculativeProxy) updateRequestMetadata(req *http.Request) {
	// Update timestamp headers
	req.Header.Set("X-Request-Time", fmt.Sprintf("%d", time.Now().Unix()))

	// Update idempotency keys if present
	if key := req.Header.Get("Idempotency-Key"); key != "" {
		newKey := fmt.Sprintf("%s-replay-%d", key, time.Now().UnixNano())
		req.Header.Set("Idempotency-Key", newKey)
	}

	// Update nonces
	if nonce := req.Header.Get("X-Nonce"); nonce != "" {
		newNonce := fmt.Sprintf("%d", time.Now().UnixNano())
		req.Header.Set("X-Nonce", newNonce)
	}
}

// SetSpeculationMode sets the speculation flag for a transaction
func (sp *SpeculativeProxy) SetSpeculationMode(txID string, speculative bool) error {
	key := fmt.Sprintf("speculation:%s", txID)
	val := "false"
	if speculative {
		val = "true"
	}
	return sp.cache.Set(sp.ctx, key, val, 10*time.Minute).Err()
}

// GetSequesteredRequests returns all sequestered requests for a transaction
func (sop *SpeculativeProxy) GetSequesteredRequests(txID string) ([]string, error) {
	pattern := fmt.Sprintf("req:%s:*", txID)
	keys, err := sop.cache.Keys(sop.ctx, pattern).Result()
	if err != nil {
		return nil, err
	}

	requests := make([]string, 0, len(keys))
	for _, key := range keys {
		req, err := sop.cache.Get(sop.ctx, key).Result()
		if err == nil {
			requests = append(requests, req)
		}
	}

	return requests, nil
}

// ShredSequesteredRequests deletes all sequestered requests for a transaction
func (sop *SpeculativeProxy) ShredSequesteredRequests(txID string) error {
	pattern := fmt.Sprintf("req:%s:*", txID)
	keys, err := sop.cache.Keys(sop.ctx, pattern).Result()
	if err != nil {
		return err
	}

	if len(keys) > 0 {
		return sop.cache.Del(sop.ctx, keys...).Err()
	}

	log.Printf("🗑️  SHREDDED %d sequestered requests for tx: %s", len(keys), txID)
	return nil
}

// Close cleanup
func (sop *SpeculativeProxy) Close() error {
	return sop.cache.Close()
}
