// Package fabric — Redis-backed EventBus for cross-pod event distribution.
//
// In a multi-pod deployment, the LocalEventBus only delivers events within a
// single process. RedisEventBus uses Redis Pub/Sub so events published on pod 1
// are received by subscribers on pod 2.
package fabric

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
)

// RedisPubSubClient is a minimal interface for Redis Pub/Sub operations.
// Separate from RedisClient because pub/sub has a different usage pattern.
type RedisPubSubClient interface {
	// Publish sends a message to a Redis channel.
	Publish(ctx context.Context, channel string, message []byte) error

	// Subscribe registers a callback for messages on a channel.
	// Returns an unsubscribe function.
	Subscribe(ctx context.Context, channel string, handler func([]byte)) (unsubscribe func(), err error)
}

// RedisEventBus distributes events across pods using Redis Pub/Sub.
// Locally, it also fans out to in-process subscribers for zero-latency
// delivery to co-located handlers.
type RedisEventBus struct {
	mu         sync.RWMutex
	pubsub     RedisPubSubClient
	prefix     string // Redis channel prefix, e.g. "ocx:events:"
	localSubs  map[EventType][]subscriberEntry
	unsubFuncs []func() // Redis unsubscribe functions for cleanup
	closed     bool
}

// NewRedisEventBus creates a new Redis-backed event bus.
func NewRedisEventBus(client RedisPubSubClient, channelPrefix string) *RedisEventBus {
	if channelPrefix == "" {
		channelPrefix = "ocx:events:"
	}
	return &RedisEventBus{
		pubsub:    client,
		prefix:    channelPrefix,
		localSubs: make(map[EventType][]subscriberEntry),
	}
}

// Publish sends an event to Redis Pub/Sub so all pods receive it.
// Returns immediately after publishing — delivery is asynchronous.
func (b *RedisEventBus) Publish(ctx context.Context, event *Event) error {
	b.mu.RLock()
	if b.closed {
		b.mu.RUnlock()
		return fmt.Errorf("event bus is closed")
	}
	b.mu.RUnlock()

	// Assign an ID if missing
	if event.ID == "" {
		event.ID = uuid.New().String()
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}

	channel := b.prefix + string(event.Type)
	if err := b.pubsub.Publish(ctx, channel, data); err != nil {
		slog.Warn("[RedisEventBus] Publish failed, falling back to local",
			"type", event.Type, "error", err)
		// Fall back to local-only delivery
		b.deliverLocal(ctx, event)
		return nil
	}

	return nil
}

// Subscribe registers a handler for a specific event type.
// The handler receives events from ALL pods (via Redis) and local publishers.
func (b *RedisEventBus) Subscribe(eventType EventType, handler EventHandler) func() {
	b.mu.Lock()
	defer b.mu.Unlock()

	subscriberCounter++
	id := subscriberCounter

	b.localSubs[eventType] = append(b.localSubs[eventType], subscriberEntry{
		id:      id,
		handler: handler,
	})

	// Subscribe to Redis channel for this event type
	channel := b.prefix + string(eventType)
	unsub, err := b.pubsub.Subscribe(context.Background(), channel, func(data []byte) {
		var event Event
		if err := json.Unmarshal(data, &event); err != nil {
			slog.Warn("[RedisEventBus] Failed to unmarshal event", "error", err)
			return
		}
		// Deliver to matching local handlers
		b.deliverLocal(context.Background(), &event)
	})

	if err != nil {
		slog.Warn("[RedisEventBus] Redis subscribe failed, local-only mode",
			"type", eventType, "error", err)
	} else {
		b.unsubFuncs = append(b.unsubFuncs, unsub)
	}

	// Return unsubscribe function
	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		subs := b.localSubs[eventType]
		for i, entry := range subs {
			if entry.id == id {
				b.localSubs[eventType] = append(subs[:i], subs[i+1:]...)
				break
			}
		}
	}
}

// Close shuts down the event bus and all Redis subscriptions.
func (b *RedisEventBus) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true

	for _, unsub := range b.unsubFuncs {
		unsub()
	}
	b.unsubFuncs = nil
	b.localSubs = nil

	slog.Info("[RedisEventBus] Closed")
	return nil
}

// deliverLocal fans out an event to all matching in-process subscribers.
func (b *RedisEventBus) deliverLocal(ctx context.Context, event *Event) {
	b.mu.RLock()
	handlers := b.localSubs[event.Type]
	b.mu.RUnlock()

	for _, entry := range handlers {
		h := entry.handler
		go func() {
			if err := h(ctx, event); err != nil {
				slog.Warn("[RedisEventBus] Handler error", "type", event.Type, "error", err)
			}
		}()
	}
}
