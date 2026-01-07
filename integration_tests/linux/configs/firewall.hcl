schema_version = "1.0"
ip_forwarding = true

# Use network-based zones for environment compatibility
zone "wan" {
  networks = ["10.0.2.0/24"]
}

zone "lan" {
  networks = ["192.168.1.0/24"]
}

policy "lan_to_wan" {
  from = "lan"
  to = "wan"
  action = "accept"
}

policy "wan_to_lan" {
  from = "wan"
  to = "lan"
  action = "drop"
  rule "allow_established" {
    conn_state = "established,related"
    action = "accept"
  }
}

nat "wan_masq" {
  type = "masquerade"
  out_interface = "lo" # Use lo as it matches all traffic in this minimal env
}
