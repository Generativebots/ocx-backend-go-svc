/*
TLS Certificate Generator
Generates on-the-fly TLS certificates for MITM interception
*/

package sop

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// CertGenerator generates TLS certificates on-the-fly
type CertGenerator struct {
	cacheDir string
	rootCA   *tls.Certificate
	cache    map[string]*tls.Certificate
	mu       sync.RWMutex
}

// NewCertGenerator creates a new certificate generator
func NewCertGenerator(cacheDir string) (*CertGenerator, error) {
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return nil, err
	}

	cg := &CertGenerator{
		cacheDir: cacheDir,
		cache:    make(map[string]*tls.Certificate),
	}

	// Load or generate root CA
	if err := cg.loadOrGenerateRootCA(); err != nil {
		return nil, err
	}

	return cg, nil
}

// loadOrGenerateRootCA loads existing CA or generates new one
func (cg *CertGenerator) loadOrGenerateRootCA() error {
	caPath := filepath.Join(cg.cacheDir, "ca.crt")
	keyPath := filepath.Join(cg.cacheDir, "ca.key")

	// Try to load existing CA
	if _, err := os.Stat(caPath); err == nil {
		cert, err := tls.LoadX509KeyPair(caPath, keyPath)
		if err == nil {
			cg.rootCA = &cert
			return nil
		}
	}

	// Generate new CA
	return cg.generateRootCA(caPath, keyPath)
}

// generateRootCA generates a new root CA certificate
func (cg *CertGenerator) generateRootCA(certPath, keyPath string) error {
	// Generate private key
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}

	// Create certificate template
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"OCX Internal CA"},
			CommonName:   "OCX Root CA",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour), // 10 years
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	// Self-sign the certificate
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return err
	}

	// Save certificate
	certOut, err := os.Create(certPath)
	if err != nil {
		return err
	}
	defer certOut.Close()

	pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	// Save private key
	keyOut, err := os.Create(keyPath)
	if err != nil {
		return err
	}
	defer keyOut.Close()

	pem.Encode(keyOut, &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(priv),
	})

	// Load into memory
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return err
	}

	cg.rootCA = &cert
	return nil
}

// GetCertificate generates or retrieves a certificate for a domain
func (cg *CertGenerator) GetCertificate(host string) (*tls.Certificate, error) {
	// Check cache
	cg.mu.RLock()
	if cert, ok := cg.cache[host]; ok {
		cg.mu.RUnlock()
		return cert, nil
	}
	cg.mu.RUnlock()

	// Generate new certificate
	cg.mu.Lock()
	defer cg.mu.Unlock()

	// Double-check (another goroutine might have generated it)
	if cert, ok := cg.cache[host]; ok {
		return cert, nil
	}

	cert, err := cg.generateCertificate(host)
	if err != nil {
		return nil, err
	}

	cg.cache[host] = cert
	return cert, nil
}

// generateCertificate generates a certificate for a specific domain
func (cg *CertGenerator) generateCertificate(host string) (*tls.Certificate, error) {
	// Parse root CA
	caCert, err := x509.ParseCertificate(cg.rootCA.Certificate[0])
	if err != nil {
		return nil, err
	}

	// Generate private key
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}

	// Create certificate template
	serialNumber, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"OCX Proxy"},
			CommonName:   host,
		},
		NotBefore:   time.Now(),
		NotAfter:    time.Now().Add(365 * 24 * time.Hour), // 1 year
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:    []string{host},
	}

	// Sign with CA
	certDER, err := x509.CreateCertificate(rand.Reader, &template, caCert, &priv.PublicKey, cg.rootCA.PrivateKey)
	if err != nil {
		return nil, err
	}

	// Create TLS certificate
	cert := &tls.Certificate{
		Certificate: [][]byte{certDER, cg.rootCA.Certificate[0]},
		PrivateKey:  priv,
	}

	return cert, nil
}

// GetRootCAPEM returns the root CA certificate in PEM format
func (cg *CertGenerator) GetRootCAPEM() ([]byte, error) {
	certPath := filepath.Join(cg.cacheDir, "ca.crt")
	return os.ReadFile(certPath)
}

// TLSConfig returns a TLS config that generates certificates on-the-fly
func (cg *CertGenerator) TLSConfig() *tls.Config {
	return &tls.Config{
		GetCertificate: func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
			return cg.GetCertificate(hello.ServerName)
		},
	}
}
