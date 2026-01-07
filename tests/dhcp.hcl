schema_version = "1.0"

# Interface definition needed for reference
interface "lo" {
  zone = "lan"
  ipv4 = ["192.168.1.1/24"]
}

zone "lan" {
  interfaces = ["lo"]
}

dhcp {
  enabled = true

  scope "lan-scope" {
    interface = "lo"
    range_start = "192.168.1.100"
    range_end = "192.168.1.200"
    router = "192.168.1.1"
    lease_time = "1h"
  }
}
