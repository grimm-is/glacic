package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCanonicalize(t *testing.T) {
	// Setup input config with deprecated fields
	input := &Config{
		SchemaVersion: "1.0",
		Interfaces: []Interface{
			{Name: "eth0", Zone: "wan"},
			{Name: "eth1", Zone: "lan"},
			{Name: "eth2"}, // No zone
		},
		Zones: []Zone{
			{
				Name:       "lan",
				Interfaces: []string{"wlan0"}, // Deprecated list
			},
			{
				Name: "dmz", // Empty matches
			},
		},
	}

	// Run canonicalization
	err := input.Canonicalize()
	assert.NoError(t, err)

	// Verify deprecated fields are cleared
	assert.Empty(t, input.Interfaces[0].Zone)
	assert.Empty(t, input.Interfaces[1].Zone)
	assert.Nil(t, input.Zones[0].Interfaces)

	// Verify migration to Matches/Interface fields

	// WAN zone should be created and have match for eth0
	wan := findOrCreateZoneForMigration(input, "wan")
	assert.NotNil(t, wan)
	assert.Equal(t, "wan", wan.Name)
	assert.Len(t, wan.Matches, 1)
	assert.Equal(t, "eth0", wan.Matches[0].Interface)

	// LAN zone should have matches for eth1 (from interface) and wlan0 (from zone.interfaces)
	lan := findOrCreateZoneForMigration(input, "lan")
	assert.NotNil(t, lan)
	assert.Len(t, lan.Matches, 2)

	// Check content of matches (order depends on implementation, so check existence)
	eth1Found := false
	wlan0Found := false
	for _, m := range lan.Matches {
		if m.Interface == "eth1" {
			eth1Found = true
		}
		if m.Interface == "wlan0" {
			wlan0Found = true
		}
	}
	assert.True(t, eth1Found, "eth1 should be in lan zone matches")
	assert.True(t, wlan0Found, "wlan0 should be in lan zone matches")
}
