/*
Mock Response Generator
Generates realistic API responses for sequestered requests
*/

package sop

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// MockGenerator generates mock API responses
type MockGenerator struct {
	schemas map[string]MockSchema
}

// MockSchema defines the response structure for an API
type MockSchema struct {
	Host     string                 `json:"host"`
	Endpoint string                 `json:"endpoint"`
	Method   string                 `json:"method"`
	Response map[string]interface{} `json:"response"`
}

// NewMockGenerator creates a new mock generator
func NewMockGenerator(schemaDir string) (*MockGenerator, error) {
	mg := &MockGenerator{
		schemas: make(map[string]MockSchema),
	}

	// Load schemas from directory
	if schemaDir != "" {
		if err := mg.loadSchemas(schemaDir); err != nil {
			return nil, err
		}
	}

	// Add default schemas
	mg.addDefaultSchemas()

	return mg, nil
}

// loadSchemas loads mock schemas from JSON files
func (mg *MockGenerator) loadSchemas(dir string) error {
	files, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return err
	}

	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}

		var schema MockSchema
		if err := json.Unmarshal(data, &schema); err != nil {
			continue
		}

		key := fmt.Sprintf("%s:%s:%s", schema.Host, schema.Method, schema.Endpoint)
		mg.schemas[key] = schema
	}

	return nil
}

// addDefaultSchemas adds common API mocks
func (mg *MockGenerator) addDefaultSchemas() {
	// Stripe Payment Intent
	mg.schemas["api.stripe.com:POST:/v1/payment_intents"] = MockSchema{
		Host:     "api.stripe.com",
		Endpoint: "/v1/payment_intents",
		Method:   "POST",
		Response: map[string]interface{}{
			"id":       "pi_mock_" + generateID(),
			"object":   "payment_intent",
			"amount":   1000,
			"currency": "usd",
			"status":   "succeeded",
			"created":  time.Now().Unix(),
		},
	}

	// Slack Message
	mg.schemas["slack.com:POST:/api/chat.postMessage"] = MockSchema{
		Host:     "slack.com",
		Endpoint: "/api/chat.postMessage",
		Method:   "POST",
		Response: map[string]interface{}{
			"ok":      true,
			"channel": "C12345678",
			"ts":      fmt.Sprintf("%d.%d", time.Now().Unix(), time.Now().Nanosecond()),
			"message": map[string]interface{}{
				"text": "Message sent (mock)",
				"user": "U12345678",
			},
		},
	}

	// GitHub API
	mg.schemas["api.github.com:POST:/repos"] = MockSchema{
		Host:     "api.github.com",
		Endpoint: "/repos",
		Method:   "POST",
		Response: map[string]interface{}{
			"id":         generateNumericID(),
			"name":       "mock-repo",
			"full_name":  "user/mock-repo",
			"private":    false,
			"created_at": time.Now().Format(time.RFC3339),
		},
	}

	// SendGrid Email
	mg.schemas["api.sendgrid.com:POST:/v3/mail/send"] = MockSchema{
		Host:     "api.sendgrid.com",
		Endpoint: "/v3/mail/send",
		Method:   "POST",
		Response: map[string]interface{}{
			"message_id": "mock_" + generateID(),
			"status":     "queued",
		},
	}

	// Generic success
	mg.schemas["*:*:*"] = MockSchema{
		Host:     "*",
		Endpoint: "*",
		Method:   "*",
		Response: map[string]interface{}{
			"success": true,
			"message": "Request sequestered (mock response)",
			"id":      generateID(),
		},
	}
}

// GenerateMock generates a mock response for a request
func (mg *MockGenerator) GenerateMock(host, method, path string) []byte {
	// Try exact match
	key := fmt.Sprintf("%s:%s:%s", host, method, path)
	if schema, ok := mg.schemas[key]; ok {
		return mg.renderResponse(schema.Response)
	}

	// Try host + method match
	key = fmt.Sprintf("%s:%s:*", host, method)
	if schema, ok := mg.schemas[key]; ok {
		return mg.renderResponse(schema.Response)
	}

	// Try host match
	key = fmt.Sprintf("%s:*:*", host)
	if schema, ok := mg.schemas[key]; ok {
		return mg.renderResponse(schema.Response)
	}

	// Fallback to generic
	if schema, ok := mg.schemas["*:*:*"]; ok {
		return mg.renderResponse(schema.Response)
	}

	// Ultimate fallback
	return []byte(`{"success":true,"message":"Mock response","sequestered":true}`)
}

// renderResponse converts response map to JSON
func (mg *MockGenerator) renderResponse(resp map[string]interface{}) []byte {
	data, err := json.Marshal(resp)
	if err != nil {
		return []byte(`{"error":"Failed to generate mock"}`)
	}
	return data
}

// generateID generates a random ID
func generateID() string {
	return fmt.Sprintf("%d%d", time.Now().Unix(), time.Now().Nanosecond())
}

// generateNumericID generates a numeric ID
func generateNumericID() int64 {
	return time.Now().UnixNano()
}
