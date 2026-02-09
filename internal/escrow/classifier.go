// Package escrow provides deterministic escrow barriers for AOCS governance.
// This file implements Class A/B tool classification per the AOCS specification.
package escrow

import (
	"encoding/json"
	"fmt"
	"sync"
)

// ActionClassification defines the reversibility class of a tool call
type ActionClassification int

const (
	// CLASS_A represents reversible operations that can use Ghost-Turn (speculative execution)
	CLASS_A ActionClassification = iota
	// CLASS_B represents irreversible operations that require Atomic-Hold with HITL
	CLASS_B
)

func (c ActionClassification) String() string {
	switch c {
	case CLASS_A:
		return "CLASS_A"
	case CLASS_B:
		return "CLASS_B"
	default:
		return "UNKNOWN"
	}
}

// EscrowPolicy defines how to handle the tool call based on classification
type EscrowPolicy int

const (
	// GHOST_TURN allows speculative execution with potential rollback
	GHOST_TURN EscrowPolicy = iota
	// ATOMIC_HOLD blocks execution until Tri-Factor Gate validation completes
	ATOMIC_HOLD
)

func (p EscrowPolicy) String() string {
	switch p {
	case GHOST_TURN:
		return "GHOST_TURN"
	case ATOMIC_HOLD:
		return "ATOMIC_HOLD"
	default:
		return "UNKNOWN"
	}
}

// ToolClassification defines the complete classification schema for a tool
type ToolClassification struct {
	// ToolID is the unique identifier for the tool (e.g., "execute_payment", "send_email")
	ToolID string `json:"tool_id"`

	// ActionClass determines reversibility: CLASS_A (reversible) or CLASS_B (irreversible)
	ActionClass ActionClassification `json:"action_class"`

	// ReversibilityIndex is a 0-100 score where 0=fully irreversible, 100=fully reversible
	ReversibilityIndex int `json:"reversibility_index"`

	// EscrowPolicy determines the execution mode: GHOST_TURN or ATOMIC_HOLD
	EscrowPolicy EscrowPolicy `json:"escrow_policy"`

	// MinReputationScore is the minimum trust score required to invoke this tool
	MinReputationScore float64 `json:"min_reputation_score"`

	// GovernanceTaxCoefficient is the cost multiplier for Jury audits of this tool
	GovernanceTaxCoefficient float64 `json:"governance_tax_coefficient"`

	// RequiredEntitlements are JIT tags required to invoke this tool
	RequiredEntitlements []string `json:"required_entitlements"`

	// Description provides human-readable context for governance UIs
	Description string `json:"description"`

	// RiskCategory classifies the business risk (FINANCIAL, DATA, INFRASTRUCTURE, etc.)
	RiskCategory string `json:"risk_category"`

	// HITLRequired indicates if human-in-the-loop is mandatory
	HITLRequired bool `json:"hitl_required"`
}

// ClassificationRequest is the input for classifying a tool call
type ClassificationRequest struct {
	ToolID          string                 `json:"tool_id"`
	AgentID         string                 `json:"agent_id"`
	TenantID        string                 `json:"tenant_id"`
	Args            map[string]interface{} `json:"args"`
	AgentTrustScore float64                `json:"agent_trust_score"`
	Entitlements    []string               `json:"entitlements"`
}

// ClassificationResult is the output of the classification engine
type ClassificationResult struct {
	ToolID           string             `json:"tool_id"`
	Classification   ToolClassification `json:"classification"`
	EscrowDecision   EscrowPolicy       `json:"escrow_decision"`
	EntitlementCheck EntitlementResult  `json:"entitlement_check"`
	TrustCheck       TrustCheckResult   `json:"trust_check"`
	DynamicOverrides []DynamicOverride  `json:"dynamic_overrides"`
	FinalVerdict     string             `json:"final_verdict"` // PROCEED, BLOCK, ESCALATE
	Reasoning        string             `json:"reasoning"`
}

// EntitlementResult captures JIT entitlement validation
type EntitlementResult struct {
	Required []string `json:"required"`
	Present  []string `json:"present"`
	Missing  []string `json:"missing"`
	Valid    bool     `json:"valid"`
}

// TrustCheckResult captures trust threshold validation
type TrustCheckResult struct {
	RequiredScore float64 `json:"required_score"`
	AgentScore    float64 `json:"agent_score"`
	Sufficient    bool    `json:"sufficient"`
	Deficit       float64 `json:"deficit"`
}

// DynamicOverride captures runtime adjustments to classification
type DynamicOverride struct {
	Reason    string       `json:"reason"`
	OldPolicy EscrowPolicy `json:"old_policy"`
	NewPolicy EscrowPolicy `json:"new_policy"`
}

// ToolClassifier is the classification engine for AOCS escrow decisions
type ToolClassifier struct {
	mu       sync.RWMutex
	registry map[string]*ToolClassification
}

// NewToolClassifier creates a new classifier with default tool registry
func NewToolClassifier() *ToolClassifier {
	tc := &ToolClassifier{
		registry: make(map[string]*ToolClassification),
	}
	tc.loadDefaultRegistry()
	return tc
}

// loadDefaultRegistry populates the classifier with known tools
func (tc *ToolClassifier) loadDefaultRegistry() {
	// CLASS_B Tools (Irreversible - require ATOMIC_HOLD)
	tc.RegisterTool(&ToolClassification{
		ToolID:                   "execute_payment",
		ActionClass:              CLASS_B,
		ReversibilityIndex:       5,
		EscrowPolicy:             ATOMIC_HOLD,
		MinReputationScore:       0.85,
		GovernanceTaxCoefficient: 3.0,
		RequiredEntitlements:     []string{"finance:write", "payment:execute"},
		Description:              "Execute monetary transaction",
		RiskCategory:             "FINANCIAL",
		HITLRequired:             true,
	})

	tc.RegisterTool(&ToolClassification{
		ToolID:                   "delete_data",
		ActionClass:              CLASS_B,
		ReversibilityIndex:       0,
		EscrowPolicy:             ATOMIC_HOLD,
		MinReputationScore:       0.90,
		GovernanceTaxCoefficient: 5.0,
		RequiredEntitlements:     []string{"data:delete", "admin:write"},
		Description:              "Permanently delete data",
		RiskCategory:             "DATA",
		HITLRequired:             true,
	})

	tc.RegisterTool(&ToolClassification{
		ToolID:                   "send_external_email",
		ActionClass:              CLASS_B,
		ReversibilityIndex:       10,
		EscrowPolicy:             ATOMIC_HOLD,
		MinReputationScore:       0.75,
		GovernanceTaxCoefficient: 2.0,
		RequiredEntitlements:     []string{"email:send", "external:access"},
		Description:              "Send email to external recipients",
		RiskCategory:             "COMMUNICATION",
		HITLRequired:             true,
	})

	tc.RegisterTool(&ToolClassification{
		ToolID:                   "deploy_infrastructure",
		ActionClass:              CLASS_B,
		ReversibilityIndex:       15,
		EscrowPolicy:             ATOMIC_HOLD,
		MinReputationScore:       0.95,
		GovernanceTaxCoefficient: 4.0,
		RequiredEntitlements:     []string{"infra:deploy", "admin:write"},
		Description:              "Deploy infrastructure changes",
		RiskCategory:             "INFRASTRUCTURE",
		HITLRequired:             true,
	})

	// CLASS_A Tools (Reversible - can use GHOST_TURN)
	tc.RegisterTool(&ToolClassification{
		ToolID:                   "read_database",
		ActionClass:              CLASS_A,
		ReversibilityIndex:       100,
		EscrowPolicy:             GHOST_TURN,
		MinReputationScore:       0.50,
		GovernanceTaxCoefficient: 1.0,
		RequiredEntitlements:     []string{"data:read"},
		Description:              "Read data from database",
		RiskCategory:             "DATA",
		HITLRequired:             false,
	})

	tc.RegisterTool(&ToolClassification{
		ToolID:                   "draft_document",
		ActionClass:              CLASS_A,
		ReversibilityIndex:       95,
		EscrowPolicy:             GHOST_TURN,
		MinReputationScore:       0.40,
		GovernanceTaxCoefficient: 1.0,
		RequiredEntitlements:     []string{"document:write"},
		Description:              "Create or edit a draft document",
		RiskCategory:             "CONTENT",
		HITLRequired:             false,
	})

	tc.RegisterTool(&ToolClassification{
		ToolID:                   "search_records",
		ActionClass:              CLASS_A,
		ReversibilityIndex:       100,
		EscrowPolicy:             GHOST_TURN,
		MinReputationScore:       0.30,
		GovernanceTaxCoefficient: 0.5,
		RequiredEntitlements:     []string{"data:read"},
		Description:              "Search records in the system",
		RiskCategory:             "DATA",
		HITLRequired:             false,
	})

	tc.RegisterTool(&ToolClassification{
		ToolID:                   "calculate_metrics",
		ActionClass:              CLASS_A,
		ReversibilityIndex:       100,
		EscrowPolicy:             GHOST_TURN,
		MinReputationScore:       0.20,
		GovernanceTaxCoefficient: 0.5,
		RequiredEntitlements:     []string{"analytics:read"},
		Description:              "Calculate analytics metrics",
		RiskCategory:             "ANALYTICS",
		HITLRequired:             false,
	})
}

// RegisterTool adds or updates a tool classification in the registry
func (tc *ToolClassifier) RegisterTool(classification *ToolClassification) {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	tc.registry[classification.ToolID] = classification
}

// GetClassification retrieves the classification for a tool
func (tc *ToolClassifier) GetClassification(toolID string) (*ToolClassification, error) {
	tc.mu.RLock()
	defer tc.mu.RUnlock()

	classification, exists := tc.registry[toolID]
	if !exists {
		return nil, fmt.Errorf("tool %s not found in registry", toolID)
	}
	return classification, nil
}

// Classify performs full classification of a tool call request
func (tc *ToolClassifier) Classify(req ClassificationRequest) (*ClassificationResult, error) {
	tc.mu.RLock()
	classification, exists := tc.registry[req.ToolID]
	tc.mu.RUnlock()

	// Handle unknown tools - default to CLASS_B (fail secure)
	if !exists {
		classification = &ToolClassification{
			ToolID:                   req.ToolID,
			ActionClass:              CLASS_B,
			ReversibilityIndex:       0,
			EscrowPolicy:             ATOMIC_HOLD,
			MinReputationScore:       0.95,
			GovernanceTaxCoefficient: 5.0,
			RequiredEntitlements:     []string{},
			Description:              "Unknown tool - classified as CLASS_B for safety",
			RiskCategory:             "UNKNOWN",
			HITLRequired:             true,
		}
	}

	result := &ClassificationResult{
		ToolID:           req.ToolID,
		Classification:   *classification,
		EscrowDecision:   classification.EscrowPolicy,
		DynamicOverrides: []DynamicOverride{},
	}

	// Check entitlements
	result.EntitlementCheck = tc.checkEntitlements(
		classification.RequiredEntitlements,
		req.Entitlements,
	)

	// Check trust score
	result.TrustCheck = tc.checkTrust(
		classification.MinReputationScore,
		req.AgentTrustScore,
	)

	// Apply dynamic overrides based on context
	result.DynamicOverrides = tc.applyDynamicOverrides(req, classification, result)

	// Determine final verdict
	result.FinalVerdict, result.Reasoning = tc.determineVerdict(result)

	return result, nil
}

// checkEntitlements validates JIT entitlement tags
func (tc *ToolClassifier) checkEntitlements(required, present []string) EntitlementResult {
	presentSet := make(map[string]bool)
	for _, e := range present {
		presentSet[e] = true
	}

	var missing []string
	for _, e := range required {
		if !presentSet[e] {
			missing = append(missing, e)
		}
	}

	return EntitlementResult{
		Required: required,
		Present:  present,
		Missing:  missing,
		Valid:    len(missing) == 0,
	}
}

// checkTrust validates agent trust score against threshold
func (tc *ToolClassifier) checkTrust(required, agent float64) TrustCheckResult {
	return TrustCheckResult{
		RequiredScore: required,
		AgentScore:    agent,
		Sufficient:    agent >= required,
		Deficit:       required - agent,
	}
}

// applyDynamicOverrides applies runtime context-based adjustments
func (tc *ToolClassifier) applyDynamicOverrides(
	req ClassificationRequest,
	classification *ToolClassification,
	result *ClassificationResult,
) []DynamicOverride {
	var overrides []DynamicOverride

	// Override 1: Low trust agents always use ATOMIC_HOLD
	if req.AgentTrustScore < 0.50 && classification.EscrowPolicy == GHOST_TURN {
		overrides = append(overrides, DynamicOverride{
			Reason:    "Agent trust score below 0.50 - escalating to ATOMIC_HOLD",
			OldPolicy: GHOST_TURN,
			NewPolicy: ATOMIC_HOLD,
		})
		result.EscrowDecision = ATOMIC_HOLD
	}

	// Override 2: High-value transactions escalate to ATOMIC_HOLD
	if amount, ok := req.Args["amount"].(float64); ok && amount > 10000 {
		if classification.EscrowPolicy == GHOST_TURN {
			overrides = append(overrides, DynamicOverride{
				Reason:    fmt.Sprintf("Transaction amount $%.2f exceeds $10,000 threshold", amount),
				OldPolicy: GHOST_TURN,
				NewPolicy: ATOMIC_HOLD,
			})
			result.EscrowDecision = ATOMIC_HOLD
		}
	}

	// Override 3: Missing entitlements always block
	if !result.EntitlementCheck.Valid {
		overrides = append(overrides, DynamicOverride{
			Reason:    fmt.Sprintf("Missing entitlements: %v", result.EntitlementCheck.Missing),
			OldPolicy: result.EscrowDecision,
			NewPolicy: ATOMIC_HOLD,
		})
		result.EscrowDecision = ATOMIC_HOLD
	}

	return overrides
}

// determineVerdict produces the final decision based on all checks
func (tc *ToolClassifier) determineVerdict(result *ClassificationResult) (string, string) {
	// Block if entitlements are missing
	if !result.EntitlementCheck.Valid {
		return "BLOCK", fmt.Sprintf(
			"Missing required entitlements: %v",
			result.EntitlementCheck.Missing,
		)
	}

	// Block if trust is insufficient
	if !result.TrustCheck.Sufficient {
		return "BLOCK", fmt.Sprintf(
			"Trust score %.2f below required %.2f (deficit: %.2f)",
			result.TrustCheck.AgentScore,
			result.TrustCheck.RequiredScore,
			result.TrustCheck.Deficit,
		)
	}

	// Escalate CLASS_B to human review
	if result.Classification.ActionClass == CLASS_B {
		return "ESCALATE", "CLASS_B action requires Tri-Factor Gate validation and HITL approval"
	}

	// Proceed with CLASS_A
	return "PROCEED", fmt.Sprintf(
		"CLASS_A action cleared for %s execution",
		result.EscrowDecision.String(),
	)
}

// ExportRegistry exports the tool registry as JSON for eBPF synchronization
func (tc *ToolClassifier) ExportRegistry() ([]byte, error) {
	tc.mu.RLock()
	defer tc.mu.RUnlock()
	return json.Marshal(tc.registry)
}

// ImportRegistry imports a tool registry from JSON
func (tc *ToolClassifier) ImportRegistry(data []byte) error {
	var registry map[string]*ToolClassification
	if err := json.Unmarshal(data, &registry); err != nil {
		return err
	}

	tc.mu.Lock()
	defer tc.mu.Unlock()
	tc.registry = registry
	return nil
}
