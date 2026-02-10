package protocol

import (
	"encoding/json"
	"strings"
	"time"
)

// A2AParser handles Google Agent-to-Agent protocol payloads
// A2A spec: https://google.github.io/A2A
//
// A2A uses JSON-RPC 2.0 with methods like:
//   - tasks/send (create or update a task)
//   - tasks/get (get task status)
//   - tasks/cancel (cancel a task)
//   - tasks/pushNotification/set (subscribe to task updates)
//   - agent/authenticatedExtendedCard (get agent capabilities)
type A2AParser struct{}

// a2aRequest represents an A2A JSON-RPC request
type a2aRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// a2aTaskSendParams represents params for tasks/send
type a2aTaskSendParams struct {
	ID      string `json:"id,omitempty"`
	Message struct {
		Role  string `json:"role"`
		Parts []struct {
			Type     string `json:"type,omitempty"`
			Text     string `json:"text,omitempty"`
			MimeType string `json:"mimeType,omitempty"`
		} `json:"parts"`
	} `json:"message"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// a2aAgentCard represents an Agent Card
type a2aAgentCard struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	URL         string `json:"url"`
	Provider    struct {
		Organization string `json:"organization"`
	} `json:"provider"`
	Skills []struct {
		ID          string   `json:"id"`
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Tags        []string `json:"tags,omitempty"`
	} `json:"skills"`
}

func (p *A2AParser) Name() AIProtocolType { return ProtoA2A }

func (p *A2AParser) CanParse(payload []byte) bool {
	s := string(payload)
	return strings.Contains(s, `"jsonrpc"`) &&
		(strings.Contains(s, `"tasks/`) ||
			strings.Contains(s, `"agent/`) ||
			strings.Contains(s, `"parts"`) ||
			strings.Contains(s, `"skills"`))
}

func (p *A2AParser) Parse(payload []byte) (*AIPayload, error) {
	jsonStart := findJSONStart(payload)
	if jsonStart < 0 {
		return nil, errNotJSON
	}

	var req a2aRequest
	if err := json.Unmarshal(payload[jsonStart:], &req); err != nil {
		return nil, err
	}

	// Verify it's actually A2A, not MCP (both use JSON-RPC 2.0)
	if !strings.HasPrefix(req.Method, "tasks/") &&
		!strings.HasPrefix(req.Method, "agent/") {
		return nil, errNotJSON
	}

	result := &AIPayload{
		Protocol:    ProtoA2A,
		RawMethod:   req.Method,
		Direction:   "request",
		Confidence:  0.92,
		DetectedAt:  time.Now(),
		Metadata:    make(map[string]interface{}),
		MessageType: classifyA2AMethod(req.Method),
	}

	switch {
	case req.Method == "tasks/send":
		var params a2aTaskSendParams
		if err := json.Unmarshal(req.Params, &params); err == nil {
			result.TaskID = params.ID
			result.ToolName = "agent_task"
			result.Confidence = 0.95

			// Extract text content from parts
			for _, part := range params.Message.Parts {
				if part.Text != "" {
					result.Arguments = map[string]interface{}{
						"text": part.Text,
						"role": params.Message.Role,
					}
					break
				}
			}
		}

	case req.Method == "tasks/get":
		result.ToolName = "task_status"
		result.MessageType = "query"

	case req.Method == "tasks/cancel":
		result.ToolName = "task_cancel"
		result.MessageType = "control"

	case strings.HasPrefix(req.Method, "agent/"):
		result.ToolName = "_agent_discovery"
		result.MessageType = "handshake"

	default:
		result.ToolName = req.Method
	}

	return result, nil
}

func classifyA2AMethod(method string) string {
	switch {
	case method == "tasks/send":
		return "tool_call"
	case method == "tasks/get":
		return "query"
	case method == "tasks/cancel":
		return "control"
	case strings.HasPrefix(method, "agent/"):
		return "handshake"
	default:
		return "agent_message"
	}
}
