// Package fabric provides WebSocket spoke connections for the Hub.
package fabric

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// L4 FIX: Build WebSocket upgrader with origin validation.
// In production (OCX_ENV=production), only origins listed in OCX_ALLOWED_ORIGINS
// are accepted. In dev/staging, all origins are allowed with a warning.
var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin:     buildCheckOrigin(),
}

const (
	pongWait   = 60 * time.Second // Time allowed to read the next pong
	pingPeriod = 30 * time.Second // Send pings at this interval (must be < pongWait)
	writeWait  = 10 * time.Second // Time allowed to write a message
	maxMsgSize = 512 * 1024       // 512KB max message size per frame
	sendBuffer = 256              // Per-spoke outbound channel buffer
)

// WebSocketSpoke represents an active WebSocket connection to a spoke.
// P0 FIX: All writes go through the Send channel → writePump goroutine,
// eliminating concurrent write races between ping, response, and broadcast.
type WebSocketSpoke struct {
	hub   *Hub
	Spoke *SpokeInfo
	Conn  *websocket.Conn
	Send  chan []byte   // Buffered outbound messages
	done  chan struct{} // Signals shutdown to writePump
	once  sync.Once     // Ensures close only happens once
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
		slog.Info("[WebSocket] L4 FIX: Origin allowlist active ( origins)", "count", len(allowed))
		return func(r *http.Request) bool {
			origin := r.Header.Get("Origin")
			if allowed[origin] {
				return true
			}
			slog.Info("[WebSocket] Rejected connection from origin", "origin", origin)
			return false
		}
	}

	// Dev/staging: allow all origins with warning
	if env == "production" && allowedRaw == "" {
		slog.Info("[WebSocket] ⚠️  OCX_ALLOWED_ORIGINS not set in production — allowing all origins (INSECURE)")
	}
	return func(r *http.Request) bool {
		return true
	}
}

// HandleWebSocket upgrades HTTP to WebSocket and registers as spoke
func (h *Hub) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Warn("WebSocket upgrade failed", "error", err)
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
		slog.Warn("Failed to register WebSocket spoke", "error", err)
		conn.Close()
		return
	}

	ws := &WebSocketSpoke{
		hub:   h,
		Spoke: spoke,
		Conn:  conn,
		Send:  make(chan []byte, sendBuffer),
		done:  make(chan struct{}),
	}

	slog.Info("WebSocket spoke connected: (tenant=)", "i_d", spoke.ID, "tenant_i_d", tenantID)
	// P0 FIX: Two goroutines with clear ownership:
	// - writePump owns ALL writes to conn (ping, data, close)
	// - readPump owns ALL reads from conn
	// This eliminates concurrent write races.
	go ws.writePump()
	go ws.readPump()
}

// close safely shuts down the spoke connection exactly once.
func (ws *WebSocketSpoke) close() {
	ws.once.Do(func() {
		close(ws.done)
		ws.hub.UnregisterSpoke(ws.Spoke.ID)
		ws.Conn.Close()
		slog.Info("WebSocket spoke disconnected", "i_d", ws.Spoke.ID)
	})
}

// writePump serializes ALL writes to the WebSocket connection.
// P0 FIX: This is the ONLY goroutine that calls conn.WriteMessage,
// eliminating the previous race between ping writes and response writes.
func (ws *WebSocketSpoke) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		ws.close()
	}()

	for {
		select {
		case message, ok := <-ws.Send:
			ws.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// Send channel closed — send close frame
				ws.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			if err := ws.Conn.WriteMessage(websocket.TextMessage, message); err != nil {
				slog.Warn("Write failed for spoke", "i_d", ws.Spoke.ID, "error", err)
				return
			}

			// Drain queued messages in the same write frame for efficiency
			n := len(ws.Send)
			for i := 0; i < n; i++ {
				msg := <-ws.Send
				if err := ws.Conn.WriteMessage(websocket.TextMessage, msg); err != nil {
					slog.Warn("Batch write failed for spoke", "i_d", ws.Spoke.ID, "error", err)
					return
				}
			}

		case <-ticker.C:
			ws.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := ws.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				slog.Warn("Ping failed for spoke", "i_d", ws.Spoke.ID, "error", err)
				return
			}

		case <-ws.done:
			return
		}
	}
}

// readPump reads messages from the WebSocket connection and routes them.
// This is the ONLY goroutine that calls conn.ReadMessage.
func (ws *WebSocketSpoke) readPump() {
	defer ws.close()

	ws.Conn.SetReadLimit(maxMsgSize)
	ws.Conn.SetReadDeadline(time.Now().Add(pongWait))
	ws.Conn.SetPongHandler(func(string) error {
		ws.Conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, payload, err := ws.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				slog.Warn("WebSocket error", "error", err)
			}
			return
		}

		// P0 FIX #2: Update spoke stats via atomic operations (see hub.go changes)
		ws.Spoke.Touch(int64(len(payload)))

		// Parse message and route through hub
		var msg WSMessage
		if err := json.Unmarshal(payload, &msg); err != nil {
			slog.Info("Invalid message format", "error", err)
			continue
		}

		// Create hub message and route
		hubMsg := &Message{
			ID:          msg.ID,
			Type:        msg.Type,
			Source:      ws.Spoke.VirtualAddr,
			Destination: VirtualAddress(msg.Destination),
			TenantID:    ws.Spoke.TenantID,
			Payload:     payload,
			Timestamp:   time.Now(),
			TTL:         5,
		}

		result, err := ws.hub.Route(context.Background(), hubMsg)
		if err != nil {
			slog.Warn("Hub routing failed", "error", err)
			// Send error response via write pump (non-blocking)
			errResp, _ := json.Marshal(map[string]string{
				"error": err.Error(),
				"id":    msg.ID,
			})
			select {
			case ws.Send <- errResp:
			default:
				slog.Warn("Send buffer full for spoke , dropping error response", "i_d", ws.Spoke.ID)
			}
			continue
		}

		// Send success response via write pump (non-blocking)
		resp, _ := json.Marshal(map[string]interface{}{
			"id":           msg.ID,
			"status":       "routed",
			"destinations": result.Destinations,
			"hops":         result.HopsUsed,
		})
		select {
		case ws.Send <- resp:
		default:
			slog.Info("Send buffer full for spoke , dropping response", "i_d", ws.Spoke.ID)
		}
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
