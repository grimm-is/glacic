schema_version = "1.0"
interface "eth0" {
  description = "WAN"
  ipv4 = ["100.64.0.1/24"]
}

interface "eth1" {
  description = "LAN"
  ipv4 = ["192.168.1.1/24"]
}

zone "wan" {
  match { interface = "eth0" }
}

zone "lan" {
  match { interface = "eth1" }
}

# Standard Port Forward with Hairpin enabled
nat "web_server" {
  type = "dnat"
  in_interface = "eth0"
  dest_port = "80"
  to_ip = "192.168.1.10"
  to_port = "8080"
  hairpin = true
}
