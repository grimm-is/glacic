//go:build linux
// +build linux

package firewall

import (
	"testing"

	"grimm.is/glacic/internal/network"
)

func TestMarkRuleBuilder(t *testing.T) {
	t.Run("basic builder", func(t *testing.T) {
		rule := NewMarkRuleBuilder("test-rule").
			Mark(network.MarkWAN1).
			Protocol("tcp").
			DstPort(80).
			InInterface("eth0").
			Comment("Test rule").
			Build()

		if rule.Name != "test-rule" {
			t.Errorf("Expected name 'test-rule', got %s", rule.Name)
		}
		if rule.Mark != network.MarkWAN1 {
			t.Errorf("Expected mark %d, got %d", network.MarkWAN1, rule.Mark)
		}
		if rule.Protocol != "tcp" {
			t.Errorf("Expected protocol 'tcp', got %s", rule.Protocol)
		}
		if rule.DstPort != 80 {
			t.Errorf("Expected port 80, got %d", rule.DstPort)
		}
		if rule.InInterface != "eth0" {
			t.Errorf("Expected interface 'eth0', got %s", rule.InInterface)
		}
		if !rule.Enabled {
			t.Error("Rule should be enabled by default")
		}
	})

	t.Run("with mask", func(t *testing.T) {
		rule := NewMarkRuleBuilder("masked").
			MarkWithMask(0x10, 0xFF).
			Build()

		if rule.Mark != 0x10 {
			t.Errorf("Expected mark 0x10, got %d", rule.Mark)
		}
		if rule.MarkMask != 0xFF {
			t.Errorf("Expected mask 0xFF, got %d", rule.MarkMask)
		}
	})

	t.Run("conntrack options", func(t *testing.T) {
		rule := NewMarkRuleBuilder("conntrack").
			SaveToConntrack().
			RestoreFromConntrack().
			Build()

		if !rule.SaveMark {
			t.Error("SaveMark should be true")
		}
		if !rule.RestoreMark {
			t.Error("RestoreMark should be true")
		}
	})

	t.Run("disabled rule", func(t *testing.T) {
		rule := NewMarkRuleBuilder("disabled").
			Disabled().
			Build()

		if rule.Enabled {
			t.Error("Rule should be disabled")
		}
	})

	t.Run("full builder chain", func(t *testing.T) {
		rule := NewMarkRuleBuilder("complex").
			Mark(network.MarkQoSRealtime).
			Protocol("udp").
			SrcPort(1024).
			DstPort(443).
			SrcIP("10.0.0.0/8").
			DstIP("8.8.8.8").
			InInterface("lan0").
			OutInterface("wan0").
			SaveToConntrack().
			Comment("Complex rule").
			Build()

		if rule.SrcPort != 1024 {
			t.Errorf("Expected src port 1024, got %d", rule.SrcPort)
		}
		if rule.SrcIP != "10.0.0.0/8" {
			t.Errorf("Expected src IP '10.0.0.0/8', got %s", rule.SrcIP)
		}
		if rule.DstIP != "8.8.8.8" {
			t.Errorf("Expected dst IP '8.8.8.8', got %s", rule.DstIP)
		}
		if rule.OutInterface != "wan0" {
			t.Errorf("Expected out interface 'wan0', got %s", rule.OutInterface)
		}
	})
}

func TestMarkPresets(t *testing.T) {
	t.Run("MarkForVPNBypass", func(t *testing.T) {
		rule := MarkForVPNBypass("1.2.3.4")
		if rule.Mark != network.MarkBypassVPN {
			t.Errorf("Wrong mark: got %d, want %d", rule.Mark, network.MarkBypassVPN)
		}
		if rule.DstIP != "1.2.3.4" {
			t.Errorf("Wrong dst IP: got %s, want 1.2.3.4", rule.DstIP)
		}
		if !rule.SaveMark {
			t.Error("SaveMark should be true")
		}
	})

	t.Run("MarkForWAN", func(t *testing.T) {
		rule := MarkForWAN(2, "192.168.1.0/24")
		expectedMark := network.MarkWAN1 + 1 // WAN2
		if rule.Mark != expectedMark {
			t.Errorf("Wrong mark: got %d, want %d", rule.Mark, expectedMark)
		}
		if rule.SrcIP != "192.168.1.0/24" {
			t.Errorf("Wrong src IP: got %s", rule.SrcIP)
		}
	})

	t.Run("MarkByPort", func(t *testing.T) {
		rule := MarkByPort("tcp", 443, network.MarkQoSInteractive)
		if rule.Protocol != "tcp" {
			t.Errorf("Wrong protocol: got %s", rule.Protocol)
		}
		if rule.DstPort != 443 {
			t.Errorf("Wrong port: got %d", rule.DstPort)
		}
		if rule.Mark != network.MarkQoSInteractive {
			t.Errorf("Wrong mark: got %d", rule.Mark)
		}
	})

	t.Run("MarkForWireGuard", func(t *testing.T) {
		rule := MarkForWireGuard(0, "10.0.0.0/8")
		expectedMark := network.MarkForWireGuard(0)
		if rule.Mark != expectedMark {
			t.Errorf("Wrong mark: got %d, want %d", rule.Mark, expectedMark)
		}
	})

	t.Run("MarkForTailscale", func(t *testing.T) {
		rule := MarkForTailscale(1, "100.64.0.0/10")
		expectedMark := network.MarkForTailscale(1)
		if rule.Mark != expectedMark {
			t.Errorf("Wrong mark: got %d, want %d", rule.Mark, expectedMark)
		}
	})

	t.Run("MarkForVPNTunnel", func(t *testing.T) {
		testCases := []struct {
			vpnType string
			index   int
		}{
			{"wireguard", 0},
			{"wg", 1},
			{"tailscale", 0},
			{"ts", 1},
			{"openvpn", 0},
			{"ipsec", 0},
			{"custom", 0},
		}

		for _, tc := range testCases {
			rule := MarkForVPNTunnel(tc.vpnType, tc.index, "10.0.0.0/8")
			if rule.Mark == 0 {
				t.Errorf("Mark should not be 0 for %s", tc.vpnType)
			}
			if !rule.Enabled {
				t.Errorf("Rule should be enabled for %s", tc.vpnType)
			}
		}
	})

	t.Run("MarkForQoS", func(t *testing.T) {
		classes := []string{"realtime", "interactive", "bulk", "background", "unknown"}
		for _, class := range classes {
			rule := MarkForQoS(class, "udp", 5060)
			if rule.Mark == 0 {
				t.Errorf("Mark should not be 0 for class %s", class)
			}
			if rule.Protocol != "udp" {
				t.Errorf("Wrong protocol for class %s", class)
			}
		}
	})
}

func TestProtocolToNumber(t *testing.T) {
	tests := []struct {
		proto    string
		expected uint8
	}{
		{"tcp", 6},
		{"udp", 17},
		{"icmp", 1},
		{"icmpv6", 58},
		{"gre", 47},
		{"esp", 50},
		{"ah", 51},
		{"sctp", 132},
		{"unknown", 0},
		{"", 0},
	}

	for _, tc := range tests {
		result := protocolToNumber(tc.proto)
		if result != tc.expected {
			t.Errorf("protocolToNumber(%s) = %d, want %d", tc.proto, result, tc.expected)
		}
	}
}
