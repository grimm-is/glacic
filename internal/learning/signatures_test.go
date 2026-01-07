package learning

import "testing"

func TestIdentifyApp(t *testing.T) {
	tests := []struct {
		domain string
		want   string
	}{
		{"netflix.com", "Netflix"},
		{"www.netflix.com", "Netflix"},
		{"sub.netflix.com", "Netflix"},
		{"netflix.net", "Netflix"},
		{"notnetflix.com", ""}, // Suffix match boundary check
		{"mynetflix.com", ""},  // Suffix match boundary check
		{"youtube.com", "YouTube"},
		{"rr1.googlevideo.com", "YouTube"},
		{"example.com", ""},
		{"", ""},
		// Case insensitivity
		{"NETFLIX.COM", "Netflix"},
		{"WwW.NeTfLiX.cOm", "Netflix"},
		// Longest match logic (hypothetical, if we had overlapping suffixes)
		// Current map doesn't have overlapping suffixes for different apps,
		// but checking known ones works.
	}

	for _, tt := range tests {
		t.Run(tt.domain, func(t *testing.T) {
			got := IdentifyApp(tt.domain)
			if got != tt.want {
				t.Errorf("IdentifyApp(%q) = %q; want %q", tt.domain, got, tt.want)
			}
		})
	}
}
