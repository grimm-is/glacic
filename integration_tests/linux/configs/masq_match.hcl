schema_version = "1.0"
interface "eth0" {
  description = "WAN"
  dhcp = true
}

interface "eth1" {
  description = "LAN"
  ipv4 = ["10.0.0.1/24"]
}

zone "wan" {
  match { interface = "eth0" }
}

zone "lan" {
  match { interface = "eth1" }
}

policy "lan" "wan" {
  action = "accept"
  masquerade = true
}
