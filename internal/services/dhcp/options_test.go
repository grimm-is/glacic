package dhcp

import (
	"testing"

	"github.com/insomniacslk/dhcp/dhcpv4"
)

func TestParseOption(t *testing.T) {
	tests := []struct {
		key      string
		value    string
		wantCode dhcpv4.OptionCode
		wantErr  bool
	}{
		{"ntp-server", "1.2.3.4", dhcpv4.OptionNTPServers, false},
		{"ntp-server", "1.2.3.4, 5.6.7.8", dhcpv4.OptionNTPServers, false},
		{"mtu", "1500", dhcpv4.OptionInterfaceMTU, false},
		{"invalid-name", "value", dhcpv4.GenericOptionCode(0), true},

		// Generic Codes
		{"66", "tftp.boot", dhcpv4.GenericOptionCode(66), false}, // Default string
		{"66", "str:tftp.boot", dhcpv4.GenericOptionCode(66), false},
		{"66", "text:tftp.boot", dhcpv4.GenericOptionCode(66), false},

		// Type verification
		{"42", "ip:1.2.3.4", dhcpv4.GenericOptionCode(42), false},
		{"43", "hex:010203", dhcpv4.GenericOptionCode(43), false},
		{"128", "u32:300", dhcpv4.GenericOptionCode(128), false},
		{"129", "u16:300", dhcpv4.GenericOptionCode(129), false},
		{"130", "u8:10", dhcpv4.GenericOptionCode(130), false},
		{"131", "bool:true", dhcpv4.GenericOptionCode(131), false},

		// Edge cases
		{"42", "ip:invalid", dhcpv4.GenericOptionCode(42), true},
		{"130", "u8:300", dhcpv4.GenericOptionCode(130), true}, // Overflow
		{"256", "value", dhcpv4.GenericOptionCode(0), true},    // Invalid code
		{"0", "value", dhcpv4.GenericOptionCode(0), true},      // Invalid code

		// Human-readable option names
		{"dns_server", "ip:8.8.8.8", dhcpv4.OptionDomainNameServer, false},
		{"dns", "ip:8.8.8.8", dhcpv4.OptionDomainNameServer, false}, // Alias
		{"tftp_server", "str:tftp.example.com", dhcpv4.GenericOptionCode(66), false},
		{"tftp-server", "str:tftp.example.com", dhcpv4.GenericOptionCode(66), false}, // Hyphen variant
		{"bootfile", "str:pxelinux.0", dhcpv4.OptionBootfileName, false},
		{"router", "ip:192.168.1.1", dhcpv4.OptionRouter, false},
		{"gateway", "ip:192.168.1.1", dhcpv4.OptionRouter, false}, // Alias
		{"subnet_mask", "ip:255.255.255.0", dhcpv4.OptionSubnetMask, false},
		{"domain_name", "str:example.com", dhcpv4.OptionDomainName, false},
		{"wins_server", "ip:192.168.1.10", dhcpv4.GenericOptionCode(44), false}, // NetBIOS
		{"wpad", "str:http://proxy.example.com/wpad.dat", dhcpv4.GenericOptionCode(252), false},

		// Auto-type inference (no prefix needed for named options)
		{"dns_server", "8.8.8.8", dhcpv4.OptionDomainNameServer, false},
		{"dns", "8.8.8.8,8.8.4.4", dhcpv4.OptionDomainNameServer, false},
		{"router", "192.168.1.1", dhcpv4.OptionRouter, false},
		{"ntp_server", "192.168.1.1", dhcpv4.OptionNTPServers, false},
		{"domain_name", "example.com", dhcpv4.OptionDomainName, false},
		{"tftp_server", "tftp.example.com", dhcpv4.GenericOptionCode(66), false},
		{"bootfile", "pxelinux.0", dhcpv4.OptionBootfileName, false},
		{"mtu", "1500", dhcpv4.OptionInterfaceMTU, false},
		{"subnet_mask", "255.255.255.0", dhcpv4.OptionSubnetMask, false},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			opt, err := parseOption(tt.key, tt.value)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseOption() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && opt.Code != tt.wantCode {
				t.Errorf("parseOption() code = %v, want %v", opt.Code, tt.wantCode)
			}
		})
	}
}

func TestParseIPList(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"1.2.3.4", 1},
		{"1.2.3.4, 5.6.7.8", 2},
		{"invalid", 0},
		{"1.2.3.4, invalid", 1},
	}

	for _, tt := range tests {
		got := parseIPList(tt.input)
		if len(got) != tt.want {
			t.Errorf("parseIPList(%q) length = %d, want %d", tt.input, len(got), tt.want)
		}
	}
}

func TestParseOption_MTU_Invalid(t *testing.T) {
	_, err := parseOption("mtu", "not-a-number")
	if err == nil {
		t.Error("expected error for invalid MTU")
	}
}

func TestParseOption_DomainSearch_NotSupported(t *testing.T) {
	_, err := parseOption("domain-search", "example.com,test.local")
	if err == nil {
		t.Error("expected error for unsupported domain-search")
	}
}

func TestParseOption_NTPServer_Empty(t *testing.T) {
	_, err := parseOption("ntp-server", "invalid-ip")
	if err == nil {
		t.Error("expected error for empty NTP server IPs")
	}
}

func TestParseIPList_MultipleIPs(t *testing.T) {
	ips := parseIPList("10.0.0.1, 10.0.0.2, 10.0.0.3")
	if len(ips) != 3 {
		t.Errorf("expected 3 IPs, got %d", len(ips))
	}
}

func TestParseIPList_IPv6(t *testing.T) {
	ips := parseIPList("::1, 2001:db8::1")
	if len(ips) != 2 {
		t.Errorf("expected 2 IPv6 IPs, got %d", len(ips))
	}
}

func TestParseIPList_Empty(t *testing.T) {
	ips := parseIPList("")
	if len(ips) != 0 {
		t.Errorf("expected 0 IPs for empty string, got %d", len(ips))
	}
}
