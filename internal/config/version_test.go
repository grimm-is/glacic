package config

import (
	"testing"
)

func TestParseVersion(t *testing.T) {
	tests := []struct {
		input    string
		expected SchemaVersion
		wantErr  bool
	}{
		{"1.0", SchemaVersion{Major: 1, Minor: 0}, false},
		{"2.1", SchemaVersion{Major: 2, Minor: 1}, false},
		{"10.20", SchemaVersion{Major: 10, Minor: 20}, false},
		{"", SchemaVersion{Major: 1, Minor: 0}, false}, // Empty defaults to 1.0
		{"1", SchemaVersion{}, true},                   // Invalid format
		{"1.0.0", SchemaVersion{}, true},               // Too many parts
		{"a.b", SchemaVersion{}, true},                 // Non-numeric
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseVersion(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseVersion(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.expected {
				t.Errorf("ParseVersion(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

func TestSchemaVersionCompare(t *testing.T) {
	tests := []struct {
		v1, v2   SchemaVersion
		expected int
	}{
		{SchemaVersion{1, 0}, SchemaVersion{1, 0}, 0},
		{SchemaVersion{1, 0}, SchemaVersion{1, 1}, -1},
		{SchemaVersion{1, 1}, SchemaVersion{1, 0}, 1},
		{SchemaVersion{1, 0}, SchemaVersion{2, 0}, -1},
		{SchemaVersion{2, 0}, SchemaVersion{1, 9}, 1},
	}

	for _, tt := range tests {
		t.Run(tt.v1.String()+"_vs_"+tt.v2.String(), func(t *testing.T) {
			got := tt.v1.Compare(tt.v2)
			if got != tt.expected {
				t.Errorf("Compare(%v, %v) = %d, want %d", tt.v1, tt.v2, got, tt.expected)
			}
		})
	}
}

func TestSchemaVersionNeedsMigration(t *testing.T) {
	tests := []struct {
		from, to SchemaVersion
		expected bool
	}{
		{SchemaVersion{1, 0}, SchemaVersion{1, 0}, false}, // Same version
		{SchemaVersion{1, 0}, SchemaVersion{1, 1}, true},  // Minor upgrade
		{SchemaVersion{1, 0}, SchemaVersion{2, 0}, true},  // Major upgrade
		{SchemaVersion{2, 0}, SchemaVersion{1, 0}, false}, // Downgrade - no migration
	}

	for _, tt := range tests {
		t.Run(tt.from.String()+"_to_"+tt.to.String(), func(t *testing.T) {
			got := tt.from.NeedsMigration(tt.to)
			if got != tt.expected {
				t.Errorf("NeedsMigration(%v, %v) = %v, want %v", tt.from, tt.to, got, tt.expected)
			}
		})
	}
}

func TestIsSupportedVersion(t *testing.T) {
	tests := []struct {
		version  SchemaVersion
		expected bool
	}{
		{SchemaVersion{1, 0}, true},
		{SchemaVersion{1, 5}, true},  // Same major version
		{SchemaVersion{2, 0}, false}, // Not yet supported
		{SchemaVersion{0, 1}, false}, // Too old
	}

	for _, tt := range tests {
		t.Run(tt.version.String(), func(t *testing.T) {
			got := IsSupportedVersion(tt.version)
			if got != tt.expected {
				t.Errorf("IsSupportedVersion(%v) = %v, want %v", tt.version, got, tt.expected)
			}
		})
	}
}
