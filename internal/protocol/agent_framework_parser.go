package protocol

import (
	"encoding/json"
	"strings"
	"time"
)

// AgentFrameworkParser handles LangChain, CrewAI, AutoGen, and similar
// agentic framework payloads. These frameworks use REST APIs and
// have identifiable patterns in their request/response payloads.
//
// Detected patterns:
//   - LangChain: invoke/stream endpoints with "input", "config", "callbacks"
//   - CrewAI: task delegation with "agent", "task", "crew"
//   - AutoGen: multi-agent messages with "sender", "receiver", "content"
//   - Semantic Kernel: plans with "skills", "steps", "ask"
//   - Haystack: pipelines with "components", "pipeline_name"
type AgentFrameworkParser struct{}

func (p *AgentFrameworkParser) Name() AIProtocolType { return ProtoLangChain }

func (p *AgentFrameworkParser) CanParse(payload []byte) bool {
	s := string(payload)
	return strings.Contains(s, `"agent"`) &&
		(strings.Contains(s, `"task"`) ||
			strings.Contains(s, `"input"`) ||
			strings.Contains(s, `"callbacks"`) ||
			strings.Contains(s, `"sender"`) ||
			strings.Contains(s, `"crew"`) ||
			strings.Contains(s, `"pipeline"`) ||
			strings.Contains(s, `"skills"`))
}

func (p *AgentFrameworkParser) Parse(payload []byte) (*AIPayload, error) {
	jsonStart := findJSONStart(payload)
	if jsonStart < 0 {
		return nil, errNotJSON
	}
	data := payload[jsonStart:]

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}

	result := &AIPayload{
		Protocol:   ProtoLangChain,
		Direction:  "request",
		DetectedAt: time.Now(),
		Metadata:   make(map[string]interface{}),
	}

	// Detect specific framework
	switch {
	case hasKeys(raw, "crew", "agents", "tasks"):
		// CrewAI pattern
		result.Protocol = ProtoCrewAI
		result.MessageType = "tool_call"
		result.Confidence = 0.88
		if task, ok := raw["task"].(string); ok {
			result.ToolName = "crew_task"
			result.Arguments = map[string]interface{}{"task": task}
		}
		if agent, ok := raw["agent"].(string); ok {
			result.AgentID = agent
		}

	case hasKeys(raw, "sender", "receiver", "content"):
		// AutoGen pattern
		result.Protocol = ProtoAutoGen
		result.MessageType = "agent_message"
		result.Confidence = 0.85
		result.ToolName = "agent_message"
		if sender, ok := raw["sender"].(string); ok {
			result.AgentID = sender
		}
		if content, ok := raw["content"].(string); ok {
			result.Arguments = map[string]interface{}{"content": content}
		}

	case hasKeys(raw, "input") && (hasKeys(raw, "callbacks") || hasKeys(raw, "config")):
		// LangChain invoke pattern
		result.Protocol = ProtoLangChain
		result.MessageType = "tool_call"
		result.Confidence = 0.87
		result.ToolName = "chain_invoke"
		if input, ok := raw["input"]; ok {
			result.Arguments = map[string]interface{}{"input": input}
		}
		if config, ok := raw["config"].(map[string]interface{}); ok {
			if runName, ok := config["run_name"].(string); ok {
				result.ToolName = runName
			}
		}

	case hasKeys(raw, "pipeline", "components"):
		// Haystack pipeline pattern
		result.Protocol = ProtoLangChain // group under agent frameworks
		result.MessageType = "tool_call"
		result.Confidence = 0.82
		if name, ok := raw["pipeline_name"].(string); ok {
			result.ToolName = name
		} else {
			result.ToolName = "pipeline_run"
		}

	case hasKeys(raw, "skills", "ask"):
		// Semantic Kernel pattern
		result.Protocol = ProtoLangChain
		result.MessageType = "tool_call"
		result.Confidence = 0.83
		if ask, ok := raw["ask"].(string); ok {
			result.ToolName = "semantic_kernel"
			result.Arguments = map[string]interface{}{"ask": ask}
		}

	default:
		// Generic agentic pattern
		result.ToolName = "agent_action"
		result.MessageType = "tool_call"
		result.Confidence = 0.60
	}

	return result, nil
}

func hasKeys(m map[string]interface{}, keys ...string) bool {
	for _, k := range keys {
		if _, ok := m[k]; !ok {
			return false
		}
	}
	return true
}
