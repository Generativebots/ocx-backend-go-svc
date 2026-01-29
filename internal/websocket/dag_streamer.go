package websocket

import (
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// DAGEvent represents a real-time event for the DAG visualization
type DAGEvent struct {
	Type          string                 `json:"type"` // "node_added", "edge_added", "node_updated", "commit", "revert"
	TransactionID string                 `json:"transaction_id"`
	Timestamp     time.Time              `json:"timestamp"`
	Data          map[string]interface{} `json:"data"`
}

// NodeData represents a node in the DAG
type NodeData struct {
	ID           string  `json:"id"`
	Label        string  `json:"label"`
	Type         string  `json:"type"`   // "agent", "tool", "jury", "gvisor", "escrow"
	Status       string  `json:"status"` // "pending", "running", "pass", "fail", "commit", "revert"
	Confidence   float64 `json:"confidence,omitempty"`
	EntropyScore float64 `json:"entropy_score,omitempty"`
}

// EdgeData represents an edge in the DAG
type EdgeData struct {
	ID           string  `json:"id"`
	Source       string  `json:"source"`
	Target       string  `json:"target"`
	EntropyScore float64 `json:"entropy_score"` // For color-coding
	Status       string  `json:"status"`        // "active", "flagged", "clear"
}

// DAGStreamer manages WebSocket connections for live DAG updates
type DAGStreamer struct {
	clients    map[*websocket.Conn]bool
	broadcast  chan DAGEvent
	register   chan *websocket.Conn
	unregister chan *websocket.Conn
	mu         sync.RWMutex
	upgrader   websocket.Upgrader
}

// NewDAGStreamer creates a new DAG streamer
func NewDAGStreamer() *DAGStreamer {
	return &DAGStreamer{
		clients:    make(map[*websocket.Conn]bool),
		broadcast:  make(chan DAGEvent, 256),
		register:   make(chan *websocket.Conn),
		unregister: make(chan *websocket.Conn),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true // Allow all origins for development
			},
		},
	}
}

// Run starts the WebSocket hub
func (ds *DAGStreamer) Run() {
	for {
		select {
		case client := <-ds.register:
			ds.mu.Lock()
			ds.clients[client] = true
			ds.mu.Unlock()
			log.Printf("📡 WebSocket client connected (total: %d)", len(ds.clients))

		case client := <-ds.unregister:
			ds.mu.Lock()
			if _, ok := ds.clients[client]; ok {
				delete(ds.clients, client)
				client.Close()
			}
			ds.mu.Unlock()
			log.Printf("📡 WebSocket client disconnected (total: %d)", len(ds.clients))

		case event := <-ds.broadcast:
			ds.mu.RLock()
			for client := range ds.clients {
				err := client.WriteJSON(event)
				if err != nil {
					log.Printf("WebSocket write error: %v", err)
					client.Close()
					delete(ds.clients, client)
				}
			}
			ds.mu.RUnlock()
		}
	}
}

// HandleWebSocket handles WebSocket connections
func (ds *DAGStreamer) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := ds.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}

	ds.register <- conn

	// Keep connection alive
	go func() {
		defer func() {
			ds.unregister <- conn
		}()

		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				break
			}
		}
	}()
}

// BroadcastEvent sends an event to all connected clients
func (ds *DAGStreamer) BroadcastEvent(event DAGEvent) {
	event.Timestamp = time.Now()
	ds.broadcast <- event
}

// StreamNodeAdded broadcasts a new node addition
func (ds *DAGStreamer) StreamNodeAdded(txID string, node NodeData) {
	ds.BroadcastEvent(DAGEvent{
		Type:          "node_added",
		TransactionID: txID,
		Data: map[string]interface{}{
			"node": node,
		},
	})
}

// StreamEdgeAdded broadcasts a new edge addition
func (ds *DAGStreamer) StreamEdgeAdded(txID string, edge EdgeData) {
	ds.BroadcastEvent(DAGEvent{
		Type:          "edge_added",
		TransactionID: txID,
		Data: map[string]interface{}{
			"edge": edge,
		},
	})
}

// StreamNodeUpdated broadcasts a node status update
func (ds *DAGStreamer) StreamNodeUpdated(txID string, nodeID string, status string, data map[string]interface{}) {
	eventData := map[string]interface{}{
		"node_id": nodeID,
		"status":  status,
	}
	for k, v := range data {
		eventData[k] = v
	}

	ds.BroadcastEvent(DAGEvent{
		Type:          "node_updated",
		TransactionID: txID,
		Data:          eventData,
	})
}

// StreamCommit broadcasts a commit event (green pulse animation)
func (ds *DAGStreamer) StreamCommit(txID string, nodeID string, confidence float64) {
	ds.BroadcastEvent(DAGEvent{
		Type:          "commit",
		TransactionID: txID,
		Data: map[string]interface{}{
			"node_id":    nodeID,
			"confidence": confidence,
			"animation":  "pulse_green",
		},
	})
}

// StreamRevert broadcasts a revert event (glitch/dissolve animation)
func (ds *DAGStreamer) StreamRevert(txID string, nodeID string, reason string) {
	ds.BroadcastEvent(DAGEvent{
		Type:          "revert",
		TransactionID: txID,
		Data: map[string]interface{}{
			"node_id":   nodeID,
			"reason":    reason,
			"animation": "dissolve",
		},
	})
}

// StreamEntropyUpdate broadcasts entropy score update for edge coloring
func (ds *DAGStreamer) StreamEntropyUpdate(txID string, edgeID string, entropyScore float64, status string) {
	ds.BroadcastEvent(DAGEvent{
		Type:          "entropy_update",
		TransactionID: txID,
		Data: map[string]interface{}{
			"edge_id":       edgeID,
			"entropy_score": entropyScore,
			"status":        status, // "clear" or "flagged"
			"color":         getEntropyColor(entropyScore),
		},
	})
}

// getEntropyColor returns color based on entropy score
func getEntropyColor(entropy float64) string {
	// Normal range: 4.0 - 5.0 (green to yellow)
	// High entropy: > 6.0 (orange to red) - encrypted/suspicious
	// Low entropy: < 3.0 (blue) - repetitive/covert channel

	if entropy > 7.0 {
		return "#ef4444" // Red - very suspicious
	} else if entropy > 6.0 {
		return "#f97316" // Orange - suspicious
	} else if entropy > 5.5 {
		return "#eab308" // Yellow - elevated
	} else if entropy >= 4.0 {
		return "#22c55e" // Green - normal
	} else if entropy >= 3.0 {
		return "#3b82f6" // Blue - low entropy
	} else {
		return "#8b5cf6" // Purple - very low (covert channel)
	}
}

// Example: Integration with speculative audit
func (ds *DAGStreamer) StreamSpeculativeAudit(txID, agentID string) {
	// 1. Add agent node
	ds.StreamNodeAdded(txID, NodeData{
		ID:     txID + "-agent",
		Label:  agentID,
		Type:   "agent",
		Status: "pending",
	})

	// 2. Add jury node
	ds.StreamNodeAdded(txID, NodeData{
		ID:     txID + "-jury",
		Label:  "Jury Audit",
		Type:   "jury",
		Status: "running",
	})

	// 3. Add edge with entropy
	ds.StreamEdgeAdded(txID, EdgeData{
		ID:           txID + "-edge-jury",
		Source:       txID + "-agent",
		Target:       txID + "-jury",
		EntropyScore: 4.5,
		Status:       "active",
	})

	// 4. Add gVisor node
	ds.StreamNodeAdded(txID, NodeData{
		ID:     txID + "-gvisor",
		Label:  "gVisor Sandbox",
		Type:   "gvisor",
		Status: "running",
	})

	// 5. Add escrow node
	ds.StreamNodeAdded(txID, NodeData{
		ID:     txID + "-escrow",
		Label:  "Escrow Gate",
		Type:   "escrow",
		Status: "pending",
	})
}

// GetStatistics returns WebSocket statistics
func (ds *DAGStreamer) GetStatistics() map[string]interface{} {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	return map[string]interface{}{
		"connected_clients": len(ds.clients),
		"broadcast_queue":   len(ds.broadcast),
	}
}
