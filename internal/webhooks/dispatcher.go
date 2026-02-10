package webhooks

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

// Dispatcher sends webhook events to registered subscribers asynchronously
type Dispatcher struct {
	registry   *Registry
	httpClient *http.Client
	queue      chan *deliveryJob
	logger     *log.Logger
	wg         sync.WaitGroup
	workers    int
}

type deliveryJob struct {
	subscriber *WebhookSubscription
	event      *WebhookEvent
	attempt    int
}

// NewDispatcher creates a webhook dispatcher with a background worker pool
func NewDispatcher(registry *Registry, workers int) *Dispatcher {
	if workers <= 0 {
		workers = 4
	}
	d := &Dispatcher{
		registry: registry,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		queue:   make(chan *deliveryJob, 1000),
		logger:  log.New(log.Writer(), "[DISPATCH] ", log.LstdFlags),
		workers: workers,
	}

	// Start worker pool
	for i := 0; i < workers; i++ {
		d.wg.Add(1)
		go d.worker(i)
	}

	return d
}

// Emit sends an event to all registered subscribers for that event type
func (d *Dispatcher) Emit(eventType EventType, tenantID string, data map[string]interface{}) {
	subscribers := d.registry.GetSubscribers(eventType)
	if len(subscribers) == 0 {
		return
	}

	event := &WebhookEvent{
		ID:        fmt.Sprintf("evt-%d", time.Now().UnixNano()),
		Type:      eventType,
		Source:    "/api/v1/govern",
		Timestamp: time.Now(),
		TenantID:  tenantID,
		Data:      data,
	}

	for _, sub := range subscribers {
		// Only deliver to same tenant
		if sub.TenantID != "" && sub.TenantID != tenantID {
			continue
		}

		select {
		case d.queue <- &deliveryJob{subscriber: sub, event: event, attempt: 1}:
		default:
			d.logger.Printf("⚠️  Webhook queue full, dropping event %s for %s", event.ID, sub.ID)
		}
	}
}

func (d *Dispatcher) worker(id int) {
	defer d.wg.Done()

	for job := range d.queue {
		d.deliver(job)
	}
}

func (d *Dispatcher) deliver(job *deliveryJob) {
	payload, err := json.Marshal(job.event)
	if err != nil {
		d.logger.Printf("❌ Failed to marshal webhook event: %v", err)
		return
	}

	req, err := http.NewRequest("POST", job.subscriber.URL, bytes.NewReader(payload))
	if err != nil {
		d.logger.Printf("❌ Failed to create webhook request: %v", err)
		return
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-OCX-Event-Type", string(job.event.Type))
	req.Header.Set("X-OCX-Event-ID", job.event.ID)
	req.Header.Set("X-OCX-Delivery-Attempt", fmt.Sprintf("%d", job.attempt))

	// Sign payload if secret is configured
	if job.subscriber.Secret != "" {
		sig := SignPayload(payload, job.subscriber.Secret)
		req.Header.Set("X-OCX-Signature", "sha256="+sig)
	}

	resp, err := d.httpClient.Do(req)
	if err != nil {
		d.logger.Printf("❌ Webhook delivery failed: %s → %v", job.subscriber.URL, err)
		d.registry.MarkFailed(job.subscriber.ID)

		// Retry up to 3 times with exponential backoff
		if job.attempt < 3 {
			time.Sleep(time.Duration(job.attempt*job.attempt) * time.Second)
			job.attempt++
			select {
			case d.queue <- job:
			default:
			}
		}
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		d.logger.Printf("⚠️  Webhook returned %d: %s → %s", resp.StatusCode, job.subscriber.URL, job.event.Type)
		d.registry.MarkFailed(job.subscriber.ID)
	} else {
		d.logger.Printf("✅ Webhook delivered: %s → %s (%s)", job.event.Type, job.subscriber.URL, job.event.ID)
	}
}

// Shutdown gracefully shuts down the dispatcher
func (d *Dispatcher) Shutdown() {
	close(d.queue)
	d.wg.Wait()
}
