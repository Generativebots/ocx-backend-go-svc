package protocol

import (
	"time"
)

// AIProtocolType identifies the detected AI protocol in intercepted traffic
type AIProtocolType string

const (
	ProtoMCP       AIProtocolType = "MCP"          // Model Context Protocol (Anthropic)
	ProtoOpenAI    AIProtocolType = "OPENAI"       // OpenAI function calling / tool_calls
	ProtoA2A       AIProtocolType = "A2A"          // Google Agent-to-Agent
	ProtoLangChain AIProtocolType = "LANGCHAIN"    // LangChain / LlamaIndex agent
	ProtoRAG       AIProtocolType = "RAG"          // RAG retrieval + generation
	ProtoCrewAI    AIProtocolType = "CREWAI"       // CrewAI multi-agent
	ProtoAutoGen   AIProtocolType = "AUTOGEN"      // Microsoft AutoGen
	ProtoCustom    AIProtocolType = "CUSTOM_AGENT" // Custom / unknown agent framework
	ProtoRaw       AIProtocolType = "RAW"          // Unparseable — raw bytes
)

// AIPayload is the protocol-agnostic representation of ANY AI agent action
// detected in intercepted traffic. Regardless of whether the source is
// MCP, OpenAI, A2A, LangChain, RAG, or a custom framework, all parsed
// results normalize into this struct.
type AIPayload struct {
	// Protocol identifies which AI protocol was detected
	Protocol AIProtocolType `json:"protocol"`

	// ToolName is the normalized tool/function being called
	// Examples: "execute_payment", "search_records", "send_email"
	ToolName string `json:"tool_name"`

	// AgentID identifies the calling agent within the protocol
	// MCP: session-based, OpenAI: from headers, A2A: agent_card.name
	AgentID string `json:"agent_id"`

	// TenantID identifies the organization/tenant
	TenantID string `json:"tenant_id,omitempty"`

	// TaskID is the task/conversation/session identifier
	TaskID string `json:"task_id,omitempty"`

	// Arguments are the parsed tool call arguments
	Arguments map[string]interface{} `json:"arguments,omitempty"`

	// Model is the LLM model being invoked (if detectable)
	Model string `json:"model,omitempty"`

	// MessageType classifies the AI interaction
	// "tool_call", "tool_result", "retrieval", "generation", "agent_message"
	MessageType string `json:"message_type"`

	// Direction: "request" or "response"
	Direction string `json:"direction"`

	// Confidence is how confident the parser is (0.0-1.0) that it correctly
	// identified the protocol
	Confidence float64 `json:"confidence"`

	// RawMethod is the original method/action before normalization
	// MCP: "tools/call", OpenAI: "chat.completions", A2A: "tasks/send"
	RawMethod string `json:"raw_method,omitempty"`

	// Metadata holds protocol-specific fields that don't fit the common model
	Metadata map[string]interface{} `json:"metadata,omitempty"`

	// DetectedAt is when the payload was parsed
	DetectedAt time.Time `json:"detected_at"`
}

// AIPayloadParser is the interface all protocol-specific parsers implement
type AIPayloadParser interface {
	// Name returns the protocol name
	Name() AIProtocolType

	// CanParse returns true if this parser can handle the given payload
	// This should be a fast check (e.g., looking for "jsonrpc" or "tool_calls")
	CanParse(payload []byte) bool

	// Parse extracts AI payload information from raw bytes
	Parse(payload []byte) (*AIPayload, error)
}

// UniversalAIParser attempts all registered parsers in priority order
// and returns the first successful parse result
type UniversalAIParser struct {
	parsers []AIPayloadParser
}

// NewUniversalAIParser creates a parser with all built-in protocol parsers
func NewUniversalAIParser() *UniversalAIParser {
	return &UniversalAIParser{
		parsers: []AIPayloadParser{
			&MCPParser{},
			&OpenAIParser{},
			&A2AParser{},
			&AgentFrameworkParser{},
			&RAGParser{},
			&GenericAIDetector{},
		},
	}
}

// Parse tries all registered parsers and returns the best match
func (u *UniversalAIParser) Parse(payload []byte) *AIPayload {
	for _, parser := range u.parsers {
		if parser.CanParse(payload) {
			result, err := parser.Parse(payload)
			if err == nil && result != nil {
				return result
			}
		}
	}

	// Fallback — raw bytes, no AI protocol detected
	return &AIPayload{
		Protocol:    ProtoRaw,
		ToolName:    "network_call",
		MessageType: "unknown",
		Direction:   "request",
		Confidence:  0.0,
		DetectedAt:  time.Now(),
	}
}

// RegisterParser adds a custom parser (for custom agent frameworks)
func (u *UniversalAIParser) RegisterParser(p AIPayloadParser) {
	// Insert before GenericAIDetector (last resort)
	if len(u.parsers) > 0 {
		u.parsers = append(u.parsers[:len(u.parsers)-1], p, u.parsers[len(u.parsers)-1])
	} else {
		u.parsers = append(u.parsers, p)
	}
}
