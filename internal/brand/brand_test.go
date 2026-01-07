package brand

import (
	"os"
	"testing"
)

func TestGet(t *testing.T) {
	b := Get()
	if b.Name == "" {
		t.Error("Brand name should not be empty")
	}
	// Version is a global variable, not in the struct
	if Version == "" {
		t.Error("Global Version should be initialized (to dev default)")
	}
	if Name == "" {
		t.Error("Global Name should be initialized")
	}
}

func TestUserAgent(t *testing.T) {
	ua := UserAgent("1.0.0")
	if ua == "" {
		t.Error("UserAgent should not be empty")
	}

	uaDefault := UserAgent("")
	if uaDefault == "" {
		t.Error("UserAgent default should not be empty")
	}
}

func TestGetDirectories(t *testing.T) {
	// Reset envs
	cleanEnv := func() {
		os.Unsetenv(ConfigEnvPrefix + "_PREFIX")
		os.Unsetenv(ConfigEnvPrefix + "_CONFIG_DIR")
		os.Unsetenv(ConfigEnvPrefix + "_STATE_DIR")
		os.Unsetenv(ConfigEnvPrefix + "_LOG_DIR")
		os.Unsetenv(ConfigEnvPrefix + "_RUN_DIR")
	}
	cleanEnv()
	defer cleanEnv()

	// Test Defaults
	if GetConfigDir() != DefaultConfigDir {
		t.Errorf("Expected default config dir %s, got %s", DefaultConfigDir, GetConfigDir())
	}
	if GetStateDir() != DefaultStateDir {
		t.Errorf("Expected default state dir %s, got %s", DefaultStateDir, GetStateDir())
	}
	if GetLogDir() != DefaultLogDir {
		t.Errorf("Expected default log dir %s, got %s", DefaultLogDir, GetLogDir())
	}
	if GetRunDir() != DefaultRunDir {
		t.Errorf("Expected default run dir %s, got %s", DefaultRunDir, GetRunDir())
	}

	// Test Prefix
	os.Setenv(ConfigEnvPrefix+"_PREFIX", "/tmp/glacic")
	if GetConfigDir() != "/tmp/glacic/config" {
		t.Errorf("Expected prefix config dir, got %s", GetConfigDir())
	}

	// Test Direct Override (Highest Priority)
	os.Setenv(ConfigEnvPrefix+"_CONFIG_DIR", "/custom/config")
	if GetConfigDir() != "/custom/config" {
		t.Errorf("Expected custom config dir, got %s", GetConfigDir())
	}
}

func TestGetSocketPath(t *testing.T) {
	path := GetSocketPath()
	if path == "" {
		t.Error("Socket path should not be empty")
	}
}
