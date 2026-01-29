package middleware

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"github.com/ocx/backend/internal/core"
)

// GovernanceHeader is the "Cognitive Contract" injected into every 3rd-party agent
const GovernanceHeader = `
[OCX GOVERNANCE PROTOCOL ACTIVE]
IDENTITY: You are a monitored enterprise agent.
CONTRACT: Your actions are audited in real-time by The Jury.
ENFORCEMENT: Hallucinations or SOP violations will trigger a hardware-level Kill-Switch.
MEMORY: Your outcomes are recorded in the OCX Ledger for reputation tracking.
`

type OCXRequest struct {
	Model    string         `json:"model"`
	Messages []core.Message `json:"messages"`
}

// EngineInterface defines the interface for communicating with the Python Brain
type EngineInterface interface {
	EvaluateIntent(req core.TokenRequest) core.TrustScore
}

// GovernanceMiddleware wraps the handler with OCX logic
func GovernanceMiddleware(engine EngineInterface, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 1. PRE-FLIGHT: Inject Governance Header
		// We need to read the body, inject the system prompt, and rewrite it.
		// Note: This matches the user's snippet logic but adapted to our struct types.

		bodyBytes, _ := io.ReadAll(r.Body)
		r.Body.Close() // Close original body

		// Attempt to parse as LLM Request (if applicable) or standard TokenRequest
		// For the demo, we assume the AgentProxy receives a specific payload.
		// If the payload is 'TokenRequest' (Action/Payload), we inject header into Context if possible.
		// However, the User's snippet assumes a Chat Completion style payload.
		// We will implement the logic as strictly requested but gracefully fallback if JSON differs.

		var llmReq OCXRequest
		if err := json.Unmarshal(bodyBytes, &llmReq); err == nil && len(llmReq.Messages) > 0 {
			// Prepend System Prompt
			sysMsg := core.Message{Role: "system", Content: GovernanceHeader}
			llmReq.Messages = append([]core.Message{sysMsg}, llmReq.Messages...)

			newBody, _ := json.Marshal(llmReq)
			r.Body = io.NopCloser(bytes.NewBuffer(newBody))
		} else {
			// If not a chat request, just restore body (or log warning)
			r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
		}

		// 2. THE JURY INTERCEPTION happens inside the Handler (AgentProxy) currently.
		// To follow the user's "Middleware" pattern strictly, we would move the Jury call HERE.
		// But `AgentProxy` needs the verdict.
		// So we will allow the request to proceed to `next` (AgentProxy) which does the Synchronous check.
		// This Middleware primarily handles the HEADER INJECTION "Cognitive Contract".

		fmt.Printf("🛡️ [Middleware] Governance Header Injected for %s\n", r.RemoteAddr)

		next(w, r)
	}
}
