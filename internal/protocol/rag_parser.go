package protocol

import (
	"encoding/json"
	"strings"
	"time"
)

// RAGParser detects Retrieval-Augmented Generation patterns in intercepted traffic.
//
// RAG pipelines typically have two phases:
//  1. Retrieval: embedding queries → vector DB search → context retrieval
//  2. Generation: augmented prompt → LLM completion → response
//
// Detectable patterns:
//   - Vector DB queries (Pinecone, Weaviate, Chroma, Qdrant, Milvus)
//   - Embedding API calls (OpenAI embeddings, Cohere, HuggingFace)
//   - Reranking calls (Cohere rerank, etc.)
type RAGParser struct{}

func (p *RAGParser) Name() AIProtocolType { return ProtoRAG }

func (p *RAGParser) CanParse(payload []byte) bool {
	s := string(payload)
	return strings.Contains(s, `"embedding"`) ||
		strings.Contains(s, `"vector"`) ||
		strings.Contains(s, `"query_embedding"`) ||
		strings.Contains(s, `"top_k"`) ||
		strings.Contains(s, `"namespace"`) ||
		strings.Contains(s, `"collection"`) ||
		(strings.Contains(s, `"documents"`) && strings.Contains(s, `"ids"`)) ||
		strings.Contains(s, `"rerank"`)
}

func (p *RAGParser) Parse(payload []byte) (*AIPayload, error) {
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
		Protocol:   ProtoRAG,
		Direction:  "request",
		DetectedAt: time.Now(),
		Metadata:   make(map[string]interface{}),
	}

	switch {
	// Embedding API call (OpenAI /v1/embeddings or similar)
	case hasKeys(raw, "input", "model") && strings.Contains(strings.ToLower(getString(raw, "model")), "embed"):
		result.ToolName = "embed_text"
		result.MessageType = "retrieval"
		result.Confidence = 0.90
		result.Model = getString(raw, "model")

	// Vector DB query (Pinecone-style)
	case hasKeys(raw, "vector", "topK") || hasKeys(raw, "query_embedding", "top_k"):
		result.ToolName = "vector_search"
		result.MessageType = "retrieval"
		result.Confidence = 0.92
		if ns, ok := raw["namespace"].(string); ok {
			result.Metadata["namespace"] = ns
		}

	// Chroma/Qdrant collection query
	case hasKeys(raw, "collection") || (hasKeys(raw, "documents") && hasKeys(raw, "ids")):
		result.ToolName = "collection_query"
		result.MessageType = "retrieval"
		result.Confidence = 0.85
		if coll, ok := raw["collection"].(string); ok {
			result.Metadata["collection"] = coll
		}

	// Reranking call
	case hasKeys(raw, "query", "documents") && (hasKeys(raw, "model") || hasKeys(raw, "top_n")):
		result.ToolName = "rerank"
		result.MessageType = "retrieval"
		result.Confidence = 0.88

	default:
		result.ToolName = "rag_operation"
		result.MessageType = "retrieval"
		result.Confidence = 0.65
	}

	return result, nil
}

func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}
