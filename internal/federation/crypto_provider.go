package federation

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
)

// ============================================================================
// DUAL CRYPTO PROVIDER — ECDSA P-256 / Ed25519
// Tenant-configurable signing algorithm for Inter-OCX Federation.
// ============================================================================

// CryptoAlgorithm identifies the signing algorithm used by a CryptoProvider.
type CryptoAlgorithm string

const (
	// AlgorithmEd25519 uses Ed25519 (RFC 8032). Deterministic, fast, 64-byte
	// fixed signatures. Default for most tenants.
	AlgorithmEd25519 CryptoAlgorithm = "ed25519"

	// AlgorithmECDSA uses ECDSA with the NIST P-256 curve. FIPS 140-2/3
	// compliant. Required for regulated financial tenants.
	AlgorithmECDSA CryptoAlgorithm = "ecdsa-p256"
)

// CryptoProvider abstracts signing and verification so the federation layer
// can operate algorithm-agnostically. Each tenant selects their preferred
// algorithm via configuration.
type CryptoProvider interface {
	// Algorithm returns the algorithm this provider implements.
	Algorithm() CryptoAlgorithm

	// PublicKeyBytes returns the raw public key bytes suitable for wire
	// transmission or embedding in an Attestation struct.
	PublicKeyBytes() []byte

	// Sign signs the given data and returns a signature.
	Sign(data []byte) ([]byte, error)

	// Verify verifies a signature over data using the given public key bytes.
	// The publicKey slice must be in the same format returned by PublicKeyBytes.
	Verify(publicKey, data, signature []byte) (bool, error)

	// EncodePublicKeyPEM returns the PEM-encoded public key for the HELLO
	// message exchange.
	EncodePublicKeyPEM() (string, error)
}

// NewCryptoProvider creates a CryptoProvider with a freshly generated key pair
// for the specified algorithm. Returns an error for unknown algorithms.
func NewCryptoProvider(algorithm CryptoAlgorithm) (CryptoProvider, error) {
	switch algorithm {
	case AlgorithmEd25519:
		return newEd25519Provider()
	case AlgorithmECDSA:
		return newECDSAProvider()
	default:
		return nil, fmt.Errorf("unsupported crypto algorithm: %s (supported: %s, %s)",
			algorithm, AlgorithmEd25519, AlgorithmECDSA)
	}
}

// ============================================================================
// Ed25519 PROVIDER
// ============================================================================

// Ed25519Provider implements CryptoProvider using Ed25519 (RFC 8032).
type Ed25519Provider struct {
	privateKey ed25519.PrivateKey
	publicKey  ed25519.PublicKey
}

func newEd25519Provider() (*Ed25519Provider, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("ed25519 key generation failed: %w", err)
	}
	return &Ed25519Provider{privateKey: priv, publicKey: pub}, nil
}

// NewEd25519ProviderFromKey wraps an existing Ed25519 key pair.
func NewEd25519ProviderFromKey(priv ed25519.PrivateKey) *Ed25519Provider {
	return &Ed25519Provider{
		privateKey: priv,
		publicKey:  priv.Public().(ed25519.PublicKey),
	}
}

func (p *Ed25519Provider) Algorithm() CryptoAlgorithm {
	return AlgorithmEd25519
}

func (p *Ed25519Provider) PublicKeyBytes() []byte {
	return []byte(p.publicKey)
}

func (p *Ed25519Provider) Sign(data []byte) ([]byte, error) {
	return ed25519.Sign(p.privateKey, data), nil
}

func (p *Ed25519Provider) Verify(publicKey, data, signature []byte) (bool, error) {
	if len(publicKey) != ed25519.PublicKeySize {
		return false, fmt.Errorf("invalid Ed25519 public key size: got %d, want %d",
			len(publicKey), ed25519.PublicKeySize)
	}
	return ed25519.Verify(ed25519.PublicKey(publicKey), data, signature), nil
}

func (p *Ed25519Provider) EncodePublicKeyPEM() (string, error) {
	derBytes, err := x509.MarshalPKIXPublicKey(p.publicKey)
	if err != nil {
		return "", fmt.Errorf("failed to marshal Ed25519 public key: %w", err)
	}
	pemBlock := &pem.Block{Type: "PUBLIC KEY", Bytes: derBytes}
	return string(pem.EncodeToMemory(pemBlock)), nil
}

// ============================================================================
// ECDSA P-256 PROVIDER
// ============================================================================

// ECDSAProvider implements CryptoProvider using ECDSA with the NIST P-256 curve.
type ECDSAProvider struct {
	privateKey *ecdsa.PrivateKey
	publicKey  *ecdsa.PublicKey
}

func newECDSAProvider() (*ECDSAProvider, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("ecdsa key generation failed: %w", err)
	}
	return &ECDSAProvider{privateKey: priv, publicKey: &priv.PublicKey}, nil
}

// NewECDSAProviderFromKey wraps an existing ECDSA key pair.
func NewECDSAProviderFromKey(priv *ecdsa.PrivateKey) *ECDSAProvider {
	return &ECDSAProvider{
		privateKey: priv,
		publicKey:  &priv.PublicKey,
	}
}

func (p *ECDSAProvider) Algorithm() CryptoAlgorithm {
	return AlgorithmECDSA
}

func (p *ECDSAProvider) PublicKeyBytes() []byte {
	// Marshal the public key to PKIX DER for wire format
	der, err := x509.MarshalPKIXPublicKey(p.publicKey)
	if err != nil {
		return nil
	}
	return der
}

func (p *ECDSAProvider) Sign(data []byte) ([]byte, error) {
	hash := sha256.Sum256(data)
	return ecdsa.SignASN1(rand.Reader, p.privateKey, hash[:])
}

func (p *ECDSAProvider) Verify(publicKeyDER, data, signature []byte) (bool, error) {
	// Parse PKIX DER public key
	pub, err := x509.ParsePKIXPublicKey(publicKeyDER)
	if err != nil {
		return false, fmt.Errorf("failed to parse ECDSA public key: %w", err)
	}

	ecPub, ok := pub.(*ecdsa.PublicKey)
	if !ok {
		return false, errors.New("public key is not ECDSA")
	}

	hash := sha256.Sum256(data)
	return ecdsa.VerifyASN1(ecPub, hash[:], signature), nil
}

func (p *ECDSAProvider) EncodePublicKeyPEM() (string, error) {
	derBytes, err := x509.MarshalPKIXPublicKey(p.publicKey)
	if err != nil {
		return "", fmt.Errorf("failed to marshal ECDSA public key: %w", err)
	}
	pemBlock := &pem.Block{Type: "PUBLIC KEY", Bytes: derBytes}
	return string(pem.EncodeToMemory(pemBlock)), nil
}

// ============================================================================
// TENANT CONFIGURATION HELPER
// ============================================================================

// DefaultCryptoAlgorithm is used when no tenant-level preference is configured.
const DefaultCryptoAlgorithm = AlgorithmEd25519

// ResolveCryptoAlgorithm returns the algorithm for a tenant, or the default.
// It reads from a tenant config map (tenant_id → algorithm string).
func ResolveCryptoAlgorithm(tenantID string, tenantConfig map[string]string, globalDefault CryptoAlgorithm) CryptoAlgorithm {
	if alg, ok := tenantConfig[tenantID]; ok {
		switch CryptoAlgorithm(alg) {
		case AlgorithmEd25519, AlgorithmECDSA:
			return CryptoAlgorithm(alg)
		}
	}
	return globalDefault
}
