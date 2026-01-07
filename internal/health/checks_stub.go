//go:build !linux
// +build !linux

package health

import (
	"context"
	"grimm.is/glacic/internal/clock"
)

// CheckNftables verifies nftables is working.
func CheckNftables(ctx context.Context) Check {
	return Check{
		Status:      StatusHealthy,
		Message:     "nftables unsupported on this OS (stubbed)",
		LastChecked: clock.Now(),
		Duration:    0,
	}
}

// CheckConntrack verifies connection tracking is working.
func CheckConntrack(ctx context.Context) Check {
	return Check{
		Status:      StatusHealthy,
		Message:     "conntrack unsupported on this OS (stubbed)",
		LastChecked: clock.Now(),
		Duration:    0,
	}
}

// CheckInterfaces verifies network interfaces are up.
func CheckInterfaces(ctx context.Context) Check {
	return Check{
		Status:      StatusHealthy,
		Message:     "netlink unsupported on this OS (stubbed)",
		LastChecked: clock.Now(),
		Duration:    0,
	}
}

// CheckMemory verifies memory is available.
func CheckMemory(ctx context.Context) Check {
	return Check{
		Status:      StatusHealthy,
		Message:     "procfs unsupported on this OS (stubbed)",
		LastChecked: clock.Now(),
		Duration:    0,
	}
}
