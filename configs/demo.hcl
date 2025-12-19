# Demo Firewall Configuration
# Basic setup for demonstration purposes

ip_forwarding = true

# Interface configuration
interface "eth0" {
  description = "WAN Interface"
  zone        = "WAN"
  dhcp        = true
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

# Basic firewall policies
policy "Green" "WAN" {
  name = "green_to_wan"

  rule "allow_internet" {
    description = "Allow Green zone internet access"
    action = "accept"
  }
}

policy "Orange" "WAN" {
  name = "orange_to_wan"

  rule "allow_internet" {
    description = "Allow Orange zone internet access"
    action = "accept"
  }
}

policy "Red" "WAN" {
  name = "red_blocked"

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
