package config

import (
	"encoding/json"
)

// Clone returns a deep copy of the configuration.
func (c *Config) Clone() *Config {
	if c == nil {
		return nil
	}
	// Use JSON round-trip for deep copy to handle all nested slices and pointers safely
	b, err := json.Marshal(c)
	if err != nil {
		// Should not happen with valid config struct
		return nil
	}
	var clone Config
	if err := json.Unmarshal(b, &clone); err != nil {
		return nil
	}
	return &clone
}
