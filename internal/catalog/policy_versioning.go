package catalog

import (
	"fmt"
	"sync"
	"time"
)

// PolicyVersion represents a single version of a tool's governance policy.
// The patent requires "version-controlled policies with rollback capability".
type PolicyVersion struct {
	Version     int                    `json:"version"`
	ToolName    string                 `json:"tool_name"`
	Policy      map[string]interface{} `json:"policy"`       // JSON-Logic body
	ActionClass string                 `json:"action_class"` // A or B
	CreatedAt   time.Time              `json:"created_at"`
	CreatedBy   string                 `json:"created_by"`
	Reason      string                 `json:"reason,omitempty"`
	Active      bool                   `json:"active"`
}

// PolicyVersionStore manages versioned policy history per tool.
type PolicyVersionStore struct {
	mu       sync.RWMutex
	versions map[string][]*PolicyVersion // toolName → ordered versions
	active   map[string]int              // toolName → active version number
}

// NewPolicyVersionStore creates a new policy version store.
func NewPolicyVersionStore() *PolicyVersionStore {
	return &PolicyVersionStore{
		versions: make(map[string][]*PolicyVersion),
		active:   make(map[string]int),
	}
}

// Push adds a new version of the policy for a tool and makes it active.
func (pvs *PolicyVersionStore) Push(toolName string, policy map[string]interface{}, actionClass, createdBy, reason string) *PolicyVersion {
	pvs.mu.Lock()
	defer pvs.mu.Unlock()

	// Deactivate previous active version
	for _, v := range pvs.versions[toolName] {
		v.Active = false
	}

	nextVersion := len(pvs.versions[toolName]) + 1
	pv := &PolicyVersion{
		Version:     nextVersion,
		ToolName:    toolName,
		Policy:      policy,
		ActionClass: actionClass,
		CreatedAt:   time.Now(),
		CreatedBy:   createdBy,
		Reason:      reason,
		Active:      true,
	}

	pvs.versions[toolName] = append(pvs.versions[toolName], pv)
	pvs.active[toolName] = nextVersion

	return pv
}

// Rollback activates a previous version of the policy.
func (pvs *PolicyVersionStore) Rollback(toolName string, targetVersion int) (*PolicyVersion, error) {
	pvs.mu.Lock()
	defer pvs.mu.Unlock()

	versions, ok := pvs.versions[toolName]
	if !ok || len(versions) == 0 {
		return nil, fmt.Errorf("no versions found for tool: %s", toolName)
	}

	if targetVersion < 1 || targetVersion > len(versions) {
		return nil, fmt.Errorf("invalid version %d for tool %s (range: 1-%d)", targetVersion, toolName, len(versions))
	}

	// Deactivate all
	for _, v := range versions {
		v.Active = false
	}

	// Activate target
	target := versions[targetVersion-1]
	target.Active = true
	pvs.active[toolName] = targetVersion

	return target, nil
}

// GetActive returns the currently active policy version for a tool.
func (pvs *PolicyVersionStore) GetActive(toolName string) *PolicyVersion {
	pvs.mu.RLock()
	defer pvs.mu.RUnlock()

	activeVer, ok := pvs.active[toolName]
	if !ok {
		return nil
	}

	versions := pvs.versions[toolName]
	if activeVer < 1 || activeVer > len(versions) {
		return nil
	}

	return versions[activeVer-1]
}

// GetHistory returns all versions for a tool.
func (pvs *PolicyVersionStore) GetHistory(toolName string) []*PolicyVersion {
	pvs.mu.RLock()
	defer pvs.mu.RUnlock()

	return pvs.versions[toolName]
}

// GetDiff returns the differences between two versions (by version number).
func (pvs *PolicyVersionStore) GetDiff(toolName string, fromVer, toVer int) (from, to *PolicyVersion, err error) {
	pvs.mu.RLock()
	defer pvs.mu.RUnlock()

	versions, ok := pvs.versions[toolName]
	if !ok {
		return nil, nil, fmt.Errorf("no versions found for tool: %s", toolName)
	}

	if fromVer < 1 || fromVer > len(versions) || toVer < 1 || toVer > len(versions) {
		return nil, nil, fmt.Errorf("invalid version range %d-%d", fromVer, toVer)
	}

	return versions[fromVer-1], versions[toVer-1], nil
}
