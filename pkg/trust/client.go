package trust

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
)

// Config holds the client configuration
type Config struct {
	ExchangeURL string
	AgentID     string
	AgentName   string
}

// Client is the Trust Exchange client
type Client struct {
	Config Config
}

// NewClient creates a new Trust Exchange client
func NewClient(cfg Config) *Client {
	if cfg.ExchangeURL == "" {
		cfg.ExchangeURL = "http://localhost:8080"
	}
	return &Client{Config: cfg}
}

// CheckIn registers the agent with the exchange
func (c *Client) CheckIn() error {
	// Simple firewall check simulation
	return nil
}

// VerifyIntent sends an intent to the exchange and returns the trust token
func (c *Client) VerifyIntent(action string, payload map[string]interface{}) (string, error) {
	reqBody, _ := json.Marshal(map[string]interface{}{
		"agent_id": c.Config.AgentID,
		"action":   action,
		"payload":  payload,
	})

	resp, err := http.Post(c.Config.ExchangeURL+"/verify-intent", "application/json", bytes.NewBuffer(reqBody))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := ioutil.ReadAll(resp.Body)
		return "", fmt.Errorf("trust exchange denied intent: %s", string(body))
	}

	var result struct {
		Token      string  `json:"token"`
		Authorized bool    `json:"authorized"`
		Score      float64 `json:"score"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	if !result.Authorized {
		return "", fmt.Errorf("intent unauthorized (Score: %.1f)", result.Score)
	}

	return result.Token, nil
}

// InjectHeader adds the trust token to an outbound request
func InjectHeader(req *http.Request, token string) {
	req.Header.Set("X-Agent-Trust-Token", token)
}
