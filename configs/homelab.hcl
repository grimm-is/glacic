schema_version = "1.0"
ip_forwarding = true
mss_clamping = true

# =============================================================================
# Interfaces: Define physical and logical network interfaces
# =============================================================================

# WAN - Fiber Uplink
interface "eth0" {
  description = "WAN - Fiber Uplink"
  dhcp        = true
}

# Security hardening for WAN
protection "wan_hardening" {
  interface        = "eth0"
  anti_spoofing    = true
  bogon_filtering  = true
  invalid_packets  = true
}

# LAN - Main Home Network
interface "eth1" {
  description = "LAN - Home Network"
  # Zone assignment handled via complex match block in zone definition below
  ipv4        = ["10.0.0.1/24"]
}

# DMZ - Public facing services
interface "eth2" {
  description = "DMZ - Public Services"
  ipv4        = ["10.0.50.1/24"]
}

# IoT - Isolated VLAN for smart devices
interface "eth1.20" {
  description = "IoT - Smart Devices"
  ipv4        = ["10.0.20.1/24"]
}

# WireGuard VPN Interface
interface "wg0" {
  description = "WireGuard VPN"
}

# =============================================================================
# Zones: Define security zones and traffic matching
# =============================================================================

zone "wan" {
  description = "Internet / Untrusted"
  interface   = "eth0"
}

zone "lan" {
  description = "Trusted Local Network"
  match { interface = "eth1" }

  management {
    web_ui = true
    ssh    = true
    api    = true # Needed for CLI access
  }
}

zone "iot" {
  description = "IoT Devices (Restricted)"
  match { interface = "eth1.20" }
  match { vlan = 20 }
}

zone "dmz" {
  description = "De-Militarized Zone"
  match { interface = "eth2" }
}

zone "vpn" {
  description = "Remote Access VPN"
  match { interface = "wg0" }
  match { interface = "wg+" } # Wildcard match for dynamic peers
  
  management {
    web_ui = true
    ssh    = true
  }
}

# =============================================================================
# Policies: Traffic Control
# =============================================================================

# LAN -> Internet: Allow all
policy "lan" "wan" {
  masquerade = true
  rule "allow_internet" {
    action = "accept"
  }
}

# IoT -> Internet: Allow cloud connectivity
policy "iot" "wan" {
  rule "allow_cloud" {
    action = "accept"
  }
}

# LAN -> IoT: Management access
policy "lan" "iot" {
  rule "manage_devices" {
    action = "accept"
  }
}

# IoT -> LAN: Block Everything (Log attempts)
policy "iot" "lan" {
  rule "log_and_drop" {
    action = "drop"
    log    = true
    log_prefix = "[IoT-Blocked] "
  }
}

# WAN -> DMZ: Web Server Access
policy "wan" "dmz" {
  rule "https_server" {
    proto = "tcp"
    dest_port = 443
    dest_ip   = "10.0.50.10"
    action   = "accept"
  }
}

# VPN -> LAN: Allow remote access
policy "vpn" "lan" {
  rule "remote_admin" {
    action = "accept"
  }
}

# =============================================================================
# NAT & Port Forwarding
# =============================================================================

# Port Forwarding: HTTPS to DMZ Server
nat "https_forward" {
  type         = "dnat"
  in_interface = "eth0"
  proto = "tcp"
  dest_port    = "443"
  to_ip        = "10.0.50.10"
  to_port      = "443"
}

# =============================================================================
# Services: DHCP, DNS, VPN
# =============================================================================

# DHCP Server
dhcp {
  enabled = true

  # LAN Scope
  scope "lan" {
    interface   = "eth1"
    range_start = "10.0.0.100"
    range_end   = "10.0.0.200"
    router      = "10.0.0.1"
    dns         = ["10.0.0.1"] # Advertise self as DNS
  }

  # IoT Scope (Isolated)
  scope "iot" {
    interface   = "eth1.20"
    range_start = "10.0.0.200"
    range_end   = "10.0.20.200"
    router      = "10.0.20.1"
    dns         = ["1.1.1.1"] # Use public DNS for IoT
  }
}

# DNS Server (Recursive + Forwarding)
# DNS Server (Recursive + Forwarding)
dns {
  mode       = "forward"
  forwarders = ["8.8.8.8", "1.1.1.1"]
  
  # Serve DNS to LAN and DMZ
  serve "lan" {
    local_domain = "home.lab"
    
    # Local DNS records
    host "10.0.0.1" { hostnames = ["router.home.lab"] }
    host "10.0.0.10" { hostnames = ["server.dmz.lab"] }

    # Block ads/malware
    blocklist "stevenblack" {
      url = "https://raw.githubusercontent.com/StevenBlack/hosts/master/hosts"
    }
  }

  serve "dmz" {
    local_domain = "dmz.lab"
  }
}

# WireGuard Server
vpn {
  wireguard "wg0" {
    enabled     = true
    interface   = "wg0"
    private_key_file = "/etc/glacic/wg0.key"
    listen_port = 51820
    address     = ["10.10.0.1/24"]
    
    peer "phone" {
      public_key = "G+w..." 
      allowed_ips = ["10.10.0.2/32"]
    }
    
    peer "laptop" {
      public_key = "X/y..."
      allowed_ips = ["10.10.0.3/32"]
    }
  }
}
