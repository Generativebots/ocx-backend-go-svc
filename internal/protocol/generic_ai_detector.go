package protocol

import (
	"encoding/json"
	"strings"
	"time"
)

// GenericAIDetector is the last-resort parser that detects general AI/ML patterns
// in intercepted traffic. It uses heuristic keyword matching to identify
// payloads that look like AI operations but don't match any specific protocol.
//
// This catches:
//   - Custom AI agent implementations
//   - Internal enterprise AI services
//   - Fine-tuned model inference endpoints
//   - Prompt engineering APIs
//   - AI orchestration workflows
type GenericAIDetector struct{}

// AI-indicative keywords grouped by category
var aiKeywords = map[string][]string{
	"tool_call": {
		"function_call", "tool_use", "tool_input", "tool_output",
		"action_input", "action_output", "invoke", "execute",
	},
	"generation": {
		"prompt", "completion", "temperature", "max_tokens",
		"top_p", "stop_sequences", "system_prompt", "assistant",
	},
	"retrieval": {
		"embedding", "similarity", "cosine", "semantic_search",
		"context_window", "chunk", "retrieval",
	},
	"agent": {
		"agent_id", "agent_name", "agent_type", "orchestrator",
		"planner", "executor", "reasoner", "chain_of_thought",
	},
}

func (p *GenericAIDetector) Name() AIProtocolType { return ProtoCustom }

func (p *GenericAIDetector) CanParse(payload []byte) bool {
	s := strings.ToLower(string(payload))

	// Count AI-indicative keyword matches
	matchCount := 0
	for _, keywords := range aiKeywords {
		for _, kw := range keywords {
			if strings.Contains(s, kw) {
				matchCount++
			}
		}
	}

	// Need at least 2 keyword matches to consider this AI-related
	return matchCount >= 2
}

func (p *GenericAIDetector) Parse(payload []byte) (*AIPayload, error) {
	jsonStart := findJSONStart(payload)
	if jsonStart < 0 {
		return nil, errNotJSON
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(payload[jsonStart:], &raw); err != nil {
		return nil, err
	}

	result := &AIPayload{
		Protocol:   ProtoCustom,
		Direction:  "request",
		DetectedAt: time.Now(),
		Metadata:   make(map[string]interface{}),
	}

	// Score each category
	s := strings.ToLower(string(payload))
	bestCategory := "unknown"
	bestScore := 0

	for category, keywords := range aiKeywords {
		score := 0
		for _, kw := range keywords {
			if strings.Contains(s, kw) {
				score++
			}
		}
		if score > bestScore {
			bestScore = score
			bestCategory = category
		}
	}

	result.MessageType = bestCategory
	result.Confidence = float64(bestScore) * 0.15 // Scale: 2 matches = 0.30, 5 = 0.75
	if result.Confidence > 0.85 {
		result.Confidence = 0.85 // Cap — it's generic detection
	}

	// Try to extract tool/function name from common field names
	for _, field := range []string{
		"function_name", "tool_name", "action", "function", "tool",
		"method", "operation", "endpoint", "skill", "capability",
	} {
		if val, ok := raw[field].(string); ok {
			result.ToolName = val
			break
		}
	}
	if result.ToolName == "" {
		result.ToolName = "ai_operation"
	}

	// Try to extract agent ID
	for _, field := range []string{"agent_id", "agent_name", "user_id", "client_id", "sender"} {
		if val, ok := raw[field].(string); ok {
			result.AgentID = val
			break
		}
	}

	// Try to extract model
	if model, ok := raw["model"].(string); ok {
		result.Model = model
	}

	result.Metadata["keyword_matches"] = bestScore
	result.Metadata["detected_category"] = bestCategory

	return result, nil
}
