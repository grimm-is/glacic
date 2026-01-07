package routing

import (
	"strings"
	"testing"

	"grimm.is/glacic/internal/config"
)

func TestGenerateDaemonsFile(t *testing.T) {
	tests := []struct {
		name string
		cfg  *config.FRRConfig
		want []string
	}{
		{
			name: "OSPF Enabled",
			cfg: &config.FRRConfig{
				OSPF: &config.OSPF{},
			},
			want: []string{"ospfd=yes", "bgpd=no"},
		},
		{
			name: "BGP Enabled",
			cfg: &config.FRRConfig{
				BGP: &config.BGP{},
			},
			want: []string{"ospfd=no", "bgpd=yes"},
		},
		{
			name: "Both Enabled",
			cfg: &config.FRRConfig{
				OSPF: &config.OSPF{},
				BGP:  &config.BGP{},
			},
			want: []string{"ospfd=yes", "bgpd=yes"},
		},
		{
			name: "None Enabled",
			cfg:  &config.FRRConfig{},
			want: []string{"ospfd=no", "bgpd=no"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := generateDaemonsFile(tt.cfg)
			for _, w := range tt.want {
				if !strings.Contains(got, w) {
					t.Errorf("generateDaemonsFile() = %v, want to contain %v", got, w)
				}
			}
		})
	}
}

func TestGenerateFRRConf(t *testing.T) {
	tests := []struct {
		name string
		cfg  *config.FRRConfig
		want []string
	}{
		{
			name: "Basic",
			cfg:  &config.FRRConfig{},
			want: []string{
				"frr version 8.0",
				"hostname firewall",
			},
		},
		{
			name: "OSPF",
			cfg: &config.FRRConfig{
				OSPF: &config.OSPF{
					RouterID: "1.1.1.1",
					Networks: []string{"192.168.1.0/24"},
					Areas: []config.OSPFArea{
						{ID: "0.0.0.1", Networks: []string{"10.0.0.0/24"}},
					},
				},
			},
			want: []string{
				"router ospf",
				"ospf router-id 1.1.1.1",
				"network 192.168.1.0/24 area 0.0.0.0",
				"network 10.0.0.0/24 area 0.0.0.1",
			},
		},
		{
			name: "BGP",
			cfg: &config.FRRConfig{
				BGP: &config.BGP{
					ASN:      65000,
					RouterID: "2.2.2.2",
					Networks: []string{"172.16.0.0/12"},
					Neighbors: []config.Neighbor{
						{IP: "10.0.0.2", RemoteASN: 65001},
					},
				},
			},
			want: []string{
				"router bgp 65000",
				"bgp router-id 2.2.2.2",
				"network 172.16.0.0/12",
				"neighbor 10.0.0.2 remote-as 65001",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := generateFRRConf(tt.cfg)
			for _, w := range tt.want {
				if !strings.Contains(got, w) {
					t.Errorf("generateFRRConf() missing %q", w)
				}
			}
		})
	}
}
