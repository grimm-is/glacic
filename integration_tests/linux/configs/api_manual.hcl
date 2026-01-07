schema_version = "1.0"

ip_forwarding = false
features {
  threat_intel     = true
  network_learning = true
  qos              = true
}
api {
  listen       = "127.0.0.1:8080"
  cors_origins = ["*"]
}
system {
  sysctl_profile = "default"
}
