schema_version = "1.0"
ip_forwarding = true

# WAN Interface
interface "eth0" {
  description = "WAN Link"
  dhcp        = true
}

# Green Zone - Full internet access, can access orange
interface "veth-green" {
  description = "Green Zone (Trusted)"
  ipv4        = ["10.1.0.1/24"]
}

# Orange Zone - DMZ, can access green and red, plus internet
interface "veth-orange" {
  description = "Orange Zone (DMZ)"
  ipv4        = ["10.2.0.1/24"]
}

# Red Zone - Isolated, no internet access
interface "veth-red" {
  description = "Red Zone (Isolated)"
  ipv4        = ["10.3.0.1/24"]
}

# Zone Definitions
zone "WAN" {
  interface = "eth0"
}

zone "Green" {
  interface = "veth-green"
}

zone "Orange" {
  interface = "veth-orange"
}

zone "Red" {
  interface = "veth-red"
}

# DHCP Server for all zones
dhcp {
  enabled = true
  scope "green-scope" {
    interface = "veth-green"
    range_start = "10.1.0.100"
    range_end   = "10.1.0.200"
    router       = "10.1.0.1"
    dns         = ["10.1.0.1", "8.8.8.8"]
  }
  scope "orange-scope" {
    interface = "veth-orange"
    range_start = "10.2.0.100"
    range_end   = "10.2.0.200"
    router       = "10.2.0.1"
    dns         = ["10.2.0.1", "8.8.8.8"]
  }
  scope "red-scope" {
    interface = "veth-red"
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

# Zone Policies

policy "Green" "Orange" {
  name = "green_to_orange_allow"

  rule "allow_all" {
    description = "Allow all traffic from Green to Orange"
    action = "accept"
  }
}

policy "Orange" "Green" {
  name = "orange_to_green_allow"

  rule "allow_all" {
    description = "Allow all traffic from Orange to Green"
    action = "accept"
  }
}

policy "Orange" "Red" {
  name = "orange_to_red_allow"

  rule "allow_all" {
    description = "Allow all traffic from Orange to Red"
    action = "accept"
  }
}

policy "Red" "Orange" {
  name = "red_to_orange_block"

  rule "block_all" {
    description = "Block Red zone from Orange zone"
    action = "drop"
  }
}

policy "Red" "Green" {
  name = "red_to_green_block"

  rule "block_all" {
    description = "Block Red zone from Green zone"
    action = "drop"
  }
}

policy "Green" "WAN" {
  name = "green_to_wan_allow"

  rule "allow_internet" {
    description = "Allow Green zone internet access"
    action = "accept"
  }
}

policy "Orange" "WAN" {
  name = "orange_to_wan_allow"

  rule "allow_internet" {
    description = "Allow Orange zone internet access"
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

# Input policies for zone-to-firewall access
policy "Green" "self" {
  name = "green_to_firewall"

  rule "allow_icmp" {
    description = "Allow ICMP (ping) to firewall"
    proto     = "icmp"
    action    = "accept"
  }

  rule "allow_dns" {
    description = "Allow DNS access to firewall"
    proto     = "udp"
    dest_port = 53
    action    = "accept"
  }

  rule "allow_dns_tcp" {
    description = "Allow DNS TCP access to firewall"
    proto     = "tcp"
    dest_port = 53
    action    = "accept"
  }

  rule "allow_api" {
    description = "Allow API access to firewall"
    proto     = "tcp"
    dest_port = 8080
    action    = "accept"
  }

  rule "allow_dhcp" {
    description = "Allow DHCP requests"
    proto     = "udp"
    dest_port = 67
    action    = "accept"
  }
}

policy "Orange" "self" {
  name = "orange_to_firewall"

  rule "allow_icmp" {
    description = "Allow ICMP (ping) to firewall"
    proto     = "icmp"
    action    = "accept"
  }

  rule "allow_dns" {
    description = "Allow DNS access to firewall"
    proto     = "udp"
    dest_port = 53
    action    = "accept"
  }

  rule "allow_dhcp" {
    description = "Allow DHCP requests"
    proto     = "udp"
    dest_port = 67
    action    = "accept"
  }
}

policy "Red" "self" {
  name = "red_to_firewall"

  rule "allow_icmp" {
    description = "Allow ICMP (ping) to firewall"
    proto     = "icmp"
    action    = "accept"
  }

  rule "allow_dns" {
    description = "Allow DNS access to firewall"
    proto     = "udp"
    dest_port = 53
    action    = "accept"
  }

  rule "allow_dhcp" {
    description = "Allow DHCP requests"
    proto     = "udp"
    dest_port = 67
    action    = "accept"
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
