# Complex Routing Configuration (BGP, OSPF, Policy Routing)
#
# This example demonstrates advanced enterprise-grade features:
# 1. Dynamic Routing via FRR (BGP & OSPF)
# 2. Custom Routing Tables
# 3. Mark Rules for Traffic Classification
# 4. Policy Routes based on Marks

schema_version = "1.0"
ip_forwarding = true

# -----------------------------------------------------------------------------
# Interfaces
# -----------------------------------------------------------------------------

interface "eth0" {
  description = "Datacenter Uplink"
  zone        = "backbone"
  ipv4        = ["10.255.0.2/30"]
}

interface "eth1" {
  description = "Office LAN"
  zone        = "office"
  ipv4        = ["10.10.0.1/24"]
}

interface "eth2" {
  description = "Public WiFi"
  zone        = "guest"
  ipv4        = ["10.20.0.1/24"]

  # Separate routing table for Guests to enforce isolation from Office
  table = 100
}

# -----------------------------------------------------------------------------
# Dynamic Routing (FRR)
# -----------------------------------------------------------------------------

frr {
  enabled = true

  # OSPF for internal routing (Office LAN)
  ospf {
    router_id = "10.255.0.2"

    area "0.0.0.0" {
      networks = ["10.10.0.0/24", "10.255.0.0/30"]
    }
  }

  # BGP for external connectivity
  bgp {
    asn       = 65001
    router_id = "10.255.0.2"

    neighbor "10.255.0.1" {
      remote_asn = 65000
    }

    networks = ["10.20.0.0/24"] # Advertise Guest network via BGP
  }
}

# -----------------------------------------------------------------------------
# Custom Routing Tables
# -----------------------------------------------------------------------------

routing_table "guest-internet" {
  id = 100

  # Guest network default route goes via specific gateway (e.g., cheaper line)
  # or in this case, we might just use the main table but isolated.
  # Here we define a static default route specific to this table.
  route "default-guest" {
    destination = "0.0.0.0/0"
    gateway     = "10.255.0.1"
  }
}

routing_table "voip-traffic" {
  id = 200

  # Dedicated line for VoIP
  route "default-voip" {
    destination = "0.0.0.0/0"
    gateway     = "10.255.0.5" # Secondary uplink
  }
}

# -----------------------------------------------------------------------------
# Traffic Classification (Mark Rules)
# -----------------------------------------------------------------------------

# Mark VoIP traffic (SIP/RTP) for special handling
mark_rule "mark-sip" {
  proto       = "udp"
  dst_port    = 5060
  mark        = "0x40" # Mark 64
  comment     = "Mark SIP Signaling"
}

mark_rule "mark-rtp" {
  proto       = "udp"
  dst_ports   = [10000, 20000] # Simplification: Port range support varies
  # Ideally we use connmark to mark the whole call related flows
  mark        = "0x40"
  comment     = "Mark RTP Media"
}

# -----------------------------------------------------------------------------
# Policy Routing
# -----------------------------------------------------------------------------

# Route marked VoIP traffic via dedicated VoIP table
policy_route "route-voip" {
  mark     = "0x40"
  table    = 200
  priority = 100
  comment  = "Send VoIP traffic via dedicated uplink"
}

# Ensure Guest traffic (from eth2) stays in Table 100
# (Interface 'table=100' config provides implicit rules, but we can be explicit)
policy_route "force-guest-table" {
  iif      = "eth2"
  table    = 100
  priority = 50
}

# -----------------------------------------------------------------------------
# Firewall Policies
# -----------------------------------------------------------------------------

# Backbone to Router (BGP/OSPF/SSH)
policy "backbone" "self" {
  rule "allow-routing-protocols" {
    proto = "ospf"
    action = "accept"
  }
  rule "allow-bgp" {
    proto = "tcp"
    dest_port = 179
    action = "accept"
  }
  rule "allow-ssh-admin" {
    proto = "tcp"
    dest_port = 22
    src_ip    = "10.255.0.1" # Only upstream router admin
    action    = "accept"
  }
}

# Office to Internet (Backbone)
policy "office" "backbone" {
  action = "accept"
}

# Guest to Internet (Backbone)
policy "guest" "backbone" {
  action = "accept"
}

# ISOLATION: No policy "office" "guest" or "guest" "office"
# Default drop ensures network isolation.
