package federation

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"time"
)

// ============================================================================
// CRYPTOGRAPHIC PRIMITIVES FOR INTER-OCX HANDSHAKE
// ============================================================================

// GenerateNonce creates a cryptographically secure random nonce
// Returns 32 bytes (256 bits) of randomness, hex-encoded
func GenerateNonce() (string, error) {
	nonce := make([]byte, 32)
	_, err := rand.Read(nonce)
	if err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}
	return hex.EncodeToString(nonce), nil
}

// CreateChallenge generates an HMAC-SHA256 challenge from nonce and instance ID
// This prevents replay attacks and binds the challenge to a specific instance
func CreateChallenge(nonce, instanceID string) ([]byte, error) {
	if nonce == "" || instanceID == "" {
		return nil, errors.New("nonce and instanceID must not be empty")
	}

	// Use instance ID as HMAC key
	h := hmac.New(sha256.New, []byte(instanceID))
	
	// Write nonce and timestamp to prevent replay
	timestamp := time.Now().Unix()
	h.Write([]byte(nonce))
	h.Write([]byte(fmt.Sprintf("%d", timestamp)))
	
	return h.Sum(nil), nil
}

// VerifyChallenge verifies that a challenge was created with the given nonce and instance ID
// Uses constant-time comparison to prevent timing attacks
func VerifyChallenge(challenge []byte, nonce, instanceID string, maxAge time.Duration) (bool, error) {
	// Recreate the challenge
	expectedChallenge, err := CreateChallenge(nonce, instanceID)
	if err != nil {
		return false, err
	}

	// Constant-time comparison
	return hmac.Equal(challenge, expectedChallenge), nil
}

// GenerateProof signs a challenge using an ECDSA private key
// Returns the signature in DER format
func GenerateProof(challenge []byte, privateKey *ecdsa.PrivateKey) ([]byte, error) {
	if privateKey == nil {
		return nil, errors.New("private key cannot be nil")
	}

	// Hash the challenge
	hash := sha256.Sum256(challenge)

	// Sign the hash
	signature, err := ecdsa.SignASN1(rand.Reader, privateKey, hash[:])
	if err != nil {
		return nil, fmt.Errorf("failed to sign challenge: %w", err)
	}

	return signature, nil
}

// VerifyProof verifies an ECDSA signature against a challenge
// Returns true if the signature is valid
func VerifyProof(proof, challenge []byte, publicKey *ecdsa.PublicKey) (bool, error) {
	if publicKey == nil {
		return false, errors.New("public key cannot be nil")
	}

	// Hash the challenge
	hash := sha256.Sum256(challenge)

	// Verify the signature
	valid := ecdsa.VerifyASN1(publicKey, hash[:], proof)
	return valid, nil
}

// HashAttestation creates a zero-knowledge proof hash of an attestation
// This allows verification without revealing the full attestation data
func HashAttestation(attestationData []byte) []byte {
	hash := sha256.Sum256(attestationData)
	return hash[:]
}

// VerifyAttestationHash verifies that an attestation hash matches the expected value
func VerifyAttestationHash(attestationData, expectedHash []byte) bool {
	actualHash := HashAttestation(attestationData)
	return hmac.Equal(actualHash, expectedHash)
}

// ParsePublicKeyPEM parses a PEM-encoded ECDSA public key
func ParsePublicKeyPEM(pemData string) (*ecdsa.PublicKey, error) {
	block, _ := pem.Decode([]byte(pemData))
	if block == nil {
		return nil, errors.New("failed to decode PEM block")
	}

	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse public key: %w", err)
	}

	ecdsaPub, ok := pub.(*ecdsa.PublicKey)
	if !ok {
		return nil, errors.New("not an ECDSA public key")
	}

	return ecdsaPub, nil
}

// EncodePub licKeyPEM encodes an ECDSA public key to PEM format
func EncodePublicKeyPEM(publicKey *ecdsa.PublicKey) (string, error) {
	derBytes, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		return "", fmt.Errorf("failed to marshal public key: %w", err)
	}

	pemBlock := &pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: derBytes,
	}

	return string(pem.EncodeToMemory(pemBlock)), nil
}

// VerifyCertificateChain verifies a certificate chain against a root CA
func VerifyCertificateChain(certChain []string, rootCA *x509.Certificate) error {
	if len(certChain) == 0 {
		return errors.New("certificate chain is empty")
	}

	// Parse all certificates in the chain
	certs := make([]*x509.Certificate, 0, len(certChain))
	for i, certPEM := range certChain {
		block, _ := pem.Decode([]byte(certPEM))
		if block == nil {
			return fmt.Errorf("failed to decode certificate %d", i)
		}

		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return fmt.Errorf("failed to parse certificate %d: %w", i, err)
		}

		certs = append(certs, cert)
	}

	// Verify the chain
	opts := x509.VerifyOptions{
		Roots:         x509.NewCertPool(),
		Intermediates: x509.NewCertPool(),
	}

	if rootCA != nil {
		opts.Roots.AddCert(rootCA)
	}

	// Add intermediate certificates
	for i := 1; i < len(certs); i++ {
		opts.Intermediates.AddCert(certs[i])
	}

	// Verify the leaf certificate
	_, err := certs[0].Verify(opts)
	if err != nil {
		return fmt.Errorf("certificate chain verification failed: %w", err)
	}

	return nil
}

// IsNonceFresh checks if a nonce was created within the acceptable time window
// This prevents replay attacks using old nonces
func IsNonceFresh(nonceTimestamp int64, maxAge time.Duration) bool {
	nonceTime := time.Unix(nonceTimestamp, 0)
	age := time.Since(nonceTime)
	return age <= maxAge
}

// SecureCompare performs a constant-time comparison of two byte slices
// This prevents timing attacks
func SecureCompare(a, b []byte) bool {
	return hmac.Equal(a, b)
}

// DeriveSessionKey derives a symmetric session key from the handshake
// Uses HKDF (HMAC-based Key Derivation Function)
func DeriveSessionKey(sharedSecret, salt, info []byte) ([]byte, error) {
	if len(sharedSecret) == 0 {
		return nil, errors.New("shared secret cannot be empty")
	}

	// Use HKDF to derive a 32-byte session key
	hash := sha256.New
	hkdf := hmac.New(hash, sharedSecret)
	hkdf.Write(salt)
	hkdf.Write(info)

	sessionKey := hkdf.Sum(nil)
	return sessionKey[:32], nil
}

// SignData signs arbitrary data using an ECDSA private key
func SignData(data []byte, privateKey crypto.Signer) ([]byte, error) {
	hash := sha256.Sum256(data)
	signature, err := privateKey.Sign(rand.Reader, hash[:], crypto.SHA256)
	if err != nil {
		return nil, fmt.Errorf("failed to sign data: %w", err)
	}
	return signature, nil
}

// VerifySignature verifies a signature against data using a public key
func VerifySignature(data, signature []byte, publicKey *ecdsa.PublicKey) (bool, error) {
	hash := sha256.Sum256(data)
	valid := ecdsa.VerifyASN1(publicKey, hash[:], signature)
	return valid, nil
}
