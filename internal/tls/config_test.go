package tls

import (
	"crypto/tls"
	"os"
	"path/filepath"
	"testing"
)

func TestNewCertificateManager(t *testing.T) {
	cm := NewCertificateManager()
	if cm == nil {
		t.Fatal("NewCertificateManager returned nil")
	}
	if cm.certificates == nil {
		t.Error("certificates map not initialized")
	}
}

func TestCertificateManager_SetAndGetCertificate(t *testing.T) {
	cm := NewCertificateManager()

	// Create a temporary certificate
	tmpDir := t.TempDir()
	certFile := filepath.Join(tmpDir, "test.crt")
	keyFile := filepath.Join(tmpDir, "test.key")

	if err := GenerateSelfSigned(certFile, keyFile, 1); err != nil {
		t.Fatalf("Failed to generate test cert: %v", err)
	}

	cert, err := LoadCertificate(certFile, keyFile)
	if err != nil {
		t.Fatalf("Failed to load test cert: %v", err)
	}

	// Set as default
	cm.SetDefaultCertificate(cert)

	// Get should return default
	clientHello := &tls.ClientHelloInfo{}
	retrieved, err := cm.GetCertificate(clientHello)
	if err != nil {
		t.Errorf("GetCertificate failed: %v", err)
	}
	if retrieved != cert {
		t.Error("Retrieved certificate doesn't match")
	}
}

func TestCertificateManager_NoCertificate(t *testing.T) {
	cm := NewCertificateManager()

	// No certificate set - should error
	clientHello := &tls.ClientHelloInfo{}
	_, err := cm.GetCertificate(clientHello)
	if err == nil {
		t.Error("Expected error when no certificate is set")
	}
}

func TestCertificateManager_SetInterfaceCertificate(t *testing.T) {
	cm := NewCertificateManager()

	tmpDir := t.TempDir()
	certFile := filepath.Join(tmpDir, "iface.crt")
	keyFile := filepath.Join(tmpDir, "iface.key")

	if err := GenerateSelfSigned(certFile, keyFile, 1); err != nil {
		t.Fatalf("Failed to generate test cert: %v", err)
	}

	cert, _ := LoadCertificate(certFile, keyFile)

	// Set for specific interface
	cm.SetCertificate("eth0", cert)

	// Verify it was set (internal check)
	cm.mu.RLock()
	if cm.certificates["eth0"] != cert {
		t.Error("Certificate not set for interface")
	}
	cm.mu.RUnlock()
}

func TestGenerateSelfSigned(t *testing.T) {
	tmpDir := t.TempDir()
	certFile := filepath.Join(tmpDir, "gen.crt")
	keyFile := filepath.Join(tmpDir, "gen.key")

	if err := GenerateSelfSigned(certFile, keyFile, 365); err != nil {
		t.Fatalf("GenerateSelfSigned failed: %v", err)
	}

	// Verify files exist
	if _, err := os.Stat(certFile); os.IsNotExist(err) {
		t.Error("Certificate file not created")
	}
	if _, err := os.Stat(keyFile); os.IsNotExist(err) {
		t.Error("Key file not created")
	}

	// Verify key file permissions (should be 0600)
	info, err := os.Stat(keyFile)
	if err != nil {
		t.Fatalf("Failed to stat key file: %v", err)
	}
	perm := info.Mode().Perm()
	if perm != 0600 {
		t.Errorf("Key file permissions should be 0600, got %o", perm)
	}
}

func TestLoadCertificate(t *testing.T) {
	tmpDir := t.TempDir()
	certFile := filepath.Join(tmpDir, "load.crt")
	keyFile := filepath.Join(tmpDir, "load.key")

	GenerateSelfSigned(certFile, keyFile, 1)

	cert, err := LoadCertificate(certFile, keyFile)
	if err != nil {
		t.Errorf("LoadCertificate failed: %v", err)
	}
	if cert == nil {
		t.Error("LoadCertificate returned nil")
	}
}

func TestLoadCertificate_InvalidPath(t *testing.T) {
	_, err := LoadCertificate("/nonexistent/cert.pem", "/nonexistent/key.pem")
	if err == nil {
		t.Error("Expected error for invalid paths")
	}
}

func TestEnsureCertificate_GeneratesIfMissing(t *testing.T) {
	tmpDir := t.TempDir()
	certFile := filepath.Join(tmpDir, "ensure.crt")
	keyFile := filepath.Join(tmpDir, "ensure.key")

	// Files don't exist yet
	cert, err := EnsureCertificate(certFile, keyFile, 365)
	if err != nil {
		t.Errorf("EnsureCertificate failed: %v", err)
	}
	if cert == nil {
		t.Error("EnsureCertificate returned nil certificate")
	}

	// Verify files were created
	if _, err := os.Stat(certFile); os.IsNotExist(err) {
		t.Error("EnsureCertificate did not create certificate file")
	}
}

func TestEnsureCertificate_LoadsExisting(t *testing.T) {
	tmpDir := t.TempDir()
	certFile := filepath.Join(tmpDir, "existing.crt")
	keyFile := filepath.Join(tmpDir, "existing.key")

	// Create certificate first
	GenerateSelfSigned(certFile, keyFile, 1)

	// EnsureCertificate should load existing
	cert, err := EnsureCertificate(certFile, keyFile, 365)
	if err != nil {
		t.Errorf("EnsureCertificate failed: %v", err)
	}
	if cert == nil {
		t.Error("EnsureCertificate returned nil for existing certificate")
	}
}
