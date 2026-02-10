/*
SOP Identity Verification Integration
Integrates PID-to-Identity Mapper with Speculative Outbound Proxy
*/

package sop

import (
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"syscall"

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

	slog.Info("Verified identity: PID=, AgentID", "pid", pid, "claimed_agent_i_d", claimedAgentID)
	return claimedAgentID, nil
}

// GetPIDFromSocket gets PID from socket connection
// This would use eBPF socket tracking in production
func (iv *IdentityVerifier) GetPIDFromSocket(conn interface{}) (uint32, error) {
	// In production, use eBPF map to lookup PID from socket
	// For now, placeholder
	return 0, fmt.Errorf("not implemented")
}

// EnrichRequestWithIdentity adds identity information to request
func (iv *IdentityVerifier) EnrichRequestWithIdentity(r *http.Request, pid uint32) error {
	identity, err := iv.mapper.GetIdentity(pid)
	if err != nil {
		return err
	}

	// Add identity headers
	r.Header.Set("X-OCX-Agent-ID", identity.AgentID)
	r.Header.Set("X-OCX-Trust-Level", fmt.Sprintf("%.2f", float64(identity.TrustLevel)/100))
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

// GetPIDFromConnection extracts PID from TCP connection using SO_PEERCRED (Linux only)
// On macOS, this functionality requires different syscalls
func GetPIDFromConnection(conn syscall.Conn) (uint32, error) {
	// TODO: This is Linux-specific and requires build tags
	// For cross-platform support, use conditional compilation:
	// - identity_verifier_linux.go with SO_PEERCRED
	// - identity_verifier_darwin.go with LOCAL_PEERPID

	// For now, return not implemented on non-Linux platforms
	return 0, fmt.Errorf("GetPIDFromConnection not implemented on this platform")

	/* Linux implementation (requires //go:build linux):
	rawConn, err := conn.SyscallConn()
	if err != nil {
		return 0, err
	}

	var pid uint32
	var sockErr error

	err = rawConn.Control(func(fd uintptr) {
		ucred, err := syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
		if err != nil {
			sockErr = err
			return
		}
		pid = uint32(ucred.Pid)
	})

	if err != nil {
		return 0, err
	}
	if sockErr != nil {
		return 0, sockErr
	}

	return pid, nil
	*/
}
