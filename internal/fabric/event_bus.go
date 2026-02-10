// Package fabric — Event Bus Interface (P2 FIX #14)
//
// Provides a pluggable event bus for inter-service event distribution.
// The Hub can publish events (trust score changed, verdict issued, billing alert)
// and services can subscribe to relevant event topics.
package fabric

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// EventType classifies event categories.
type EventType string

const (
	EventTrustScoreChanged EventType = "trust.score.changed"
	EventVerdictIssued     EventType = "verdict.issued"
	EventBillingAlert      EventType = "billing.alert"
	EventSpokeConnected    EventType = "spoke.connected"
	EventSpokeDisconnected EventType = "spoke.disconnected"
	EventHandshakeComplete EventType = "handshake.complete"
	EventPolicyViolation   EventType = "policy.violation"
)

// Event represents a domain event in the OCX system.
type Event struct {
	ID        string                 `json:"id"`
	Type      EventType              `json:"type"`
	Source    string                 `json:"source"`
	TenantID  string                 `json:"tenant_id"`
	Payload   map[string]interface{} `json:"payload"`
	Timestamp time.Time              `json:"timestamp"`
}

// EventHandler processes events of a subscribed type.
type EventHandler func(ctx context.Context, event *Event) error

// EventBus provides publish/subscribe for domain events.
//
// P2 FIX #14: The Hub currently routes messages synchronously within a single
// process. No event bus exists for async event processing (e.g., "trust score
// changed" → notify all interested services). This interface allows plugging
// in Redis Pub/Sub, NATS, or a local in-process implementation.
//
// TODO(scale): Implement RedisEventBus backed by Redis Pub/Sub for
// cross-process event distribution.
type EventBus interface {
	// Publish sends an event to all subscribers of the event type.
	Publish(ctx context.Context, event *Event) error

	// Subscribe registers a handler for a specific event type.
	// Returns an unsubscribe function.
	Subscribe(eventType EventType, handler EventHandler) (unsubscribe func())

	// Close shuts down the event bus.
	Close() error
}

// ============================================================================
// LOCAL EVENT BUS (in-process, for single-pod deployments)
// ============================================================================

// LocalEventBus provides an in-memory pub/sub implementation.
// Suitable for single-process deployments; use RedisEventBus for multi-pod.
type LocalEventBus struct {
	mu          sync.RWMutex
	subscribers map[EventType][]subscriberEntry
	closed      bool
}

type subscriberEntry struct {
	id      int
	handler EventHandler
}

var subscriberCounter int

// NewLocalEventBus creates a new in-memory event bus.
func NewLocalEventBus() *LocalEventBus {
	return &LocalEventBus{
		subscribers: make(map[EventType][]subscriberEntry),
	}
}

// Publish sends an event to all matching subscribers asynchronously.
func (b *LocalEventBus) Publish(ctx context.Context, event *Event) error {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.closed {
		return nil
	}

	handlers := b.subscribers[event.Type]
	for _, entry := range handlers {
		h := entry.handler
		go func() {
			if err := h(ctx, event); err != nil {
				slog.Warn("[EventBus] Handler error for", "type", event.Type, "error", err)
			}
		}()
	}

	return nil
}

// Subscribe registers a handler for a specific event type.
func (b *LocalEventBus) Subscribe(eventType EventType, handler EventHandler) func() {
	b.mu.Lock()
	defer b.mu.Unlock()

	subscriberCounter++
	id := subscriberCounter
	b.subscribers[eventType] = append(b.subscribers[eventType], subscriberEntry{
		id:      id,
		handler: handler,
	})

	// Return unsubscribe function
	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		subs := b.subscribers[eventType]
		for i, entry := range subs {
			if entry.id == id {
				b.subscribers[eventType] = append(subs[:i], subs[i+1:]...)
				break
			}
		}
	}
}

// Close shuts down the event bus.
func (b *LocalEventBus) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	b.subscribers = nil
	return nil
}
