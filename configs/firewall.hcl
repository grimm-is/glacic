# Demo Firewall Configuration
# This sets up basic routing and firewall rules

schema_version = "1.0"

# Basic settings
ip_forwarding = true

# Interfaces configuration
interface "eth0" {
  description = "WAN Interface (Internet)"
  dhcp = true
}

interface "eth1" {
  description = "DMZ Interface (Management)"
  ipv4 = ["192.168.100.10/24"]
  gateway = "192.168.100.1"
}

interface "eth2" {
  description = "LAN Interface (Internal Network)"
  ipv4 = ["192.168.1.1/24"]
}

# Zone definitions
zone "wan" {
  interface = "eth0"
}

zone "dmz" {
  interface = "eth1"
  management {
    web = true
    ssh = true
    icmp = true
  }
}

zone "lan" {
  interface = "eth2"
  management {
    web = true
    ssh = true
    icmp = true
  }
}

# DHCP for LAN clients
dhcp {
  enabled = true
  scope "lan" {
    interface = "eth2"
    range_start = "192.168.1.100"
    range_end = "192.168.1.200"
    router = "192.168.1.1"
    lease_time = "12h"
    dns = ["8.8.8.8", "8.8.4.4"]

    # Custom DHCP options
    options = {
      # Named options (type auto-detected)
      domain_name = "home.local"
      ntp_server = "192.168.1.1"

      # Custom numeric codes (prefix required)
      # "150" = "ip:192.168.1.10"    # Cisco TFTP server
      # "252" = "str:http://proxy.local/wpad.dat"  # WPAD URL
    }
  }
}

# Basic firewall policies
policy "lan" "wan" {
  name = "lan-to-wan"
  description = "Allow LAN to access Internet"
  action = "accept"
}

policy "wan" "lan" {
  name = "wan-to-lan"
  description = "Block unsolicited WAN to LAN"
  action = "drop"
}

policy "dmz" "self" {
  name = "dmz-access"
  description = "Allow Web UI access from DMZ"
  action = "accept"
}

# NAT for outbound traffic
nat "outbound" {
  type = "masquerade"
  out_interface = "eth0"
}

# DNS forwarding
dns_server {
  enabled = true
  listen_on = ["192.168.1.1", "192.168.100.10"]
  forwarders = ["8.8.8.8", "8.8.4.4"]
}
