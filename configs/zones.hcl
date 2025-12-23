schema_version = "1.0"
ip_forwarding = true

# Security Protection (anti-spoofing, bogon filtering, rate limiting)
# Note: Using global_protection for backward compatibility
global_protection {
  anti_spoofing        = true   # Drop spoofed private IPs on WAN
  bogon_filtering      = true   # Drop reserved/invalid IP ranges
  invalid_packets      = true   # Drop malformed packets
  syn_flood_protection = true   # Rate limit SYN packets
  syn_flood_rate       = 25     # SYN packets per second
  syn_flood_burst      = 50     # Allow burst
  icmp_rate_limit      = true   # Rate limit ICMP
  icmp_rate            = 10     # ICMP packets per second
}

# WAN Interface
interface "eth0" {
  description = "WAN Link"
  dhcp        = true
}

# Green Zone - Full internet access, can access orange
interface "eth1" {
  description = "Green Zone (Trusted)"
  ipv4        = ["10.1.0.1/24"]
}

# Orange Zone - DMZ, can access green and red, plus internet
interface "eth2" {
  description = "Orange Zone (DMZ)"
  ipv4        = ["10.2.0.1/24"]
}

# Red Zone - Isolated, can only provide services to orange
interface "eth3" {
  description = "Red Zone (Isolated)"
  ipv4        = ["10.3.0.1/24"]
}

# Zone Definitions
zone "WAN" {
  interface = "eth0"
}

zone "Green" {
  interface = "eth1"
}

zone "Orange" {
  interface = "eth2"
}

zone "Red" {
  interface = "eth3"
}

# DHCP Server for all zones
dhcp {
  enabled = true
  scope "green-scope" {
    interface = "eth1"
    range_start = "10.1.0.100"
    range_end   = "10.1.0.200"
    router       = "10.1.0.1"
    dns         = ["10.1.0.1", "8.8.8.8"]
  }
  scope "orange-scope" {
    interface = "eth2"
    range_start = "10.2.0.100"
    range_end   = "10.2.0.200"
    router       = "10.2.0.1"
    dns         = ["10.2.0.1", "8.8.8.8"]
  }
  scope "red-scope" {
    interface = "eth3"
    range_start = "10.3.0.100"
    range_end   = "10.3.0.200"
    router       = "10.3.0.1"
    dns         = ["10.3.0.1", "8.8.8.8"]
  }
}

# DNS Server for all zones
dns_server {
  enabled = true
  listen_on = ["10.1.0.1", "10.2.0.1", "10.3.0.1"]
  forwarders = ["8.8.8.8", "1.1.1.1"]

  zone "home.arpa." {
    record "firewall" {
      type = "A"
      value = "10.1.0.1"
      ttl = 300
    }
  }
}

# Zone-based Firewall Policies
policy "Green" "Orange" {
  name = "green_to_orange"

  rule "allow_all" {
    description = "Allow Green zone to access Orange zone services"
    action = "accept"
  }
}

policy "Orange" "Green" {
  name = "orange_to_green"

  rule "allow_all" {
    description = "Allow Orange zone to access Green zone services"
    action = "accept"
  }
}

policy "Orange" "Red" {
  name = "orange_to_red"

  rule "allow_all" {
    description = "Allow Orange zone to access Red zone services"
    action = "accept"
  }
}

policy "Red" "Green" {
  name = "red_isolation"

  rule "block_all" {
    description = "Block Red zone from accessing other zones"
    action = "drop"
  }
}

policy "Red" "Orange" {
  name = "red_to_orange_block"

  rule "block_all" {
    description = "Block Red zone from accessing Orange zone"
    action = "drop"
  }
}

# Internet Access Policies (via WAN)
policy "Green" "WAN" {
  name = "green_to_wan"

  rule "allow_internet" {
    description = "Allow Green zone full internet access"
    action = "accept"
  }
}

policy "Orange" "WAN" {
  name = "orange_to_wan"

  rule "allow_internet" {
    description = "Allow Orange zone full internet access"
    action = "accept"
  }
}

policy "Red" "WAN" {
  name = "red_to_wan_block"

  rule "block_internet" {
    description = "Block Red zone from internet access"
    action = "drop"
  }
}

# Input policies for zone-to-firewall access (using service helpers)
policy "Green" "self" {
  name = "green_to_firewall"

  # Using service helpers - much cleaner than specifying ports!
  rule "allow_dns" {
    description = "Allow DNS access to firewall"
    services    = ["dns"]  # Expands to tcp/53 + udp/53
    action      = "accept"
  }

  rule "allow_dhcp" {
    description = "Allow DHCP access"
    services    = ["dhcp"]  # Expands to udp/67 + udp/68
    action      = "accept"
  }

  rule "allow_api" {
    description = "Allow API/Web UI access to firewall"
    proto       = "tcp"
    dest_port   = 8080
    action      = "accept"
  }

  rule "allow_ping" {
    description = "Allow ICMP ping"
    services    = ["ping"]
    action      = "accept"
  }
}

# Orange zone access to firewall
policy "Orange" "self" {
  name = "orange_to_firewall"

  rule "allow_dns" {
    description = "Allow DNS"
    services    = ["dns"]
    action      = "accept"
  }

  rule "allow_dhcp" {
    services = ["dhcp"]
    action   = "accept"
  }
}

# Red zone - very limited firewall access
policy "Red" "self" {
  name = "red_to_firewall"

  rule "allow_dhcp" {
    description = "Only allow DHCP for IP assignment"
    services    = ["dhcp"]
    action      = "accept"
  }

  rule "block_all" {
    description = "Block all other access from Red zone"
    action      = "drop"
    log         = true
    log_prefix  = "RED-FW-DROP: "
  }
}

# NAT Masquerading for zones with internet access
nat "green_masquerade" {
  type         = "masquerade"
  out_interface = "eth0"
}

nat "orange_masquerade" {
  type         = "masquerade"
  out_interface = "eth0"
}
