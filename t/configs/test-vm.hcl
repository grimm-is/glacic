# Test VM Firewall Configuration
# Uses static IPs suitable for QEMU user networking

schema_version = "1.0"
ip_forwarding = true

# Disable API authentication for feature tests (default is true if missing)
api {
  require_auth = false
}

# Interface configuration - static IPs for test VM
interface "eth0" {
  description = "WAN Interface (QEMU user net)"
  zone        = "WAN"
  ipv4        = ["10.0.2.15/24"]
  gateway     = "10.0.2.2"
}

interface "eth1" {
  description = "Green Zone (Trusted LAN)"
  zone        = "Green"
  ipv4        = ["10.1.0.1/24"]
}

interface "eth2" {
  description = "Orange Zone (DMZ)"
  zone        = "Orange"
  ipv4        = ["10.2.0.1/24"]
}

interface "eth3" {
  description = "Red Zone (Restricted)"
  zone        = "Red"
  ipv4        = ["10.3.0.1/24"]
}

# Basic firewall policies (using 2-label syntax)
policy "Green" "WAN" {
  rule "allow_internet" {
    description = "Allow Green zone internet access"
    action = "accept"
  }
}

policy "Orange" "WAN" {
  rule "allow_internet" {
    description = "Allow Orange zone internet access"
    action = "accept"
  }
}

policy "WAN" "*" {
  rule "allow_established" {
    description = "Allow established connections"
    conn_state  = "established,related"
    action      = "accept"
  }

  rule "drop_all" {
    description = "Drop all other inbound"
    action      = "drop"
  }
}

# DHCP server for LAN clients
dhcp {
  enabled = true

  scope "lan" {
    interface = "eth1"
    range_start = "10.1.0.100"
    range_end = "10.1.0.200"
    router = "10.1.0.1"
    lease_time = "1h"
    dns = ["10.1.0.1"]
  }
}

# DNS server
dns_server {
  enabled = true
  listen_on = ["10.1.0.1"]
  forwarders = ["8.8.8.8", "8.8.4.4"]
}
