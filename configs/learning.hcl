# Glacic Learning Engine Configuration Example
# This enables the "Trust On First Use" (TOFU) model for network flow management

schema_version = "1.0"

# Enable the Glacic learning engine
rule_learning {
  enabled = true

  # TOFU Mode: When true, new flows are auto-frozen (allowed) during initial setup
  # When false, new flows are pending (dropped) until manually triaged
  learning_mode = true

  # nflog group for capturing unknown traffic
  log_group = 100

  # Rate limit for learning (prevents flooding)
  rate_limit = "10/minute"

  # How long to keep pending flows before cleanup
  retention_days = 30

  # Networks to ignore from learning (internal traffic, etc.)
  ignore_networks = [
    "127.0.0.0/8",      # Loopback
    "169.254.0.0/16",   # Link-local
  ]
}

# Example zones for learning
zone "lan" {
  # LAN covers both bridge and physical interface
  match {
    interface = "br-lan"
  }
  match {
    interface = "eth1"
  }
  description = "Local Area Network"

  services {
    dhcp = true
    dns = true
    ntp = true
  }

  management {
    web_ui = true
    ssh = true
    icmp = true
  }
}

zone "wan" {
  interface = "eth0"
  description = "Wide Area Network (Internet)"
}

# Example policy that triggers learning
policy "lan" "wan" {
  name = "lan_wan"
  action = "drop"  # Unknown traffic is dropped (or learned if learning_mode=true)

  # Explicit rules (these bypass learning)
  rule "allow_http" {
    proto = "tcp"
    dest_port = 80
    action = "accept"
  }

  rule "allow_https" {
    proto = "tcp"
    dest_port = 443
    action = "accept"
  }

  rule "allow_dns" {
    proto = "udp"
    dest_port = 53
    action = "accept"
  }
}
