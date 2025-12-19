# Demo Firewall Configuration
# Basic setup for demonstration purposes

ip_forwarding = true

# API Configuration (Disable Sandbox for Demo)
api {
  enabled = true
  disable_sandbox = true
  listen = "0.0.0.0:8080"
}

# Interface configuration
interface "eth0" {
  description = "WAN Interface"
  dhcp        = true
  access_web_ui = true
  web_ui_port = 8080
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

interface "eth4" {
  description = "Management Interface"
  zone        = "mgmt"
  dhcp        = true
  access_web_ui = true
  web_ui_port = 8080
}

zone "mgmt" {
  description = "Trusted Management Zone"
  management {
    web_ui = true
    api    = true
    ssh    = true
    icmp   = true
  }
}

# Basic firewall policies
policy "green_to_wan" {
  from = "Green"
  to   = "WAN"
  
  rule "allow_internet" {
    description = "Allow Green zone internet access"
    action = "accept"
  }
}

policy "orange_to_wan" {
  from = "Orange"
  to   = "WAN"
  
  rule "allow_internet" {
    description = "Allow Orange zone internet access"
    action = "accept"
  }
}

policy "red_blocked" {
  from = "Red"
  to   = "WAN"
  
  rule "block_internet" {
    description = "Block Red zone from internet"
    action = "drop"
  }
}

# NAT for outbound traffic
nat "outbound" {
  type          = "masquerade"
  out_interface = "eth0"
}
