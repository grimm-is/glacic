package config

// ExpandPolicies expands wildcard policies into concrete zone pairs.
// A policy with `*` or glob patterns is expanded into multiple policies,
// one for each matching zone.
func ExpandPolicies(policies []Policy, zones []Zone) []Policy {
	// Build zone name list
	zoneNames := make([]string, 0, len(zones))
	for _, z := range zones {
		zoneNames = append(zoneNames, z.Name)
	}
	// Add special zones
	zoneNames = append(zoneNames, "self")

	var expanded []Policy

	for _, policy := range policies {
		// Check if either From or To is a wildcard
		fromWildcard := isWildcardZone(policy.From)
		toWildcard := isWildcardZone(policy.To)

		if !fromWildcard && !toWildcard {
			// No wildcards, keep as-is
			expanded = append(expanded, policy)
			continue
		}

		// Expand wildcards
		var fromZones, toZones []string

		if fromWildcard {
			for _, z := range zoneNames {
				if matchesZone(policy.From, z) {
					fromZones = append(fromZones, z)
				}
			}
		} else {
			fromZones = []string{policy.From}
		}

		if toWildcard {
			for _, z := range zoneNames {
				if matchesZone(policy.To, z) {
					toZones = append(toZones, z)
				}
			}
		} else {
			toZones = []string{policy.To}
		}

		// Generate expanded policies
		for _, from := range fromZones {
			for _, to := range toZones {
				// Skip self-to-self for wildcard expansions (unless explicitly requested)
				if from == to && (fromWildcard || toWildcard) {
					continue
				}

				expandedPolicy := policy // Copy
				expandedPolicy.From = from
				expandedPolicy.To = to
				// Generate name if not set
				if expandedPolicy.Name == "" {
					expandedPolicy.Name = from + "-" + to
				} else {
					expandedPolicy.Name = policy.Name + "_" + from + "_" + to
				}
				expanded = append(expanded, expandedPolicy)
			}
		}
	}

	return expanded
}
