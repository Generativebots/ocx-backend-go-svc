package protocol

import (
	"encoding/json"
	"strings"
	"time"
)

// OpenAIParser handles OpenAI-style API payloads including:
//   - Chat Completions with tool_calls / function_call
//   - Assistants API with tool invocations
//   - Compatible APIs: Azure OpenAI, Groq, Together, Fireworks, vLLM, Ollama
//
// These all use the same schema: POST /v1/chat/completions
// with "tools" array and "tool_calls" in responses
type OpenAIParser struct{}

// openaiRequest represents a chat completion request
type openaiRequest struct {
	Model       string          `json:"model,omitempty"`
	Messages    []openaiMessage `json:"messages,omitempty"`
	Tools       []openaiToolDef `json:"tools,omitempty"`
	ToolChoice  interface{}     `json:"tool_choice,omitempty"`
	Stream      bool            `json:"stream,omitempty"`
	Temperature float64         `json:"temperature,omitempty"`
}

type openaiMessage struct {
	Role       string           `json:"role"`
	Content    interface{}      `json:"content,omitempty"`
	ToolCalls  []openaiToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
	Name       string           `json:"name,omitempty"`
}

type openaiToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"` // JSON string
	} `json:"function"`
}

type openaiToolDef struct {
	Type     string `json:"type"`
	Function struct {
		Name        string      `json:"name"`
		Description string      `json:"description"`
		Parameters  interface{} `json:"parameters"`
	} `json:"function"`
}

// openaiResponse represents a chat completion response with tool_calls
type openaiResponse struct {
	Choices []struct {
		Message struct {
			Role      string           `json:"role"`
			ToolCalls []openaiToolCall `json:"tool_calls,omitempty"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Model string `json:"model,omitempty"`
}

func (p *OpenAIParser) Name() AIProtocolType { return ProtoOpenAI }

func (p *OpenAIParser) CanParse(payload []byte) bool {
	s := string(payload)
	return (strings.Contains(s, `"model"`) &&
		(strings.Contains(s, `"messages"`) || strings.Contains(s, `"tool_calls"`))) ||
		strings.Contains(s, `"chat/completions"`) ||
		strings.Contains(s, `"function_call"`) ||
		(strings.Contains(s, `"choices"`) && strings.Contains(s, `"finish_reason"`))
}

func (p *OpenAIParser) Parse(payload []byte) (*AIPayload, error) {
	jsonStart := findJSONStart(payload)
	if jsonStart < 0 {
		return nil, errNotJSON
	}
	data := payload[jsonStart:]

	result := &AIPayload{
		Protocol:   ProtoOpenAI,
		Direction:  "request",
		Confidence: 0.85,
		DetectedAt: time.Now(),
		Metadata:   make(map[string]interface{}),
	}

	// Try as response first (has "choices" with "tool_calls")
	var resp openaiResponse
	if err := json.Unmarshal(data, &resp); err == nil && len(resp.Choices) > 0 {
		result.Direction = "response"
		result.Model = resp.Model

		for _, choice := range resp.Choices {
			if len(choice.Message.ToolCalls) > 0 {
				tc := choice.Message.ToolCalls[0] // Primary tool call
				result.ToolName = tc.Function.Name
				result.MessageType = "tool_call"
				result.Confidence = 0.97

				var args map[string]interface{}
				if json.Unmarshal([]byte(tc.Function.Arguments), &args) == nil {
					result.Arguments = args
				}
				result.Metadata["tool_call_id"] = tc.ID
				result.Metadata["total_tool_calls"] = len(choice.Message.ToolCalls)
				return result, nil
			}
		}

		// Response but no tool calls — it's a generation
		result.ToolName = "llm_completion"
		result.MessageType = "generation"
		return result, nil
	}

	// Try as request
	var req openaiRequest
	if err := json.Unmarshal(data, &req); err == nil && len(req.Messages) > 0 {
		result.Model = req.Model

		// Check if this is a tool result being sent back
		for _, msg := range req.Messages {
			if msg.Role == "tool" && msg.ToolCallID != "" {
				result.ToolName = msg.Name
				result.MessageType = "tool_result"
				result.Direction = "request"
				result.Confidence = 0.95
				result.Metadata["tool_call_id"] = msg.ToolCallID
				return result, nil
			}
		}

		// Check for tool definitions (tells us what AI can call)
		if len(req.Tools) > 0 {
			toolNames := make([]string, 0, len(req.Tools))
			for _, t := range req.Tools {
				toolNames = append(toolNames, t.Function.Name)
			}
			result.Metadata["available_tools"] = toolNames
			result.Metadata["tool_count"] = len(req.Tools)
		}

		result.ToolName = "llm_completion"
		result.MessageType = "generation"
		result.Confidence = 0.90
		return result, nil
	}

	return nil, errNotJSON
}
