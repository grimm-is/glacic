package imports

import (
	"testing"
)

func TestParseMikroTikNAT(t *testing.T) {
	input := `/ip firewall nat
add action=masquerade chain=srcnat out-interface=ether1 comment="defconf: masquerade"
add action=dst-nat chain=dstnat dst-port=80 protocol=tcp to-addresses=192.168.88.10 to-ports=80 comment="Web Server"
`
	cfg, err := ParseMikroTikExportStr(input)
	if err != nil {
		t.Fatalf("ParseMikroTikExportStr failed: %v", err)
	}

	res := cfg.ToImportResult()

	if len(res.NATRules) != 1 {
		t.Errorf("Expected 1 NAT rule, got %d", len(res.NATRules))
	}
	if res.NATRules[0].Type != "masquerade" {
		t.Errorf("Expected masquerade, got %s", res.NATRules[0].Type)
	}

	if len(res.PortForwards) != 1 {
		t.Errorf("Expected 1 port forward, got %d", len(res.PortForwards))
	}
	if res.PortForwards[0].InternalIP != "192.168.88.10" {
		t.Errorf("Port forward IP mismatch: %s", res.PortForwards[0].InternalIP)
	}
}

func TestParseMikroTikDHCP(t *testing.T) {
	input := `/ip dhcp-server
add address-pool=default-dhcp interface=bridge name=defconf
/ip dhcp-server lease
add address=192.168.88.100 mac-address=00:11:22:33:44:55 server=defconf host-name="printer" comment="printer"
`
	cfg, err := ParseMikroTikExportStr(input)
	if err != nil {
		t.Fatal(err)
	}

	if len(cfg.DHCPServers) != 1 {
		t.Errorf("Expected 1 scope, got %d", len(cfg.DHCPServers))
	}
	if cfg.DHCPServers[0].Name != "defconf" {
		t.Error("DHCP server name mismatch")
	}

	if len(cfg.DHCPLeases) != 1 {
		t.Errorf("Expected 1 lease, got %d", len(cfg.DHCPLeases))
	}
	if cfg.DHCPLeases[0].Hostname != "printer" {
		t.Error("Lease hostname mismatch")
	}
}

func TestParseMikroTikDNS(t *testing.T) {
	input := `/ip dns static
add address=192.168.88.1 name=router.lan
`
	cfg, err := ParseMikroTikExportStr(input)
	if err != nil {
		t.Fatal(err)
	}

	res := cfg.ToImportResult()

	if len(res.StaticHosts) != 1 {
		t.Errorf("Expected 1 host, got %d", len(res.StaticHosts))
	}
	if res.StaticHosts[0].Hostname != "router.lan" {
		t.Error("Hostname mismatch")
	}
}
