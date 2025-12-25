# Walkthrough: Sysctl Tuning Implementation

## Overview
Implemented router-optimized sysctl tuning with hardware-aware profiles to replace Linux's default server-oriented settings.

## Changes Made

### 1. Core Tuning Module
**File**: `internal/network/sysctl_tuning.go`

Created comprehensive tuning system with:
- Hardware detection (RAM via `/proc/meminfo`, CPU via `runtime.NumCPU()`)
- 4 preset profiles with documented tradeoffs
- Automatic parameter scaling based on available resources
- User override support

#### Profiles Implemented

**default** (Recommended)
- 128-512MB RAM typical
- 32k conntrack entries per GB RAM
- BBR congestion control (fallback: cubic)
- Full security hardening

**performance** (High-throughput)
- 512MB-2GB RAM typical (2-4x default)
- 65k conntrack entries per GB RAM
- Larger TCP buffers (16MB per GB, capped at 64MB)
- TCP Fast Open, TIME_WAIT reuse

**low-memory** (Embedded <256MB RAM)
- 64-128MB RAM typical
- 16k conntrack entries per GB RAM
- Smaller buffers (256KB per GB, capped at 2MB)
- Aggressive connection timeouts

**security** (Paranoid hardening)
- Similar RAM to default
- Strict RP filter, no TCP timestamps (anti-fingerprinting)
- Aggressive conntrack timeouts (30min established, 30s TIME_WAIT)
- ICMP rate limiting

### 2. Configuration Schema
**Files**: 
- `internal/config/config.go` - Added `System *SystemConfig` field
- `internal/config/system.go` - Added `SystemConfig` struct

HCL syntax:
```hcl
system {
  sysctl_profile = "default"  # or performance, low-memory, security
  
  sysctl = {
    "net.core.rmem_max" = "134217728"
  }
}
```

### 3. Startup Integration
**File**: `cmd/ctl_helpers.go`

- Added `applySysctlTuning()` helper
- Integrated into network stack initialization (after interfaces, before services)
- Auto-applies "default" profile if no config specified

### 4. Testing
**File**: `internal/network/sysctl_tuning_test.go`

Unit tests for:
- Profile initialization
- Override handling
- Hardware detection

All tests passing ✓

### 5. Documentation
**File**: `docs/SYSCTL_TUNING.md`

Comprehensive guide including:
- Profile comparison table
- RAM usage estimates
- Applied parameter examples
- Verification commands

## Key Features

### Hardware-Aware Scaling
```go
// Example for default profile with different RAM
1GB RAM  → 32k conntrack, 4MB buffers
4GB RAM  → 128k conntrack, 16MB buffers
8GB RAM  → 256k conntrack, 16MB buffers (capped)
```

### Security Hardening (All Profiles)
- Reverse path filtering (anti-spoofing)
- ICMP redirect blocking
- SYN cookie protection
- Martian packet logging
- Source routing disabled

### Congestion Control Selection
Automatically selects best available algorithm:
- Prefers BBR (if available) for default/performance
- Falls back to cubic/htcp
- Security profile forces cubic for predictability

### User Overrides
Applied after profile tuning:
```hcl
system {
  sysctl_profile = "performance"
  sysctl = {
    "net.ipv4.tcp_congestion_control" = "cubic"  # Override BBR
  }
}
```

## Verification

### Build Test
```bash
make build
```
✓ Build successful

### Unit Tests
```bash
go test -run TestSysctl ./internal/network/ -v
```
```
=== RUN   TestSysctlProfile_Default
--- PASS: TestSysctlProfile_Default (0.00s)
=== RUN   TestSysctlProfile_Performance
--- PASS: TestSysctlProfile_Performance (0.00s)
=== RUN   TestSysctlProfile_Low

Memory
--- PASS: TestSysctlProfile_LowMemory (0.00s)
=== RUN   TestSysctlProfile_Security
--- PASS: TestSysctlProfile_Security (0.00s)
PASS
```

## Example Usage

### Default (No Config)
```hcl
# Automatically applies "default" profile
interface "eth0" {
  ipv4 = ["192.168.1.1/24"]
}
```

### High-Performance Router
```hcl
system {
  sysctl_profile = "performance"
}

interface "wan0" {
  ipv4 = ["dhcp"]
}
```

### Embedded Device
```hcl
system {
  sysctl_profile = "low-memory"
}
```

### Custom Tuning
```hcl
system {
  sysctl_profile = "default"
  
  sysctl = {
    "net.netfilter.nf_conntrack_max" = "524288"  # 512k connections
    "net.core.rmem_max" = "134217728"            # 128MB buffers
  }
}
```

## Implementation Notes

- Tuning is applied **once at startup** (after interfaces, before services)
- Failed sysctls are logged but don't block startup (kernel version compatibility)
- Hardware detection is automatic and logged for visibility
- Overrides take precedence over profile defaults

## Next Steps

Potential future enhancements:
- API endpoints for runtime sysctl viewing/modification
- Integration test for sysctl application
- Performance benchmarking across profiles
- Additional profiles (e.g., "ultra-low-latency" for gaming/trading)
