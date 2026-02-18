package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/ocx/backend/internal/catalog"
	"github.com/ocx/backend/internal/config"
	"github.com/ocx/backend/internal/escrow"
	"github.com/ocx/backend/internal/events"
	"github.com/ocx/backend/internal/evidence"
	"github.com/ocx/backend/internal/governance"
	"github.com/ocx/backend/internal/gvisor"
	"github.com/ocx/backend/internal/plan"
	"github.com/ocx/backend/internal/reputation"
	"github.com/ocx/backend/internal/security"
	"github.com/ocx/backend/internal/webhooks"
)

// HandleGovern is the main governance endpoint that classifies, escrows,
// speculatively executes, and audits AI tool calls.
// Implements Patent Claims 1, 2, 3, 7, 8, 9, 10, 11, 12.
func HandleGovern(
	cfg *config.Config,
	classifier *escrow.ToolClassifier,
	gate *escrow.EscrowGate,
	triGate *escrow.TriFactorGate,
	mp *escrow.MicropaymentEscrow,
	jit *escrow.JITEntitlementManager,
	vault *evidence.EvidenceVault,
	wallet *reputation.ReputationWallet,
	tc *catalog.ToolCatalog,
	wd webhooks.WebhookEmitter,
	bus events.EventEmitter,
	compStack *escrow.CompensationStack,
	tokenBroker *security.TokenBroker,
	cae *security.ContinuousAccessEvaluator,
	sandbox *gvisor.SandboxExecutor,
	ghostEngine *governance.GhostStateEngine,
	sopManager *plan.SOPGraphManager,
	auditor *security.SessionAuditor,
) http.HandlerFunc {
	// Configurable timeout — defaults to 60 seconds if not set in config
	timeoutSec := cfg.Contracts.RuntimeTimeoutMs / 1000
	if timeoutSec <= 0 {
		timeoutSec = 60
	}

	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), time.Duration(timeoutSec)*time.Second)
		defer cancel()

		// Parse SDK request
		var req struct {
			ToolName  string                 `json:"tool_name"`
			AgentID   string                 `json:"agent_id"`
			TenantID  string                 `json:"tenant_id"`
			Arguments map[string]interface{} `json:"arguments"`
			Model     string                 `json:"model"`
			SessionID string                 `json:"session_id"`
			Protocol  string                 `json:"protocol"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
			return
		}

		txID := r.Header.Get("X-Transaction-ID")
		if txID == "" {
			txID = "gov-" + time.Now().Format("20060102-150405.000")
		}

		slog.Info("/govern: tool= agent= tenant= protocol", "tool_name", req.ToolName, "agent_i_d", req.AgentID, "tenant_i_d", req.TenantID, "protocol", req.Protocol)
		// Step 1: Get agent trust score from Supabase via ReputationWallet
		// No hardcoded default — the wallet queries the agents table directly.
		var trustScore float64
		if wallet != nil {
			s, err := wallet.GetTrustScore(ctx, req.AgentID, req.TenantID)
			if err != nil {
				slog.Warn("Trust score retrieval failed, using wallet default",
					"agent_id", req.AgentID, "error", err)
			}
			trustScore = s
		} else {
			slog.Error("ReputationWallet is nil — cannot evaluate trust, blocking")
			http.Error(w, `{"error":"governance wallet unavailable"}`, http.StatusServiceUnavailable)
			return
		}

		// Step 1b: Check Tool Catalog policy (if tool is registered)
		var verdict, actionClass, reason, escrowID, entitlementID, evidenceHash string
		var govTax float64
		var policyBlocked bool
		var speculativeHash string
		var tokenResponse *security.JITToken
		var ghostSideEffects []governance.SideEffect
		var sopDriftReport *plan.DriftReport

		if tc != nil {
			if tool, ok := tc.Get(req.ToolName); ok {
				// Enforce min trust score (Claim 3)
				if tool.GovernancePolicy.MinTrustScore > 0 && trustScore < tool.GovernancePolicy.MinTrustScore {
					verdict = "BLOCK"
					reason = fmt.Sprintf("Trust score %.2f below tool minimum %.2f", trustScore, tool.GovernancePolicy.MinTrustScore)
					actionClass = string(tool.ActionClass)
					policyBlocked = true
				}
				// Enforce human review requirement
				if !policyBlocked && tool.GovernancePolicy.RequireHumanReview {
					verdict = "ESCROW"
					reason = "Tool requires human review per catalog policy"
					actionClass = string(tool.ActionClass)
					policyBlocked = true
				}
			}
		}

		// Step 2: Classify the tool call (if not already blocked by policy)
		if !policyBlocked {
			// Fetch agent's active JIT entitlements (Claim 7)
			var agentEntitlements []string
			if jit != nil {
				activeEnts := jit.GetActiveEntitlements(req.AgentID)
				for _, ent := range activeEnts {
					agentEntitlements = append(agentEntitlements, ent.Permission)
				}
			}

			classification, err := classifier.Classify(escrow.ClassificationRequest{
				ToolID:          req.ToolName,
				AgentID:         req.AgentID,
				TenantID:        req.TenantID,
				Args:            req.Arguments,
				AgentTrustScore: trustScore,
				Entitlements:    agentEntitlements,
			})
			if err != nil {
				// Fail-secure: treat unknown tools as CLASS_B
				verdict = "ESCROW"
				actionClass = "CLASS_B"
				reason = "Classification failed — tool held for review"
				govTax = cfg.Escrow.FailureTaxRate
			} else {
				actionClass = classification.Classification.ActionClass.String()
				govTax = classification.Classification.GovernanceTaxCoefficient

				switch classification.FinalVerdict {
				case "ALLOW":
					verdict = "ALLOW"
					reason = "Tool call approved by governance"
				case "BLOCK":
					verdict = "BLOCK"
					reason = "Tool call blocked by policy"
				case "HOLD":
					verdict = "ESCROW"
					reason = "Tool call held for tri-factor review"

					// ===========================================================
					// CLAIM 1 + 2: Speculative Execution + Tri-Factor Barrier
					// Patent: "intercepting a request, executing speculatively
					// in a sandbox, generating a revert function, auditing
					// asynchronously, and gating release until audit completion"
					// ===========================================================

					if classification.Classification.ActionClass == escrow.CLASS_B {
						// Step 3a (Claim 9): Create ghost state snapshot for
						// business-state sandbox
						if ghostEngine != nil {
							ghost := ghostEngine.Snapshot(txID,
								map[string]interface{}{
									req.AgentID: map[string]interface{}{
										"trust_score": trustScore,
										"tool":        req.ToolName,
									},
								},
								map[string]float64{req.AgentID: trustScore},
								map[string][]string{req.AgentID: {req.ToolName + ":execute"}},
							)
							// Simulate tool on ghost state
							simResult, simErr := ghostEngine.SimulateOnGhost(
								txID, req.ToolName, req.AgentID, req.Arguments,
							)
							if simErr == nil && simResult != nil {
								speculativeHash = simResult.StateHash
								ghostSideEffects = simResult.SideEffects
								if !simResult.PolicyPassed {
									verdict = "BLOCK"
									reason = fmt.Sprintf("Ghost simulation policy violation: %v",
										simResult.Violations)
								}
							}
							_ = ghost // used via txID reference
						}

						// Step 3b (Claim 1): Speculative execution in gVisor sandbox
						if sandbox != nil && verdict != "BLOCK" {
							specPayload := &gvisor.ToolCallPayload{
								TransactionID: txID,
								AgentID:       req.AgentID,
								ToolName:      req.ToolName,
								Parameters:    req.Arguments,
								Context:       map[string]interface{}{"tenant_id": req.TenantID},
							}
							specResult, specErr := sandbox.ExecuteSpeculative(ctx, specPayload)
							if specErr == nil && specResult != nil {
								speculativeHash = specResult.RevertToken
								// §9: Register compensation — revert on failure
								if compStack != nil {
									capturedToken := specResult.RevertToken
									compStack.Push(txID, "revert speculative execution "+req.ToolName, func() error {
										slog.Info("Reverting speculative execution", "captured_token", capturedToken)
										return nil // Revert handled by state cloner via revert token
									})
								}
							}
						}

						// Step 3c (Claim 2): Tri-Factor Gate sequestration
						if triGate != nil {
							payload, _ := json.Marshal(req.Arguments)
							pendingItem, seqErr := triGate.Sequester(
								ctx, txID, req.TenantID, payload,
								classification,
							)
							if seqErr == nil && pendingItem != nil {
								escrowID = txID
							}
						} else {
							// Fallback to basic EscrowGate
							holdErr := gate.Hold(txID, req.TenantID, []byte(req.ToolName))
							if holdErr == nil {
								escrowID = txID
							}
						}
					}
				default:
					verdict = "ALLOW"
					reason = "Default allow"
				}
			}
		}

		// ===========================================================
		// CLAIM 13: SOP Drift Detection
		// "drift computed as divergence from a machine-readable SOP
		//  graph [...] used to adjust governance tax"
		// ===========================================================
		if sopManager != nil && !policyBlocked && req.SessionID != "" {
			// Look for a registered SOP graph matching this session
			graphID := "sop-" + req.SessionID
			if _, ok := sopManager.GetGraph(graphID); !ok {
				graphID = "sop-default"
			}
			if _, ok := sopManager.GetGraph(graphID); ok {
				observed := &plan.ExecutionPath{
					AgentID:   req.AgentID,
					TenantID:  req.TenantID,
					SessionID: req.SessionID,
					Steps: []plan.ExecutionStep{{
						ToolName:    req.ToolName,
						ActionClass: actionClass,
						Timestamp:   time.Now().Unix(),
						Success:     verdict != "BLOCK",
						TrustScore:  trustScore,
					}},
				}
				if drift, dErr := sopManager.ComputeDrift(graphID, observed); dErr == nil && drift != nil {
					sopDriftReport = drift
					if drift.GovernanceTaxAdjustment > 1.0 {
						govTax = govTax * drift.GovernanceTaxAdjustment
						slog.Info("SOP drift detected",
							"edit_distance", drift.PathEditDistance,
							"violations", drift.PolicyViolationCount,
							"tax_adjustment", drift.GovernanceTaxAdjustment,
							"graph_id", graphID)
					}
					if drift.NormalizedEditDistance > 0.8 && verdict == "ALLOW" {
						verdict = "ESCROW"
						reason = fmt.Sprintf("SOP drift too high (%.2f) — held for review", drift.NormalizedEditDistance)
					}
				}
			}
		}

		// Step 4: Micropayment hold + compensation registration
		if mp != nil && verdict == "ALLOW" {
			mp.HoldFunds(txID, req.TenantID, req.AgentID, req.ToolName, actionClass, govTax, 1.0)
			// §9: Register compensation — refund on rollback
			if compStack != nil {
				compStack.Push(txID, "refund micropayment for "+req.ToolName, func() error {
					return mp.RefundFunds(txID)
				})
			}
		}

		// Step 5: JIT Entitlement — TTL from config + compensation
		jitTTL := time.Duration(cfg.Escrow.JITEntitlementTTL) * time.Second
		if jit != nil && verdict == "ALLOW" {
			ent, entErr := jit.GrantEphemeral(
				req.AgentID, req.ToolName+":execute",
				jitTTL,
				"ocx-governance", "SDK govern request",
				map[string]interface{}{"tx_id": txID},
			)
			if entErr == nil && ent != nil {
				entitlementID = ent.ID
				// §9: Register compensation — revoke entitlement on rollback
				if compStack != nil {
					perm := req.ToolName + ":execute"
					agent := req.AgentID
					compStack.Push(txID, "revoke JIT entitlement "+perm, func() error {
						return jit.RevokeEntitlement(agent, perm, "compensation rollback")
					})
				}
			}
		}

		// ===========================================================
		// CLAIM 7: JIT Token Broker
		// "token broker issuing JIT tokens upon sufficient trust and an
		//  attribution header cryptographically bound to each token"
		// ===========================================================
		if tokenBroker != nil && verdict == "ALLOW" {
			token, tokErr := tokenBroker.IssueToken(
				req.AgentID, req.TenantID,
				req.ToolName+":execute",
				trustScore,
			)
			if tokErr == nil && token != nil {
				tokenResponse = token
				// Set attribution header on response
				w.Header().Set("X-OCX-Attribution", token.Attribution)

				// Register with CAE for continuous monitoring (Claim 8)
				if cae != nil {
					cae.RegisterSession(token.TokenID, req.AgentID, req.TenantID, trustScore)
				}
			}
		}

		// Step 6: Evidence record
		if vault != nil {
			outcome := evidence.OutcomeAllow
			if verdict == "BLOCK" {
				outcome = evidence.OutcomeBlock
			} else if verdict == "ESCROW" {
				outcome = evidence.OutcomeHold
			}
			record, recErr := vault.RecordTransaction(
				ctx, req.TenantID, req.AgentID, txID,
				req.ToolName, actionClass, outcome,
				trustScore, reason,
				req.Arguments,
			)
			if recErr == nil && record != nil {
				evidenceHash = record.Hash
			}
		}

		// Step 7: Emit CloudEvent + dispatch webhooks
		eventType := "ocx.verdict." + strings.ToLower(verdict)
		eventData := map[string]interface{}{
			"transaction_id": txID,
			"tool_name":      req.ToolName,
			"agent_id":       req.AgentID,
			"tenant_id":      req.TenantID,
			"verdict":        verdict,
			"action_class":   actionClass,
			"trust_score":    trustScore,
		}
		if bus != nil {
			bus.Emit(eventType, "/api/v1/govern", txID, eventData)
		}
		if wd != nil {
			wd.Emit(webhooks.EventType(eventType), req.TenantID, eventData)
		}

		// Step 7b: Session Audit Log — security forensics
		if auditor != nil {
			tokenID := ""
			if tokenResponse != nil {
				tokenID = tokenResponse.TokenID
			}
			auditor.LogFromRequest(r, txID, req.TenantID, req.AgentID,
				"GOVERN", verdict, trustScore,
				map[string]interface{}{
					"tool_name":    req.ToolName,
					"action_class": actionClass,
					"token_id":     tokenID,
					"protocol":     req.Protocol,
				},
			)
		}

		// Step 8 (§9): Compensation — execute or clear based on verdict
		var compensationResults []escrow.CompensationResult
		if compStack != nil {
			if verdict == "BLOCK" {
				// BLOCK → undo all side-effects (LIFO)
				compensationResults = compStack.Execute(txID)
				// Discard ghost state
				if ghostEngine != nil {
					ghostEngine.Discard(txID)
				}
				// Revoke any issued tokens (Claim 8)
				if tokenBroker != nil {
					tokenBroker.RevokeAllForAgent(req.AgentID)
				}
			} else {
				// ALLOW or ESCROW → commit (clear the stack)
				compStack.Clear(txID)
				// Commit ghost state on ALLOW
				if ghostEngine != nil && verdict == "ALLOW" {
					ghostEngine.Commit(txID)
				}
			}
		}

		// Step 9 (§4): Micropayment settlement based on verdict
		if mp != nil {
			if verdict == "ALLOW" {
				mp.ReleaseFunds(txID) // finalize charge
			} else if verdict == "BLOCK" {
				// Already handled by compensation stack, but ensure cleanup
				mp.RefundFunds(txID)
			}
		}

		// ===========================================================
		// CLAIM 12: Sovereign Mode enforcement
		// "preventing transmission of speculative outputs beyond said
		//  boundary until escrow release"
		// ===========================================================
		if cfg.Sovereign.Enabled && cfg.Sovereign.BoundaryEnforced {
			if verdict == "ESCROW" {
				// In sovereign mode, strip speculative output from response
				speculativeHash = "[SOVEREIGN-SEALED]"
				ghostSideEffects = nil
			}
		}

		w.Header().Set("Content-Type", "application/json")
		if verdict == "BLOCK" {
			w.WriteHeader(http.StatusForbidden)
		} else if verdict == "ESCROW" {
			w.WriteHeader(http.StatusAccepted)
		}

		response := map[string]interface{}{
			"transaction_id":   txID,
			"verdict":          verdict,
			"action_class":     actionClass,
			"reason":           reason,
			"trust_score":      trustScore,
			"governance_tax":   govTax,
			"escrow_id":        escrowID,
			"entitlement_id":   entitlementID,
			"evidence_hash":    evidenceHash,
			"speculative_hash": speculativeHash,
			"processed_at":     time.Now(),
		}
		if len(compensationResults) > 0 {
			response["compensation"] = compensationResults
		}
		if tokenResponse != nil {
			response["jit_token"] = map[string]interface{}{
				"token_id":    tokenResponse.TokenID,
				"token":       tokenResponse.Token,
				"attribution": tokenResponse.Attribution,
				"expires_at":  tokenResponse.ExpiresAt,
			}
		}
		if len(ghostSideEffects) > 0 {
			response["ghost_side_effects"] = len(ghostSideEffects)
		}
		if sopDriftReport != nil {
			response["sop_drift"] = map[string]interface{}{
				"path_edit_distance":        sopDriftReport.PathEditDistance,
				"normalized_edit_distance":  sopDriftReport.NormalizedEditDistance,
				"policy_violations":         sopDriftReport.PolicyViolationCount,
				"governance_tax_adjustment": sopDriftReport.GovernanceTaxAdjustment,
				"missing_steps":             sopDriftReport.MissingSteps,
				"extra_steps":               sopDriftReport.ExtraSteps,
			}
		}
		json.NewEncoder(w).Encode(response)
	}
}
