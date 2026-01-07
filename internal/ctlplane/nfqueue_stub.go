//go:build !linux

package ctlplane

import "fmt"

// NFQueueReader is a stub for non-Linux systems.
type NFQueueReader struct {
	VerdictFunc func(entry NFLogEntry) bool
}

// NewNFQueueReader creates a stub reader.
func NewNFQueueReader(queueNum uint16) *NFQueueReader {
	return &NFQueueReader{}
}

// SetVerdictFunc is a no-op on non-Linux.
func (r *NFQueueReader) SetVerdictFunc(fn func(entry NFLogEntry) bool) {
	r.VerdictFunc = fn
}

// Start returns an error on non-Linux systems.
func (r *NFQueueReader) Start() error {
	return fmt.Errorf("nfqueue is only supported on Linux")
}

// Stop is a no-op on non-Linux.
func (r *NFQueueReader) Stop() {}

// IsRunning always returns false on non-Linux.
func (r *NFQueueReader) IsRunning() bool {
	return false
}

// NFQueueStats holds statistics for the queue reader.
type NFQueueStats struct {
	PacketsProcessed uint64 `json:"packets_processed"`
	PacketsAccepted  uint64 `json:"packets_accepted"`
	PacketsDropped   uint64 `json:"packets_dropped"`
	VerdictErrors    uint64 `json:"verdict_errors"`
}

// GetStats returns empty stats on non-Linux.
func (r *NFQueueReader) GetStats() NFQueueStats {
	return NFQueueStats{}
}
