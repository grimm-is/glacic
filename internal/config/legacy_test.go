package config

import (
	"strings"
	"testing"
)

func TestTransformLegacyHCL(t *testing.T) {
	tests := []struct {
		name           string
		input          string
		wantContain    string
		wantTransforms int
	}{
		{
			name: "transform unlabeled protection block",
			input: `
ip_forwarding = true

protection {
  anti_spoofing = true
  bogon_filtering = true
}

interface "eth0" {
  zone = "WAN"
}
`,
			wantContain:    `protection "legacy_global"`,
			wantTransforms: 1,
		},
		{
			name: "no transform needed for global_protection",
			input: `
ip_forwarding = true

global_protection {
  anti_spoofing = true
}
`,
			wantContain:    `protection "legacy_global"`,
			wantTransforms: 1,
		},
		{
			name: "no transform needed for labeled protection",
			input: `
ip_forwarding = true

protection "wan_protection" {
  interface = "eth0"
  anti_spoofing = true
}
`,
			wantContain:    `protection "wan_protection"`,
			wantTransforms: 0,
		},
		{
			name: "preserve indentation",
			input: `
  protection {
    anti_spoofing = true
  }
`,
			wantContain:    `  protection "legacy_global" {`,
			wantTransforms: 1,
		},
		{
			name: "transform deprecated vpn_link_group",
			input: `
vpn_link_group "primary" {
  source_networks = ["192.168.1.0/24"]
  vpn_link "wg0" {
    interface = "wg0"
  }
}
`,
			wantContain:    `uplink_group "primary"`,
			wantTransforms: 2, // vpn_link_group + vpn_link
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, transforms := TransformLegacyHCL([]byte(tt.input))

			if !strings.Contains(string(got), tt.wantContain) {
				t.Errorf("TransformLegacyHCL() output doesn't contain %q\nGot:\n%s", tt.wantContain, string(got))
			}

			if len(transforms) != tt.wantTransforms {
				t.Errorf("TransformLegacyHCL() applied %d transforms, want %d", len(transforms), tt.wantTransforms)
			}
		})
	}
}

func TestDetectLegacyFeatures(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantLen int
	}{
		{
			name: "modern config with version",
			input: `
schema_version = "1.0"
ip_forwarding = true
`,
			wantLen: 0,
		},
		{
			name: "legacy config without version",
			input: `
ip_forwarding = true
`,
			wantLen: 1, // missing schema_version
		},
		{
			name: "legacy config with old protection block",
			input: `
ip_forwarding = true
protection {
  anti_spoofing = true
}
`,
			wantLen: 2, // missing schema_version + old protection
		},
		{
			name: "deprecated vpn_link_group detected",
			input: `
schema_version = "1.0"
vpn_link_group "primary" {
  source_networks = ["10.0.0.0/8"]
}
`,
			wantLen: 1, // deprecated vpn_link_group
		},
		{
			name: "deprecated vpn_link detected",
			input: `
schema_version = "1.0"
uplink_group "primary" {
  vpn_link "wg0" {
    interface = "wg0"
  }
}
`,
			wantLen: 1, // deprecated vpn_link
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			features := DetectLegacyFeatures([]byte(tt.input))
			if len(features) != tt.wantLen {
				t.Errorf("DetectLegacyFeatures() found %d features, want %d: %v", len(features), tt.wantLen, features)
			}
		})
	}
}

func TestIsLegacyConfig(t *testing.T) {
	modern := `
schema_version = "1.0"
ip_forwarding = true
`
	legacy := `
ip_forwarding = true
protection {
  anti_spoofing = true
}
`

	if IsLegacyConfig([]byte(modern)) {
		t.Error("IsLegacyConfig() returned true for modern config")
	}

	if !IsLegacyConfig([]byte(legacy)) {
		t.Error("IsLegacyConfig() returned false for legacy config")
	}
}
