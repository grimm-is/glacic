package config

import "sort"

// BuiltinService defines a registered service application.
type BuiltinService struct {
	Description string
	Ports       []ZoneServicePort
}

// BuiltinServices contains a list of common services for UI/Firewall use.
var BuiltinServices = map[string]BuiltinService{
	"ssh": {
		Description: "Secure Shell access",
		Ports: []ZoneServicePort{
			{Protocol: "tcp", Port: 22},
		},
	},
	"http": {
		Description: "Web Server (Unencrypted)",
		Ports: []ZoneServicePort{
			{Protocol: "tcp", Port: 80},
		},
	},
	"https": {
		Description: "Web Server (Encrypted)",
		Ports: []ZoneServicePort{
			{Protocol: "tcp", Port: 443},
		},
	},
	"dns": {
		Description: "Domain Name System",
		Ports: []ZoneServicePort{
			{Protocol: "udp", Port: 53},
			{Protocol: "tcp", Port: 53},
		},
	},
	"dhcp": {
		Description: "Dynamic Host Configuration Protocol",
		Ports: []ZoneServicePort{
			{Protocol: "udp", Port: 67},
			{Protocol: "udp", Port: 68},
		},
	},
	"ntp": {
		Description: "Network Time Protocol",
		Ports: []ZoneServicePort{
			{Protocol: "udp", Port: 123},
		},
	},
	"snmp": {
		Description: "Simple Network Management Protocol",
		Ports: []ZoneServicePort{
			{Protocol: "udp", Port: 161},
		},
	},
	"syslog": {
		Description: "Remote Syslog",
		Ports: []ZoneServicePort{
			{Protocol: "udp", Port: 514},
		},
	},
	"smtp": {
		Description: "Simple Mail Transfer Protocol",
		Ports: []ZoneServicePort{
			{Protocol: "tcp", Port: 25},
		},
	},
	"smb": {
		Description: "SMB / Windows File Sharing",
		Ports: []ZoneServicePort{
			{Protocol: "tcp", Port: 445},
			{Protocol: "tcp", Port: 139},
			{Protocol: "udp", Port: 137},
			{Protocol: "udp", Port: 138},
		},
	},
	"ldap": {
		Description: "Lightweight Directory Access Protocol",
		Ports: []ZoneServicePort{
			{Protocol: "tcp", Port: 389},
		},
	},
	"ldaps": {
		Description: "LDAP over SSL",
		Ports: []ZoneServicePort{
			{Protocol: "tcp", Port: 636},
		},
	},
	"imap": {
		Description: "Internet Message Access Protocol",
		Ports: []ZoneServicePort{
			{Protocol: "tcp", Port: 143},
		},
	},
	"imaps": {
		Description: "IMAP over SSL",
		Ports: []ZoneServicePort{
			{Protocol: "tcp", Port: 993},
		},
	},
	"pop3": {
		Description: "Post Office Protocol 3",
		Ports: []ZoneServicePort{
			{Protocol: "tcp", Port: 110},
		},
	},
	"pop3s": {
		Description: "POP3 over SSL",
		Ports: []ZoneServicePort{
			{Protocol: "tcp", Port: 995},
		},
	},
	"bgp": {
		Description: "Border Gateway Protocol",
		Ports: []ZoneServicePort{
			{Protocol: "tcp", Port: 179},
		},
	},
	"ftp": {
		Description: "File Transfer Protocol (Control)",
		Ports: []ZoneServicePort{
			{Protocol: "tcp", Port: 21},
		},
	},
	"ftp-data": {
		Description: "File Transfer Protocol (Data)",
		Ports: []ZoneServicePort{
			{Protocol: "tcp", Port: 20},
		},
	},
	"mysql": {
		Description: "MySQL Database",
		Ports: []ZoneServicePort{
			{Protocol: "tcp", Port: 3306},
		},
	},
	"postgres": {
		Description: "PostgreSQL Database",
		Ports: []ZoneServicePort{
			{Protocol: "tcp", Port: 5432},
		},
	},
	"wireguard": {
		Description: "WireGuard VPN",
		Ports: []ZoneServicePort{
			{Protocol: "udp", Port: 51820},
		},
	},
}

// LookupService finds a registered service by name.
func LookupService(name string) (BuiltinService, bool) {
	svc, ok := BuiltinServices[name]
	return svc, ok
}

// ListServices returns a sorted list of all registered service names.
func ListServices() []string {
	names := make([]string, 0, len(BuiltinServices))
	for name := range BuiltinServices {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
