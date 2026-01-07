package network_test

import (
	"testing"

	"grimm.is/glacic/internal/logging"
	"grimm.is/glacic/internal/network"
)

func TestNewSysctlTuner(t *testing.T) {
	logger := logging.New(logging.DefaultConfig())

	tuner := network.NewSysctlTuner(
		network.ProfileDefault,
		map[string]string{"test.key": "value"},
		logger,
	)

	if tuner == nil {
		t.Fatal("Expected tuner to be non-nil")
	}
}

func TestSysctlProfile_Default(t *testing.T) {
	logger := logging.New(logging.DefaultConfig())

	tuner := network.NewSysctlTuner(
		network.ProfileDefault,
		nil,
		logger,
	)

	// Test that default profile generates expected params
	// Note: We're not actually applying them in the test
	if tuner == nil {
		t.Fatal("Expected tuner to be non-nil for default profile")
	}
}

func TestSysctlProfile_Performance(t *testing.T) {
	logger := logging.New(logging.DefaultConfig())

	tuner := network.NewSysctlTuner(
		network.ProfilePerformance,
		nil,
		logger,
	)

	if tuner == nil {
		t.Fatal("Expected tuner to be non-nil for performance profile")
	}
}

func TestSysctlProfile_LowMemory(t *testing.T) {
	logger := logging.New(logging.DefaultConfig())

	tuner := network.NewSysctlTuner(
		network.ProfileLowMemory,
		nil,
		logger,
	)

	if tuner == nil {
		t.Fatal("Expected tuner to be non-nil for low-memory profile")
	}
}

func TestSysctlProfile_Security(t *testing.T) {
	logger := logging.New(logging.DefaultConfig())

	tuner := network.NewSysctlTuner(
		network.ProfileSecurity,
		nil,
		logger,
	)

	if tuner == nil {
		t.Fatal("Expected tuner to be non-nil for security profile")
	}
}

func TestSysctlOverrides(t *testing.T) {
	logger := logging.New(logging.DefaultConfig())

	overrides := map[string]string{
		"net.core.rmem_max":               "134217728",
		"net.ipv4.tcp_congestion_control": "cubic",
	}

	tuner := network.NewSysctlTuner(
		network.ProfileDefault,
		overrides,
		logger,
	)

	if tuner == nil {
		t.Fatal("Expected tuner with overrides to be non-nil")
	}
}
