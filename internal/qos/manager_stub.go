//go:build !linux
// +build !linux

package qos

import (
	"grimm.is/glacic/internal/config"
	"grimm.is/glacic/internal/logging"
)

// Manager handles QoS traffic shaping configuration (Stub).
type Manager struct{}

// NewManager creates a new QoS manager (Stub).
func NewManager(logger *logging.Logger) *Manager {
	return &Manager{}
}

// ApplyConfig applies QoS configuration to interfaces (Stub).
func (m *Manager) ApplyConfig(cfg *config.Config) error {
	return nil
}
