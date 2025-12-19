schema_version = "1.0"

zone "lan" {
  networks = ["127.0.0.0/8"]
}

dns_server {
  enabled = true
  listen_on = ["0.0.0.0"] # Listen on all

  host "1.2.3.4" {
    hostnames = ["test.local"]
  }
}
