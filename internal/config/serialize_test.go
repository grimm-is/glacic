package config

import (
	"testing"
)

func TestHCLSerializationRoundTrip(t *testing.T) {
	// Create a complex config object
	input := &Config{
		SchemaVersion: "1.0",
		IPForwarding:  true,
		Interfaces: []Interface{
			{
				Name:    "eth0",
				Zone:    "wan",
				IPv4:    []string{"192.168.1.1/24"},
				Gateway: "192.168.1.254",
				VLANs: []VLAN{
					{
						ID:   "10",
						Zone: "iot",
						IPv4: []string{"10.0.10.1/24"},
					},
				},
			},
		},
		IPSets: []IPSet{
			{
				Name:         "blocklist",
				Type:         "ipv4_addr",
				Entries:      []string{"1.1.1.1", "8.8.8.8"},
				AutoUpdate:   true,
				RefreshHours: 24,
			},
		},
		Policies: []Policy{
			{
				Name:   "lan-to-wan",
				From:   "lan",
				To:     "wan",
				Action: "accept",
				Rules: []PolicyRule{
					{
						Name:     "block-bad-merged",
						Action:   "drop",
						SrcIPSet: "blocklist",
					},
				},
			},
		},
		FRR: &FRRConfig{
			Enabled: true,
			OSPF: &OSPF{
				RouterID: "1.1.1.1",
				Networks: []string{"10.0.0.0/8"},
				Areas: []OSPFArea{
					{
						ID:       "0.0.0.0",
						Networks: []string{"192.168.1.0/24"},
					},
				},
			},
		},
	}

	// 1. Serialize to HCL
	hclBytes, err := GenerateHCL(input)
	if err != nil {
		t.Fatalf("Failed to serialize to HCL: %v", err)
	}

	// 2. Deserialize back to Config
	output, err := LoadHCL(hclBytes, "test.hcl")
	if err != nil {
		t.Logf("Generated HCL:\n%s", string(hclBytes))
		t.Fatalf("Failed to deserialize HCL: %v", err)
	}

	// 3. Compare relevant fields
	// Note: DeepEqual might fail if default values are populated during Load
	// So we check specific fields that we care about preserving

	if output.IPForwarding != input.IPForwarding {
		t.Errorf("IPForwarding mismatch: got %v, want %v", output.IPForwarding, input.IPForwarding)
	}

	if len(output.Interfaces) != 1 {
		t.Fatalf("Interfaces count mismatch: got %d, want 1", len(output.Interfaces))
	}
	if output.Interfaces[0].Name != "eth0" {
		t.Errorf("Interface Name mismatch")
	}
	if len(output.Interfaces[0].VLANs) != 1 {
		t.Errorf("VLAN count mismatch")
	}

	if output.FRR == nil || !output.FRR.Enabled {
		t.Errorf("FRR config lost")
	}
	if len(output.FRR.OSPF.Areas) != 1 {
		t.Errorf("OSPF Areas mismatch")
	}
}
