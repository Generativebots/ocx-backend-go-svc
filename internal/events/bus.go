package events

import (
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"
)

// EventEmitter is the interface for publishing CloudEvents.
// Both the in-memory EventBus and PubSubEventBus satisfy this interface.
type EventEmitter interface {
	Emit(eventType, source, subject string, data map[string]interface{})
}

// CloudEvent is the CloudEvents 1.0 envelope for all OCX events.
// Compatible with CNCF CloudEvents specification.
type CloudEvent struct {
	SpecVersion string                 `json:"specversion"`
	Type        string                 `json:"type"`
	Source      string                 `json:"source"`
	ID          string                 `json:"id"`
	Time        time.Time              `json:"time"`
	Subject     string                 `json:"subject,omitempty"`
	TenantID    string                 `json:"tenantid,omitempty"`
	Data        map[string]interface{} `json:"data"`
}

// NewCloudEvent creates a CloudEvents 1.0 compliant event
func NewCloudEvent(eventType, source, subject string, data map[string]interface{}) *CloudEvent {
	return &CloudEvent{
		SpecVersion: "1.0",
		Type:        eventType,
		Source:      source,
		ID:          fmt.Sprintf("ce-%d", time.Now().UnixNano()),
		Time:        time.Now(),
		Subject:     subject,
		Data:        data,
	}
}

// JSON serializes the event
func (ce *CloudEvent) JSON() ([]byte, error) {
	return json.Marshal(ce)
}

// SSEFormat returns the event in Server-Sent Events format
func (ce *CloudEvent) SSEFormat() ([]byte, error) {
	data, err := json.Marshal(ce)
	if err != nil {
		return nil, err
	}
	return []byte(fmt.Sprintf("event: %s\ndata: %s\nid: %s\n\n", ce.Type, data, ce.ID)), nil
}

// EventBus is an in-process pub/sub event bus.
// Subscribers receive CloudEvents in real time.
type EventBus struct {
	mu          sync.RWMutex
	subscribers map[string][]chan *CloudEvent // eventType -> channels
	allSubs     []chan *CloudEvent            // subscribers to all events
	logger      *log.Logger
	bufferSize  int
}

// NewEventBus creates a new event bus
func NewEventBus() *EventBus {
	return &EventBus{
		subscribers: make(map[string][]chan *CloudEvent),
		allSubs:     make([]chan *CloudEvent, 0),
		logger:      log.New(log.Writer(), "[EVENTS] ", log.LstdFlags),
		bufferSize:  100,
	}
}

// Subscribe creates a channel that receives events of specific types.
// Pass empty eventTypes to receive ALL events.
func (eb *EventBus) Subscribe(eventTypes ...string) chan *CloudEvent {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	ch := make(chan *CloudEvent, eb.bufferSize)

	if len(eventTypes) == 0 {
		eb.allSubs = append(eb.allSubs, ch)
	} else {
		for _, et := range eventTypes {
			eb.subscribers[et] = append(eb.subscribers[et], ch)
		}
	}

	return ch
}

// Unsubscribe removes a subscription channel
func (eb *EventBus) Unsubscribe(ch chan *CloudEvent) {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	// Remove from type-specific subs
	for et, subs := range eb.subscribers {
		filtered := make([]chan *CloudEvent, 0)
		for _, s := range subs {
			if s != ch {
				filtered = append(filtered, s)
			}
		}
		eb.subscribers[et] = filtered
	}

	// Remove from all subs
	filtered := make([]chan *CloudEvent, 0)
	for _, s := range eb.allSubs {
		if s != ch {
			filtered = append(filtered, s)
		}
	}
	eb.allSubs = filtered

	close(ch)
}

// Publish sends an event to all matching subscribers
func (eb *EventBus) Publish(event *CloudEvent) {
	eb.mu.RLock()
	defer eb.mu.RUnlock()

	// Deliver to type-specific subscribers
	for _, ch := range eb.subscribers[event.Type] {
		select {
		case ch <- event:
		default:
			// Channel full, skip
		}
	}

	// Deliver to "all" subscribers
	for _, ch := range eb.allSubs {
		select {
		case ch <- event:
		default:
		}
	}
}

// Emit is a convenience method to create and publish an event
func (eb *EventBus) Emit(eventType, source, subject string, data map[string]interface{}) {
	event := NewCloudEvent(eventType, source, subject, data)
	eb.Publish(event)
}

// SubscriberCount returns the total number of active subscribers
func (eb *EventBus) SubscriberCount() int {
	eb.mu.RLock()
	defer eb.mu.RUnlock()

	count := len(eb.allSubs)
	for _, subs := range eb.subscribers {
		count += len(subs)
	}
	return count
}
