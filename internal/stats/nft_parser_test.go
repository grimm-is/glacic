package stats

import (
	"testing"
)

func TestParseNFTCounters(t *testing.T) {
	// Sample nft -j list ruleset output with counters and comments
	jsonData := []byte(`{
		"nftables": [
			{"metainfo": {"version": "1.0.2", "json_schema_version": 1}},
			{"table": {"family": "inet", "name": "glacic", "handle": 1}},
			{"chain": {"family": "inet", "table": "glacic", "name": "input", "handle": 1}},
			{
				"rule": {
					"family": "inet",
					"table": "glacic",
					"chain": "input",
					"handle": 5,
					"expr": [
						{"match": {"op": "==", "left": {"payload": {"protocol": "tcp", "field": "dport"}}, "right": 22}},
						{"counter": {"packets": 150, "bytes": 12500}},
						{"accept": null},
						{"comment": "rule:allow_ssh"}
					]
				}
			},
			{
				"rule": {
					"family": "inet",
					"table": "glacic",
					"chain": "input",
					"handle": 6,
					"expr": [
						{"match": {"op": "==", "left": {"payload": {"protocol": "tcp", "field": "dport"}}, "right": 80}},
						{"counter": {"packets": 500, "bytes": 45000}},
						{"accept": null},
						{"comment": "rule:allow_http"}
					]
				}
			},
			{
				"rule": {
					"family": "inet",
					"table": "glacic",
					"chain": "input",
					"handle": 7,
					"expr": [
						{"counter": {"packets": 10, "bytes": 800}},
						{"drop": null}
					]
				}
			}
		]
	}`)

	counters, err := ParseNFTCounters(jsonData)
	if err != nil {
		t.Fatalf("ParseNFTCounters failed: %v", err)
	}

	// Should have 2 rules with IDs (the third has no comment)
	if len(counters) != 2 {
		t.Errorf("Expected 2 counters, got %d", len(counters))
	}

	if counters["allow_ssh"] != 12500 {
		t.Errorf("Expected allow_ssh bytes=12500, got %d", counters["allow_ssh"])
	}

	if counters["allow_http"] != 45000 {
		t.Errorf("Expected allow_http bytes=45000, got %d", counters["allow_http"])
	}
}

func TestParseNFTCounters_Empty(t *testing.T) {
	jsonData := []byte(`{"nftables": [{"metainfo": {"version": "1.0.2"}}]}`)

	counters, err := ParseNFTCounters(jsonData)
	if err != nil {
		t.Fatalf("ParseNFTCounters failed: %v", err)
	}

	if len(counters) != 0 {
		t.Errorf("Expected 0 counters, got %d", len(counters))
	}
}

func TestParseNFTCounters_InvalidJSON(t *testing.T) {
	_, err := ParseNFTCounters([]byte(`not json`))
	if err == nil {
		t.Error("Expected error for invalid JSON")
	}
}

func TestExtractRuleID(t *testing.T) {
	tests := []struct {
		comment  string
		expected string
	}{
		{"rule:allow_ssh", "allow_ssh"},
		{"rule:uuid-1234-5678", "uuid-1234-5678"},
		{"rule:", ""},
		{"not a rule comment", ""},
		{"", ""},
		{"RULE:uppercase", ""}, // Case-sensitive
	}

	for _, tt := range tests {
		result := extractRuleID(tt.comment)
		if result != tt.expected {
			t.Errorf("extractRuleID(%q) = %q, want %q", tt.comment, result, tt.expected)
		}
	}
}
