package catalog

import (
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"
)

// ActionClass is the tool's risk classification
type ActionClass string

const (
	ClassA ActionClass = "CLASS_A" // Reversible, low risk
	ClassB ActionClass = "CLASS_B" // Irreversible, high risk
)

// GovernancePolicy defines the rules for governing a tool
type GovernancePolicy struct {
	MinTrustScore      float64  `json:"min_trust_score"`
	RequireHumanReview bool     `json:"require_human_review"`
	MaxAmount          float64  `json:"max_amount,omitempty"`
	AllowedTiers       []string `json:"allowed_tiers,omitempty"`
	CooldownSeconds    int      `json:"cooldown_seconds,omitempty"`
	MaxCallsPerMinute  int      `json:"max_calls_per_minute,omitempty"`
}

// ToolDefinition is a registered tool in the catalog
type ToolDefinition struct {
	Name             string           `json:"name"`
	Description      string           `json:"description"`
	ActionClass      ActionClass      `json:"action_class"`
	Schema           json.RawMessage  `json:"schema,omitempty"`
	GovernancePolicy GovernancePolicy `json:"governance_policy"`
	TenantID         string           `json:"tenant_id,omitempty"`
	RegisteredBy     string           `json:"registered_by,omitempty"`
	CreatedAt        time.Time        `json:"created_at"`
	UpdatedAt        time.Time        `json:"updated_at"`
}

// ToolCatalog is the API-driven registry of tools and their governance policies.
// Instead of hardcoding tools in the ToolClassifier, organizations register
// their tools via API and define governance policies dynamically.
type ToolCatalog struct {
	mu     sync.RWMutex
	tools  map[string]*ToolDefinition // name -> def
	logger *log.Logger
}

// NewToolCatalog creates a new tool catalog
func NewToolCatalog() *ToolCatalog {
	tc := &ToolCatalog{
		tools:  make(map[string]*ToolDefinition),
		logger: log.New(log.Writer(), "[CATALOG] ", log.LstdFlags),
	}

	// Register default tools (same as existing ToolClassifier)
	tc.registerDefaults()
	return tc
}

func (tc *ToolCatalog) registerDefaults() {
	defaults := []*ToolDefinition{
		{
			Name:        "execute_payment",
			Description: "Process a financial payment",
			ActionClass: ClassB,
			GovernancePolicy: GovernancePolicy{
				MinTrustScore:      0.8,
				RequireHumanReview: true,
				MaxAmount:          10000,
			},
		},
		{
			Name:        "delete_data",
			Description: "Permanently delete data records",
			ActionClass: ClassB,
			GovernancePolicy: GovernancePolicy{
				MinTrustScore:      0.9,
				RequireHumanReview: true,
			},
		},
		{
			Name:        "send_email",
			Description: "Send an outbound email",
			ActionClass: ClassB,
			GovernancePolicy: GovernancePolicy{
				MinTrustScore:     0.6,
				MaxCallsPerMinute: 10,
			},
		},
		{
			Name:        "search_records",
			Description: "Search internal records",
			ActionClass: ClassA,
			GovernancePolicy: GovernancePolicy{
				MinTrustScore: 0.3,
			},
		},
		{
			Name:        "read_file",
			Description: "Read a file from storage",
			ActionClass: ClassA,
			GovernancePolicy: GovernancePolicy{
				MinTrustScore: 0.2,
			},
		},
		{
			Name:        "network_call",
			Description: "Generic network API call",
			ActionClass: ClassA,
			GovernancePolicy: GovernancePolicy{
				MinTrustScore: 0.4,
			},
		},
	}

	for _, tool := range defaults {
		tool.CreatedAt = time.Now()
		tool.UpdatedAt = time.Now()
		tool.RegisteredBy = "system"
		tc.tools[tool.Name] = tool
	}
}

// Register adds or updates a tool in the catalog
func (tc *ToolCatalog) Register(tool *ToolDefinition) error {
	tc.mu.Lock()
	defer tc.mu.Unlock()

	if tool.Name == "" {
		return fmt.Errorf("tool name is required")
	}
	if tool.ActionClass != ClassA && tool.ActionClass != ClassB {
		return fmt.Errorf("action_class must be CLASS_A or CLASS_B")
	}

	now := time.Now()
	if existing, ok := tc.tools[tool.Name]; ok {
		tool.CreatedAt = existing.CreatedAt
	} else {
		tool.CreatedAt = now
	}
	tool.UpdatedAt = now

	tc.tools[tool.Name] = tool
	tc.logger.Printf("📦 Registered tool: %s (%s, min_trust=%.2f)",
		tool.Name, tool.ActionClass, tool.GovernancePolicy.MinTrustScore)
	return nil
}

// Get retrieves a tool definition by name
func (tc *ToolCatalog) Get(name string) (*ToolDefinition, bool) {
	tc.mu.RLock()
	defer tc.mu.RUnlock()
	tool, ok := tc.tools[name]
	return tool, ok
}

// Delete removes a tool from the catalog
func (tc *ToolCatalog) Delete(name string) error {
	tc.mu.Lock()
	defer tc.mu.Unlock()

	if _, ok := tc.tools[name]; !ok {
		return fmt.Errorf("tool %q not found", name)
	}
	delete(tc.tools, name)
	tc.logger.Printf("🗑️  Removed tool: %s", name)
	return nil
}

// List returns all registered tools
func (tc *ToolCatalog) List() []*ToolDefinition {
	tc.mu.RLock()
	defer tc.mu.RUnlock()

	result := make([]*ToolDefinition, 0, len(tc.tools))
	for _, tool := range tc.tools {
		result = append(result, tool)
	}
	return result
}

// ListForTenant returns tools registered by a specific tenant
func (tc *ToolCatalog) ListForTenant(tenantID string) []*ToolDefinition {
	tc.mu.RLock()
	defer tc.mu.RUnlock()

	result := make([]*ToolDefinition, 0)
	for _, tool := range tc.tools {
		if tool.TenantID == "" || tool.TenantID == tenantID {
			result = append(result, tool)
		}
	}
	return result
}

// CheckPolicy verifies if an agent meets the governance policy for a tool
func (tc *ToolCatalog) CheckPolicy(toolName string, trustScore float64, tier string) (bool, string) {
	tc.mu.RLock()
	defer tc.mu.RUnlock()

	tool, ok := tc.tools[toolName]
	if !ok {
		return true, "" // Unknown tools use default policy
	}

	if trustScore < tool.GovernancePolicy.MinTrustScore {
		return false, fmt.Sprintf("trust score %.2f below minimum %.2f for %s",
			trustScore, tool.GovernancePolicy.MinTrustScore, toolName)
	}

	if len(tool.GovernancePolicy.AllowedTiers) > 0 {
		allowed := false
		for _, t := range tool.GovernancePolicy.AllowedTiers {
			if t == tier {
				allowed = true
				break
			}
		}
		if !allowed {
			return false, fmt.Sprintf("tier %s not in allowed tiers %v for %s",
				tier, tool.GovernancePolicy.AllowedTiers, toolName)
		}
	}

	return true, ""
}

// Count returns the number of registered tools
func (tc *ToolCatalog) Count() int {
	tc.mu.RLock()
	defer tc.mu.RUnlock()
	return len(tc.tools)
}
