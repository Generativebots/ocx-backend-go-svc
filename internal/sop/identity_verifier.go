/*
SOP Identity Verification Integration
Integrates PID-to-Identity Mapper with Speculative Outbound Proxy
*/

package sop

import (
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"runtime"
	"strconv"

	"github.com/ocx/backend/internal/identity"
)

// IdentityVerifier wraps the identity mapper for SOP use
type IdentityVerifier struct {
	mapper *identity.IdentityMapper
}

// NewIdentityVerifier creates a new identity verifier
func NewIdentityVerifier(mapper *identity.IdentityMapper) *IdentityVerifier {
	return &IdentityVerifier{
		mapper: mapper,
	}
}

// VerifyRequest verifies the identity of a request
func (iv *IdentityVerifier) VerifyRequest(r *http.Request) (string, error) {
	// Get PID from request context
	// In production, this would come from eBPF socket tracking
	pidStr := r.Header.Get("X-OCX-PID")
	if pidStr == "" {
		return "", fmt.Errorf("missing PID header")
	}

	pid64, err := strconv.ParseUint(pidStr, 10, 32)
	if err != nil {
		return "", fmt.Errorf("invalid PID: %w", err)
	}
	pid := uint32(pid64)

	// Get claimed AgentID from header
	claimedAgentID := r.Header.Get("X-OCX-Agent-ID")
	if claimedAgentID == "" {
		return "", fmt.Errorf("missing Agent-ID header")
	}

	// Verify identity via kernel map
	verified, err := iv.mapper.VerifyIdentity(pid, claimedAgentID)
	if err != nil {
		return "", fmt.Errorf("identity verification failed: %w", err)
	}

	if !verified {
		return "", fmt.Errorf("identity mismatch: claimed=%s", claimedAgentID)
	}

	slog.Info("Verified identity", "pid", pid, "agent_id", claimedAgentID)
	return claimedAgentID, nil
}

// GetPIDFromSocket gets PID from socket connection using cross-platform
// system calls. On Linux, uses SO_PEERCRED; on macOS, uses LOCAL_PEERPID.
func (iv *IdentityVerifier) GetPIDFromSocket(conn net.Conn) (uint32, error) {
	tcpConn, ok := conn.(*net.TCPConn)
	if !ok {
		return 0, fmt.Errorf("connection is not TCP, cannot resolve PID")
	}

	rawConn, err := tcpConn.SyscallConn()
	if err != nil {
		return 0, fmt.Errorf("cannot get raw connection: %w", err)
	}

	var pid uint32
	var controlErr error

	err = rawConn.Control(func(fd uintptr) {
		pid, controlErr = getPIDFromFD(fd)
	})

	if err != nil {
		return 0, fmt.Errorf("rawConn.Control failed: %w", err)
	}
	if controlErr != nil {
		return 0, fmt.Errorf("getPIDFromFD failed: %w", controlErr)
	}

	slog.Debug("Resolved PID from socket", "pid", pid, "platform", runtime.GOOS)
	return pid, nil
}

// EnrichRequestWithIdentity adds identity information to request
func (iv *IdentityVerifier) EnrichRequestWithIdentity(r *http.Request, pid uint32) error {
	ident, err := iv.mapper.GetIdentity(pid)
	if err != nil {
		return err
	}

	// Add identity headers
	r.Header.Set("X-OCX-Agent-ID", ident.AgentID)
	r.Header.Set("X-OCX-Trust-Level", fmt.Sprintf("%.2f", float64(ident.TrustLevel)/100))
	r.Header.Set("X-OCX-PID", fmt.Sprintf("%d", pid))

	return nil
}

// Middleware for identity verification
func (iv *IdentityVerifier) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify identity
		agentID, err := iv.VerifyRequest(r)
		if err != nil {
			slog.Warn("Identity verification failed", "error", err)
			http.Error(w, "Unauthorized: Identity verification failed", http.StatusUnauthorized)
			return
		}

		// Add verified identity to context
		r.Header.Set("X-OCX-Verified-Agent-ID", agentID)

		// Continue
		next.ServeHTTP(w, r)
	})
}
