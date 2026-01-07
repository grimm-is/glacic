# dec/16/2025 10:00:00 by RouterOS 7.1.1
/interface wireguard
add listen-port=13231 mtu=1420 name=wg0
add listen-port=13232 name=wg1
/ip address
add address=10.100.0.1/24 interface=wg0 network=10.100.0.0
