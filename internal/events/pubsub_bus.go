package events

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"time"

	"cloud.google.com/go/pubsub"
)

// PubSubEventBus wraps the in-memory EventBus and also publishes every event
// to a Google Cloud Pub/Sub topic for durable, cross-service delivery.
//
// Fan-out strategy:
//   - Pub/Sub: durable, at-least-once delivery to downstream consumers
//   - In-memory: immediate push to SSE /events/stream subscribers
//
// Usage:
//
//	bus, err := events.NewPubSubEventBus("my-project", "ocx-events")
//	bus.Emit("ocx.verdict.allow", "/api/v1/govern", "tx-123", data)
//	defer bus.Close()
type PubSubEventBus struct {
	*EventBus // embedded — SSE subscribers, Subscribe/Unsubscribe still work

	client *pubsub.Client
	topic  *pubsub.Topic
	logger *log.Logger
}

// NewPubSubEventBus creates a Pub/Sub-backed event bus.
// It creates the topic if it does not exist.
func NewPubSubEventBus(projectID, topicID string) (*PubSubEventBus, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	client, err := pubsub.NewClient(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("pubsub.NewClient: %w", err)
	}

	topic := client.Topic(topicID)

	// Check if topic exists; create if not
	exists, err := topic.Exists(ctx)
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("topic.Exists: %w", err)
	}
	if !exists {
		topic, err = client.CreateTopic(ctx, topicID)
		if err != nil {
			client.Close()
			return nil, fmt.Errorf("CreateTopic: %w", err)
		}
		slog.Info("Created Pub/Sub topic", "topic_i_d", topicID)
	}

	// Enable message ordering by key (tenant isolation)
	topic.EnableMessageOrdering = true

	bus := &PubSubEventBus{
		EventBus: NewEventBus(),
		client:   client,
		topic:    topic,
		logger:   log.New(log.Writer(), "[PUBSUB] ", log.LstdFlags),
	}

	bus.logger.Printf("✅ Connected to Pub/Sub topic: projects/%s/topics/%s", projectID, topicID)
	return bus, nil
}

// Emit creates a CloudEvent, publishes it to Pub/Sub, and fans out to in-memory
// subscribers (SSE stream). This is the primary method called by handleGovern.
func (pb *PubSubEventBus) Emit(eventType, source, subject string, data map[string]interface{}) {
	event := NewCloudEvent(eventType, source, subject, data)

	// 1. Publish to Cloud Pub/Sub (durable)
	pb.publishToPubSub(event)

	// 2. Fan out to in-memory subscribers (SSE stream)
	pb.EventBus.Publish(event)
}

// publishToPubSub serializes the CloudEvent and publishes it as a Pub/Sub message.
// Message attributes map to CloudEvents metadata for server-side filtering.
func (pb *PubSubEventBus) publishToPubSub(event *CloudEvent) {
	payload, err := event.JSON()
	if err != nil {
		pb.logger.Printf("❌ Failed to marshal event %s: %v", event.ID, err)
		return
	}

	// Extract tenant ID from event data for ordering key
	tenantID := ""
	if event.TenantID != "" {
		tenantID = event.TenantID
	} else if tid, ok := event.Data["tenant_id"].(string); ok {
		tenantID = tid
	}

	msg := &pubsub.Message{
		Data: payload,
		Attributes: map[string]string{
			"ce-specversion": event.SpecVersion,
			"ce-type":        event.Type,
			"ce-source":      event.Source,
			"ce-id":          event.ID,
			"ce-time":        event.Time.Format(time.RFC3339Nano),
			"ce-tenantid":    tenantID,
		},
		OrderingKey: tenantID, // tenant-scoped ordering
	}

	result := pb.topic.Publish(context.Background(), msg)

	// Non-blocking: check result in a goroutine to avoid latency in the hot path
	go func() {
		serverID, err := result.Get(context.Background())
		if err != nil {
			pb.logger.Printf("❌ Pub/Sub publish failed: %s → %v", event.ID, err)
			return
		}
		pb.logger.Printf("📤 Published event %s → msgID=%s (type=%s)", event.ID, serverID, event.Type)
	}()
}

// PublishRaw publishes a pre-built CloudEvent to Pub/Sub and in-memory bus.
// Useful for replaying or forwarding events.
func (pb *PubSubEventBus) PublishRaw(event *CloudEvent) {
	pb.publishToPubSub(event)
	pb.EventBus.Publish(event)
}

// Close gracefully shuts down the Pub/Sub client.
// Call this from main() defer or shutdown handler.
func (pb *PubSubEventBus) Close() error {
	pb.topic.Stop()
	if err := pb.client.Close(); err != nil {
		return fmt.Errorf("pubsub client close: %w", err)
	}
	pb.logger.Printf("🔌 Pub/Sub client closed")
	return nil
}

// TopicPath returns the fully-qualified Pub/Sub topic path.
func (pb *PubSubEventBus) TopicPath() string {
	return pb.topic.String()
}

// HealthCheck verifies the Pub/Sub topic is reachable.
func (pb *PubSubEventBus) HealthCheck(ctx context.Context) error {
	exists, err := pb.topic.Exists(ctx)
	if err != nil {
		return fmt.Errorf("topic health check: %w", err)
	}
	if !exists {
		return fmt.Errorf("topic does not exist")
	}
	return nil
}

// MarshalStats returns basic telemetry about the bus.
func (pb *PubSubEventBus) MarshalStats() map[string]interface{} {
	return map[string]interface{}{
		"backend":         "gcp-pubsub",
		"topic":           pb.topic.String(),
		"sse_subscribers": pb.EventBus.SubscriberCount(),
	}
}

// ensure interface compatibility
var _ EventEmitter = (*PubSubEventBus)(nil)
