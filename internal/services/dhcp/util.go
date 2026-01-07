package dhcp

import "net"

func parseIPs(ips []string) []net.IP {
	var ret []net.IP
	for _, s := range ips {
		if ip := net.ParseIP(s); ip != nil {
			ret = append(ret, ip)
		}
	}
	return ret
}
