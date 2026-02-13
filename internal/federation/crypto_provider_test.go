package federation

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// CRYPTO PROVIDER UNIT TESTS
// ============================================================================

func TestEd25519Provider_SignVerify(t *testing.T) {
	provider, err := NewCryptoProvider(AlgorithmEd25519)
	require.NoError(t, err)
	assert.Equal(t, AlgorithmEd25519, provider.Algorithm())

	data := []byte("federation handshake challenge data")

	// Sign
	sig, err := provider.Sign(data)
	require.NoError(t, err)
	assert.Len(t, sig, ed25519.SignatureSize, "Ed25519 signature must be 64 bytes")

	// Verify with correct data
	valid, err := provider.Verify(provider.PublicKeyBytes(), data, sig)
	require.NoError(t, err)
	assert.True(t, valid, "signature should verify with correct data")

	// Verify with wrong data
	valid, err = provider.Verify(provider.PublicKeyBytes(), []byte("tampered data"), sig)
	require.NoError(t, err)
	assert.False(t, valid, "signature should NOT verify with tampered data")
}

func TestECDSAProvider_SignVerify(t *testing.T) {
	provider, err := NewCryptoProvider(AlgorithmECDSA)
	require.NoError(t, err)
	assert.Equal(t, AlgorithmECDSA, provider.Algorithm())

	data := []byte("federation handshake challenge data")

	// Sign
	sig, err := provider.Sign(data)
	require.NoError(t, err)
	assert.NotEmpty(t, sig)

	// Verify with correct data
	valid, err := provider.Verify(provider.PublicKeyBytes(), data, sig)
	require.NoError(t, err)
	assert.True(t, valid, "signature should verify with correct data")

	// Verify with wrong data
	valid, err = provider.Verify(provider.PublicKeyBytes(), []byte("tampered data"), sig)
	require.NoError(t, err)
	assert.False(t, valid, "signature should NOT verify with tampered data")
}

func TestCryptoProvider_CrossAlgorithmRejection(t *testing.T) {
	// Generate keys from both algorithms
	ed25519Prov, err := NewCryptoProvider(AlgorithmEd25519)
	require.NoError(t, err)

	ecdsaProv, err := NewCryptoProvider(AlgorithmECDSA)
	require.NoError(t, err)

	data := []byte("cross-algorithm test data")

	// Sign with Ed25519
	edSig, err := ed25519Prov.Sign(data)
	require.NoError(t, err)

	// Try to verify Ed25519 sig with ECDSA public key → should fail
	valid, err := ecdsaProv.Verify(ecdsaProv.PublicKeyBytes(), data, edSig)
	// Either error or false is acceptable — the signature must not validate
	if err == nil {
		assert.False(t, valid, "Ed25519 signature must NOT verify with ECDSA provider")
	}

	// Sign with ECDSA
	ecSig, err := ecdsaProv.Sign(data)
	require.NoError(t, err)

	// Try to verify ECDSA sig with Ed25519 public key → should fail
	valid, err = ed25519Prov.Verify(ed25519Prov.PublicKeyBytes(), data, ecSig)
	// Ed25519 Verify just returns false for wrong sigs, no error expected
	if err == nil {
		assert.False(t, valid, "ECDSA signature must NOT verify with Ed25519 provider")
	}
}

func TestNewCryptoProvider_InvalidAlgorithm(t *testing.T) {
	_, err := NewCryptoProvider("rsa-4096")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported crypto algorithm")
}

func TestCryptoProvider_PublicKeyPEM(t *testing.T) {
	tests := []struct {
		name string
		alg  CryptoAlgorithm
	}{
		{"Ed25519", AlgorithmEd25519},
		{"ECDSA-P256", AlgorithmECDSA},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider, err := NewCryptoProvider(tt.alg)
			require.NoError(t, err)

			pem, err := provider.EncodePublicKeyPEM()
			require.NoError(t, err)
			assert.Contains(t, pem, "BEGIN PUBLIC KEY")
			assert.Contains(t, pem, "END PUBLIC KEY")
		})
	}
}

func TestEd25519ProviderFromKey(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	provider := NewEd25519ProviderFromKey(priv)
	assert.Equal(t, AlgorithmEd25519, provider.Algorithm())

	data := []byte("test data")
	sig, err := provider.Sign(data)
	require.NoError(t, err)

	valid, err := provider.Verify(provider.PublicKeyBytes(), data, sig)
	require.NoError(t, err)
	assert.True(t, valid)
}

func TestECDSAProviderFromKey(t *testing.T) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	provider := NewECDSAProviderFromKey(priv)
	assert.Equal(t, AlgorithmECDSA, provider.Algorithm())

	data := []byte("test data")
	sig, err := provider.Sign(data)
	require.NoError(t, err)

	valid, err := provider.Verify(provider.PublicKeyBytes(), data, sig)
	require.NoError(t, err)
	assert.True(t, valid)
}

func TestResolveCryptoAlgorithm(t *testing.T) {
	tenantConfig := map[string]string{
		"tenant-regulated": "ecdsa-p256",
		"tenant-fast":      "ed25519",
		"tenant-invalid":   "rsa-4096",
	}

	// Tenant with explicit ECDSA config
	alg := ResolveCryptoAlgorithm("tenant-regulated", tenantConfig, DefaultCryptoAlgorithm)
	assert.Equal(t, AlgorithmECDSA, alg)

	// Tenant with explicit Ed25519 config
	alg = ResolveCryptoAlgorithm("tenant-fast", tenantConfig, DefaultCryptoAlgorithm)
	assert.Equal(t, AlgorithmEd25519, alg)

	// Tenant with invalid config → falls back to default
	alg = ResolveCryptoAlgorithm("tenant-invalid", tenantConfig, DefaultCryptoAlgorithm)
	assert.Equal(t, DefaultCryptoAlgorithm, alg)

	// Unknown tenant → falls back to default
	alg = ResolveCryptoAlgorithm("unknown-tenant", tenantConfig, DefaultCryptoAlgorithm)
	assert.Equal(t, DefaultCryptoAlgorithm, alg)
}

// ============================================================================
// BENCHMARKS — Ed25519 vs ECDSA P-256
// ============================================================================

func BenchmarkEd25519_Sign(b *testing.B) {
	provider, _ := NewCryptoProvider(AlgorithmEd25519)
	data := []byte("benchmark signing payload for federation handshake")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		provider.Sign(data)
	}
}

func BenchmarkECDSA_Sign(b *testing.B) {
	provider, _ := NewCryptoProvider(AlgorithmECDSA)
	data := []byte("benchmark signing payload for federation handshake")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		provider.Sign(data)
	}
}

func BenchmarkEd25519_Verify(b *testing.B) {
	provider, _ := NewCryptoProvider(AlgorithmEd25519)
	data := []byte("benchmark verification payload for federation handshake")
	sig, _ := provider.Sign(data)
	pubKey := provider.PublicKeyBytes()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		provider.Verify(pubKey, data, sig)
	}
}

func BenchmarkECDSA_Verify(b *testing.B) {
	provider, _ := NewCryptoProvider(AlgorithmECDSA)
	data := []byte("benchmark verification payload for federation handshake")
	sig, _ := provider.Sign(data)
	pubKey := provider.PublicKeyBytes()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		provider.Verify(pubKey, data, sig)
	}
}
