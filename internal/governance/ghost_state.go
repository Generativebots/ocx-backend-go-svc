// Package governance provides the Ghost State Engine (Patent Claim 9).
// Implements business-state sandboxing: snapshot → simulate → diff.
// "speculative execution occurs in a sandboxed business state separate
//
//	from CPU execution state"
package governance

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// ============================================================================
// GHOST STATE ENGINE — Patent Claim 9
// Business-state sandbox for speculative execution of tool calls
// ============================================================================

// GhostState represents a deep-cloned snapshot of the current system state
// against which speculative actions can be simulated without affecting
// the real system state.
type GhostState struct {
	mu         sync.RWMutex
	SnapshotID string
	CreatedAt  time.Time

	// Cloned state buckets
	AgentState       map[string]interface{} // agent trust scores, entitlements
	ResourceState    map[string]interface{} // external resources, DB records
	WalletState      map[string]float64     // reputation wallet balances
	EntitlementState map[string][]string    // active entitlements per agent
	PendingActions   []GhostAction          // actions simulated on this ghost

	// Integrity hash for comparison
	BaselineHash string
}

// GhostAction represents an action simulated against the ghost state.
type GhostAction struct {
	ActionID    string
	ToolName    string
	AgentID     string
	Timestamp   time.Time
	Input       map[string]interface{}
	Output      interface{}
	SideEffects []SideEffect
}

// SideEffect represents a state change caused by a ghost action.
type SideEffect struct {
	Type     string // "WRITE", "DELETE", "TRANSFER", "EXTERNAL_REQUEST"
	Resource string
	Before   interface{}
	After    interface{}
}

// SimulationResult contains the outcome of running a tool against ghost state.
type SimulationResult struct {
	Success      bool
	Output       interface{}
	SideEffects  []SideEffect
	StateHash    string
	PolicyPassed bool
	Violations   []string
	Duration     time.Duration
}

// GhostStateEngine manages ghost state creation, simulation, and comparison.
type GhostStateEngine struct {
	mu           sync.RWMutex
	activeGhosts map[string]*GhostState // txID → ghost
	simulators   map[string]ToolSimulator
	persister    GhostStatePersistence // T8: optional persistence backend
}

// GhostStatePersistence is an optional interface for persisting ghost state snapshots.
// T8 fix: Prevents snapshot loss on restart. Implement with Redis, PostgreSQL, etc.
type GhostStatePersistence interface {
	SaveSnapshot(txID string, ghost *GhostState) error
	LoadSnapshot(txID string) (*GhostState, error)
	DeleteSnapshot(txID string) error
}

// ToolSimulator simulates a specific tool's execution against ghost state.
type ToolSimulator interface {
	Simulate(ghost *GhostState, agentID string, args map[string]interface{}) (*SimulationResult, error)
}

// NewGhostStateEngine creates a new ghost state engine.
func NewGhostStateEngine() *GhostStateEngine {
	engine := &GhostStateEngine{
		activeGhosts: make(map[string]*GhostState),
		simulators:   make(map[string]ToolSimulator),
	}

	// Register built-in simulators
	engine.RegisterSimulator("*", &GenericSimulator{})

	return engine
}

// SetPersister sets the optional persistence backend for ghost state.
// T8 fix: When set, snapshots are persisted on creation and cleaned up on commit/discard.
func (gse *GhostStateEngine) SetPersister(p GhostStatePersistence) {
	gse.mu.Lock()
	defer gse.mu.Unlock()
	gse.persister = p
}

// RegisterSimulator registers a tool simulator.
func (gse *GhostStateEngine) RegisterSimulator(toolPattern string, sim ToolSimulator) {
	gse.mu.Lock()
	defer gse.mu.Unlock()
	gse.simulators[toolPattern] = sim
}

// Snapshot creates a ghost state from current system state.
func (gse *GhostStateEngine) Snapshot(
	txID string,
	agentStates map[string]interface{},
	walletBalances map[string]float64,
	entitlements map[string][]string,
) *GhostState {
	gse.mu.Lock()
	defer gse.mu.Unlock()

	// Deep clone all state
	ghost := &GhostState{
		SnapshotID:       fmt.Sprintf("ghost_%s_%d", txID, time.Now().UnixNano()%1e9),
		CreatedAt:        time.Now(),
		AgentState:       deepCloneMap(agentStates),
		ResourceState:    make(map[string]interface{}),
		WalletState:      deepCloneFloatMap(walletBalances),
		EntitlementState: deepCloneStringSliceMap(entitlements),
		PendingActions:   make([]GhostAction, 0),
	}

	// Compute baseline hash for integrity comparison
	ghost.BaselineHash = ghost.computeHash()

	gse.activeGhosts[txID] = ghost

	// T8: Persist snapshot if persistence backend is configured
	if gse.persister != nil {
		if err := gse.persister.SaveSnapshot(txID, ghost); err != nil {
			// Log but don't fail — persistence is best-effort
			fmt.Printf("[GhostState] Warning: failed to persist snapshot %s: %v\n", txID, err)
		}
	}

	return ghost
}

// SimulateOnGhost runs a tool against the ghost state without
// affecting the real system state.
func (gse *GhostStateEngine) SimulateOnGhost(
	txID, toolName, agentID string,
	args map[string]interface{},
) (*SimulationResult, error) {
	gse.mu.RLock()
	ghost, exists := gse.activeGhosts[txID]
	gse.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("no ghost state found for transaction %s", txID)
	}

	// Find simulator for this tool
	sim := gse.getSimulator(toolName)
	if sim == nil {
		return nil, fmt.Errorf("no simulator registered for tool %s", toolName)
	}

	start := time.Now()
	result, err := sim.Simulate(ghost, agentID, args)
	if err != nil {
		return nil, fmt.Errorf("simulation failed: %w", err)
	}
	result.Duration = time.Since(start)

	// Record the action on the ghost
	ghost.mu.Lock()
	ghost.PendingActions = append(ghost.PendingActions, GhostAction{
		ActionID:    fmt.Sprintf("act_%d", len(ghost.PendingActions)+1),
		ToolName:    toolName,
		AgentID:     agentID,
		Timestamp:   time.Now(),
		Input:       args,
		Output:      result.Output,
		SideEffects: result.SideEffects,
	})
	ghost.mu.Unlock()

	// Compute post-simulation state hash
	result.StateHash = ghost.computeHash()

	return result, nil
}

// Diff compares ghost state against the baseline to identify all changes.
func (gse *GhostStateEngine) Diff(txID string) ([]SideEffect, error) {
	gse.mu.RLock()
	ghost, exists := gse.activeGhosts[txID]
	gse.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("no ghost state found for transaction %s", txID)
	}

	// Collect all side effects from all ghost actions
	ghost.mu.RLock()
	defer ghost.mu.RUnlock()

	var allEffects []SideEffect
	for _, action := range ghost.PendingActions {
		allEffects = append(allEffects, action.SideEffects...)
	}

	return allEffects, nil
}

// Commit applies ghost state changes to the real system.
// Called after escrow release.
func (gse *GhostStateEngine) Commit(txID string) error {
	gse.mu.Lock()
	defer gse.mu.Unlock()

	_, exists := gse.activeGhosts[txID]
	if !exists {
		return fmt.Errorf("no ghost state found for transaction %s", txID)
	}

	// In production: apply each SideEffect to the real system
	// For now: clean up the ghost state (changes are committed upstream)
	delete(gse.activeGhosts, txID)

	// T8: Clean up persisted snapshot
	if gse.persister != nil {
		gse.persister.DeleteSnapshot(txID)
	}

	return nil
}

// Discard removes ghost state without applying changes (on revert).
func (gse *GhostStateEngine) Discard(txID string) {
	gse.mu.Lock()
	defer gse.mu.Unlock()
	delete(gse.activeGhosts, txID)

	// T8: Clean up persisted snapshot
	if gse.persister != nil {
		gse.persister.DeleteSnapshot(txID)
	}
}

// GetActiveCount returns the number of active ghost states.
func (gse *GhostStateEngine) GetActiveCount() int {
	gse.mu.RLock()
	defer gse.mu.RUnlock()
	return len(gse.activeGhosts)
}

// --- Internal helpers ---

func (gse *GhostStateEngine) getSimulator(toolName string) ToolSimulator {
	gse.mu.RLock()
	defer gse.mu.RUnlock()

	if sim, ok := gse.simulators[toolName]; ok {
		return sim
	}
	// Fallback to wildcard
	return gse.simulators["*"]
}

func (gs *GhostState) computeHash() string {
	gs.mu.RLock()
	defer gs.mu.RUnlock()

	h := sha256.New()
	data, _ := json.Marshal(map[string]interface{}{
		"agents":       gs.AgentState,
		"wallets":      gs.WalletState,
		"entitlements": gs.EntitlementState,
		"actions":      len(gs.PendingActions),
	})
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}

func deepCloneMap(src map[string]interface{}) map[string]interface{} {
	if src == nil {
		return make(map[string]interface{})
	}
	dst := make(map[string]interface{}, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func deepCloneFloatMap(src map[string]float64) map[string]float64 {
	if src == nil {
		return make(map[string]float64)
	}
	dst := make(map[string]float64, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func deepCloneStringSliceMap(src map[string][]string) map[string][]string {
	if src == nil {
		return make(map[string][]string)
	}
	dst := make(map[string][]string, len(src))
	for k, v := range src {
		cp := make([]string, len(v))
		copy(cp, v)
		dst[k] = cp
	}
	return dst
}

// ============================================================================
// GENERIC SIMULATOR — Default simulator for any tool
// ============================================================================

// GenericSimulator provides a basic simulation that tracks state changes
// without executing external actions.
type GenericSimulator struct{}

func (gs *GenericSimulator) Simulate(
	ghost *GhostState,
	agentID string,
	args map[string]interface{},
) (*SimulationResult, error) {
	effects := make([]SideEffect, 0)

	// Simulate wallet deduction if cost is specified
	if cost, ok := args["cost"].(float64); ok {
		ghost.mu.Lock()
		before := ghost.WalletState[agentID]
		ghost.WalletState[agentID] -= cost
		after := ghost.WalletState[agentID]
		ghost.mu.Unlock()

		effects = append(effects, SideEffect{
			Type:     "TRANSFER",
			Resource: "wallet:" + agentID,
			Before:   before,
			After:    after,
		})
	}

	// Simulate resource write if target is specified
	if target, ok := args["target"].(string); ok {
		ghost.mu.Lock()
		before := ghost.ResourceState[target]
		ghost.ResourceState[target] = args["value"]
		after := ghost.ResourceState[target]
		ghost.mu.Unlock()

		effects = append(effects, SideEffect{
			Type:     "WRITE",
			Resource: target,
			Before:   before,
			After:    after,
		})
	}

	// Policy check: ensure wallet doesn't go negative
	violations := make([]string, 0)
	ghost.mu.RLock()
	if balance, ok := ghost.WalletState[agentID]; ok && balance < 0 {
		violations = append(violations, fmt.Sprintf("wallet balance negative: %.2f", balance))
	}
	ghost.mu.RUnlock()

	return &SimulationResult{
		Success:      len(violations) == 0,
		Output:       map[string]interface{}{"simulated": true, "agent": agentID},
		SideEffects:  effects,
		PolicyPassed: len(violations) == 0,
		Violations:   violations,
	}, nil
}
