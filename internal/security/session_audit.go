package security

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ============================================================================
// SESSION AUDIT LOGGER — Security Forensics
// ============================================================================

// SessionAuditEntry represents a single audit log entry for persistence.
type SessionAuditEntry struct {
	SessionID     string                 `json:"session_id"`
	TenantID      string                 `json:"tenant_id"`
	AgentID       string                 `json:"agent_id"`
	EventType     string                 `json:"event_type"`
	IPAddress     string                 `json:"ip_address,omitempty"`
	UserAgent     string                 `json:"user_agent,omitempty"`
	Country       string                 `json:"country,omitempty"`
	City          string                 `json:"city,omitempty"`
	Region        string                 `json:"region,omitempty"`
	Latitude      float64                `json:"latitude,omitempty"`
	Longitude     float64                `json:"longitude,omitempty"`
	ISP           string                 `json:"isp,omitempty"`
	RequestPath   string                 `json:"request_path,omitempty"`
	RequestMethod string                 `json:"request_method,omitempty"`
	TrustScore    float64                `json:"trust_score,omitempty"`
	Verdict       string                 `json:"verdict,omitempty"`
	RiskFlags     []string               `json:"risk_flags"`
	Metadata      map[string]interface{} `json:"metadata"`
}

// GeoInfo represents IP geolocation data.
type GeoInfo struct {
	Country   string  `json:"country"`
	City      string  `json:"city"`
	Region    string  `json:"regionName"`
	Latitude  float64 `json:"lat"`
	Longitude float64 `json:"lon"`
	ISP       string  `json:"isp"`
	Org       string  `json:"org"`
	AS        string  `json:"as"`
	Query     string  `json:"query"`
	Status    string  `json:"status"`
}

// AuditStore is the interface for persisting audit entries.
type AuditStore interface {
	InsertAuditLog(entry interface{}) error
}

// SessionAuditor handles security audit logging with geo resolution.
type SessionAuditor struct {
	store    AuditStore
	geoCache sync.Map // IP → *geoCacheEntry
	cacheTTL time.Duration
	client   *http.Client
}

type geoCacheEntry struct {
	geo       *GeoInfo
	expiresAt time.Time
}

// NewSessionAuditor creates a new session auditor.
func NewSessionAuditor(store AuditStore) *SessionAuditor {
	return &SessionAuditor{
		store:    store,
		cacheTTL: 1 * time.Hour,
		client: &http.Client{
			Timeout: 3 * time.Second,
		},
	}
}

// ExtractClientInfo extracts IP, user-agent, and request metadata from an HTTP request.
func ExtractClientInfo(r *http.Request) (ip, userAgent, path, method string) {
	// Priority: X-Forwarded-For → X-Real-IP → RemoteAddr
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		ip = strings.TrimSpace(parts[0])
	} else if realIP := r.Header.Get("X-Real-IP"); realIP != "" {
		ip = strings.TrimSpace(realIP)
	} else {
		ip = r.RemoteAddr
		// Strip port
		if host, _, err := net.SplitHostPort(ip); err == nil {
			ip = host
		}
	}
	userAgent = r.Header.Get("User-Agent")
	path = r.URL.Path
	method = r.Method
	return
}

// ResolveGeo resolves IP to geographic location via ip-api.com (free tier).
// Results are cached in-memory for cacheTTL to avoid rate limiting.
func (sa *SessionAuditor) ResolveGeo(ip string) *GeoInfo {
	// Skip private/localhost IPs
	if isPrivateIP(ip) {
		return &GeoInfo{Country: "LOCAL", City: "localhost", ISP: "private"}
	}

	// Check cache
	if cached, ok := sa.geoCache.Load(ip); ok {
		entry := cached.(*geoCacheEntry)
		if time.Now().Before(entry.expiresAt) {
			return entry.geo
		}
		sa.geoCache.Delete(ip)
	}

	// Call ip-api.com (free, 45 req/min, no key needed)
	url := fmt.Sprintf("http://ip-api.com/json/%s?fields=status,country,regionName,city,lat,lon,isp,org,as,query", ip)
	resp, err := sa.client.Get(url)
	if err != nil {
		slog.Warn("[SessionAuditor] Geo resolution failed", "ip", ip, "error", err)
		return nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}

	var geo GeoInfo
	if err := json.Unmarshal(body, &geo); err != nil || geo.Status != "success" {
		slog.Warn("[SessionAuditor] Geo resolution returned non-success", "ip", ip, "status", geo.Status)
		return nil
	}

	// Cache
	sa.geoCache.Store(ip, &geoCacheEntry{
		geo:       &geo,
		expiresAt: time.Now().Add(sa.cacheTTL),
	})

	return &geo
}

// LogEvent persists an audit entry with geolocation to the store.
func (sa *SessionAuditor) LogEvent(entry *SessionAuditEntry) {
	if sa.store == nil {
		return
	}

	// Resolve geo if IP present and no country already set
	if entry.IPAddress != "" && entry.Country == "" {
		if geo := sa.ResolveGeo(entry.IPAddress); geo != nil {
			entry.Country = geo.Country
			entry.City = geo.City
			entry.Region = geo.Region
			entry.Latitude = geo.Latitude
			entry.Longitude = geo.Longitude
			entry.ISP = geo.ISP
		}
	}

	// Detect risk flags
	if entry.RiskFlags == nil {
		entry.RiskFlags = detectRiskFlags(entry)
	}

	if entry.Metadata == nil {
		entry.Metadata = make(map[string]interface{})
	}

	// Non-blocking persist
	go func() {
		if err := sa.store.InsertAuditLog(entry); err != nil {
			slog.Error("[SessionAuditor] Failed to persist audit entry",
				"agent_id", entry.AgentID,
				"event_type", entry.EventType,
				"error", err,
			)
		}
	}()
}

// LogFromRequest is a convenience that extracts client info from the request
// and logs an audit event.
func (sa *SessionAuditor) LogFromRequest(r *http.Request, sessionID, tenantID, agentID, eventType, verdict string, trustScore float64, meta map[string]interface{}) {
	ip, ua, path, method := ExtractClientInfo(r)
	sa.LogEvent(&SessionAuditEntry{
		SessionID:     sessionID,
		TenantID:      tenantID,
		AgentID:       agentID,
		EventType:     eventType,
		IPAddress:     ip,
		UserAgent:     ua,
		RequestPath:   path,
		RequestMethod: method,
		TrustScore:    trustScore,
		Verdict:       verdict,
		Metadata:      meta,
	})
}

// detectRiskFlags identifies potential security risks from the audit entry.
func detectRiskFlags(entry *SessionAuditEntry) []string {
	var flags []string

	// Localhost/private IP accessing production resources
	if isPrivateIP(entry.IPAddress) {
		flags = append(flags, "PRIVATE_NETWORK")
	}

	// Very low trust score
	if entry.TrustScore > 0 && entry.TrustScore < 0.3 {
		flags = append(flags, "LOW_TRUST")
	}

	// Blocked verdict
	if entry.Verdict == "BLOCK" {
		flags = append(flags, "BLOCKED_ACTION")
	}

	if len(flags) == 0 {
		flags = []string{}
	}
	return flags
}

// isPrivateIP checks if an IP address is in a private range or localhost.
func isPrivateIP(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return true // Treat unparseable as private
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() {
		return true
	}
	return false
}
