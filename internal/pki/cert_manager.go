package pki

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"

	"context"
	"log"

	"grimm.is/glacic/internal/brand"
	"grimm.is/glacic/internal/clock"
)

type CertManager struct {
	CertDir string
}

func NewCertManager(certDir string) *CertManager {
	return &CertManager{CertDir: certDir}
}

func (m *CertManager) EnsureCert() error {
	certPath := filepath.Join(m.CertDir, "cert.pem")
	keyPath := filepath.Join(m.CertDir, "key.pem")

	// Check if already exist
	if _, err := os.Stat(certPath); err == nil {
		if _, err := os.Stat(keyPath); err == nil {
			// VALIDATION: Check for expiry (The "365-Day Time Bomb" Fix)
			// If cert is invalid or expiring soon, we must regenerate it.
			if valid, err := m.checkCertValidity(certPath); err == nil && valid {
				return nil // Valid and healthy
			}
			// If invalid/expiring, we fall through to regeneration (log it?)
			// Ideally we should log here but we don't have a logger in this struct.
			// We'll proceed to regenerate.
		}
	}

	// Create directory
	if err := os.MkdirAll(m.CertDir, 0700); err != nil {
		return fmt.Errorf("failed to create cert dir: %w", err)
	}

	return m.generateSelfSigned(certPath, keyPath)
}

func (m *CertManager) generateSelfSigned(certPath, keyPath string) error {
	// Generate Key
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return fmt.Errorf("failed to generate private key: %w", err)
	}

	// Template
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: brand.LowerName + "-internal",
		},
		NotBefore: clock.Now(),
		NotAfter:  clock.Now().Add(365 * 24 * time.Hour),

		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	// Add IP SANs (Manual + Automatic)
	template.IPAddresses = append(template.IPAddresses, net.ParseIP("127.0.0.1"), net.ParseIP("169.254.255.2"))

	// Auto-discover all host IPs to ensure we cover the access IP
	if ifaces, err := net.Interfaces(); err == nil {
		for _, i := range ifaces {
			addrs, err := i.Addrs()
			if err != nil {
				continue
			}
			for _, addr := range addrs {
				var ip net.IP
				switch v := addr.(type) {
				case *net.IPNet:
					ip = v.IP
				case *net.IPAddr:
					ip = v.IP
				}
				if ip != nil {
					template.IPAddresses = append(template.IPAddresses, ip)
				}
			}
		}
	}

	// Also add hostname (brand name)
	template.DNSNames = append(template.DNSNames, brand.LowerName, "localhost")

	// Create Cert
	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return fmt.Errorf("failed to create certificate: %w", err)
	}

	// Write Cert
	certOut, err := os.Create(certPath)
	if err != nil {
		return fmt.Errorf("failed to open .pem for writing: %w", err)
	}
	defer certOut.Close()
	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes}); err != nil {
		return fmt.Errorf("failed to write data to cert.pem: %w", err)
	}

	// Write Key
	keyOut, err := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("failed to open key.pem for writing: %w", err)
	}
	defer keyOut.Close()

	privBytes := x509.MarshalPKCS1PrivateKey(priv)
	if err := pem.Encode(keyOut, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: privBytes}); err != nil {
		return fmt.Errorf("failed to write data to key.pem: %w", err)
	}

	return nil
}

// checkCertValidity checks if the certificate is valid and not expiring soon (30 days)
func (m *CertManager) checkCertValidity(certPath string) (bool, error) {
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return false, err
	}

	block, _ := pem.Decode(certPEM)
	if block == nil {
		return false, fmt.Errorf("failed to decode PEM")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return false, err
	}

	// Check if expired or expiring within 30 days
	if time.Until(cert.NotAfter) < 30*24*time.Hour {
		return false, nil // Expiring soon
	}

	// Validate SANs: Ensure current host IPs are in the cert
	// This forces regeneration if the machine IP changes or if we improved the generation logic
	if ifaces, err := net.Interfaces(); err == nil {
		for _, i := range ifaces {
			addrs, err := i.Addrs()
			if err != nil {
				continue
			}
			for _, addr := range addrs {
				var ip net.IP
				switch v := addr.(type) {
				case *net.IPNet:
					ip = v.IP
				case *net.IPAddr:
					ip = v.IP
				}

				// Skip loopback and ipv6 link-local for strictness check to avoid constant regen
				// But we definitely want to check the main LAN IP (e.g. 172.x)
				if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
					continue
				}

				found := false
				for _, certIP := range cert.IPAddresses {
					if certIP.Equal(ip) {
						found = true
						break
					}
				}
				if !found {
					// Missing an IP, should regenerate
					return false, nil
				}
			}
		}
	}

	return true, nil
}

// StartAutoRenew starts a background goroutine to check for certificate expiry
func (m *CertManager) StartAutoRenew(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := m.EnsureCert(); err != nil {
					log.Printf("[CertManager] Failed to renew certificate: %v", err)
				}
			}
		}
	}()
}
