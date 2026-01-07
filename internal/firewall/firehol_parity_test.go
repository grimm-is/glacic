package firewall

import (
	"strings"
	"testing"

	"grimm.is/glacic/internal/config"
)

func TestFireHOLParityGeneration(t *testing.T) {
	cfg := &Config{
		MSSClamping: true,
		MarkRules: []config.MarkRule{
			{
				Name:         "test-mark",
				Enabled:      true,
				SrcIP:        "10.0.0.1",
				Mark:         "0x1", // String in MarkRule
				SaveMark:     true,
				OutInterface: "eth0",
			},
		},
		UIDRouting: []config.UIDRouting{
			{
				Name:    "test-uid",
				Enabled: true,
				UID:     1000,
				Uplink:  "ignored-for-test",
			},
		},
		NAT: []config.NATRule{
			{
				Type:         "snat",
				OutInterface: "eth0",
				SrcIP:        "10.0.0.0/24",
				Mark:         10,
				SNATIP:       "1.2.3.4",
			},
		},
	}

	// 1. Verify MSS Clamping in Filter Table
	sb, err := BuildFilterTableScript(cfg, nil, "glacic_test", "")
	if err != nil {
		t.Fatalf("BuildFilterTableScript failed: %v", err)
	}
	script := sb.Build()
	if !strings.Contains(script, "tcp flags syn tcp option maxseg size set rt mtu") {
		t.Errorf("MSS Clamping rule missing. Got:\n%s", script)
	}

	// 2. Verify Mark Rules in Mangle Table
	sb, err = BuildMangleTableScript(cfg)
	if err != nil {
		t.Fatalf("BuildMangleTableScript failed: %v", err)
	}
	script = sb.Build()

	// Check for Mark Rule
	expectedMark := `ip saddr 10.0.0.1 meta mark set 0x1`
	if !strings.Contains(script, expectedMark) {
		t.Errorf("Mark rule missing. Expected '%s', Got:\n%s", expectedMark, script)
	}

	// Check for SaveMark
	if !strings.Contains(script, "ct mark set 0x1") {
		t.Errorf("SaveMark rule missing. Got:\n%s", script)
	}

	// 3. Verify SNAT in NAT Table
	sb, err = BuildNATTableScript(cfg)
	if err != nil {
		t.Fatalf("BuildNATTableScript failed: %v", err)
	}
	script = sb.Build()

	// Check for SNAT
	// oifname "eth0" ip saddr 10.0.0.0/24 mark 0xa snat to 1.2.3.4
	// Allow for spacing flexibility in check
	if !strings.Contains(script, `snat to 1.2.3.4`) {
		t.Errorf("SNAT rule missing. Got:\n%s", script)
	}
	if !strings.Contains(script, `ip saddr 10.0.0.0/24`) {
		t.Errorf("SNAT SrcIP matcher missing. Got:\n%s", script)
	}
	if !strings.Contains(script, `mark 0xa`) {
		t.Errorf("SNAT Mark matcher missing. Got:\n%s", script)
	}
}
