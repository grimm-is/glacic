package network

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"

	"grimm.is/glacic/internal/logging"
)

// SysctlProfile represents a sysctl tuning profile.
type SysctlProfile string

const (
	ProfileDefault     SysctlProfile = "default"
	ProfilePerformance SysctlProfile = "performance"
	ProfileLowMemory   SysctlProfile = "low-memory"
	ProfileSecurity    SysctlProfile = "security"
)

// SysctlTuner applies sysctl tuning based on hardware and profile.
type SysctlTuner struct {
	profile   SysctlProfile
	overrides map[string]string
	ramGB     int
	cpuCount  int
	logger    *logging.Logger
}

// NewSysctlTuner creates a new sysctl tuner with hardware detection.
func NewSysctlTuner(profile SysctlProfile, overrides map[string]string, logger *logging.Logger) *SysctlTuner {
	if logger == nil {
		logger = logging.New(logging.DefaultConfig())
	}

	ramGB := detectRAM()
	cpuCount := runtime.NumCPU()

	logger.Info(fmt.Sprintf("Sysctl tuner initialized: profile=%s, ram=%dGB, cpu=%d cores", profile, ramGB, cpuCount))

	return &SysctlTuner{
		profile:   profile,
		overrides: overrides,
		ramGB:     ramGB,
		cpuCount:  cpuCount,
		logger:    logger,
	}
}

// Apply applies the sysctl tuning to the system.
func (t *SysctlTuner) Apply() error {
	// Get profile parameters
	params := t.generateParams()

	// Apply each parameter
	applied := 0
	for key, value := range params {
		if err := WriteSysctl(key, value); err != nil {
			// Log but continue - some sysctls may not exist on all kernels
			t.logger.Warn(fmt.Sprintf("Failed to set %s=%s: %v", key, value, err))
		} else {
			applied++
		}
	}

	t.logger.Info(fmt.Sprintf("Applied %d/%d sysctl parameters", applied, len(params)))
	return nil
}

// generateParams generates sysctl parameters based on profile and hardware.
func (t *SysctlTuner) generateParams() map[string]string {
	params := make(map[string]string)

	// Start with base security hardening (all profiles)
	t.applySecurityBase(params)

	// Apply profile-specific tuning
	switch t.profile {
	case ProfileDefault:
		t.applyDefaultProfile(params)
	case ProfilePerformance:
		t.applyPerformanceProfile(params)
	case ProfileLowMemory:
		t.applyLowMemoryProfile(params)
	case ProfileSecurity:
		t.applySecurityProfile(params)
	default:
		t.logger.Warn(fmt.Sprintf("Unknown profile %s, using default", t.profile))
		t.applyDefaultProfile(params)
	}

	// Apply user overrides last
	for key, value := range t.overrides {
		params[key] = value
	}

	return params
}

// applySecurityBase applies security hardening common to all profiles.
func (t *SysctlTuner) applySecurityBase(params map[string]string) {
	// Reverse path filtering (prevent IP spoofing)
	// Use loose mode (2) for multi-WAN/asymmetric routing compatibility
	// Strict mode (1) breaks when packets arrive on one interface but would route out another
	params["net.ipv4.conf.all.rp_filter"] = "2"
	params["net.ipv4.conf.default.rp_filter"] = "2"

	// Disable ICMP redirects (security)
	params["net.ipv4.conf.all.accept_redirects"] = "0"
	params["net.ipv4.conf.all.send_redirects"] = "0"
	params["net.ipv4.conf.default.accept_redirects"] = "0"
	params["net.ipv4.conf.default.send_redirects"] = "0"
	params["net.ipv6.conf.all.accept_redirects"] = "0"
	params["net.ipv6.conf.default.accept_redirects"] = "0"

	// Disable source routing (security)
	params["net.ipv4.conf.all.accept_source_route"] = "0"
	params["net.ipv4.conf.default.accept_source_route"] = "0"

	// Enable SYN cookies (DDoS protection)
	params["net.ipv4.tcp_syncookies"] = "1"

	// Log martian packets (security)
	params["net.ipv4.conf.all.log_martians"] = "1"

	// Ignore ICMP echo requests to broadcast (security)
	params["net.ipv4.icmp_echo_ignore_broadcasts"] = "1"

	// Ignore bogus ICMP error responses (security)
	params["net.ipv4.icmp_ignore_bogus_error_responses"] = "1"
}

// applyDefaultProfile applies default router tuning.
func (t *SysctlTuner) applyDefaultProfile(params map[string]string) {
	// Connection tracking (128k per GB of RAM)
	conntrackMax := t.ramGB * 32768
	if conntrackMax < 32768 {
		conntrackMax = 32768 // Minimum
	}
	params["net.netfilter.nf_conntrack_max"] = strconv.Itoa(conntrackMax)
	params["net.netfilter.nf_conntrack_buckets"] = strconv.Itoa(conntrackMax / 4)

	// TCP tuning for routers
	params["net.ipv4.tcp_window_scaling"] = "1"
	params["net.ipv4.tcp_timestamps"] = "1"
	params["net.ipv4.tcp_sack"] = "1"
	params["net.ipv4.tcp_fack"] = "1"

	// Congestion control (BBR if available, else cubic)
	params["net.ipv4.tcp_congestion_control"] = t.selectCongestionControl("bbr", "cubic")

	// Buffer tuning (4MB per GB, capped at 16MB)
	rmemMax := t.ramGB * 4 * 1024 * 1024
	if rmemMax > 16*1024*1024 {
		rmemMax = 16 * 1024 * 1024
	}
	if rmemMax < 256*1024 {
		rmemMax = 256 * 1024
	}
	params["net.core.rmem_max"] = strconv.Itoa(rmemMax)
	params["net.core.wmem_max"] = strconv.Itoa(rmemMax)

	// Auto-tuning ranges
	params["net.ipv4.tcp_rmem"] = fmt.Sprintf("4096 87380 %d", rmemMax/2)
	params["net.ipv4.tcp_wmem"] = fmt.Sprintf("4096 16384 %d", rmemMax/2)

	// Netdev backlog (1000 per CPU)
	backlog := t.cpuCount * 1000
	if backlog < 1000 {
		backlog = 1000
	}
	params["net.core.netdev_max_backlog"] = strconv.Itoa(backlog)

	// Router-specific: Enable packet forwarding optimizations
	params["net.ipv4.ip_forward"] = "1"
	params["net.ipv6.conf.all.forwarding"] = "1"
}

// applyPerformanceProfile applies high-performance tuning.
func (t *SysctlTuner) applyPerformanceProfile(params map[string]string) {
	// Start with default as base
	t.applyDefaultProfile(params)

	// Higher connection tracking (256k per GB)
	conntrackMax := t.ramGB * 65536
	if conntrackMax < 65536 {
		conntrackMax = 65536
	}
	params["net.netfilter.nf_conntrack_max"] = strconv.Itoa(conntrackMax)
	params["net.netfilter.nf_conntrack_buckets"] = strconv.Itoa(conntrackMax / 4)

	// Larger buffers (16MB per GB, capped at 64MB)
	rmemMax := t.ramGB * 16 * 1024 * 1024
	if rmemMax > 64*1024*1024 {
		rmemMax = 64 * 1024 * 1024
	}
	if rmemMax < 4*1024*1024 {
		rmemMax = 4 * 1024 * 1024
	}
	params["net.core.rmem_max"] = strconv.Itoa(rmemMax)
	params["net.core.wmem_max"] = strconv.Itoa(rmemMax)

	// Auto-tuning with larger windows
	params["net.ipv4.tcp_rmem"] = fmt.Sprintf("4096 131072 %d", rmemMax/2)
	params["net.ipv4.tcp_wmem"] = fmt.Sprintf("4096 65536 %d", rmemMax/2)

	// Larger backlog
	backlog := t.cpuCount * 2000
	params["net.core.netdev_max_backlog"] = strconv.Itoa(backlog)

	// Performance optimizations
	params["net.core.somaxconn"] = "4096"
	params["net.ipv4.tcp_max_syn_backlog"] = "8192"

	// BBR strongly preferred for performance
	params["net.ipv4.tcp_congestion_control"] = t.selectCongestionControl("bbr", "htcp", "cubic")

	// Enable TCP Fast Open
	params["net.ipv4.tcp_fastopen"] = "3"

	// Reuse TIME_WAIT sockets
	params["net.ipv4.tcp_tw_reuse"] = "1"
}

// applyLowMemoryProfile applies constrained device tuning.
func (t *SysctlTuner) applyLowMemoryProfile(params map[string]string) {
	// Start with default
	t.applyDefaultProfile(params)

	// Lower connection tracking (16k per GB, min 16k)
	conntrackMax := t.ramGB * 16384
	if conntrackMax < 16384 {
		conntrackMax = 16384
	}
	params["net.netfilter.nf_conntrack_max"] = strconv.Itoa(conntrackMax)
	params["net.netfilter.nf_conntrack_buckets"] = strconv.Itoa(conntrackMax / 4)

	// Smaller buffers (256KB per GB, capped at 2MB)
	rmemMax := t.ramGB * 256 * 1024
	if rmemMax > 2*1024*1024 {
		rmemMax = 2 * 1024 * 1024
	}
	if rmemMax < 128*1024 {
		rmemMax = 128 * 1024
	}
	params["net.core.rmem_max"] = strconv.Itoa(rmemMax)
	params["net.core.wmem_max"] = strconv.Itoa(rmemMax)

	// Conservative auto-tuning
	params["net.ipv4.tcp_rmem"] = fmt.Sprintf("4096 32768 %d", rmemMax/2)
	params["net.ipv4.tcp_wmem"] = fmt.Sprintf("4096 16384 %d", rmemMax/2)

	// Smaller backlog
	backlog := t.cpuCount * 500
	if backlog < 500 {
		backlog = 500
	}
	params["net.core.netdev_max_backlog"] = strconv.Itoa(backlog)

	// Use cubic (lower memory overhead than BBR)
	params["net.ipv4.tcp_congestion_control"] = "cubic"

	// Aggressive connection cleanup
	params["net.netfilter.nf_conntrack_tcp_timeout_established"] = "3600" // 1 hour
	params["net.netfilter.nf_conntrack_tcp_timeout_time_wait"] = "30"
}

// applySecurityProfile applies paranoid security tuning.
func (t *SysctlTuner) applySecurityProfile(params map[string]string) {
	// Start with default
	t.applyDefaultProfile(params)

	// Stricter RP filter
	params["net.ipv4.conf.all.rp_filter"] = "1"
	params["net.ipv4.conf.default.rp_filter"] = "1"

	// Use cubic for predictability
	params["net.ipv4.tcp_congestion_control"] = "cubic"

	// Stricter SYN handling
	params["net.ipv4.tcp_max_syn_backlog"] = "2048"
	params["net.ipv4.tcp_synack_retries"] = "2"
	params["net.ipv4.tcp_syn_retries"] = "3"

	// Aggressive timeouts (DDoS mitigation)
	params["net.netfilter.nf_conntrack_tcp_timeout_established"] = "1800" // 30 min
	params["net.netfilter.nf_conntrack_tcp_timeout_time_wait"] = "30"
	params["net.netfilter.nf_conntrack_tcp_timeout_close_wait"] = "30"
	params["net.netfilter.nf_conntrack_tcp_timeout_fin_wait"] = "60"

	// Disable TCP timestamps (fingerprinting)
	params["net.ipv4.tcp_timestamps"] = "0"

	// Rate limit ICMP
	params["net.ipv4.icmp_ratelimit"] = "100"
	params["net.ipv4.icmp_ratemask"] = "6168" // Limit dest unreachable, time exceeded, parameter problem
}

// selectCongestionControl selects the first available congestion control algorithm.
func (t *SysctlTuner) selectCongestionControl(preferred ...string) string {
	available := t.getAvailableCongestionControls()

	for _, pref := range preferred {
		for _, avail := range available {
			if pref == avail {
				return pref
			}
		}
	}

	// Fallback
	if len(available) > 0 {
		return available[0]
	}
	return "cubic"
}

// getAvailableCongestionControls reads the list of available congestion control algorithms.
func (t *SysctlTuner) getAvailableCongestionControls() []string {
	data, err := os.ReadFile("/proc/sys/net/ipv4/tcp_available_congestion_control")
	if err != nil {
		return []string{"cubic"}
	}
	return strings.Fields(strings.TrimSpace(string(data)))
}

// detectRAM detects total system RAM in GB.
func detectRAM() int {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 1 // Safe fallback
	}

	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "MemTotal:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				kb, _ := strconv.Atoi(fields[1])
				gb := kb / (1024 * 1024)
				if gb < 1 {
					return 1
				}
				return gb
			}
		}
	}
	return 1
}
