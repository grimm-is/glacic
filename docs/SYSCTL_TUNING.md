# Example: Sysctl Tuning Configuration

## Default Profile (Recommended)
# No configuration needed - applied automatically
# Hardware-aware defaults optimized for routers

## Performance Profile (High-throughput)
system {
  sysctl_profile = "performance"
}

## Low-Memory Profile (Embedded devices)
system {
  sysctl_profile = "low-memory"
}

## Security Profile (Paranoid hardening)
system {
  sysctl_profile = "security"
}

## Custom Tuning (Profile + Manual Overrides)
system {
  sysctl_profile = "default"
  
  # Manual overrides applied after profile
  sysctl = {
    "net.core.rmem_max" = "134217728"           # 128MB receive buffer
    "net.ipv4.tcp_congestion_control" = "cubic" # Force cubic instead of BBR
    "net.netfilter.nf_conntrack_max" = "262144" # 256k tracked connections
  }
}

## Profile Comparison

### Default (Recommended)
- **RAM**: 128-512MB typical for 100k connections
- **Throughput**: Up to 1Gbps with NAT + firewall
- **Security**: Full hardening (rp_filter, SYN cookies, no redirects)
- **Use when**: General purpose router, mixed traffic

### Performance (High-bandwidth)
- **RAM**: 512MB-2GB typical (2-4x default)
- **Throughput**: Multi-Gbps routing
- **Tradeoff**: Uses more RAM for buffers and conntrack
- **Use when**: >1Gbps bandwidth, >100k connections

### Low-Memory (Embedded)
- **RAM**: 64-128MB typical
- **Throughput**: 100-500Mbps
- **Tradeoff**: Lower connection limits, may drop under heavy load
- **Use when**: <256MB RAM devices (RPi Zero, embedded)

### Security (Paranoid)
- **RAM**: Similar to default
- **Security**: Maximum hardening, strict validation
- **Tradeoff**: Less tolerant of broken TCP implementations
- **Use when**: Exposed to internet, DDoS mitigation

## Hardware Detection

Tuning is automatically scaled based on:
- **RAM**: Detected from `/proc/meminfo`
- **CPU Cores**: Detected from `runtime.NumCPU()`

Example scaling (default profile):
- 1GB RAM: 32k conntrack entries, 4MB buffers
- 4GB RAM: 128k conntrack entries, 16MB buffers
- 8GB RAM: 256k conntrack entries, 16MB buffers (capped)

## Applied Parameters (Examples)

### Security Hardening (All Profiles)
```
net.ipv4.conf.all.rp_filter = 1                    # Prevent IP spoofing
net.ipv4.conf.all.accept_redirects = 0             # No ICMP redirects
net.ipv4.tcp_syncookies = 1                        # SYN flood protection
net.ipv4.conf.all.log_martians = 1                 # Log suspicious packets
```

### Connection Tracking (Profile-Specific)
```
# Default
net.netfilter.nf_conntrack_max = 128000            # 128k conn (4GB RAM)

# Performance
net.netfilter.nf_conntrack_max = 262144            # 256k conn (4GB RAM)

# Low-Memory
net.netfilter.nf_conntrack_max = 32768             # 32k conn (2GB RAM)
```

### TCP Tuning (Profile-Specific)
```
# Default / Performance
net.ipv4.tcp_congestion_control = bbr              # BBR (if available)
net.ipv4.tcp_window_scaling = 1
net.core.rmem_max = 16777216                       # 16MB receive buffer

# Security
net.ipv4.tcp_congestion_control = cubic            # Predictable behavior
net.ipv4.tcp_timestamps = 0                        # Prevent fingerprinting
```

## Verification

Check applied sysctls:
```bash
sysctl -a | grep net.ipv4.tcp_congestion_control
sysctl -a | grep net.netfilter.nf_conntrack_max
```

View system logs:
```bash
journalctl -u glacic | grep sysctl
```
