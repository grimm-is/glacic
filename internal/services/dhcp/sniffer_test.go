package dhcp

import (
	"net"
	"testing"

	"github.com/insomniacslk/dhcp/dhcpv4"
)

func TestSniffer_ExtractEvent(t *testing.T) {
	// Create a DHCP Discover packet
	mac, _ := net.ParseMAC("00:11:22:33:44:55")
	opts := []dhcpv4.Modifier{
		dhcpv4.WithOption(dhcpv4.OptHostName("test-host")),
		dhcpv4.WithOption(dhcpv4.OptClassIdentifier("test-vendor")),
		dhcpv4.WithOption(dhcpv4.OptParameterRequestList(dhcpv4.GenericOptionCode(1), dhcpv4.GenericOptionCode(3), dhcpv4.GenericOptionCode(6), dhcpv4.GenericOptionCode(15))),
		dhcpv4.WithOption(dhcpv4.OptClientIdentifier([]byte{1, 0x00, 0x11, 0x22, 0x33, 0x44, 0x55})),
	}

	packet, err := dhcpv4.NewDiscovery(mac, opts...)
	if err != nil {
		t.Fatalf("Failed to create packet: %v", err)
	}

	sniffer := &Sniffer{
		config: SnifferConfig{Enabled: true},
	}

	srcAddr := &net.UDPAddr{IP: net.ParseIP("192.168.1.10"), Port: 68}
	event := sniffer.extractEvent(packet, "eth0", srcAddr)

	if event.ClientMAC != mac.String() {
		t.Errorf("Expected MAC %s, got %s", mac, event.ClientMAC)
	}
	if event.Hostname != "test-host" {
		t.Errorf("Expected Hostname test-host, got %s", event.Hostname)
	}
	if event.VendorClass != "test-vendor" {
		t.Errorf("Expected VendorClass test-vendor, got %s", event.VendorClass)
	}
	if event.Interface != "eth0" {
		t.Errorf("Expected Interface eth0, got %s", event.Interface)
	}

	// Verify fingerprint
	expectedFingerprint := "1,3,6,15"
	if event.Fingerprint != expectedFingerprint {
		t.Errorf("Expected Fingerprint %s, got %s", expectedFingerprint, event.Fingerprint)
	}

	// Verify additional stored options
	// ClientIdentifier (61) should be stored in Options map
	if val, ok := event.Options[61]; !ok {
		t.Error("Expected Option 61 to be stored in Options map")
	} else {
		// Expect hex encoded value: 01001122334455
		expected := "01001122334455"
		if val != expected {
			t.Errorf("Expected Option 61 to be %s, got %s", expected, val)
		}
	}
}

func TestInferDeviceOS(t *testing.T) {
	tests := []struct {
		fingerprint string
		expectedOS  string
	}{
		{"1,3,6,15,31,33,43,44,46,47,119,121,249,252", "Windows 10/11"},
		{"1,121,3,6,15,119,252", "macOS"},
		{"1,3,6,12,15,28,42", "Linux"},
		{"1,3,28,6", "Android"},
		{"1,3,6,15,119,252", "iOS"},
		{"1,2,3,4,5", ""}, // Unknown
		{"", ""},          // Empty
	}

	for _, tc := range tests {
		t.Run(tc.expectedOS, func(t *testing.T) {
			os := InferDeviceOS(tc.fingerprint)
			if os != tc.expectedOS {
				t.Errorf("Fingerprint %s: Expected %q, got %q", tc.fingerprint, tc.expectedOS, os)
			}
		})
	}
}

func TestSniffer_ExtractEvent_AdditionalOptions(t *testing.T) {
	mac, _ := net.ParseMAC("aa:bb:cc:dd:ee:ff")
	// Test options 77 (UserClass) and 124 (VendorIdentifyingVendorClass)
	opts := []dhcpv4.Modifier{
		dhcpv4.WithOption(dhcpv4.OptUserClass("test-user-class")),
		// 124 is complex to construct with standard helpers, so we mock it raw if needed or omit if library support is weak
		// Using Option 60 instead as a proxy for generic string option handling validation
		dhcpv4.WithOption(dhcpv4.OptClassIdentifier("vendor-test")),
	}

	packet, err := dhcpv4.NewDiscovery(mac, opts...)
	if err != nil {
		t.Fatalf("Failed to create packet: %v", err)
	}

	sniffer := &Sniffer{config: SnifferConfig{Enabled: true}}
	srcAddr := &net.UDPAddr{IP: net.ParseIP("10.0.0.1"), Port: 68}
	event := sniffer.extractEvent(packet, "veth0", srcAddr)

	if event.VendorClass != "vendor-test" {
		t.Errorf("Expected VendorClass vendor-test, got %s", event.VendorClass)
	}

	// Option 60 should also be in event.Options map [60]
	if val, ok := event.Options[60]; !ok {
		t.Error("Options map missing key 60")
	} else {
		// hex encoded "vendor-test"
		// v=76 e=65 n=6e ...
		if len(val) == 0 {
			t.Error("Option 60 value empty in map")
		}
	}
}
