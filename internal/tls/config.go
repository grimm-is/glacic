package tls

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"grimm.is/glacic/internal/clock"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// CertificateMode defines the certificate source
type CertificateMode string

const (
	ModeSelfSigned CertificateMode = "self-signed"
	ModeACME       CertificateMode = "acme"
	ModeTailscale  CertificateMode = "tailscale"
	ModeManual     CertificateMode = "manual"
)

// CertificateManager manages multiple certificates for different interfaces
type CertificateManager struct {
	certificates map[string]*tls.Certificate // interface name -> certificate
	defaultCert  *tls.Certificate            // fallback certificate
	mu           sync.RWMutex
}

// NewCertificateManager creates a new certificate manager
func NewCertificateManager() *CertificateManager {
	return &CertificateManager{
		certificates: make(map[string]*tls.Certificate),
	}
}

// SetCertificate sets a certificate for a specific interface
func (cm *CertificateManager) SetCertificate(interfaceName string, cert *tls.Certificate) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.certificates[interfaceName] = cert
}

// SetDefaultCertificate sets the fallback certificate
func (cm *CertificateManager) SetDefaultCertificate(cert *tls.Certificate) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.defaultCert = cert
}

// GetCertificate returns the appropriate certificate for a client connection
func (cm *CertificateManager) GetCertificate(clientHello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	// Future enhancement: Interface detection based on local address for multi-cert scenarios.
	// Current design uses a single default certificate which is sufficient for most deployments.
	if cm.defaultCert != nil {
		return cm.defaultCert, nil
	}

	return nil, fmt.Errorf("no certificate available")
}

// GenerateSelfSigned generates a self-signed certificate
func GenerateSelfSigned(certFile, keyFile string, validDays int) error {
	// Generate private key
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("failed to generate private key: %w", err)
	}

	// Create certificate template
	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return fmt.Errorf("failed to generate serial number: %w", err)
	}

	notBefore := clock.Now()
	notAfter := notBefore.Add(time.Duration(validDays) * 24 * time.Hour)

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"Glacic Firewall"},
			CommonName:   "Glacic Firewall",
		},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"localhost", "firewall.local"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
	}

	// Create self-signed certificate
	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return fmt.Errorf("failed to create certificate: %w", err)
	}

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(certFile), 0755); err != nil {
		return fmt.Errorf("failed to create certificate directory: %w", err)
	}

	// Write certificate to file
	certOut, err := os.Create(certFile)
	if err != nil {
		return fmt.Errorf("failed to create certificate file: %w", err)
	}
	defer certOut.Close()

	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes}); err != nil {
		return fmt.Errorf("failed to write certificate: %w", err)
	}

	// Write private key to file
	keyOut, err := os.OpenFile(keyFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("failed to create key file: %w", err)
	}
	defer keyOut.Close()

	privBytes, err := x509.MarshalECPrivateKey(privateKey)
	if err != nil {
		return fmt.Errorf("failed to marshal private key: %w", err)
	}

	if err := pem.Encode(keyOut, &pem.Block{Type: "EC PRIVATE KEY", Bytes: privBytes}); err != nil {
		return fmt.Errorf("failed to write private key: %w", err)
	}

	return nil
}

// LoadCertificate loads a certificate from files
func LoadCertificate(certFile, keyFile string) (*tls.Certificate, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load certificate: %w", err)
	}
	return &cert, nil
}

// EnsureCertificate ensures a certificate exists, generating self-signed if needed
func EnsureCertificate(certFile, keyFile string, validDays int) (*tls.Certificate, error) {
	// Check if certificate files exist
	if _, err := os.Stat(certFile); os.IsNotExist(err) {
		// Generate self-signed certificate
		if err := GenerateSelfSigned(certFile, keyFile, validDays); err != nil {
			return nil, fmt.Errorf("failed to generate self-signed certificate: %w", err)
		}
	}

	// Load the certificate
	return LoadCertificate(certFile, keyFile)
}
