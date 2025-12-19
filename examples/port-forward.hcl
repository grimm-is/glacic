# Port Forwarding (DNAT) Configuration
#
# This example demonstrates how to expose internal services to the internet
# using Network Address Translation (NAT) rules.

schema_version = "1.0"
ip_forwarding = true

# -----------------------------------------------------------------------------
# Interfaces
# -----------------------------------------------------------------------------

interface "eth0" {
  description = "WAN"
  zone        = "wan"
  dhcp        = true
}

interface "eth1" {
  description = "LAN"
  zone        = "lan"
  ipv4        = ["10.0.0.1/24"]
}

# -----------------------------------------------------------------------------
# NAT (Port Forwards)
# -----------------------------------------------------------------------------

# 1. Simple Web Server Forwarding
# Forward TCP port 80 on WAN IP to internal web server 10.0.0.10
nat "web-server" {
  type        = "dnat"
  in_interface = "eth0"
  proto       = "tcp"
  dest_port   = "80"
  to_ip       = "10.0.0.10"
  to_port     = "80"
}

# 2. Forward SSH with Port Translation
# Forward TCP port 2222 on WAN to standard SSH (22) on internally server
nat "ssh-server" {
  type        = "dnat"
  in_interface = "eth0"
  proto       = "tcp"
  dest_port   = "2222"
  to_ip       = "10.0.0.20"
  to_port     = "22"
}

# 3. Restricted Access Forwarding (Source IP Limit)
# Only allow specific remote IP (Office VPN) to access Admin Panel
nat "admin-panel" {
  type        = "dnat"
  in_interface = "eth0"
  src_ip      = "203.0.113.50" # Only this source IP matches
  proto       = "tcp"
  dest_port   = "8443"
  to_ip       = "10.0.0.10"
  to_port     = "443"
}

# 4. Range Forwarding (Gaming/Apps)
# Forward UDP ports 10000-10100
# Supported via string ranges in dest_port
nat "game-server" {
  type        = "dnat"
  in_interface = "eth0"
  proto       = "udp"
  dest_port   = "10000-10100"
  to_ip       = "10.0.0.50"
  # Ports are mapped 1:1 by default if to_port is omitted or we can specify range if supported by nftables dnat
  # Generally dnat to IP preserves port if not specified, or maps range-to-range if range specified.
  # For this example, let's just forward to the IP.
}

# Masquerade outbound traffic (Standard Internet Access)
nat "masquerade" {
  type          = "masquerade"
  out_interface = "eth0"
}

# -----------------------------------------------------------------------------
# Firewall Policies
# -----------------------------------------------------------------------------
# Note: DNAT happens in PREROUTING. The firewall filter rules (FORWARD/INPUT)
# see the packet AFTER destination IP is changed.
# So we must allow traffic to the INTERNAL IP (10.0.0.x).

policy "wan" "lan" {
  # Allow traffic to Web Server
  rule "allow-web" {
    proto      = "tcp"
    dest_ip    = "10.0.0.10"
    dest_port  = 80
    action     = "accept"
  }

  # Allow traffic to SSH Server
  rule "allow-ssh" {
    proto      = "tcp"
    dest_ip    = "10.0.0.20"
    dest_port  = 22
    action     = "accept"
  }

  # Allow traffic to Admin Panel
  rule "allow-admin" {
    proto      = "tcp"
    src_ip     = "203.0.113.50" # Redundant check but good security practice
    dest_ip    = "10.0.0.10"
    dest_port  = 443
    action     = "accept"
  }

  rule "allow-game" {
    proto      = "udp"
    dest_ip    = "10.0.0.50"
    dest_port  = 10000
    action     = "accept"
  }
}
