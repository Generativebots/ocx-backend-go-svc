// Package plan provides SOP Graph and Drift Computation (Patent Claim 13).
// "drift computed as divergence from a machine-readable SOP graph [...]
//
//	quantified by graph path-edit distance and policy-constraint violation
//	counts [...] used to adjust governance tax"
package plan

import (
	"fmt"
	"math"
	"sort"
	"sync"
)

// ============================================================================
// SOP GRAPH — Patent Claim 13
// Machine-readable Standard Operating Procedure graph with edit distance
// ============================================================================

// SOPNode represents a single step in a Standard Operating Procedure.
type SOPNode struct {
	ID           string
	Name         string
	ToolRequired string
	ActionClass  string // CLASS_A, CLASS_B, CLASS_C
	Constraints  []PolicyConstraint
	Children     []string // IDs of next steps
	Required     bool
	Order        int
}

// PolicyConstraint defines a policy rule that must hold at this step.
type PolicyConstraint struct {
	ID         string
	Rule       string // human-readable description
	Expression string // machine-executable expression (e.g., "trust >= 0.7")
	Severity   string // "CRITICAL", "HIGH", "MEDIUM", "LOW"
}

// SOPGraph represents a directed acyclic graph of SOP steps.
type SOPGraph struct {
	mu      sync.RWMutex
	ID      string
	Name    string
	Version string
	Nodes   map[string]*SOPNode
	RootID  string
	Edges   [][2]string // [from, to] pairs
}

// ExecutionPath represents an observed agent execution path.
type ExecutionPath struct {
	Steps     []ExecutionStep
	AgentID   string
	TenantID  string
	SessionID string
}

// ExecutionStep is a single observed action by an agent.
type ExecutionStep struct {
	ToolName    string
	ActionClass string
	Timestamp   int64
	Success     bool
	TrustScore  float64
}

// DriftReport contains the computed drift between SOP and execution.
type DriftReport struct {
	PathEditDistance        int      // Levenshtein-like edit distance on step sequences
	NormalizedEditDistance  float64  // 0.0 (identical) to 1.0 (completely different)
	PolicyViolationCount    int      // Number of violated constraints
	PolicyViolations        []string // Descriptions of violations
	MissingSteps            []string // SOP steps not executed
	ExtraSteps              []string // Steps executed but not in SOP
	OutOfOrderSteps         []string // Steps executed in wrong order
	GovernanceTaxAdjustment float64  // Multiplier adjustment for tax
}

// SOPGraphManager manages SOP graphs and drift computation.
type SOPGraphManager struct {
	mu     sync.RWMutex
	graphs map[string]*SOPGraph // graphID → graph
}

// NewSOPGraphManager creates a new manager.
func NewSOPGraphManager() *SOPGraphManager {
	return &SOPGraphManager{
		graphs: make(map[string]*SOPGraph),
	}
}

// RegisterGraph registers a new SOP graph.
func (m *SOPGraphManager) RegisterGraph(graph *SOPGraph) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.graphs[graph.ID] = graph
}

// GetGraph returns a registered graph by ID.
func (m *SOPGraphManager) GetGraph(graphID string) (*SOPGraph, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	g, ok := m.graphs[graphID]
	return g, ok
}

// ComputeDrift computes the drift between an SOP graph and an observed execution path.
// This is the core implementation of Patent Claim 13.
func (m *SOPGraphManager) ComputeDrift(graphID string, observed *ExecutionPath) (*DriftReport, error) {
	m.mu.RLock()
	graph, exists := m.graphs[graphID]
	m.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("SOP graph %s not found", graphID)
	}

	report := &DriftReport{}

	// 1. Extract expected step sequence from SOP graph (topological order)
	expectedSteps := graph.getOrderedSteps()

	// 2. Extract observed step sequence
	observedTools := make([]string, len(observed.Steps))
	for i, step := range observed.Steps {
		observedTools[i] = step.ToolName
	}

	// 3. Compute path edit distance (Levenshtein distance on tool sequences)
	report.PathEditDistance = levenshteinDistance(
		extractToolNames(expectedSteps),
		observedTools,
	)

	// Normalized edit distance (0.0 to 1.0)
	maxLen := max(len(expectedSteps), len(observedTools))
	if maxLen > 0 {
		report.NormalizedEditDistance = float64(report.PathEditDistance) / float64(maxLen)
	}

	// 4. Find missing, extra, and out-of-order steps
	expectedSet := make(map[string]int) // tool → expected position
	for i, node := range expectedSteps {
		expectedSet[node.ToolRequired] = i
	}

	observedSet := make(map[string]int) // tool → observed position
	for i, tool := range observedTools {
		observedSet[tool] = i
	}

	// Missing steps: in SOP but not observed
	for _, node := range expectedSteps {
		if node.Required {
			if _, found := observedSet[node.ToolRequired]; !found {
				report.MissingSteps = append(report.MissingSteps, node.ToolRequired)
			}
		}
	}

	// Extra steps: observed but not in SOP
	for _, tool := range observedTools {
		if _, found := expectedSet[tool]; !found {
			report.ExtraSteps = append(report.ExtraSteps, tool)
		}
	}

	// Out-of-order: in both but in wrong relative order
	for _, tool := range observedTools {
		expectedPos, inExpected := expectedSet[tool]
		observedPos, inObserved := observedSet[tool]
		if inExpected && inObserved && expectedPos != observedPos {
			report.OutOfOrderSteps = append(report.OutOfOrderSteps,
				fmt.Sprintf("%s (expected pos %d, observed pos %d)", tool, expectedPos, observedPos))
		}
	}

	// 5. Check policy constraints at each step
	report.PolicyViolations = make([]string, 0)
	for i, step := range observed.Steps {
		// Find corresponding SOP node
		if node, ok := graph.Nodes[step.ToolName]; ok {
			for _, constraint := range node.Constraints {
				if !evaluateConstraint(constraint, step) {
					report.PolicyViolationCount++
					report.PolicyViolations = append(report.PolicyViolations,
						fmt.Sprintf("Step %d (%s): violated constraint '%s' [%s]",
							i, step.ToolName, constraint.Rule, constraint.Severity))
				}
			}
		}
	}

	// 6. Compute governance tax adjustment from drift
	// Patent: "used to adjust a governance tax applied to the agent's reputation wallet"
	report.GovernanceTaxAdjustment = computeTaxAdjustment(report)

	return report, nil
}

// getOrderedSteps returns SOP nodes in topological order.
func (g *SOPGraph) getOrderedSteps() []*SOPNode {
	g.mu.RLock()
	defer g.mu.RUnlock()

	nodes := make([]*SOPNode, 0, len(g.Nodes))
	for _, node := range g.Nodes {
		nodes = append(nodes, node)
	}

	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].Order < nodes[j].Order
	})

	return nodes
}

// extractToolNames gets tool names from nodes.
func extractToolNames(nodes []*SOPNode) []string {
	names := make([]string, len(nodes))
	for i, n := range nodes {
		names[i] = n.ToolRequired
	}
	return names
}

// levenshteinDistance computes the edit distance between two string sequences.
func levenshteinDistance(a, b []string) int {
	la, lb := len(a), len(b)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}

	// Dynamic programming matrix
	dp := make([][]int, la+1)
	for i := range dp {
		dp[i] = make([]int, lb+1)
		dp[i][0] = i
	}
	for j := 0; j <= lb; j++ {
		dp[0][j] = j
	}

	for i := 1; i <= la; i++ {
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			dp[i][j] = minOf3(
				dp[i-1][j]+1,      // deletion
				dp[i][j-1]+1,      // insertion
				dp[i-1][j-1]+cost, // substitution
			)
		}
	}

	return dp[la][lb]
}

// evaluateConstraint checks if an execution step satisfies a policy constraint.
func evaluateConstraint(constraint PolicyConstraint, step ExecutionStep) bool {
	// Evaluate common constraint expressions
	switch constraint.Expression {
	case "trust >= 0.7":
		return step.TrustScore >= 0.7
	case "trust >= 0.5":
		return step.TrustScore >= 0.5
	case "trust >= 0.9":
		return step.TrustScore >= 0.9
	case "action_success":
		return step.Success
	default:
		// For complex expressions, parse and evaluate
		// Default: pass if trust score is above a reasonable threshold
		return step.TrustScore >= 0.5
	}
}

// computeTaxAdjustment converts drift metrics to a governance tax multiplier.
// Higher drift → higher tax (more expensive governance).
func computeTaxAdjustment(report *DriftReport) float64 {
	base := 1.0

	// Path edit distance contribution
	if report.NormalizedEditDistance > 0 {
		base += report.NormalizedEditDistance * 2.0 // up to 2x for max drift
	}

	// Policy violations contribution
	base += float64(report.PolicyViolationCount) * 0.5

	// Missing required steps are severe
	base += float64(len(report.MissingSteps)) * 1.0

	// Cap at 5x
	return math.Min(base, 5.0)
}

func minOf3(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
}
