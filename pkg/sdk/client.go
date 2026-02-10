// Package sdk provides the OCX Governance SDK for AI agent integration.
//
// This is the "code drop" — the library that AI developers embed in their
// agents to route ALL tool calls through the OCX governance pipeline.
//
// Three integration patterns:
//
//  1. Direct: ocx.ExecuteTool("execute_payment", args) — wraps any tool call
//  2. Middleware: ocx.GovMiddleware(handler) — HTTP middleware for API servers
//  3. Proxy: Point your AI agent's base URL to the OCX proxy gateway
//
// Quick Start:
//
//	client := sdk.NewClient(sdk.Config{
//	    GatewayURL: "https://ocx-gateway.yourcompany.com",
//	    TenantID:   "your-tenant-id",
//	    APIKey:     os.Getenv("OCX_API_KEY"),
//	})
//
//	// Before: directCall("execute_payment", args)
//	// After:
//	result, err := client.ExecuteTool(ctx, "execute_payment", args)
//	if result.Verdict == sdk.VerdictAllow {
//	    // Tool was approved — execute it
//	}
package sdk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Config holds the OCX SDK configuration.
type Config struct {
	// GatewayURL is the OCX API Gateway endpoint (required)
	// Examples: "https://ocx.yourcompany.com", "http://localhost:8080"
	GatewayURL string

	// TenantID identifies your organization (required)
	TenantID string

	// APIKey authenticates requests to OCX (required in production)
	APIKey string

	// AgentID identifies this specific AI agent instance
	// Auto-generated if empty
	AgentID string

	// Timeout for governance decisions (default 30s)
	Timeout time.Duration

	// OnBlock is called when a tool call is blocked by governance
	OnBlock func(result *GovernanceResult)

	// OnEscrow is called when a tool call is held in escrow (Class B)
	OnEscrow func(result *GovernanceResult)
}

// Client is the OCX Governance SDK client. Embed this in your AI agent
// to route all tool calls through the patent governance pipeline.
type Client struct {
	config     Config
	httpClient *http.Client
}

// NewClient creates a new OCX SDK client.
//
//	client := sdk.NewClient(sdk.Config{
//	    GatewayURL: "https://ocx-gateway.example.com",
//	    TenantID:   "acme-corp",
//	    APIKey:     os.Getenv("OCX_API_KEY"),
//	})
func NewClient(cfg Config) *Client {
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.AgentID == "" {
		cfg.AgentID = fmt.Sprintf("agent-%d", time.Now().UnixNano())
	}

	return &Client{
		config: cfg,
		httpClient: &http.Client{
			Timeout: cfg.Timeout,
		},
	}
}

// ExecuteTool sends a tool call through the OCX governance pipeline.
// This is the primary integration point — call this instead of invoking
// your tool directly.
//
// Example:
//
//	result, err := client.ExecuteTool(ctx, "execute_payment", map[string]interface{}{
//	    "amount":   100.00,
//	    "currency": "USD",
//	    "to":       "vendor@example.com",
//	})
//	switch result.Verdict {
//	case sdk.VerdictAllow:
//	    // Approved — safe to execute the actual tool
//	    executePayment(result.Arguments)
//	case sdk.VerdictBlock:
//	    // Blocked by governance — do NOT execute
//	    log.Printf("Blocked: %s", result.Reason)
//	case sdk.VerdictEscrow:
//	    // Held for human review — wait or proceed based on policy
//	    log.Printf("In escrow: %s", result.EscrowID)
//	}
func (c *Client) ExecuteTool(ctx context.Context, toolName string, args map[string]interface{}) (*GovernanceResult, error) {
	req := &ToolRequest{
		ToolName:  toolName,
		AgentID:   c.config.AgentID,
		TenantID:  c.config.TenantID,
		Arguments: args,
		Timestamp: time.Now(),
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("ocx-sdk: failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST",
		c.config.GatewayURL+"/api/v1/govern", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("ocx-sdk: failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Tenant-ID", c.config.TenantID)
	httpReq.Header.Set("X-Agent-ID", c.config.AgentID)
	if c.config.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.config.APIKey)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ocx-sdk: gateway request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("ocx-sdk: failed to read response: %w", err)
	}

	var result GovernanceResult
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("ocx-sdk: failed to parse response: %w", err)
	}

	// Trigger callbacks
	switch result.Verdict {
	case VerdictBlock:
		if c.config.OnBlock != nil {
			c.config.OnBlock(&result)
		}
	case VerdictEscrow:
		if c.config.OnEscrow != nil {
			c.config.OnEscrow(&result)
		}
	}

	return &result, nil
}

// CheckEntitlement verifies if the agent has permission to call a specific tool.
func (c *Client) CheckEntitlement(ctx context.Context, toolName string) (bool, error) {
	httpReq, err := http.NewRequestWithContext(ctx, "GET",
		fmt.Sprintf("%s/api/v1/entitlements/active?agent_id=%s", c.config.GatewayURL, c.config.AgentID), nil)
	if err != nil {
		return false, err
	}
	httpReq.Header.Set("X-Tenant-ID", c.config.TenantID)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK, nil
}

// GetTrustScore retrieves the agent's current trust score.
func (c *Client) GetTrustScore(ctx context.Context) (float64, string, error) {
	httpReq, err := http.NewRequestWithContext(ctx, "GET",
		fmt.Sprintf("%s/api/reputation/%s", c.config.GatewayURL, c.config.AgentID), nil)
	if err != nil {
		return 0, "", err
	}
	httpReq.Header.Set("X-Tenant-ID", c.config.TenantID)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()

	var result struct {
		Score float64 `json:"score"`
		Tier  string  `json:"tier"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, "", err
	}

	return result.Score, result.Tier, nil
}
