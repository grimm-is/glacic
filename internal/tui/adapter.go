package tui

import (
	"grimm.is/glacic/internal/config"
	"grimm.is/glacic/internal/ctlplane"
)

// Ensure Backend implementation
// Note: We use the direct client for now, but in future this might be a gRPC client
type LocalBackend struct {
	client *ctlplane.Client
}

func NewLocalBackend(client *ctlplane.Client) *LocalBackend {
	return &LocalBackend{client: client}
}

func (b *LocalBackend) GetStatus() (*EnrichedStatus, error) {
	// For MVP, just return basic status.
	// Real impl would fetch /api/status + system stats
	return &EnrichedStatus{
		Running: true,
		Uptime:  "Unknown", // Needs parsing from real API
	}, nil
}

func (b *LocalBackend) GetFlows(filter string) ([]Flow, error) {
	// TODO: Wire up to real ctlplane.Client.GetFlows() method
	// The API endpoint /api/flows exists - this just needs to call the client
	return []Flow{
		{Proto: "tcp", Src: "192.168.1.100:5432", Dst: "1.1.1.1:443", State: "ESTABLISHED"},
	}, nil
}

func (b *LocalBackend) GetConfig() (*config.Config, error) {
	return b.client.GetConfig()
}

// Ensure Legacy Types that might be referenced elsewhere or needed
// We moved EnrichedStatus to model.go but if it was deleted we need to ensure it exists
// It is in model.go now.
