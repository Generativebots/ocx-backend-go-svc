// Package fabric provides WebSocket spoke connections for the Hub.
package fabric

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// L4 FIX: Build WebSocket upgrader with origin validation.
// In production (OCX_ENV=production), only origins listed in OCX_ALLOWED_ORIGINS
// are accepted. In dev/staging, all origins are allowed with a warning.
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     buildCheckOrigin(),
}

// WebSocketSpoke represents an active WebSocket connection to a spoke
type WebSocketSpoke struct {
	Spoke *SpokeInfo
	Conn  *websocket.Conn
	Send  chan []byte
}

// buildCheckOrigin returns a CheckOrigin function based on the deployment environment.
// L4 FIX: In production, validates origins against OCX_ALLOWED_ORIGINS.
func buildCheckOrigin() func(r *http.Request) bool {
	env := os.Getenv("OCX_ENV")
	allowedRaw := os.Getenv("OCX_ALLOWED_ORIGINS")

	if env == "production" && allowedRaw != "" {
		allowed := make(map[string]bool)
		for _, origin := range strings.Split(allowedRaw, ",") {
			allowed[strings.TrimSpace(origin)] = true
		}
		log.Printf("[WebSocket] L4 FIX: Origin allowlist active (%d origins)", len(allowed))
		return func(r *http.Request) bool {
			origin := r.Header.Get("Origin")
			if allowed[origin] {
				return true
			}
			log.Printf("[WebSocket] ❌ Rejected connection from origin: %s", origin)
			return false
		}
	}

	// Dev/staging: allow all origins with warning
	if env == "production" && allowedRaw == "" {
		log.Println("[WebSocket] ⚠️  OCX_ALLOWED_ORIGINS not set in production — allowing all origins (INSECURE)")
	}
	return func(r *http.Request) bool {
		return true
	}
}

// HandleWebSocket upgrades HTTP to WebSocket and registers as spoke
func (h *Hub) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade failed: %v", err)
		return
	}

	// Extract agent info from headers
	tenantID := r.Header.Get("X-Tenant-ID")
	agentID := r.Header.Get("X-Agent-ID")

	if tenantID == "" {
		tenantID = "default"
	}
	if agentID == "" {
		agentID = "ws-" + time.Now().Format("20060102150405")
	}

	// Register as spoke with default capabilities
	spoke, err := h.RegisterSpoke(tenantID, agentID, []Capability{CapabilityData}, 0.5, nil)
	if err != nil {
		log.Printf("Failed to register WebSocket spoke: %v", err)
		conn.Close()
		return
	}

	log.Printf("WebSocket spoke connected: %s (tenant=%s)", spoke.ID, tenantID)

	// Handle connection
	go h.handleSpokeConnection(spoke, conn)
}

// handleSpokeConnection manages the WebSocket message loop
func (h *Hub) handleSpokeConnection(spoke *SpokeInfo, conn *websocket.Conn) {
	const (
		pongWait   = 60 * time.Second // Time allowed to read the next pong
		pingPeriod = 30 * time.Second // Send pings at this interval (must be < pongWait)
		writeWait  = 10 * time.Second // Time allowed to write a message
	)

	defer func() {
		h.UnregisterSpoke(spoke.ID)
		conn.Close()
		log.Printf("WebSocket spoke disconnected: %s", spoke.ID)
	}()

	// Set read deadline
	conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	// M5 FIX: Start a ping ticker goroutine to keep the connection alive.
	// Previously the server had a PongHandler but never sent Ping frames,
	// so connections would die after 60s of spoke silence.
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(pingPeriod)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				conn.SetWriteDeadline(time.Now().Add(writeWait))
				if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
					log.Printf("Ping failed for spoke %s: %v", spoke.ID, err)
					return
				}
			case <-done:
				return
			}
		}
	}()

	defer close(done) // Stop the ping ticker when the read loop exits

	for {
		_, payload, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket error: %v", err)
			}
			break
		}

		// Update last seen
		spoke.LastSeen = time.Now()
		spoke.MessageCount++
		spoke.BytesRecv += int64(len(payload))

		// Parse message and route through hub
		var msg WSMessage
		if err := json.Unmarshal(payload, &msg); err != nil {
			log.Printf("Invalid message format: %v", err)
			continue
		}

		// Create hub message and route
		hubMsg := &Message{
			ID:          msg.ID,
			Type:        msg.Type,
			Source:      spoke.VirtualAddr,
			Destination: VirtualAddress(msg.Destination),
			TenantID:    spoke.TenantID,
			Payload:     payload,
			Timestamp:   time.Now(),
			TTL:         5,
		}

		result, err := h.Route(context.Background(), hubMsg)
		if err != nil {
			log.Printf("Hub routing failed: %v", err)
			// Send error response
			errResp, _ := json.Marshal(map[string]string{
				"error": err.Error(),
				"id":    msg.ID,
			})
			conn.SetWriteDeadline(time.Now().Add(writeWait))
			conn.WriteMessage(websocket.TextMessage, errResp)
			continue
		}

		// Send success response
		resp, _ := json.Marshal(map[string]interface{}{
			"id":           msg.ID,
			"status":       "routed",
			"destinations": result.Destinations,
			"hops":         result.HopsUsed,
		})
		conn.SetWriteDeadline(time.Now().Add(writeWait))
		conn.WriteMessage(websocket.TextMessage, resp)
	}
}

// WSMessage represents a WebSocket message from a spoke
type WSMessage struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	Destination string `json:"destination"`
	Payload     []byte `json:"payload,omitempty"`
}

// BroadcastToTenant sends a message to all spokes in a tenant
func (h *Hub) BroadcastToTenant(tenantID string, message []byte) error {
	msg := &Message{
		ID:          time.Now().Format("20060102150405.000"),
		Type:        "broadcast",
		Destination: VirtualAddress("broadcast://" + tenantID),
		TenantID:    tenantID,
		Payload:     message,
		Timestamp:   time.Now(),
		TTL:         5,
	}

	_, err := h.Route(context.Background(), msg)
	return err
}
