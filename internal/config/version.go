package config

import (
	"fmt"
	"strconv"
	"strings"
)

// CurrentSchemaVersion is the latest config schema version
// CurrentSchemaVersion is the latest config schema version
// const CurrentSchemaVersion = "1.0" // Moved to config.go

// SchemaVersion represents a semantic version for config schemas
type SchemaVersion struct {
	Major int
	Minor int
}

// ParseVersion parses a version string like "1.0" or "2.1"
func ParseVersion(s string) (SchemaVersion, error) {
	if s == "" {
		// Default to 1.0 for configs without version (legacy)
		return SchemaVersion{Major: 1, Minor: 0}, nil
	}

	parts := strings.Split(s, ".")
	if len(parts) != 2 {
		return SchemaVersion{}, fmt.Errorf("invalid version format: %s (expected X.Y)", s)
	}

	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return SchemaVersion{}, fmt.Errorf("invalid major version: %s", parts[0])
	}

	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return SchemaVersion{}, fmt.Errorf("invalid minor version: %s", parts[1])
	}

	return SchemaVersion{Major: major, Minor: minor}, nil
}

// String returns the version as "X.Y"
func (v SchemaVersion) String() string {
	return fmt.Sprintf("%d.%d", v.Major, v.Minor)
}

// Compare returns -1 if v < other, 0 if equal, 1 if v > other
func (v SchemaVersion) Compare(other SchemaVersion) int {
	if v.Major < other.Major {
		return -1
	}
	if v.Major > other.Major {
		return 1
	}
	if v.Minor < other.Minor {
		return -1
	}
	if v.Minor > other.Minor {
		return 1
	}
	return 0
}

// IsCompatible checks if this version can be read by a reader for targetVersion
// Minor version increases are backward compatible, major version changes are not
func (v SchemaVersion) IsCompatible(targetVersion SchemaVersion) bool {
	return v.Major == targetVersion.Major && v.Minor <= targetVersion.Minor
}

// NeedsMigration returns true if this version needs migration to reach target
func (v SchemaVersion) NeedsMigration(target SchemaVersion) bool {
	return v.Compare(target) < 0
}

// SupportedVersions lists all schema versions we can read
var SupportedVersions = []SchemaVersion{
	{Major: 1, Minor: 0},
}

// IsSupportedVersion checks if we have a reader for this version
func IsSupportedVersion(v SchemaVersion) bool {
	for _, supported := range SupportedVersions {
		if v.Major == supported.Major {
			return true
		}
	}
	return false
}
