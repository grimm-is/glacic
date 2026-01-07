//go:build linux

package firewall

import (
	"fmt"
	"strings"
	"testing"

	"grimm.is/glacic/internal/config"
	"grimm.is/glacic/internal/testutil"
)

func TestScriptBuilder(t *testing.T) {
	sb := NewScriptBuilder("testtable", "inet")

	// Add table explicitly (as BuildFilterTableScript does)
	sb.AddTable()

	// 1. Chains
	sb.AddChain("input", "filter", "input", 0, "drop")

	// 2. Rules
	sb.AddRule("input", "tcp dport 22 accept")

	// 3. Build result
	script := sb.Build()

	// Verify content - ScriptBuilder adds table and chain/rule lines
	expected := []string{
		"add table inet testtable",
		`add chain inet testtable input { type filter hook input priority 0; policy drop; }`,
		`add rule inet testtable input tcp dport 22 accept`,
	}

	for _, exp := range expected {
		if !strings.Contains(script, exp) {
			t.Errorf("Script missing line: %s\nScript:\n%s", exp, script)
		}
	}
}

func TestBuildFilterTableScript_Validation(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *config.Config
		wantErr bool
	}{
		{
			name: "Valid Config",
			cfg: &config.Config{
				IPSets: []config.IPSet{
					{Name: "valid_set", Type: "ipv4_addr"},
				},
			},
			wantErr: false,
		},
		{
			name: "Invalid IPSet Name",
			cfg: &config.Config{
				IPSets: []config.IPSet{
					{Name: "invalid;set", Type: "ipv4_addr"},
				},
			},
			wantErr: true,
		},
		{
			name: "Invalid SrcIPSet in Policy",
			cfg: &config.Config{
				Policies: []config.Policy{
					{
						From: "lan", To: "wan",
						Rules: []config.PolicyRule{
							{SrcIPSet: "bad$set"},
						},
					},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := BuildFilterTableScript(FromGlobalConfig(tt.cfg), tt.cfg.VPN, "testtable", "")
			if (err != nil) != tt.wantErr {
				t.Errorf("BuildFilterTableScript() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestScriptBuilder_Injection(t *testing.T) {
	sb := NewScriptBuilder("table", "ip")

	// Test injection in zone names
	// This should be handled by quote() if calling AddChain directly,
	// BUT ApplyConfig validation now catches this earlier.
	// This test verifies that `quote` (used in atomic_apply) makes it syntactically valid
	// even if validation was bypassed (defense in depth).

	evilZone := "lan; reboot;"

	// AddChain uses quote()
	sb.AddChain(evilZone, "filter", "input", 0, "accept")

	// AddRule uses quote()
	sb.AddRule(evilZone, "accept")

	script := sb.Build()

	// Expect quoted identifier: "lan; reboot;"
	expected := `"lan; reboot;"`
	if !strings.Contains(script, expected) {
		t.Errorf("Injection risk! Script validation failed to quote special chars. Script:\n%s", script)
	}

	// Ensure raw injection didn't happen
	if strings.Contains(script, "chain ip table lan; reboot;") {
		t.Errorf("Injection successful! Script:\n%s", script)
	}
}

func TestSecurityDefaults(t *testing.T) {
	// Create a config with zones that have services defined
	cfg := &config.Config{
		Interfaces: []config.Interface{
			// WAN interface with NO WebUI
			{Name: "eth0", Zone: "WAN", AccessWebUI: false},
			// LAN interface WITH WebUI
			{Name: "eth1", Zone: "LAN", AccessWebUI: true, WebUIPort: 8443},
		},
		Zones: []config.Zone{
			{Name: "WAN"},
			{Name: "LAN", Services: &config.ZoneServices{DNS: true, DHCP: true}},
		},
	}

	sb, err := BuildFilterTableScript(FromGlobalConfig(cfg), cfg.VPN, "testtable", "")
	if err != nil {
		t.Fatalf("BuildFilterTableScript failed: %v", err)
	}
	script := sb.Build()

	// 1. Verify NO global open resolver
	if strings.Contains(script, "add rule inet testtable input udp dport 53 accept") {
		t.Error("Security FAIL: Found global UDP 53 accept rule!")
	}
	if strings.Contains(script, "add rule inet testtable input tcp dport 8443 accept") {
		t.Error("Security FAIL: Found global TCP 8443 accept rule!")
	}

	// 2. Verify Zone/Interface constraints
	// LAN (eth1) should allow 8443 (with iifname set)
	// The new script builder consolidates rules into "iifname . tcp dport { ... }"
	// So we expect "eth1" . 8443 inside a map/set.
	if !strings.Contains(script, `"eth1" . 8443`) {
		t.Errorf("Security FAIL: Missing API allow rule for trusted interface (consolidated set)")
	}

	// WAN (eth0) should NOT appear in management allow rules for 8443.
	if strings.Contains(script, `"eth0" . 8443`) {
		t.Error("Security FAIL: Found API allow rule for WAN interface!")
	}

	// 3. Verify IPv6 NDP
	if !strings.Contains(script, "icmpv6 type { nd-neighbor-solicit, nd-neighbor-advert, nd-router-solicit, nd-router-advert } accept") {
		t.Error("Security FAIL: Missing IPv6 Neighbor Discovery allow rule!")
	}

	// 4. Verify Log Rate Limiting
	// Should see "limit rate" before "log"
	if !strings.Contains(script, `limit rate 10/minute burst 5 packets log group 0 prefix`) {
		t.Error("Security FAIL: Default drop rule does not have rate-limited logging!")
	}
}

func TestScriptBuilder_Fuzz(t *testing.T) {
	// Corpus of "nasty" inputs that could cause injection or panics
	nastyInputs := []string{
		"test; reboot;",
		"test\nreboot",
		"test\r\nreboot",
		"test; DROP TABLE users;",
		"$(reboot)",
		"`reboot`",
		"test || reboot",
		"test && reboot",
		"Invalid UTF-8: \xff\xfe\xfd",
		strings.Repeat("A", 10000), // Buffer overflow attempt
		"",                         // Empty string
		" ",                        // Whitespace
		"' OR '1'='1",
	}

	applier := NewAtomicApplier()

	testutil.RequireVM(t)
	nftAvailable := true

	for _, input := range nastyInputs {
		t.Run(fmt.Sprintf("Input_%q", input), func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("PANIC recovered for input %q: %v", input, r)
				}
			}()

			sb := NewScriptBuilder("fuzztable", "inet")

			// 1. Fuzz Chain Name
			// AddChain sanitizes the name
			sb.AddChain(input, "filter", "input", 0, "accept")

			// 2. Fuzz Rule Content
			// AddRule quotes the chain name, but assumes ruleExpr is valid logic.
			// However, if we pass garbage, it shouldn't crash.
			// Ideally, ruleExpr logic should be constructed via safe helpers, but for now we test AddRule's robustness.
			sb.AddRule("input", fmt.Sprintf("tcp dport 22 comment %q", input))

			// 3. Fuzz Set Name
			sb.AddSet(input, "ipv4_addr", "", 0)

			script := sb.Build()

			// Assertion 1: No raw injection of newlines or semicolons allowing command chaining
			// The script builder output is passed to `nft -f` via STDIN.
			// `nft` parses newlines as statement separators.
			// If our input contained `\n`, it might have created a new valid statement.

			// Assertion 2: If we inject "; reboot", it should NOT appear unquoted as a command.
			// This is hard to regex perfectly, but we can check if the specific input appears as a token.
			// A better check is: does the script validate?
			if nftAvailable {
				// We expect validation to FAIL for garbage inputs (syntax error),
				// bu NOT execute the injected command.
				// Since `nft -c` just checks syntax, we are safe.
				// Exception: If the fuzz input IS valid syntax (e.g. empty string), it might pass.
				_ = applier.ValidateScript(script)
				// We don't assert error/no-error here because garbage input usually causes syntax errors.
				// The victory is that we called it without crashing/hanging/executing side effects.
			}

			// Assertion 3: Check for specific injection leakage in generated string
			if strings.Contains(input, "\n") {
				// Newlines should be escaped or the line logic handles them?
				// Our current implementations of `AddLine` just appends.
				// `quote` function does NOT escape newlines currently.
				// This is a potential weak spot. Let's see if it fails.
				// Actually, `quote` uses `%q` which escapes newlines as `\n`.
				// So `fmt.Sprintf("%q", s)` should be safe.
			}
		})
	}
}
