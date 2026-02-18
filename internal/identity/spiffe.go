/*
SPIFFE Integration
Provides cryptographic identity verification using SPIFFE/SPIRE
*/

package identity

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"fmt"
	"log/slog"
	"time"

	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"github.com/spiffe/go-spiffe/v2/spiffetls/tlsconfig"
	"github.com/spiffe/go-spiffe/v2/workloadapi"
)

// SPIFFEVerifier verifies SPIFFE SVIDs
type SPIFFEVerifier struct {
	source *workloadapi.X509Source
	ctx    context.Context
}

// NewSPIFFEVerifier creates a new SPIFFE verifier
func NewSPIFFEVerifier(socketPath string) (*SPIFFEVerifier, error) {
	// Use a timeout to avoid blocking startup when SPIRE agent is unavailable
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Connect to SPIRE agent
	source, err := workloadapi.NewX509Source(
		ctx,
		workloadapi.WithClientOptions(workloadapi.WithAddr(socketPath)),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to SPIRE: %w", err)
	}

	slog.Info("Connected to SPIRE agent at", "socket_path", socketPath)
	return &SPIFFEVerifier{
		source: source,
		ctx:    context.Background(),
	}, nil
}

// VerifySVID verifies a SPIFFE SVID and returns its hash
func (sv *SPIFFEVerifier) VerifySVID(spiffeID string) (uint64, error) {
	// Parse SPIFFE ID
	id, err := spiffeid.FromString(spiffeID)
	if err != nil {
		return 0, fmt.Errorf("invalid SPIFFE ID: %w", err)
	}

	// Get SVID from source
	svid, err := sv.source.GetX509SVID()
	if err != nil {
		return 0, fmt.Errorf("failed to get SVID: %w", err)
	}

	// Verify SPIFFE ID matches
	if svid.ID.String() != id.String() {
		return 0, fmt.Errorf("SPIFFE ID mismatch: expected %s, got %s", id, svid.ID)
	}

	// Calculate hash of SVID
	hash := sv.calculateSVIDHash(svid.Certificates[0].Raw)

	slog.Info("Verified SPIFFE ID: (hash: )", "spiffe_i_d", spiffeID, "hash", hash)
	return hash, nil
}

// calculateSVIDHash calculates a 64-bit hash of the SVID certificate
func (sv *SPIFFEVerifier) calculateSVIDHash(certDER []byte) uint64 {
	hash := sha256.Sum256(certDER)

	// Take first 8 bytes as uint64
	var result uint64
	for i := 0; i < 8; i++ {
		result = (result << 8) | uint64(hash[i])
	}

	return result
}

// GetTLSConfig returns TLS config with SPIFFE authentication
func (sv *SPIFFEVerifier) GetTLSConfig() (*tls.Config, error) {
	// Create TLS config for mTLS
	tlsConf := tlsconfig.MTLSClientConfig(sv.source, sv.source, tlsconfig.AuthorizeAny())
	return tlsConf, nil
}

// Close cleanup
func (sv *SPIFFEVerifier) Close() error {
	return sv.source.Close()
}

// GenerateSPIFFEID generates a SPIFFE ID for an agent
func GenerateSPIFFEID(trustDomain, agentID string) string {
	return fmt.Sprintf("spiffe://%s/agent/%s", trustDomain, agentID)
}

// Example SPIFFE IDs:
// spiffe://ocx.example.com/agent/agent-12345
// spiffe://ocx.example.com/agent/procurement-bot
// spiffe://ocx.example.com/agent/finance-controller
