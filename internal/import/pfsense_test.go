package imports

import (
	"encoding/xml"
	"os"
	"testing"
)

func TestParsePfSenseConfig(t *testing.T) {
	xmlData := `
<pfsense>
	<version>21.0</version>
	<lastchange></lastchange>
	<system>
		<hostname>pfSense</hostname>
		<domain>localdomain</domain>
	</system>
	<interfaces>
		<wan>
			<enable/>
			<if>em0</if>
			<ipaddr>dhcp</ipaddr>
			<descr>WAN</descr>
		</wan>
		<lan>
			<enable/>
			<if>em1</if>
			<ipaddr>192.168.1.1</ipaddr>
			<subnet>24</subnet>
			<descr>LAN</descr>
		</lan>
	</interfaces>
	<dhcpd>
		<lan>
			<enable/>
			<range>
				<from>192.168.1.100</from>
				<to>192.168.1.199</to>
			</range>
		</lan>
	</dhcpd>
	<filter>
		<rule>
			<id></id>
			<type>pass</type>
			<interface>lan</interface>
			<protocol>tcp</protocol>
			<source>
				<network>lan</network>
			</source>
			<destination>
				<any/>
			</destination>
			<descr>Allow LAN to Any</descr>
		</rule>
	</filter>
	<nat>
		<rule>
			<target>192.168.1.50</target>
			<local-port>80</local-port>
			<interface>wan</interface>
			<protocol>tcp</protocol>
			<destination>
				<network>wanip</network>
				<port>80</port>
			</destination>
			<descr>Web Server</descr>
		</rule>
		<outbound>
			<mode>automatic</mode>
		</outbound>
	</nat>
	<aliases>
		<alias>
			<name>WebServers</name>
			<type>network</type>
			<address>192.168.1.10 192.168.1.11</address>
			<descr>Web Server Farm</descr>
		</alias>
	</aliases>
</pfsense>
`
	// Test the high wrapper that likely calls ParsePfSenseBackup in pfsense_firewall.go
	// Or directly calls Unmarshal.

	// Create temp file
	tmpFile, err := os.CreateTemp("", "pfsense-*.xml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())
	if _, err := tmpFile.Write([]byte(xmlData)); err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()

	// Parse
	cfg, err := ParsePfSenseConfig(tmpFile.Name())
	if err != nil {
		t.Fatalf("ParsePfSenseConfig failed: %v", err)
	}

	// Verify System
	if cfg.XMLName.Local != "pfsense" {
		t.Error("Not a pfsense config")
	}

	// Verify System
	if cfg.Version != "21.0" {
		t.Error("Version mismatch")
	}

	// Verify Interfaces
	// PfSenseConfig does not have Interfaces in default struct (it's in FullConfig or ImportResult)

	// Verify DHCP - PfSenseConfig -> DHCPD -> Interfaces map[string]PfSenseDHCPInterface
	lan, ok := cfg.DHCPD.Interfaces["lan"]
	if !ok {
		t.Error("Expected LAN DHCP config")
	}
	if lan.Enable == "" { // xml:"enable"
		// enable tag present means enabled
	}

	// Verify Aliases
	if len(cfg.Aliases) == 0 {
		t.Error("Expected aliases")
	}
	if cfg.Aliases[0].Name != "WebServers" {
		t.Error("Alias name mismatch")
	}

	// Ensure Full Config Parser works (returns ImportResult)
	res, err := ParsePfSenseBackup(tmpFile.Name())
	if err != nil {
		t.Fatalf("ParsePfSenseBackup failed: %v", err)
	}
	if len(res.FilterRules) == 0 {
		t.Error("Expected ImportResult filter rules")
	}
	if res.FilterRules[0].Description != "Allow LAN to Any" {
		t.Error("Filter rule description mismatch")
	}

	// Check NAT (Outbound)
	if len(res.NATRules) == 0 {
		t.Error("Expected NAT rules")
	}
	if res.NATRules[0].Description != "Automatic outbound NAT" {
		t.Errorf("Expected 'Automatic outbound NAT', got '%s'", res.NATRules[0].Description)
	}

	// Check Port Forwards
	if len(res.PortForwards) == 0 {
		t.Error("Expected Port Forwards")
	}
	if res.PortForwards[0].Description != "Web Server" {
		t.Errorf("Expected 'Web Server', got '%s'", res.PortForwards[0].Description)
	}

	// Check DHCP Scopes
	if len(res.DHCPScopes) == 0 {
		t.Error("Expected DHCP Scopes")
	} else {
		if res.DHCPScopes[0].RangeStart != "192.168.1.100" {
			t.Errorf("Expected RangeStart '192.168.1.100', got '%s'", res.DHCPScopes[0].RangeStart)
		}
	}
}

func TestParsePfSenseConfig_Struct(t *testing.T) {
	// Direct unmarshal test to ensure struct tags are correct
	xmlData := `<pfsense><version>2.5</version></pfsense>`
	var cfg PfSenseFullConfig
	err := xml.Unmarshal([]byte(xmlData), &cfg)
	if err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if cfg.Version != "2.5" {
		t.Error("Version parsing failed")
	}
}

func TestPfSenseGenerateHCL(t *testing.T) {
	res := &ImportResult{
		Hostname: "fw",
		Domain:   "local",
		Interfaces: []ImportedInterface{
			{OriginalName: "wan", OriginalIf: "em0", SuggestedIf: "eth0", Zone: "WAN", IsDHCP: true},
		},
		FilterRules: []ImportedFilterRule{
			{Description: "Allow all", Action: "accept", Protocol: "any"},
		},
	}

	hcl := res.GenerateHCLConfig()
	if hcl == "" {
		t.Error("Generated HCL is empty")
	}
}
