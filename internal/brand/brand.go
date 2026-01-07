// Package brand provides centralized branding constants for the firewall.
// This makes it easy to fork or white-label the product by changing brand.json.
//
// The brand identity is loaded from brand.json at compile time via go:embed.
// This allows other tools (scripts, docs generators) to read the same file.
package brand

import (
	_ "embed"
	"encoding/json"
	"os"
	"path/filepath"
)

//go:embed brand.json
var brandJSON []byte

// Brand holds all branding information
type Brand struct {
	Name             string `json:"name"`
	LowerName        string `json:"lowerName"`
	Vendor           string `json:"vendor"`
	Website          string `json:"website"`
	Repository       string `json:"repository"`
	Description      string `json:"description"`
	Tagline          string `json:"tagline"`
	ConfigEnvPrefix  string `json:"configEnvPrefix"`
	DefaultConfigDir string `json:"defaultConfigDir"`
	DefaultStateDir  string `json:"defaultStateDir"`
	DefaultLogDir    string `json:"defaultLogDir"`
	DefaultRunDir    string `json:"defaultRunDir"`
	SocketName       string `json:"socketName"`
	BinaryName       string `json:"binaryName"`
	ServiceName      string `json:"serviceName"`
	ConfigFileName   string `json:"configFileName"`
	Copyright        string `json:"copyright"`
	License          string `json:"license"`
}

var b Brand

func init() {
	if err := json.Unmarshal(brandJSON, &b); err != nil {
		panic("failed to parse brand.json: " + err.Error())
	}

	// Initialize exported variables after JSON is parsed
	Name = b.Name
	LowerName = b.LowerName
	Vendor = b.Vendor
	Website = b.Website
	Repository = b.Repository
	Description = b.Description
	Tagline = b.Tagline
	ConfigEnvPrefix = b.ConfigEnvPrefix
	DefaultConfigDir = b.DefaultConfigDir
	DefaultStateDir = b.DefaultStateDir
	DefaultLogDir = b.DefaultLogDir
	DefaultRunDir = b.DefaultRunDir
	SocketName = b.SocketName
	BinaryName = b.BinaryName
	ServiceName = b.ServiceName
	ConfigFileName = b.ConfigFileName
	Copyright = b.Copyright
	License = b.License
}

// Exported variables for backward compatibility and convenience
var (
	Name             string
	LowerName        string
	Vendor           string
	Website          string
	Repository       string
	Description      string
	Tagline          string
	ConfigEnvPrefix  string
	DefaultConfigDir string
	DefaultStateDir  string
	DefaultLogDir    string
	DefaultRunDir    string
	SocketName       string
	BinaryName       string
	ServiceName      string
	ConfigFileName   string
	Copyright        string
	License          string

	// Version is set at build time via -ldflags
	Version      = "dev"
	BuildTime    = "unknown"
	BuildArch    = "unknown"
	GitCommit    = "unknown"
	GitBranch    = "unknown"
	GitMergeBase = "unknown"
)

// Get returns the full Brand struct
func Get() Brand {
	return b
}

// UserAgent returns a User-Agent string for HTTP requests
func UserAgent(version string) string {
	if version == "" {
		version = "dev"
	}
	return Name + "/" + version
}

// GetStateDir returns the state directory, checking env vars first.
// Priority: GLACIC_STATE_DIR > GLACIC_PREFIX/state > DefaultStateDir
func GetStateDir() string {
	if dir := os.Getenv(ConfigEnvPrefix + "_STATE_DIR"); dir != "" {
		return dir
	}
	if prefix := os.Getenv(ConfigEnvPrefix + "_PREFIX"); prefix != "" {
		return filepath.Join(prefix, "state")
	}
	return DefaultStateDir
}

// GetLogDir returns the log directory, checking env vars first.
// Priority: GLACIC_LOG_DIR > GLACIC_PREFIX/log > DefaultLogDir
func GetLogDir() string {
	if dir := os.Getenv(ConfigEnvPrefix + "_LOG_DIR"); dir != "" {
		return dir
	}
	if prefix := os.Getenv(ConfigEnvPrefix + "_PREFIX"); prefix != "" {
		return filepath.Join(prefix, "log")
	}
	return DefaultLogDir
}

// GetConfigDir returns the config directory, checking env vars first.
// Priority: GLACIC_CONFIG_DIR > GLACIC_PREFIX/config > DefaultConfigDir
func GetConfigDir() string {
	if dir := os.Getenv(ConfigEnvPrefix + "_CONFIG_DIR"); dir != "" {
		return dir
	}
	if prefix := os.Getenv(ConfigEnvPrefix + "_PREFIX"); prefix != "" {
		return filepath.Join(prefix, "config")
	}
	return DefaultConfigDir
}

// GetRunDir returns the runtime directory for sockets and PID files.
// Priority: GLACIC_RUN_DIR > GLACIC_PREFIX/run > DefaultRunDir
func GetRunDir() string {
	if dir := os.Getenv(ConfigEnvPrefix + "_RUN_DIR"); dir != "" {
		return dir
	}
	if prefix := os.Getenv(ConfigEnvPrefix + "_PREFIX"); prefix != "" {
		return filepath.Join(prefix, "run")
	}
	return DefaultRunDir
}

// GetSocketPath returns the full path to the control plane socket.
// The socket name includes the brand name for uniqueness.
// Returns: /var/run/glacic-ctl.sock (or equivalent based on env/prefix)
func GetSocketPath() string {
	runDir := GetRunDir()
	// Use format: <lowerName>-<socketName> e.g., glacic-ctl.sock
	return filepath.Join(runDir, LowerName+"-"+SocketName)
}
