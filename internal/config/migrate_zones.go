package config

// migrate_zones.go canonicalizes deprecated zone fields:
//   - Interface.Zone -> Zone.Match
//   - Zone.Interfaces -> Zone.Match

// findOrCreateZoneForMigration returns a pointer to the zone with the given name.
// If it doesn't exist, it creates it.
func findOrCreateZoneForMigration(c *Config, name string) *Zone {
	for i := range c.Zones {
		if c.Zones[i].Name == name {
			return &c.Zones[i]
		}
	}

	newZone := Zone{
		Name:    name,
		Matches: []ZoneMatch{},
	}
	c.Zones = append(c.Zones, newZone)
	return &c.Zones[len(c.Zones)-1]
}

func canonicalizeZones(c *Config) error {
	// 1. Migrate Interface.Zone -> Zone.Match
	for i := range c.Interfaces {
		iface := &c.Interfaces[i]
		if iface.Zone != "" {
			zone := findOrCreateZoneForMigration(c, iface.Zone)

			if zone.Matches == nil {
				zone.Matches = []ZoneMatch{}
			}

			alreadyMatched := false
			for _, m := range zone.Matches {
				if m.Interface == iface.Name {
					alreadyMatched = true
					break
				}
			}

			if !alreadyMatched {
				zone.Matches = append(zone.Matches, ZoneMatch{
					Interface: iface.Name,
				})
			}

			iface.Zone = ""
		}
	}

	// 2. Migrate Zone.Interfaces -> Zone.Match
	for i := range c.Zones {
		zone := &c.Zones[i]

		if len(zone.Interfaces) > 0 {
			if zone.Matches == nil {
				zone.Matches = []ZoneMatch{}
			}

			for _, ifaceName := range zone.Interfaces {
				zone.Matches = append(zone.Matches, ZoneMatch{
					Interface: ifaceName,
				})
			}

			zone.Interfaces = nil
		}
	}

	return nil
}

func init() {
	RegisterPostLoadMigration(PostLoadMigration{
		Name:        "zone_canonicalize",
		Description: "Migrate deprecated zone fields to canonical Match blocks",
		Migrate:     canonicalizeZones,
	})
}
