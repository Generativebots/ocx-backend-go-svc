package webhooks

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"sync"
	"time"
)

// WebhookEmitter is the interface for dispatching webhook events.
// Both the in-memory Dispatcher and CloudDispatcher satisfy this interface.
type WebhookEmitter interface {
	Emit(eventType EventType, tenantID string, data map[string]interface{})
	Shutdown()
}

// EventType defines the types of events that can trigger webhooks
type EventType string

const (
	EventVerdictAllow    EventType = "verdict.allow"
	EventVerdictBlock    EventType = "verdict.block"
	EventVerdictEscrow   EventType = "verdict.escrow"
	EventEscrowReleased  EventType = "escrow.released"
	EventEscrowExpired   EventType = "escrow.expired"
	EventTrustChanged    EventType = "trust.changed"
	EventToolRegistered  EventType = "tool.registered"
	EventToolRemoved     EventType = "tool.removed"
	EventEntitlementUsed EventType = "entitlement.used"
)

// WebhookSubscription represents a registered webhook
type WebhookSubscription struct {
	ID        string      `json:"id"`
	URL       string      `json:"url"`
	Events    []EventType `json:"events"`
	Secret    string      `json:"secret,omitempty"`
	Active    bool        `json:"active"`
	TenantID  string      `json:"tenant_id"`
	CreatedAt time.Time   `json:"created_at"`
	FailCount int         `json:"fail_count"`
}

// WebhookEvent is the payload sent to webhook subscribers
type WebhookEvent struct {
	ID        string                 `json:"id"`
	Type      EventType              `json:"type"`
	Source    string                 `json:"source"`
	Timestamp time.Time              `json:"timestamp"`
	TenantID  string                 `json:"tenant_id"`
	Data      map[string]interface{} `json:"data"`
}

// Registry stores and manages webhook subscriptions
type Registry struct {
	mu          sync.RWMutex
	hooks       map[string]*WebhookSubscription // id -> hook
	byEvent     map[EventType][]*WebhookSubscription
	logger      *log.Logger
	maxPerEvent int
}

// NewRegistry creates a new webhook registry
func NewRegistry() *Registry {
	return &Registry{
		hooks:       make(map[string]*WebhookSubscription),
		byEvent:     make(map[EventType][]*WebhookSubscription),
		logger:      log.New(log.Writer(), "[WEBHOOKS] ", log.LstdFlags),
		maxPerEvent: 50,
	}
}

// Register adds a webhook subscription
func (r *Registry) Register(sub *WebhookSubscription) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if sub.URL == "" {
		return fmt.Errorf("webhook URL is required")
	}
	if len(sub.Events) == 0 {
		return fmt.Errorf("at least one event type is required")
	}

	if sub.ID == "" {
		sub.ID = fmt.Sprintf("wh-%d", time.Now().UnixNano())
	}
	sub.Active = true
	sub.CreatedAt = time.Now()
	sub.FailCount = 0

	r.hooks[sub.ID] = sub

	for _, evt := range sub.Events {
		r.byEvent[evt] = append(r.byEvent[evt], sub)
	}

	r.logger.Printf("📡 Registered webhook %s → %s (events: %v)", sub.ID, sub.URL, sub.Events)
	return nil
}

// Unregister removes a webhook subscription
func (r *Registry) Unregister(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	sub, ok := r.hooks[id]
	if !ok {
		return fmt.Errorf("webhook %s not found", id)
	}

	delete(r.hooks, id)

	// Remove from event index
	for _, evt := range sub.Events {
		filtered := make([]*WebhookSubscription, 0)
		for _, s := range r.byEvent[evt] {
			if s.ID != id {
				filtered = append(filtered, s)
			}
		}
		r.byEvent[evt] = filtered
	}

	r.logger.Printf("🗑️  Unregistered webhook %s", id)
	return nil
}

// GetSubscribers returns all active subscribers for an event type
func (r *Registry) GetSubscribers(eventType EventType) []*WebhookSubscription {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var active []*WebhookSubscription
	for _, sub := range r.byEvent[eventType] {
		if sub.Active {
			active = append(active, sub)
		}
	}
	return active
}

// ListAll returns all registered webhooks
func (r *Registry) ListAll() []*WebhookSubscription {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]*WebhookSubscription, 0, len(r.hooks))
	for _, sub := range r.hooks {
		result = append(result, sub)
	}
	return result
}

// MarkFailed increments failure count and disables after 10 failures
func (r *Registry) MarkFailed(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	sub, ok := r.hooks[id]
	if !ok {
		return
	}
	sub.FailCount++
	if sub.FailCount >= 10 {
		sub.Active = false
		r.logger.Printf("⚠️  Webhook %s disabled after %d failures", id, sub.FailCount)
	}
}

// SignPayload creates HMAC-SHA256 signature for webhook verification
func SignPayload(payload []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}
