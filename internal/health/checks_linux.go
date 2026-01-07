//go:build linux
// +build linux

package health

import (
	"context"
	"fmt"
	"grimm.is/glacic/internal/clock"
	"net"
	"os"
	"strings"
	"time"

	"github.com/google/nftables"
	"github.com/vishvananda/netlink"
)

// CheckNftables verifies nftables is working.
func CheckNftables(ctx context.Context) Check {
	start := clock.Now()
	check := Check{LastChecked: start}

	// Use nftables library instead of shelling out
	conn, err := nftables.New()
	if err != nil {
		check.Status = StatusUnhealthy
		check.Message = fmt.Sprintf("failed to open nftables connection: %v", err)
	} else {
		// Just list tables to verify access
		tables, err := conn.ListTables()
		if err != nil {
			check.Status = StatusUnhealthy
			check.Message = fmt.Sprintf("failed to list tables: %v", err)
		} else {
			check.Status = StatusHealthy
			check.Message = fmt.Sprintf("nftables operational (%d tables)", len(tables))
		}
	}

	check.Duration = time.Since(start)
	return check
}

// CheckConntrack verifies connection tracking is working.
func CheckConntrack(ctx context.Context) Check {
	start := clock.Now()
	check := Check{LastChecked: start}

	data, err := os.ReadFile("/proc/sys/net/netfilter/nf_conntrack_count")
	if err != nil {
		check.Status = StatusDegraded
		check.Message = fmt.Sprintf("cannot read conntrack: %v", err)
	} else {
		check.Status = StatusHealthy
		check.Message = fmt.Sprintf("conntrack entries: %s", string(data))
	}

	check.Duration = time.Since(start)
	return check
}

// CheckInterfaces verifies network interfaces are up.
func CheckInterfaces(ctx context.Context) Check {
	start := clock.Now()
	check := Check{LastChecked: start}

	// Use netlink library instead of "ip link show"
	links, err := netlink.LinkList()
	if err != nil {
		check.Status = StatusUnhealthy
		check.Message = fmt.Sprintf("netlink failed: %v", err)
	} else {
		// Count UP interfaces
		upCount := 0
		for _, link := range links {
			if link.Attrs().Flags&net.FlagUp != 0 {
				upCount++
			}
		}
		check.Status = StatusHealthy
		check.Message = fmt.Sprintf("%d interfaces up", upCount)
	}

	check.Duration = time.Since(start)
	return check
}

// CheckMemory verifies memory is available.
func CheckMemory(ctx context.Context) Check {
	start := clock.Now()
	check := Check{LastChecked: start}

	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		check.Status = StatusDegraded
		check.Message = fmt.Sprintf("cannot read meminfo: %v", err)
	} else {
		// Parse MemAvailable
		// Use strings.Split instead of custom splitLines
		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "MemAvailable:") {
				check.Status = StatusHealthy
				check.Message = line
				break
			}
		}
		if check.Status == "" {
			check.Status = StatusHealthy
			check.Message = "memory info available"
		}
	}

	check.Duration = time.Since(start)
	return check
}
