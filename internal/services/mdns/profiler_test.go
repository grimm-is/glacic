package mdns

import (
	"strings"
	"testing"
)

func TestAnalyzeDevice_GoogleCast(t *testing.T) {
	// Data from user example: Sony TV
	sonyTXT := map[string]string{
		"fn":           "SONY XBR-55A8H",
		"md":           "BRAVIA 4K UR3",
		"model":        "XBR-55A8H",
		"manufacturer": "Sony",
		"bs":           "FA8F29C30445",
		"st":           "0",
		"ca":           "264709",
		"rs":           "", // Empty app means Backdrop
	}

	parsed := &ParsedMDNS{
		Services:   []string{"_googlecast._tcp"},
		TXTRecords: sonyTXT,
	}

	profile := AnalyzeDevice(parsed)

	if profile.Type != DeviceTypeCast {
		t.Errorf("Expected DeviceTypeCast, got %q", profile.Type)
	}

	expectedModel := "Sony XBR-55A8H"
	if profile.Model != expectedModel {
		t.Errorf("Expected Model %q, got %q", expectedModel, profile.Model)
	}

	expectedState := "Idle"
	if profile.State != expectedState {
		t.Errorf("Expected State %q, got %q", expectedState, profile.State)
	}

	// Active Cast device example
	googleTXT := map[string]string{
		"fn": "Family Room Google TV",
		"md": "Chromecast",
		"ca": "465413",
		"bs": "FA8FCA9C07E1",
		"st": "1",       // Active
		"rs": "Netflix", // Simulating an active app
	}

	parsedActive := &ParsedMDNS{
		Services:   []string{"_googlecast._tcp"},
		TXTRecords: googleTXT,
	}

	profileActive := AnalyzeDevice(parsedActive)

	if profileActive.State != "Active / Playing" {
		t.Errorf("Expected Active state, got %q", profileActive.State)
	}

	if !strings.Contains(profileActive.ExtraInfo, "Video, Audio") {
		t.Errorf("Expected capabilities to include Video, Audio, got %q", profileActive.ExtraInfo)
	}
}

func TestAnalyzeDevice_HomeKit(t *testing.T) {
	// Data from user example: Smart Plug
	plugTXT := map[string]string{
		"md": "EP25",
		"ci": "7", // Outlet
		"sf": "1", // Unpaired
		"c#": "2",
		"id": "AF:B5:D8:2E:3A:B7",
	}

	parsed := &ParsedMDNS{
		Services:   []string{"_hap._tcp"},
		TXTRecords: plugTXT,
	}

	profile := AnalyzeDevice(parsed)

	if profile.Type != DeviceTypeHomeKit {
		t.Errorf("Expected DeviceTypeHomeKit, got %q", profile.Type)
	}

	expectedFriendly := "EP25 (Outlet)"
	if profile.FriendlyName != expectedFriendly {
		t.Errorf("Expected FriendlyName %q, got %q", expectedFriendly, profile.FriendlyName)
	}

	if !strings.Contains(profile.State, "Unpaired") {
		t.Errorf("Expected State to mention Unpaired, got %q", profile.State)
	}
}

func TestAnalyzeDevice_Extended(t *testing.T) {
	// 1. Matter Device
	matterTXT := map[string]string{
		"DN":  "Kitchen Downlight",
		"VP":  "65521+32769",
		"SII": "5000",
	}
	parsedMatter := &ParsedMDNS{
		Services:   []string{"_matter._tcp"},
		TXTRecords: matterTXT,
	}
	profileMatter := AnalyzeDevice(parsedMatter)
	if profileMatter.Type != DeviceTypeMatter {
		t.Errorf("Expected DeviceTypeMatter, got %q", profileMatter.Type)
	}
	if profileMatter.FriendlyName != "Kitchen Downlight" {
		t.Errorf("Expected FriendlyName 'Kitchen Downlight', got %q", profileMatter.FriendlyName)
	}
	if !strings.Contains(profileMatter.ExtraInfo, "Sleepy: true") {
		t.Errorf("Expected Sleepy: true, got %q", profileMatter.ExtraInfo)
	}

	// 2. Home Assistant
	haTXT := map[string]string{
		"location_name": "The Burrow",
		"version":       "2024.6.2",
		"uuid":          "abc-123",
	}
	parsedHA := &ParsedMDNS{
		Services:   []string{"_home-assistant._tcp"},
		TXTRecords: haTXT,
	}
	profileHA := AnalyzeDevice(parsedHA)
	if profileHA.Type != DeviceTypeHomeAssistant {
		t.Errorf("Expected DeviceTypeHomeAssistant, got %q", profileHA.Type)
	}
	if profileHA.FriendlyName != "The Burrow" {
		t.Errorf("Expected FriendlyName 'The Burrow', got %q", profileHA.FriendlyName)
	}

	// 3. Synology NAS (Time Machine)
	nasTXT := map[string]string{
		"sys":    "waMa",
		"dk0":    "8589934592",
		"adVN":   "Backups",
		"vendor": "Synology",
		"model":  "DS920+",
	}
	parsedNAS := &ParsedMDNS{
		Services:   []string{"_adisk._tcp", "_smb._tcp"},
		TXTRecords: nasTXT,
	}
	profileNAS := AnalyzeDevice(parsedNAS)
	if profileNAS.Type != DeviceTypeTimeMachine {
		t.Errorf("Expected DeviceTypeTimeMachine, got %q", profileNAS.Type)
	}
	if profileNAS.FriendlyName != "Backups" {
		t.Errorf("Expected FriendlyName 'Backups', got %q", profileNAS.FriendlyName)
	}
	// 8589934592 KB = 8192 GB
	if profileNAS.TimeMachine.CapacityGB != 8192 {
		t.Errorf("Expected 8192 GB, got %d", profileNAS.TimeMachine.CapacityGB)
	}

	// 4. Fallback / Generic
	unknownTXT := map[string]string{
		"distro":      "Ubuntu 22.04 LTS",
		"kernel":      "Linux 5.15",
		"pretty_name": "Ubuntu Jammy Jellyfish",
		"ssh":         "true",
	}
	parsedUnknown := &ParsedMDNS{
		Services:   []string{"_foo._tcp"}, // Changed from _ipp to _foo to avoid Printer detection
		TXTRecords: unknownTXT,
		Hostname:   "random-server",
	}
	profileUnknown := AnalyzeDevice(parsedUnknown)

	if profileUnknown.Type != DeviceTypeUnknown {
		t.Errorf("Expected DeviceTypeUnknown, got %q", profileUnknown.Type)
	}
	// Fallback logic currently doesn't set FriendlyName from Hostname in this test setup
	// But it should populate Generic info
	if profileUnknown.Generic == nil {
		t.Fatal("Expected Generic info to be populated")
	}
	if val, ok := profileUnknown.Generic.InterestingKeys["distro"]; !ok || val != "Ubuntu 22.04 LTS" {
		t.Errorf("Expected InterestingKey 'distro' to be captured")
	}
}

func TestAnalyzeDevice_Unknown(t *testing.T) {
	parsed := &ParsedMDNS{
		Hostname:   "random-printer",
		Services:   []string{"_bar._tcp"},
		TXTRecords: map[string]string{},
	}

	profile := AnalyzeDevice(parsed)

	if profile.Type != DeviceTypeUnknown {
		t.Errorf("Expected DeviceTypeUnknown, got %q", profile.Type)
	}

	if profile.FriendlyName != "random-printer" {
		t.Errorf("Expected FriendlyName 'random-printer', got %q", profile.FriendlyName)
	}
}

func TestAnalyzeDevice_ServiceEnrichment(t *testing.T) {
	parsed := &ParsedMDNS{
		Hostname: "generic-device",
		Services: []string{
			"_ssh._tcp",
			"_http._tcp.local.",
			"_unknown_service._tcp",
			"_googlecast._tcp", // Should define Type=Cast, but we check if readable also gets populated in fallback scenarios?
			// Actually parseGeneric is only called for DeviceTypeUnknown in AnalyzeDevice FALLBACK logic.
			// But wait, AnalyzeDevice also calls parseGeneric if no primary role is found?
			// In user's code: "Run Generic Fallback ... If no primary role found, OR if we just want to see if we can explain the services better".
			// In MY implementation (restored):
			// if profile.Type == DeviceTypeUnknown { profile.Generic = parseGeneric(...) }
			// So if I pass _googlecast, it will be detected as Cast, and parseGeneric WON'T be called?
			// Let's check my restored code.
		},
		TXTRecords: map[string]string{
			"distro": "Ubuntu",
		},
	}

	// If I want to test parseGeneric, I must ensure it DOES NOT match a specific profiler.
	// _ssh and _http don't trigger specific profilers.
	// _googlecast DOES.
	// So I should remove _googlecast from this test if I want to test Generic info population for an Unknown device.
	// OR I should call parseGeneric directly for unit testing purposes if I wasn't testing AnalyzeDevice integration.
	// But I am testing AnalyzeDevice.

	parsed.Services = []string{"_ssh._tcp", "_http._tcp.local.", "_unknown_custom._tcp"}

	profile := AnalyzeDevice(parsed)

	if profile.Type != DeviceTypeUnknown {
		t.Fatalf("Expected DeviceTypeUnknown, got %q", profile.Type)
	}

	if profile.Generic == nil {
		t.Fatal("Expected Generic info")
	}

	readable := profile.Generic.ReadableServices

	// Helper to check for existence
	has := func(s string) bool {
		for _, r := range readable {
			if r == s {
				return true
			}
		}
		return false
	}

	if !has("SSH Remote Login") {
		t.Errorf("Expected 'SSH Remote Login', got %v", readable)
	}
	if !has("Web Server") {
		t.Errorf("Expected 'Web Server' (from _http._tcp.local.), got %v", readable)
	}
	if !has("_unknown_custom._tcp") {
		t.Errorf("Expected raw service '_unknown_custom._tcp', got %v", readable)
	}
}

func TestAnalyzeDevice_Priority(t *testing.T) {
	// Scenario: A MacBook Pro running software that advertises Google Cast.
	// It has the 'model' key from AirPlay/Device-Info, but also _googlecast service.

	txt := map[string]string{
		"fn":      "Ben's MacBook Pro",
		"md":      "Chromecast", // Cast often reports this
		"model":   "MacBookPro18,3",
		"osxvers": "23", // macOS 14
	}

	parsed := &ParsedMDNS{
		Services:   []string{"_googlecast._tcp", "_airplay._tcp", "_device-info._tcp"},
		TXTRecords: txt,
	}

	profile := AnalyzeDevice(parsed)

	// WE WANT: DeviceTypeApple (high confidence hardware match)
	// CURRENTLY: It likely returns DeviceTypeCast because Cast is checked first.

	if profile.Type != DeviceTypeApple {
		t.Errorf("Expected DeviceTypeApple (Hardware Priority), got %q", profile.Type)
	}

	// Verify that we still populated the Cast info though!
	if profile.Cast == nil {
		t.Error("Expected Cast info to be populated even if main type is Apple")
	}
}
