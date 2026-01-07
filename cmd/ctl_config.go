package cmd

// CtlRuntimeConfig holds all runtime configuration for the control plane.
type CtlRuntimeConfig struct {
	ConfigFile string
	TestMode   bool
	StateDir   string
	DryRun     bool
	Listeners  map[string]interface{}
	IsUpgrade  bool
}

// NewCtlRuntimeConfig creates runtime configuration from CLI args.
func NewCtlRuntimeConfig(configFile string, testMode bool, stateDir string, dryRun bool, listeners map[string]interface{}) *CtlRuntimeConfig {
	return &CtlRuntimeConfig{
		ConfigFile: configFile,
		TestMode:   testMode,
		StateDir:   stateDir,
		DryRun:     dryRun,
		Listeners:  listeners,
		IsUpgrade:  len(listeners) > 0,
	}
}
