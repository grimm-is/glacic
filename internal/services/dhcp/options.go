package dhcp

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/insomniacslk/dhcp/dhcpv4"
)

// parseOption parses a DHCP option from key-value configuration.
//
// Named options (~40 supported):
//
//	dns_server, tftp_server, ntp_server, router, bootfile, domain_name, mtu, etc.
//	Type auto-detected, prefix optional: dns_server = "8.8.8.8"
//
// Numeric codes (1-255):
//
//	Must specify type prefix: "66" = "str:tftp.boot", "150" = "ip:192.168.1.10"
//
// Type prefixes:
//
//	ip:    IP address or comma-separated list
//	str:   String (text/str are aliases)
//	hex:   Hex string (e.g., "010203" or "0x01:02:03")
//	u8:    8-bit unsigned integer (0-255)
//	u16:   16-bit unsigned integer (0-65535)
//	u32:   32-bit unsigned integer
//	bool:  Boolean (true/false, 1/0)
//
// Examples:
//
//	dns_server = "8.8.8.8"              // Named option, auto-typed
//	"150" = "ip:192.168.1.10"           // Numeric code, prefix required
//	vendor_specific = "010203"          // Named option, auto-typed as hex
func parseOption(key, value string) (dhcpv4.Option, error) {
	// Parse option code (numeric or named)
	var code dhcpv4.OptionCode

	// Handle named options first (legacy support + human-readable aliases)
	// Based on RFC 2132 and common industry usage
	switch strings.ToLower(strings.ReplaceAll(key, "-", "_")) {
	// Network Configuration (1-9)
	case "subnet_mask", "subnet":
		code = dhcpv4.OptionSubnetMask
	case "time_offset":
		code = dhcpv4.OptionTimeOffset
	case "router", "gateway", "default_gateway":
		code = dhcpv4.OptionRouter
	case "time_server":
		code = dhcpv4.OptionTimeServer
	case "name_server", "ien116_name_server":
		code = dhcpv4.OptionNameServer
	case "dns_server", "dns", "domain_name_server":
		code = dhcpv4.OptionDomainNameServer
	case "log_server":
		code = dhcpv4.OptionLogServer
	case "quote_server", "cookie_server":
		code = dhcpv4.GenericOptionCode(8)
	case "lpr_server", "print_server":
		code = dhcpv4.GenericOptionCode(9)

	// Hostname and Domain (12, 15)
	case "hostname", "host_name":
		code = dhcpv4.OptionHostName
	case "domain_name", "domain":
		code = dhcpv4.OptionDomainName

	// Boot and Path (16-19)
	case "swap_server":
		code = dhcpv4.GenericOptionCode(16)
	case "root_path":
		code = dhcpv4.OptionRootPath
	case "extensions_path":
		code = dhcpv4.GenericOptionCode(18)

	// IP Layer (19-27)
	case "ip_forwarding":
		code = dhcpv4.GenericOptionCode(19)
	case "non_local_source_routing":
		code = dhcpv4.GenericOptionCode(20)
	case "mtu", "interface_mtu":
		code = dhcpv4.OptionInterfaceMTU
	case "broadcast_address":
		code = dhcpv4.OptionBroadcastAddress

	// ARP and TCP (27-40)
	case "arp_cache_timeout":
		code = dhcpv4.GenericOptionCode(35)
	case "tcp_ttl":
		code = dhcpv4.GenericOptionCode(37)
	case "tcp_keepalive_interval":
		code = dhcpv4.GenericOptionCode(38)

	// Application Services (42-76)
	case "ntp_server", "ntp_servers", "ntp":
		code = dhcpv4.OptionNTPServers
	case "vendor_specific", "vendor_specific_info":
		code = dhcpv4.GenericOptionCode(43)
	case "netbios_name_server", "wins_server":
		code = dhcpv4.GenericOptionCode(44)
	case "netbios_datagram_distribution_server":
		code = dhcpv4.GenericOptionCode(45)
	case "netbios_node_type":
		code = dhcpv4.GenericOptionCode(46)
	case "netbios_scope":
		code = dhcpv4.GenericOptionCode(47)
	case "x_window_font_server":
		code = dhcpv4.GenericOptionCode(48)
	case "x_window_display_manager":
		code = dhcpv4.GenericOptionCode(49)

	// DHCP Extensions (50-61)
	case "requested_ip", "requested_ip_address":
		code = dhcpv4.OptionRequestedIPAddress
	case "lease_time", "ip_address_lease_time":
		code = dhcpv4.OptionIPAddressLeaseTime
	case "option_overload":
		code = dhcpv4.GenericOptionCode(52)
	case "dhcp_message_type":
		code = dhcpv4.OptionDHCPMessageType
	case "server_identifier", "dhcp_server_id":
		code = dhcpv4.OptionServerIdentifier
	case "parameter_request_list":
		code = dhcpv4.GenericOptionCode(55)
	case "message":
		code = dhcpv4.OptionMessage
	case "max_dhcp_message_size":
		code = dhcpv4.GenericOptionCode(57)
	case "renewal_time", "t1":
		code = dhcpv4.GenericOptionCode(58)
	case "rebinding_time", "t2":
		code = dhcpv4.GenericOptionCode(59)
	case "vendor_class", "vendor_class_identifier":
		code = dhcpv4.GenericOptionCode(60)
	case "client_identifier", "client_id":
		code = dhcpv4.OptionClientIdentifier

	// Boot/PXE (66-67, 150)
	case "tftp_server", "tftp_server_name":
		code = dhcpv4.GenericOptionCode(66)
	case "bootfile", "bootfile_name", "boot_file":
		code = dhcpv4.OptionBootfileName
	case "tftp_server_ip", "tftp_server_address":
		code = dhcpv4.GenericOptionCode(150)

	// Advanced Routing (121, 249)
	case "classless_static_route", "static_route":
		code = dhcpv4.GenericOptionCode(121)
	case "classless_static_route_ms", "ms_classless_static_route":
		code = dhcpv4.GenericOptionCode(249)

	// Proxy and URL (252)
	case "wpad", "proxy_autodiscovery", "auto_proxy_config":
		code = dhcpv4.GenericOptionCode(252)

	// Legacy/Unsupported
	case "domain_search":
		// Domain search list (option 119) requires RFC 1035 label encoding.
		// Not implemented due to encoding complexity; use domain option instead.
		return dhcpv4.Option{}, fmt.Errorf("domain_search option not implemented; use domain option instead")
	default:
		// Try to parse numeric code
		if c, err := strconv.Atoi(key); err == nil && c > 0 && c <= 255 {
			code = dhcpv4.GenericOptionCode(c)
		} else {
			return dhcpv4.Option{}, fmt.Errorf("unknown DHCP option code: %s", key)
		}
	}

	// Detect value type based on prefix
	val := value
	typePrefix := ""
	if idx := strings.Index(val, ":"); idx > 0 {
		prefix := strings.ToLower(val[:idx])
		switch prefix {
		case "ip", "str", "text", "hex", "u8", "u16", "u32", "bool":
			typePrefix = prefix
			val = val[idx+1:]
		}
	}

	// If no type prefix, infer from option code
	if typePrefix == "" {
		switch code {
		// IP Address options (single or list)
		case dhcpv4.OptionSubnetMask,
			dhcpv4.OptionRouter,
			dhcpv4.OptionTimeServer,
			dhcpv4.OptionNameServer,
			dhcpv4.OptionDomainNameServer,
			dhcpv4.OptionLogServer,
			dhcpv4.OptionNTPServers,
			dhcpv4.OptionRequestedIPAddress,
			dhcpv4.OptionServerIdentifier,
			dhcpv4.OptionBroadcastAddress,
			dhcpv4.GenericOptionCode(8),   // Quote/Cookie Server
			dhcpv4.GenericOptionCode(9),   // LPR Server
			dhcpv4.GenericOptionCode(16),  // Swap Server
			dhcpv4.GenericOptionCode(44),  // NetBIOS Name Server
			dhcpv4.GenericOptionCode(45),  // NetBIOS DD Server
			dhcpv4.GenericOptionCode(48),  // X Window Font Server
			dhcpv4.GenericOptionCode(49),  // X Window Display Manager
			dhcpv4.GenericOptionCode(150): // TFTP Server IP
			typePrefix = "ip"

		// String options
		case dhcpv4.OptionHostName,
			dhcpv4.OptionDomainName,
			dhcpv4.OptionRootPath,
			dhcpv4.OptionBootfileName,
			dhcpv4.OptionMessage,
			dhcpv4.GenericOptionCode(18),  // Extensions Path
			dhcpv4.GenericOptionCode(47),  // NetBIOS Scope
			dhcpv4.GenericOptionCode(66),  // TFTP Server Name
			dhcpv4.GenericOptionCode(252): // WPAD
			typePrefix = "str"

		// Integer options (u8, u16, u32)
		case dhcpv4.OptionInterfaceMTU,
			dhcpv4.GenericOptionCode(57): // Max DHCP Message Size
			typePrefix = "u16"

		case dhcpv4.OptionTimeOffset,
			dhcpv4.OptionIPAddressLeaseTime,
			dhcpv4.GenericOptionCode(35), // ARP Cache Timeout
			dhcpv4.GenericOptionCode(38), // TCP Keepalive Interval
			dhcpv4.GenericOptionCode(58), // Renewal Time (T1)
			dhcpv4.GenericOptionCode(59): // Rebinding Time (T2)
			typePrefix = "u32"

		case dhcpv4.GenericOptionCode(19), // IP Forwarding
			dhcpv4.GenericOptionCode(20), // Non-local Source Routing
			dhcpv4.GenericOptionCode(37), // TCP TTL
			dhcpv4.GenericOptionCode(46), // NetBIOS Node Type
			dhcpv4.GenericOptionCode(52), // Option Overload
			dhcpv4.OptionDHCPMessageType:
			typePrefix = "u8"

		// Vendor-specific and complex options default to hex
		case dhcpv4.GenericOptionCode(43), // Vendor-Specific
			dhcpv4.GenericOptionCode(121), // Classless Static Route
			dhcpv4.GenericOptionCode(249): // MS Classless Static Route
			typePrefix = "hex"

		// Client Identifier can be various formats, default to hex
		case dhcpv4.OptionClientIdentifier,
			dhcpv4.GenericOptionCode(55), // Parameter Request List
			dhcpv4.GenericOptionCode(60): // Vendor Class Identifier
			typePrefix = "hex"

		default:
			// For unknown numeric codes, default to string
			typePrefix = "str"
		}
	}

	// Encode value based on type
	switch typePrefix {
	case "ip":
		ips := parseIPList(val)
		if len(ips) == 0 {
			return dhcpv4.Option{}, fmt.Errorf("invalid IP(s): %s", val)
		}
		// Special handling for NTP (returns explicit OptionNTPServers) vs generic
		if code == dhcpv4.OptionNTPServers {
			return dhcpv4.OptNTPServers(ips...), nil
		}

		// For other IP options, we flatten the bytes
		var b []byte
		for _, ip := range ips {
			if ip4 := ip.To4(); ip4 != nil {
				b = append(b, ip4...)
			} else {
				// DHCPv4 options typically only support IPv4 (4 bytes)
				// Some might support 16 bytes, but let's stick to IPv4 for safety
				return dhcpv4.Option{}, fmt.Errorf("DHCPv4 option requires IPv4 address: %s", ip)
			}
		}
		return dhcpv4.OptGeneric(code, b), nil

	case "str", "text":
		return dhcpv4.OptGeneric(code, []byte(val)), nil

	case "hex":
		b, err := hexDecode(val) // Helper needed
		if err != nil {
			return dhcpv4.Option{}, fmt.Errorf("invalid hex string: %w", err)
		}
		return dhcpv4.OptGeneric(code, b), nil

	case "u8":
		i, err := strconv.ParseUint(val, 10, 8)
		if err != nil {
			return dhcpv4.Option{}, fmt.Errorf("invalid u8 value: %v", err)
		}
		return dhcpv4.OptGeneric(code, []byte{uint8(i)}), nil

	case "u16":
		i, err := strconv.ParseUint(val, 10, 16)
		if err != nil {
			return dhcpv4.Option{}, fmt.Errorf("invalid u16 value: %v", err)
		}
		b := make([]byte, 2)
		binary.BigEndian.PutUint16(b, uint16(i))
		return dhcpv4.OptGeneric(code, b), nil

	case "u32":
		i, err := strconv.ParseUint(val, 10, 32)
		if err != nil {
			return dhcpv4.Option{}, fmt.Errorf("invalid u32 value: %v", err)
		}
		b := make([]byte, 4)
		binary.BigEndian.PutUint32(b, uint32(i))
		return dhcpv4.OptGeneric(code, b), nil

	case "bool":
		bVal, err := strconv.ParseBool(val)
		if err != nil {
			return dhcpv4.Option{}, fmt.Errorf("invalid boolean value: %v", err)
		}
		if bVal {
			return dhcpv4.OptGeneric(code, []byte{1}), nil
		}
		return dhcpv4.OptGeneric(code, []byte{0}), nil
	}

	return dhcpv4.Option{}, fmt.Errorf("parsing logic error for key: %s", key)
}

// hexDecode decodes a hex string, allowing optional "0x" prefix and skipping spaces/colons
func hexDecode(s string) ([]byte, error) {
	s = strings.TrimPrefix(s, "0x")
	s = strings.ReplaceAll(s, ":", "")
	s = strings.ReplaceAll(s, " ", "")
	return hex.DecodeString(s)
}
func parseIPList(s string) []net.IP {
	var ips []net.IP
	parts := strings.Split(s, ",")
	for _, p := range parts {
		ip := net.ParseIP(strings.TrimSpace(p))
		if ip != nil {
			ips = append(ips, ip)
		}
	}
	return ips
}
