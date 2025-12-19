package threatintel

import (
	"strings"
	"testing"
)

func TestService_ParseList(t *testing.T) {
	input := `
# Comment
1.2.3.4
5.6.7.8
2001:db8::1

example.com
bad-site.org
`
	s := &Service{}
	v4, v6, domains, err := s.parseList(strings.NewReader(input))
	if err != nil {
		t.Fatalf("parseList failed: %v", err)
	}

	if len(v4) != 2 {
		t.Errorf("Expected 2 IPv4, got %d", len(v4))
	}
	if len(v6) != 1 {
		t.Errorf("Expected 1 IPv6, got %d", len(v6))
	}
	if len(domains) != 2 {
		t.Errorf("Expected 2 domains, got %d", len(domains))
	}

	if v4[0] != "1.2.3.4" {
		t.Errorf("Expected 1.2.3.4, got %s", v4[0])
	}
}
