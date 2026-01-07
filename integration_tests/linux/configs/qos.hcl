schema_version = "1.0"
ip_forwarding = true

interface "eth0" {
  dhcp = true
}

interface "lo" {
}

zone "wan" {
  interface = "eth0"
}
zone "lan" {
  interface = "lo"
}

# QoS Policy for LAN (lo)
qos_policy "lan-qos" {
  interface = "lo"
  enabled = true
  download_mbps = 100
  upload_mbps = 100

  # High Priority Class (VoIP)
  class "voip" {
    priority = 1
    rate = "10mbit"
    ceil = "100mbit"
    burst = "15k"
  }

  # Normal Priority Class
  class "web" {
    priority = 3
    rate = "50mbit"
    ceil = "100mbit"
    burst = "15k"
  }

  # Classification Rules
  rule "ssh-high-prio" {
    class = "voip"
    dest_port = 22
    proto = "tcp"
  }

  rule "http-normal" {
    class = "web"
    dest_port = 80
    proto = "tcp"
  }
}
