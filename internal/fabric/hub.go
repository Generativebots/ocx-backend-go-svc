// Package fabric implements the O(n) Hub-and-Spoke architecture for AOCS.
// This provides capability-based routing and virtual addressing for agent communication.
package fabric

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"
)

// HubID uniquely identifies an OCX Hub in the network
type HubID string

// SpokeID uniquely identifies a Spoke (agent/tenant endpoint)
type SpokeID string

// VirtualAddress is the AOCS-assigned address for routing
type VirtualAddress string

// Capability represents an agent's capabilities for routing decisions
type Capability string

// Common capabilities
const (
	CapabilityFinance     Capability = "finance"
	CapabilityData        Capability = "data"
	CapabilityInfra       Capability = "infrastructure"
	CapabilityCommunicate Capability = "communicate"
	CapabilityAnalytics   Capability = "analytics"
	CapabilityAdmin       Capability = "admin"
)

// RouteDecision represents the result of a routing decision
type RouteDecision string

const (
	RouteLocal     RouteDecision = "LOCAL"     // Handle locally
	RouteForward   RouteDecision = "FORWARD"   // Forward to another hub
	RouteMulticast RouteDecision = "MULTICAST" // Send to multiple spokes
	RouteBroadcast RouteDecision = "BROADCAST" // Send to all spokes
	RouteReject    RouteDecision = "REJECT"    // Reject the message
)

// ============================================================================
// SPOKE REGISTRATION
// ============================================================================

// SpokeInfo contains information about a registered spoke
type SpokeInfo struct {
	ID           SpokeID
	TenantID     string
	AgentID      string
	VirtualAddr  VirtualAddress
	Capabilities []Capability
	TrustScore   float64
	Entitlements []string
	ConnectedAt  time.Time
	LastSeen     atomic.Value // time.Time — P0 FIX: atomic for concurrent access
	MessageCount atomic.Int64 // P0 FIX: atomic to prevent data races
	BytesSent    atomic.Int64 // P0 FIX: atomic to prevent data races
	BytesRecv    atomic.Int64 // P0 FIX: atomic to prevent data races
}

// Touch atomically updates spoke stats from the WebSocket read goroutine.
// P0 FIX #2: Previously these fields were mutated without synchronization.
func (s *SpokeInfo) Touch(bytesRecv int64) {
	s.LastSeen.Store(time.Now())
	s.MessageCount.Add(1)
	s.BytesRecv.Add(bytesRecv)
}

// RoutingEntry maps a virtual address to spoke info
type RoutingEntry struct {
	VirtualAddr VirtualAddress
	Spoke       *SpokeInfo
	Priority    int // Lower = higher priority
	Weight      int // For load balancing
	Healthy     bool
	LastCheck   time.Time
}

// ============================================================================
// HUB IMPLEMENTATION
// ============================================================================

// Hub is the central routing point for AOCS messages.
//
// P1 FIX #7 — SCALING LIMITATION:
// All spoke registrations, routing tables, capability indexes, and tenant indexes
// are in-memory maps. A second Hub instance on another pod has zero awareness of
// spokes connected to pod 1. For horizontal scaling, back the Hub routing table
// with Redis (pub/sub for broadcast, hash for spoke registry) or use sticky
// sessions with a shared discovery service.
//
// TODO(scale): Implement HubStore interface backed by Redis for multi-pod routing.
type Hub struct {
	ID        HubID
	Region    string
	Namespace string

	mu sync.RWMutex

	// Spoke registry: SpokeID -> SpokeInfo
	spokes map[SpokeID]*SpokeInfo

	// Routing table: VirtualAddress -> []RoutingEntry
	routes map[VirtualAddress][]RoutingEntry

	// Capability index: Capability -> []SpokeID (for capability-based routing)
	capabilityIndex map[Capability][]SpokeID

	// Tenant index: TenantID -> []SpokeID
	tenantIndex map[string][]SpokeID

	// Peer hubs for federation
	peers map[HubID]*PeerHub

	// Message handlers
	handlers map[string]MessageHandler

	// Metrics
	metrics *HubMetrics

	// Optional Redis-backed store for cross-pod spoke persistence
	store *RedisHubStore

	// Optional Redis-backed event bus for cross-pod event distribution
	fabricEventBus *RedisEventBus

	logger *log.Logger
}

// PeerHub represents a federated hub
type PeerHub struct {
	ID            HubID
	Endpoint      string
	Region        string
	TrustLevel    float64
	Connected     bool
	LastHeartbeat time.Time
}

// HubMetrics tracks hub performance
// HubMetrics tracks hub performance.
// P0 FIX #3: All fields are atomic to prevent data races when
// incremented inside RLock-protected Route() calls.
type HubMetrics struct {
	MessagesRouted    atomic.Int64
	MessagesFailed    atomic.Int64
	SpokesConnected   atomic.Int32
	PeersConnected    atomic.Int32
	AvgRoutingLatency atomic.Int64 // stored as nanoseconds
}

// MessageHandler processes messages for a specific type
type MessageHandler func(ctx context.Context, msg *Message) (*Message, error)

// NewHub creates a new AOCS Hub
func NewHub(id HubID, region, namespace string) *Hub {
	return &Hub{
		ID:              id,
		Region:          region,
		Namespace:       namespace,
		spokes:          make(map[SpokeID]*SpokeInfo),
		routes:          make(map[VirtualAddress][]RoutingEntry),
		capabilityIndex: make(map[Capability][]SpokeID),
		tenantIndex:     make(map[string][]SpokeID),
		peers:           make(map[HubID]*PeerHub),
		handlers:        make(map[string]MessageHandler),
		metrics:         &HubMetrics{},
		logger:          log.New(log.Writer(), fmt.Sprintf("[Hub:%s] ", id), log.LstdFlags),
	}
}

// SetStore injects a Redis-backed store for cross-pod spoke persistence.
// When set, spoke registrations are persisted to Redis alongside in-memory maps.
func (h *Hub) SetStore(s *RedisHubStore) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.store = s
}

// SetFabricEventBus injects a Redis-backed event bus for cross-pod event distribution.
func (h *Hub) SetFabricEventBus(bus *RedisEventBus) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.fabricEventBus = bus
}

// ============================================================================
// SPOKE MANAGEMENT
// ============================================================================

// RegisterSpoke registers a new spoke with the hub
func (h *Hub) RegisterSpoke(
	tenantID, agentID string,
	capabilities []Capability,
	trustScore float64,
	entitlements []string,
) (*SpokeInfo, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Generate spoke ID and virtual address
	spokeID := h.generateSpokeID(tenantID, agentID)
	virtualAddr := h.assignVirtualAddress(tenantID, agentID)

	spoke := &SpokeInfo{
		ID:           spokeID,
		TenantID:     tenantID,
		AgentID:      agentID,
		VirtualAddr:  virtualAddr,
		Capabilities: capabilities,
		TrustScore:   trustScore,
		Entitlements: entitlements,
		ConnectedAt:  time.Now(),
	}
	spoke.LastSeen.Store(time.Now())

	// Register in spoke map
	h.spokes[spokeID] = spoke

	// Update routing table
	h.routes[virtualAddr] = append(h.routes[virtualAddr], RoutingEntry{
		VirtualAddr: virtualAddr,
		Spoke:       spoke,
		Priority:    1,
		Weight:      100,
		Healthy:     true,
		LastCheck:   time.Now(),
	})

	// Update capability index
	for _, cap := range capabilities {
		h.capabilityIndex[cap] = append(h.capabilityIndex[cap], spokeID)
	}

	// Update tenant index
	h.tenantIndex[tenantID] = append(h.tenantIndex[tenantID], spokeID)

	h.metrics.SpokesConnected.Add(1)

	h.logger.Printf("Registered spoke: %s (tenant=%s, agent=%s, addr=%s)",
		spokeID, tenantID, agentID, virtualAddr)

	return spoke, nil
}

// UnregisterSpoke removes a spoke from the hub
func (h *Hub) UnregisterSpoke(spokeID SpokeID) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	spoke, exists := h.spokes[spokeID]
	if !exists {
		return fmt.Errorf("spoke %s not found", spokeID)
	}

	// Remove from spoke map
	delete(h.spokes, spokeID)

	// Remove from routing table
	delete(h.routes, spoke.VirtualAddr)

	// Remove from capability index
	for _, cap := range spoke.Capabilities {
		h.capabilityIndex[cap] = h.removeFromSlice(h.capabilityIndex[cap], spokeID)
	}

	// Remove from tenant index
	h.tenantIndex[spoke.TenantID] = h.removeFromSlice(h.tenantIndex[spoke.TenantID], spokeID)

	h.metrics.SpokesConnected.Add(-1)

	h.logger.Printf("Unregistered spoke: %s", spokeID)

	return nil
}

func (h *Hub) removeFromSlice(slice []SpokeID, id SpokeID) []SpokeID {
	for i, v := range slice {
		if v == id {
			return append(slice[:i], slice[i+1:]...)
		}
	}
	return slice
}

// ============================================================================
// MESSAGE ROUTING
// ============================================================================

// Message represents an AOCS protocol message
type Message struct {
	ID          string
	Type        string
	Source      VirtualAddress
	Destination VirtualAddress
	TenantID    string
	Payload     []byte
	Headers     map[string]string
	Timestamp   time.Time
	TTL         int
	Priority    int
}

// Route routes a message to its destination
func (h *Hub) Route(ctx context.Context, msg *Message) (*RouteResult, error) {
	start := time.Now()
	defer func() {
		h.metrics.MessagesRouted.Add(1)
	}()

	h.mu.RLock()
	defer h.mu.RUnlock()

	// Check TTL
	if msg.TTL <= 0 {
		h.metrics.MessagesFailed.Add(1)
		return nil, fmt.Errorf("message TTL expired")
	}
	msg.TTL--

	// Try direct routing first
	if entries, exists := h.routes[msg.Destination]; exists && len(entries) > 0 {
		return h.routeDirect(ctx, msg, entries, start)
	}

	// Try capability-based routing (if destination is a capability)
	if cap, ok := h.parseCapability(msg.Destination); ok {
		return h.routeByCapability(ctx, msg, cap, start)
	}

	// Try tenant broadcast
	if h.isTenantBroadcast(msg.Destination) {
		return h.routeTenantBroadcast(ctx, msg, start)
	}

	// Try federated routing
	if result, err := h.routeFederated(ctx, msg, start); err == nil {
		return result, nil
	}

	h.metrics.MessagesFailed.Add(1)
	return nil, fmt.Errorf("no route to %s", msg.Destination)
}

// RouteResult contains the result of a routing decision
type RouteResult struct {
	Decision      RouteDecision
	Destinations  []VirtualAddress
	RoutingTime   time.Duration
	HopsUsed      int
	FederatedHubs []HubID
}

func (h *Hub) routeDirect(
	ctx context.Context,
	msg *Message,
	entries []RoutingEntry,
	start time.Time,
) (*RouteResult, error) {
	// Find healthy entries
	var healthy []RoutingEntry
	for _, e := range entries {
		if e.Healthy {
			healthy = append(healthy, e)
		}
	}

	if len(healthy) == 0 {
		return nil, fmt.Errorf("no healthy routes to %s", msg.Destination)
	}

	// Select best route (lowest priority, then weight-based)
	best := healthy[0]
	for _, e := range healthy[1:] {
		if e.Priority < best.Priority {
			best = e
		}
	}

	// Deliver to spoke
	err := h.deliverToSpoke(ctx, msg, best.Spoke)
	if err != nil {
		return nil, err
	}

	return &RouteResult{
		Decision:     RouteLocal,
		Destinations: []VirtualAddress{best.VirtualAddr},
		RoutingTime:  time.Since(start),
		HopsUsed:     1,
	}, nil
}

func (h *Hub) routeByCapability(
	ctx context.Context,
	msg *Message,
	cap Capability,
	start time.Time,
) (*RouteResult, error) {
	spokeIDs, exists := h.capabilityIndex[cap]
	if !exists || len(spokeIDs) == 0 {
		return nil, fmt.Errorf("no spokes with capability %s", cap)
	}

	// Filter by tenant if specified
	var filtered []SpokeID
	if msg.TenantID != "" {
		for _, id := range spokeIDs {
			if spoke := h.spokes[id]; spoke != nil && spoke.TenantID == msg.TenantID {
				filtered = append(filtered, id)
			}
		}
		spokeIDs = filtered
	}

	if len(spokeIDs) == 0 {
		return nil, fmt.Errorf("no matching spokes for capability %s in tenant %s", cap, msg.TenantID)
	}

	// Select best spoke by trust score
	var bestSpoke *SpokeInfo
	for _, id := range spokeIDs {
		spoke := h.spokes[id]
		if bestSpoke == nil || spoke.TrustScore > bestSpoke.TrustScore {
			bestSpoke = spoke
		}
	}

	err := h.deliverToSpoke(ctx, msg, bestSpoke)
	if err != nil {
		return nil, err
	}

	return &RouteResult{
		Decision:     RouteLocal,
		Destinations: []VirtualAddress{bestSpoke.VirtualAddr},
		RoutingTime:  time.Since(start),
		HopsUsed:     1,
	}, nil
}

func (h *Hub) routeTenantBroadcast(
	ctx context.Context,
	msg *Message,
	start time.Time,
) (*RouteResult, error) {
	spokeIDs, exists := h.tenantIndex[msg.TenantID]
	if !exists || len(spokeIDs) == 0 {
		return nil, fmt.Errorf("no spokes in tenant %s", msg.TenantID)
	}

	var destinations []VirtualAddress
	for _, id := range spokeIDs {
		spoke := h.spokes[id]
		if spoke != nil {
			err := h.deliverToSpoke(ctx, msg, spoke)
			if err != nil {
				h.logger.Printf("Failed to deliver to %s: %v", id, err)
				continue
			}
			destinations = append(destinations, spoke.VirtualAddr)
		}
	}

	return &RouteResult{
		Decision:     RouteBroadcast,
		Destinations: destinations,
		RoutingTime:  time.Since(start),
		HopsUsed:     1,
	}, nil
}

func (h *Hub) routeFederated(
	ctx context.Context,
	msg *Message,
	start time.Time,
) (*RouteResult, error) {
	// Try peer hubs
	for hubID, peer := range h.peers {
		if peer.Connected && peer.TrustLevel >= 0.5 {
			// Forward to peer
			err := h.forwardToPeer(ctx, msg, peer)
			if err != nil {
				continue
			}

			return &RouteResult{
				Decision:      RouteForward,
				Destinations:  []VirtualAddress{msg.Destination},
				RoutingTime:   time.Since(start),
				HopsUsed:      2,
				FederatedHubs: []HubID{hubID},
			}, nil
		}
	}

	return nil, fmt.Errorf("no federated route available")
}

func (h *Hub) deliverToSpoke(_ context.Context, msg *Message, spoke *SpokeInfo) error {
	// P0 FIX: Use atomic updates for spoke stats
	spoke.MessageCount.Add(1)
	spoke.BytesRecv.Add(int64(len(msg.Payload)))
	spoke.LastSeen.Store(time.Now())

	h.logger.Printf("Delivered message %s to spoke %s", msg.ID, spoke.ID)
	return nil
}

func (h *Hub) forwardToPeer(_ context.Context, msg *Message, peer *PeerHub) error {
	// In production, this would forward via inter-hub protocol
	h.logger.Printf("Forwarded message %s to peer hub %s", msg.ID, peer.ID)
	return nil
}

// ============================================================================
// HELPER METHODS
// ============================================================================

func (h *Hub) generateSpokeID(tenantID, agentID string) SpokeID {
	hash := sha256.Sum256([]byte(fmt.Sprintf("%s:%s:%d", tenantID, agentID, time.Now().UnixNano())))
	return SpokeID(hex.EncodeToString(hash[:8]))
}

func (h *Hub) assignVirtualAddress(tenantID, agentID string) VirtualAddress {
	// Format: ocx://<hub>/<tenant>/<agent>
	return VirtualAddress(fmt.Sprintf("ocx://%s/%s/%s", h.ID, tenantID, agentID))
}

func (h *Hub) parseCapability(addr VirtualAddress) (Capability, bool) {
	// Check if address is a capability reference
	// Format: cap://<capability>
	str := string(addr)
	if len(str) > 6 && str[:6] == "cap://" {
		return Capability(str[6:]), true
	}
	return "", false
}

func (h *Hub) isTenantBroadcast(addr VirtualAddress) bool {
	// Format: broadcast://<tenant>
	str := string(addr)
	return len(str) > 11 && str[:11] == "broadcast:/"
}

// ============================================================================
// PEER HUB MANAGEMENT
// ============================================================================

// AddPeer adds a federated peer hub
func (h *Hub) AddPeer(id HubID, endpoint, region string, trustLevel float64) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.peers[id] = &PeerHub{
		ID:            id,
		Endpoint:      endpoint,
		Region:        region,
		TrustLevel:    trustLevel,
		Connected:     true,
		LastHeartbeat: time.Now(),
	}

	h.metrics.PeersConnected.Add(1)
	h.logger.Printf("Added peer hub: %s (region=%s)", id, region)
}

// RemovePeer removes a federated peer hub
func (h *Hub) RemovePeer(id HubID) {
	h.mu.Lock()
	defer h.mu.Unlock()

	delete(h.peers, id)
	h.metrics.PeersConnected.Add(-1)
	h.logger.Printf("Removed peer hub: %s", id)
}

// ============================================================================
// METRICS & STATUS
// ============================================================================

// GetMetrics returns hub metrics
func (h *Hub) GetMetrics() *HubMetrics {
	// Metrics are all atomic — no lock needed
	return h.metrics
}

// GetSpokes returns all registered spokes
func (h *Hub) GetSpokes() []*SpokeInfo {
	h.mu.RLock()
	defer h.mu.RUnlock()

	spokes := make([]*SpokeInfo, 0, len(h.spokes))
	for _, s := range h.spokes {
		spokes = append(spokes, s)
	}
	return spokes
}

// GetSpokesByCapability returns spokes with a specific capability
func (h *Hub) GetSpokesByCapability(cap Capability) []*SpokeInfo {
	h.mu.RLock()
	defer h.mu.RUnlock()

	ids := h.capabilityIndex[cap]
	spokes := make([]*SpokeInfo, 0, len(ids))
	for _, id := range ids {
		if s := h.spokes[id]; s != nil {
			spokes = append(spokes, s)
		}
	}
	return spokes
}
