package protocol

import (
	"encoding/json"
	"strings"
	"time"
)

// MCPParser handles Model Context Protocol (Anthropic) JSON-RPC 2.0 payloads
// MCP spec: https://modelcontextprotocol.io
//
// MCP uses JSON-RPC 2.0 with methods like:
//   - tools/call (agent invokes a tool)
//   - tools/list (agent lists available tools)
//   - resources/read (agent reads a resource)
//   - prompts/get (agent retrieves a prompt)
//   - sampling/createMessage (server requests LLM completion)
type MCPParser struct{}

// mcpRequest represents a JSON-RPC 2.0 request in MCP format
type mcpRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// mcpToolCallParams is the params for tools/call
type mcpToolCallParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments,omitempty"`
	Meta      map[string]interface{} `json:"_meta,omitempty"`
}

// mcpResourceReadParams is the params for resources/read
type mcpResourceReadParams struct {
	URI string `json:"uri"`
}

func (p *MCPParser) Name() AIProtocolType { return ProtoMCP }

func (p *MCPParser) CanParse(payload []byte) bool {
	// Quick heuristic: MCP uses JSON-RPC 2.0
	s := string(payload)
	return strings.Contains(s, `"jsonrpc"`) &&
		(strings.Contains(s, `"tools/`) ||
			strings.Contains(s, `"resources/`) ||
			strings.Contains(s, `"prompts/`) ||
			strings.Contains(s, `"sampling/`) ||
			strings.Contains(s, `"completion/`) ||
			strings.Contains(s, `"initialize"`))
}

func (p *MCPParser) Parse(payload []byte) (*AIPayload, error) {
	// Find the JSON object in the payload (may have HTTP headers before it)
	jsonStart := findJSONStart(payload)
	if jsonStart < 0 {
		return nil, errNotJSON
	}

	var req mcpRequest
	if err := json.Unmarshal(payload[jsonStart:], &req); err != nil {
		return nil, err
	}

	if req.JSONRPC != "2.0" {
		return nil, errNotJSON
	}

	result := &AIPayload{
		Protocol:    ProtoMCP,
		RawMethod:   req.Method,
		Direction:   "request",
		Confidence:  0.95,
		DetectedAt:  time.Now(),
		Metadata:    make(map[string]interface{}),
		MessageType: classifyMCPMethod(req.Method),
	}

	switch {
	case req.Method == "tools/call":
		var params mcpToolCallParams
		if err := json.Unmarshal(req.Params, &params); err == nil {
			result.ToolName = params.Name
			result.Arguments = params.Arguments
			result.Confidence = 0.99
		}

	case req.Method == "tools/list":
		result.ToolName = "_list_tools"
		result.MessageType = "discovery"

	case req.Method == "resources/read":
		var params mcpResourceReadParams
		if err := json.Unmarshal(req.Params, &params); err == nil {
			result.ToolName = "resource_read"
			result.Arguments = map[string]interface{}{"uri": params.URI}
		}

	case req.Method == "resources/list":
		result.ToolName = "_list_resources"
		result.MessageType = "discovery"

	case req.Method == "prompts/get":
		result.ToolName = "prompt_get"
		result.MessageType = "retrieval"

	case req.Method == "sampling/createMessage":
		result.ToolName = "llm_completion"
		result.MessageType = "generation"

	case req.Method == "initialize":
		result.ToolName = "_handshake"
		result.MessageType = "handshake"

	default:
		result.ToolName = req.Method
	}

	// Check if this is a response (has "result" or "error" field)
	if strings.Contains(string(payload[jsonStart:]), `"result"`) ||
		strings.Contains(string(payload[jsonStart:]), `"error"`) {
		result.Direction = "response"
	}

	return result, nil
}

func classifyMCPMethod(method string) string {
	switch {
	case strings.HasPrefix(method, "tools/"):
		return "tool_call"
	case strings.HasPrefix(method, "resources/"):
		return "retrieval"
	case strings.HasPrefix(method, "prompts/"):
		return "retrieval"
	case strings.HasPrefix(method, "sampling/"):
		return "generation"
	default:
		return "control"
	}
}
