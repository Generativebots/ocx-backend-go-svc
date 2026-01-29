package federation

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// ============================================================================
// NETWORK EFFECTS TRACKING
// ============================================================================

// NetworkEffectsTracker tracks the growth and value of the OCX network
type NetworkEffectsTracker struct {
	mu sync.RWMutex

	// Network phases
	currentPhase NetworkPhase

	// Registered agents/instances
	agents map[string]*NetworkAgent

	// Relationships between agents
	relationships map[string]*Relationship

	// Metrics
	metrics *NetworkMetrics
}

// NetworkPhase represents the current phase of network growth
type NetworkPhase string

const (
	PhaseBilateral NetworkPhase = "bilateral" // 2-10 participants
	PhaseIndustry  NetworkPhase = "industry"  // 10-100 participants
	PhaseGlobal    NetworkPhase = "global"    // 100+ participants
)

// NetworkAgent represents a participant in the OCX network
type NetworkAgent struct {
	AgentID       string
	Organization  string
	JoinedAt      time.Time
	Active        bool
	Relationships int
	TotalValue    float64 // Total value created through relationships
}

// Relationship represents a connection between two agents
type Relationship struct {
	RelationshipID   string
	Agent1ID         string
	Agent2ID         string
	EstablishedAt    time.Time
	LastInteraction  time.Time
	InteractionCount int
	TrustLevel       float64
	ValueCreated     float64 // Economic value from this relationship
}

// NetworkMetrics tracks network-wide statistics
type NetworkMetrics struct {
	TotalAgents        int
	ActiveAgents       int
	TotalRelationships int
	NetworkValue       float64 // n × (n-1) / 2
	AverageTrustLevel  float64
	TotalInteractions  int64
	ValueCreated       float64
	GrowthRate         float64 // % growth per month
	LastUpdated        time.Time
}

// NewNetworkEffectsTracker creates a new network effects tracker
func NewNetworkEffectsTracker() *NetworkEffectsTracker {
	return &NetworkEffectsTracker{
		currentPhase:  PhaseBilateral,
		agents:        make(map[string]*NetworkAgent),
		relationships: make(map[string]*Relationship),
		metrics: &NetworkMetrics{
			LastUpdated: time.Now(),
		},
	}
}

// RegisterAgent adds a new agent to the network
func (net *NetworkEffectsTracker) RegisterAgent(ctx context.Context, agentID, organization string) error {
	net.mu.Lock()
	defer net.mu.Unlock()

	if _, exists := net.agents[agentID]; exists {
		return fmt.Errorf("agent already registered: %s", agentID)
	}

	net.agents[agentID] = &NetworkAgent{
		AgentID:      agentID,
		Organization: organization,
		JoinedAt:     time.Now(),
		Active:       true,
	}

	// Update metrics
	net.updateMetrics()

	return nil
}

// EstablishRelationship creates a relationship between two agents
func (net *NetworkEffectsTracker) EstablishRelationship(ctx context.Context, agent1ID, agent2ID string, trustLevel float64) error {
	net.mu.Lock()
	defer net.mu.Unlock()

	// Validate agents exist
	agent1, ok1 := net.agents[agent1ID]
	agent2, ok2 := net.agents[agent2ID]

	if !ok1 || !ok2 {
		return fmt.Errorf("one or both agents not found")
	}

	// Create relationship ID
	relationshipID := fmt.Sprintf("%s:%s", agent1ID, agent2ID)

	// Check if relationship already exists
	if _, exists := net.relationships[relationshipID]; exists {
		return fmt.Errorf("relationship already exists")
	}

	// Create relationship
	net.relationships[relationshipID] = &Relationship{
		RelationshipID:  relationshipID,
		Agent1ID:        agent1ID,
		Agent2ID:        agent2ID,
		EstablishedAt:   time.Now(),
		LastInteraction: time.Now(),
		TrustLevel:      trustLevel,
	}

	// Update agent relationship counts
	agent1.Relationships++
	agent2.Relationships++

	// Update metrics
	net.updateMetrics()

	return nil
}

// RecordInteraction records an interaction between two agents
func (net *NetworkEffectsTracker) RecordInteraction(ctx context.Context, agent1ID, agent2ID string, valueCreated float64) error {
	net.mu.Lock()
	defer net.mu.Unlock()

	relationshipID := fmt.Sprintf("%s:%s", agent1ID, agent2ID)

	relationship, ok := net.relationships[relationshipID]
	if !ok {
		// Try reverse
		relationshipID = fmt.Sprintf("%s:%s", agent2ID, agent1ID)
		relationship, ok = net.relationships[relationshipID]
		if !ok {
			return fmt.Errorf("relationship not found")
		}
	}

	// Update relationship
	relationship.InteractionCount++
	relationship.LastInteraction = time.Now()
	relationship.ValueCreated += valueCreated

	// Update agent values
	if agent1, ok := net.agents[agent1ID]; ok {
		agent1.TotalValue += valueCreated / 2
	}
	if agent2, ok := net.agents[agent2ID]; ok {
		agent2.TotalValue += valueCreated / 2
	}

	// Update metrics
	net.updateMetrics()

	return nil
}

// updateMetrics recalculates network metrics
func (net *NetworkEffectsTracker) updateMetrics() {
	// Count active agents
	activeCount := 0
	totalTrust := 0.0
	totalValue := 0.0

	for _, agent := range net.agents {
		if agent.Active {
			activeCount++
			totalValue += agent.TotalValue
		}
	}

	// Calculate network value using Metcalfe's Law: n × (n-1) / 2
	n := float64(activeCount)
	networkValue := n * (n - 1) / 2

	// Calculate average trust level
	avgTrust := 0.0
	if len(net.relationships) > 0 {
		for _, rel := range net.relationships {
			totalTrust += rel.TrustLevel
		}
		avgTrust = totalTrust / float64(len(net.relationships))
	}

	// Count total interactions
	totalInteractions := int64(0)
	for _, rel := range net.relationships {
		totalInteractions += int64(rel.InteractionCount)
	}

	// Update metrics
	net.metrics.TotalAgents = len(net.agents)
	net.metrics.ActiveAgents = activeCount
	net.metrics.TotalRelationships = len(net.relationships)
	net.metrics.NetworkValue = networkValue
	net.metrics.AverageTrustLevel = avgTrust
	net.metrics.TotalInteractions = totalInteractions
	net.metrics.ValueCreated = totalValue
	net.metrics.LastUpdated = time.Now()

	// Update phase based on agent count
	net.updatePhase(activeCount)
}

// updatePhase updates the current network phase
func (net *NetworkEffectsTracker) updatePhase(activeCount int) {
	if activeCount >= 100 {
		net.currentPhase = PhaseGlobal
	} else if activeCount >= 10 {
		net.currentPhase = PhaseIndustry
	} else {
		net.currentPhase = PhaseBilateral
	}
}

// GetMetrics returns current network metrics
func (net *NetworkEffectsTracker) GetMetrics() *NetworkMetrics {
	net.mu.RLock()
	defer net.mu.RUnlock()

	// Return a copy
	metrics := *net.metrics
	return &metrics
}

// GetCurrentPhase returns the current network phase
func (net *NetworkEffectsTracker) GetCurrentPhase() NetworkPhase {
	net.mu.RLock()
	defer net.mu.RUnlock()

	return net.currentPhase
}

// GetAgentRelationships returns all relationships for an agent
func (net *NetworkEffectsTracker) GetAgentRelationships(agentID string) []*Relationship {
	net.mu.RLock()
	defer net.mu.RUnlock()

	var relationships []*Relationship

	for _, rel := range net.relationships {
		if rel.Agent1ID == agentID || rel.Agent2ID == agentID {
			relationships = append(relationships, rel)
		}
	}

	return relationships
}

// GetTopAgents returns the top N agents by value created
func (net *NetworkEffectsTracker) GetTopAgents(n int) []*NetworkAgent {
	net.mu.RLock()
	defer net.mu.RUnlock()

	// Convert to slice
	agents := make([]*NetworkAgent, 0, len(net.agents))
	for _, agent := range net.agents {
		if agent.Active {
			agents = append(agents, agent)
		}
	}

	// Sort by total value (simple bubble sort for small n)
	for i := 0; i < len(agents)-1; i++ {
		for j := 0; j < len(agents)-i-1; j++ {
			if agents[j].TotalValue < agents[j+1].TotalValue {
				agents[j], agents[j+1] = agents[j+1], agents[j]
			}
		}
	}

	// Return top n
	if n > len(agents) {
		n = len(agents)
	}

	return agents[:n]
}

// CalculateGrowthRate calculates the network growth rate
func (net *NetworkEffectsTracker) CalculateGrowthRate(period time.Duration) float64 {
	net.mu.RLock()
	defer net.mu.RUnlock()

	cutoff := time.Now().Add(-period)
	newAgents := 0

	for _, agent := range net.agents {
		if agent.JoinedAt.After(cutoff) {
			newAgents++
		}
	}

	if net.metrics.TotalAgents == 0 {
		return 0
	}

	return (float64(newAgents) / float64(net.metrics.TotalAgents)) * 100
}

// GetNetworkHealth returns a health score for the network (0-100)
func (net *NetworkEffectsTracker) GetNetworkHealth() float64 {
	net.mu.RLock()
	defer net.mu.RUnlock()

	// Health factors:
	// 1. Active agent ratio (30%)
	// 2. Average trust level (30%)
	// 3. Relationship density (20%)
	// 4. Growth rate (20%)

	activeRatio := 0.0
	if net.metrics.TotalAgents > 0 {
		activeRatio = float64(net.metrics.ActiveAgents) / float64(net.metrics.TotalAgents)
	}

	trustScore := net.metrics.AverageTrustLevel

	relationshipDensity := 0.0
	maxRelationships := float64(net.metrics.ActiveAgents * (net.metrics.ActiveAgents - 1) / 2)
	if maxRelationships > 0 {
		relationshipDensity = float64(net.metrics.TotalRelationships) / maxRelationships
	}

	growthRate := net.CalculateGrowthRate(30 * 24 * time.Hour) // Last 30 days
	growthScore := growthRate / 100                            // Normalize to 0-1

	// Weighted health score
	health := (0.30 * activeRatio) +
		(0.30 * trustScore) +
		(0.20 * relationshipDensity) +
		(0.20 * growthScore)

	return health * 100 // Convert to 0-100 scale
}
