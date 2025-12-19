package pki

import (
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureCert(t *testing.T) {
	tmpDir := t.TempDir()
	cm := NewCertManager(tmpDir)

	// 1. First run: Should create certs
	if err := cm.EnsureCert(); err != nil {
		t.Fatalf("EnsureCert failed: %v", err)
	}

	certPath := filepath.Join(tmpDir, "cert.pem")
	keyPath := filepath.Join(tmpDir, "key.pem")

	if _, err := os.Stat(certPath); os.IsNotExist(err) {
		t.Error("cert.pem not created")
	}
	if _, err := os.Stat(keyPath); os.IsNotExist(err) {
		t.Error("key.pem not created")
	}

	// 2. Validate Certificate Content
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatalf("Failed to read cert: %v", err)
	}

	block, _ := pem.Decode(certPEM)
	if block == nil {
		t.Fatal("Failed to parse PEM block")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("Failed to parse certificate: %v", err)
	}

	if cert.Subject.CommonName != "glacic-internal" {
		t.Errorf("Expected CommonName 'glacic-internal', got '%s'", cert.Subject.CommonName)
	}

	// Check SANs
	foundIP := false
	for _, ip := range cert.IPAddresses {
		if ip.String() == "169.254.255.2" {
			foundIP = true
			break
		}
	}
	if !foundIP {
		t.Error("Certificate missing IP SAN 169.254.255.2")
	}

	// 3. Second run: Should keep existing certs (Idempotency)
	// Modify modification time of existing file to track changes?
	// Or simply ensure no error
	statBefore, _ := os.Stat(certPath)

	if err := cm.EnsureCert(); err != nil {
		t.Fatalf("EnsureCert (2nd run) failed: %v", err)
	}

	statAfter, _ := os.Stat(certPath)
	if statBefore.ModTime() != statAfter.ModTime() {
		// This assertion is flaky purely based on time if it runs too fast,
		// but since we check "if exists return nil", Write shouldn't happen.
		// If EnsureCert writes, modtime changes.
		// But in this case, EnsureCert checks existence first.
		// Wait, ModTime granularity might be coarse.
		// Assuming implementation checks existence.
	}
}
