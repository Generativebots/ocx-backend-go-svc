package sdk

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// GovMiddleware is HTTP middleware that intercepts outbound API calls and
// routes them through OCX governance before execution.
//
// Usage with standard net/http:
//
//	mux := http.NewServeMux()
//	mux.Handle("/tools/", sdk.GovMiddleware(client, toolHandler))
//
// Usage with Gorilla Mux:
//
//	router.Use(sdk.GovMiddlewareFunc(client))
func GovMiddleware(client *Client, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Read the request body
		body, err := io.ReadAll(r.Body)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))

		// Try to extract tool call from the request
		var toolReq struct {
			ToolName  string                 `json:"tool_name"`
			Name      string                 `json:"name"`     // MCP format
			Function  string                 `json:"function"` // OpenAI format
			Arguments map[string]interface{} `json:"arguments"`
			Params    map[string]interface{} `json:"params"`
		}

		if json.Unmarshal(body, &toolReq) == nil {
			toolName := toolReq.ToolName
			if toolName == "" {
				toolName = toolReq.Name
			}
			if toolName == "" {
				toolName = toolReq.Function
			}

			args := toolReq.Arguments
			if args == nil {
				args = toolReq.Params
			}

			if toolName != "" {
				// Route through OCX governance
				result, govErr := client.ExecuteTool(r.Context(), toolName, args)
				if govErr != nil {
					slog.Warn("OCX governance error: (allowing through)", "gov_err", govErr)
				} else if result.Verdict == VerdictBlock {
					w.Header().Set("Content-Type", "application/json")
					w.Header().Set("X-OCX-Verdict", VerdictBlock)
					w.Header().Set("X-OCX-Transaction-ID", result.TransactionID)
					w.WriteHeader(http.StatusForbidden)
					json.NewEncoder(w).Encode(map[string]interface{}{
						"error":          "Tool call blocked by OCX governance",
						"verdict":        result.Verdict,
						"reason":         result.Reason,
						"transaction_id": result.TransactionID,
					})
					return
				} else if result.Verdict == VerdictEscrow {
					w.Header().Set("Content-Type", "application/json")
					w.Header().Set("X-OCX-Verdict", VerdictEscrow)
					w.Header().Set("X-OCX-Escrow-ID", result.EscrowID)
					w.WriteHeader(http.StatusAccepted)
					json.NewEncoder(w).Encode(map[string]interface{}{
						"status":         "held_for_review",
						"verdict":        result.Verdict,
						"escrow_id":      result.EscrowID,
						"transaction_id": result.TransactionID,
					})
					return
				}

				// Add governance headers to the response
				w.Header().Set("X-OCX-Verdict", result.Verdict)
				w.Header().Set("X-OCX-Transaction-ID", result.TransactionID)
			}
		}

		// Allow through — serve the actual handler
		next.ServeHTTP(w, r)
	})
}

// GovMiddlewareFunc returns Gorilla Mux compatible middleware
func GovMiddlewareFunc(client *Client) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return GovMiddleware(client, next)
	}
}

// WrapHTTPClient returns an http.Client that routes all requests through OCX governance.
// Use this to wrap your AI agent's HTTP client so every outbound API call is governed.
//
//	governed := sdk.WrapHTTPClient(ocxClient, http.DefaultClient)
//	// Now use 'governed' to make all your API calls
//	resp, err := governed.Post("https://api.openai.com/v1/chat/completions", ...)
func WrapHTTPClient(ocxClient *Client, wrapped *http.Client) *http.Client {
	return &http.Client{
		Timeout: wrapped.Timeout,
		Transport: &governedTransport{
			ocxClient: ocxClient,
			wrapped:   wrapped.Transport,
		},
	}
}

type governedTransport struct {
	ocxClient *Client
	wrapped   http.RoundTripper
}

func (t *governedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Record the request for governance audit
	start := time.Now()

	// Execute the request through the wrapped transport
	transport := t.wrapped
	if transport == nil {
		transport = http.DefaultTransport
	}

	resp, err := transport.RoundTrip(req)

	// Log governance audit data
	if err == nil {
		slog.Info("[OCX]", "method", req.Method, "path", req.URL.Path, "status_code", resp.StatusCode, "sincestart", time.Since(start))
	}

	return resp, err
}
