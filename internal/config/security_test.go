package config

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestWireGuardConfig_MarshalJSON(t *testing.T) {
	cfg := WireGuardConfig{
		Name:       "wg0",
		PrivateKey: "private-key-12345",
		Peers: []WireGuardPeerConfig{
			{
				Name:         "peer1",
				PublicKey:    "public-key-abcde",
				PresharedKey: "preshared-key-67890",
			},
		},
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("Failed to marshal config: %v", err)
	}

	jsonStr := string(data)

	// Check if PrivateKey is hidden
	if strings.Contains(jsonStr, "private-key-12345") {
		t.Errorf("Security vulnerability: PrivateKey is visible in JSON output: %s", jsonStr)
	}

	if !strings.Contains(jsonStr, "(hidden)") {
		t.Errorf("Expected PrivateKey to be masked with '(hidden)', got: %s", jsonStr)
	}

	// Check if PresharedKey is hidden
	if strings.Contains(jsonStr, "preshared-key-67890") {
		t.Errorf("Security vulnerability: PresharedKey is visible in JSON output: %s", jsonStr)
	}
}
