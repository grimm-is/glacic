# Wildcard Policy Example
# Demonstrates the use of * and glob patterns in policies

schema_version = "1.0"
ip_forwarding = true

interface "eth0" {
  zone = "wan"
  dhcp = true
}

interface "eth1" {
  zone = "lan"
  ipv4 = ["192.168.1.1/24"]
}

interface "eth2" {
  zone = "vpn_office"
  ipv4 = ["10.10.0.1/24"]
}

interface "eth3" {
  zone = "vpn_home"
  ipv4 = ["10.20.0.1/24"]
}

# Wildcard policy: all VPN zones can reach WAN
policy "vpn*" "wan" {
  rule "allow-all" {
    action = "accept"
  }
}

# Wildcard policy: LAN can reach any zone
policy "lan" "*" {
  rule "allow-all" {
    action = "accept"
  }
}

# Standard policy: WAN to self
policy "wan" "self" {
  rule "allow-ssh" {
    proto = "tcp"
    dest_port = 22
    action = "accept"
  }
}

nat "masquerade" {
  type = "masquerade"
  out_interface = "eth0"
}
