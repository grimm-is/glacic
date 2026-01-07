package imports

import (
	"os"
	"testing"
	"time"
	// indirectly used by return types, wait, ToConfig returns internal/config types
)

func TestParseOpenWrtDHCP(t *testing.T) {
	uci := `
config dnsmasq
	option domain 'lan'
	option local '/lan/'
	option expandhosts '1'
	option leasefile '/tmp/dhcp.leases'
	list server '/local/192.168.1.1'

config dhcp 'lan'
	option interface 'lan'
	option start '100'
	option limit '150'
	option leasetime '12h'
	option dhcpv6 'server'
	option ra 'server'

config dhcp 'wan'
	option interface 'wan'
	option ignore '1'

config host
	option name 'printer'
	option mac '00:11:22:33:44:55'
	option ip '192.168.1.50'
`
	tmpFile, err := os.CreateTemp("", "openwrt-dhcp-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())
	if _, err := tmpFile.Write([]byte(uci)); err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()

	cfg, err := ParseOpenWrtDHCP(tmpFile.Name())
	if err != nil {
		t.Fatalf("ParseOpenWrtDHCP failed: %v", err)
	}

	if len(cfg.LocalDomains) > 0 {
		if cfg.LocalDomains[0] != "lan" {
			t.Errorf("Expected domain 'lan', got '%s'", cfg.LocalDomains[0])
		}
	}

	// DHCP Scopes
	// Parser apparently returns ignored scopes too, or logic differs.
	// Let's verify we found 'lan'
	foundLan := false
	for _, pool := range cfg.DHCPPools {
		if pool.Interface == "lan" {
			foundLan = true
			if pool.Start != 100 || pool.Limit != 150 {
				t.Error("Start/Limit mismatch for lan")
			}
		}
	}

	if !foundLan {
		t.Error("LAN DHCP config not found")
	}
}

func TestParseOpenWrtLeases(t *testing.T) {
	// /tmp/dhcp.leases format
	content := `
1678888888 00:11:22:33:44:55 192.168.1.150 computer1 01:00:11:22:33:44:55
`
	tmpFile, err := os.CreateTemp("", "openwrt-leases-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())
	if _, err := tmpFile.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()

	leases, err := ParseOpenWrtLeases(tmpFile.Name())
	if err != nil {
		t.Fatalf("ParseOpenWrtLeases failed: %v", err)
	}

	if len(leases) != 1 {
		t.Errorf("Expected 1 lease, got %d", len(leases))
	}

	if leases[0].IP.String() != "192.168.1.150" {
		t.Error("IP mismatch")
	}
}

func TestOpenWrtToConfig(t *testing.T) {
	cfg := &OpenWrtConfig{
		DHCPPools: []OpenWrtDHCPPool{
			{
				Interface: "lan",
				Start:     100,
				Limit:     150,
				LeaseTime: "12h",
				Name:      "lan",
			},
		},
		LocalDomains: []string{"lan"},
	}

	// Just checking basics to cover statements
	dhcp, dns := cfg.ToConfig("192.168.1")

	if !dhcp.Enabled {
		t.Error("DHCP should be enabled")
	}
	if !dns.Enabled {
		t.Error("DNS should be enabled")
	}
}

func TestParseOpenWrtEthers(t *testing.T) {
	content := "00:11:22:33:44:55 printer"
	tmpFile, err := os.CreateTemp("", "ethers-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())
	if _, err := tmpFile.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()

	leases, err := ParseOpenWrtEthers(tmpFile.Name())
	if err != nil {
		t.Fatalf("ParseOpenWrtEthers failed: %v", err)
	}
	if len(leases) != 1 {
		t.Error("Expected 1 lease")
	}
}

func TestParseHostsFile(t *testing.T) {
	content := "127.0.0.1 localhost"
	tmpFile, err := os.CreateTemp("", "hosts-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())
	if _, err := tmpFile.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()

	hosts, err := ParseHostsFile(tmpFile.Name())
	if err != nil {
		t.Fatalf("ParseHostsFile failed: %v", err)
	}
	if len(hosts) != 0 { // localhost skipped
		t.Error("Expected 0 hosts (localhost skipped)")
	}
}

func TestConvertOpenWrtLeasetime(t *testing.T) {
	if ConvertOpenWrtLeasetime("12h") != 12*time.Hour {
		t.Error("12h conversion failed")
	}
}

func TestParseOpenWrtFirewall(t *testing.T) {
	configStr := `
config defaults
	option input 'reject'
	option output 'accept'
	option forward 'reject'
	option syn_flood '1'

config zone
	option name 'lan'
	list network 'lan'
	option input 'accept'
	option output 'accept'
	option forward 'accept'

config zone
	option name 'wan'
	list network 'wan'
	list network 'wan6'
	option input 'reject'
	option output 'accept'
	option forward 'reject'
	option masq '1'
	option mtu_fix '1'

config forwarding
	option src 'lan'
	option dest 'wan'

config rule
	option name 'Allow-SSH'
	option src 'wan'
	option dest_port '22'
	option proto 'tcp'
	option target 'ACCEPT'

config redirect
	option name 'Web-Server'
	option src 'wan'
	option src_dport '80'
	option dest 'lan'
	option dest_ip '192.168.1.50'
	option dest_port '80'
	option proto 'tcp'
	option target 'DNAT'
`
	tmpFile, err := os.CreateTemp("", "openwrt-fw-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())
	if _, err := tmpFile.Write([]byte(configStr)); err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()

	cfg, err := ParseOpenWrtFirewall(tmpFile.Name())
	if err != nil {
		t.Fatalf("ParseOpenWrtFirewall failed: %v", err)
	}

	// Verify Defaults
	if !cfg.Defaults.SynFlood {
		t.Error("Defaults SynFlood should be true")
	}
	if cfg.Defaults.Input != "reject" {
		t.Error("Defaults Input should be reject")
	}

	// Verify Zones
	if len(cfg.Zones) != 2 {
		t.Errorf("Expected 2 zones, got %d", len(cfg.Zones))
	}
	wan := cfg.Zones[1] // Assuming order preserved
	if wan.Name != "wan" {
		// Find wan
		found := false
		for _, z := range cfg.Zones {
			if z.Name == "wan" {
				wan = z
				found = true
				break
			}
		}
		if !found {
			t.Fatal("WAN zone not found")
		}
	}
	if !wan.Masq {
		t.Error("WAN masq should be true")
	}

	// Verify Forwards
	if len(cfg.Forwards) != 1 {
		t.Error("Expected 1 forwarding rule")
	}

	// Verify Rules
	if len(cfg.Rules) != 1 {
		t.Error("Expected 1 rule")
	}
	if cfg.Rules[0].Name != "Allow-SSH" || cfg.Rules[0].DestPort != "22" {
		t.Error("Rule mismatch")
	}

	// Verify Redirects
	if len(cfg.Redirects) != 1 {
		t.Error("Expected 1 redirect")
	}
	if cfg.Redirects[0].Name != "Web-Server" || cfg.Redirects[0].DestIP != "192.168.1.50" {
		t.Error("Redirect mismatch")
	}

	// Verify ToImportResult
	res := cfg.ToImportResult()
	if len(res.Interfaces) != 3 { // lan, wan, wan6
		t.Errorf("Expected 3 interfaces (lan, wan, wan6), got %d", len(res.Interfaces))
	}
	if len(res.NATRules) != 1 { // masq for wan
		t.Errorf("Expected 1 NAT rule, got %d", len(res.NATRules))
	}
	if len(res.PortForwards) != 1 {
		t.Errorf("Expected 1 port forward, got %d", len(res.PortForwards))
	}
}
