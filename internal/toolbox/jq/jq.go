package jq

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

// Run executes minimal jq
// Syntax: jq 'query' [file]
// Supports: .field, .field.subfield
func Run(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: jq 'query' [file]")
	}

	query := args[0]
	var input io.Reader = os.Stdin

	if len(args) > 1 {
		f, err := os.Open(args[1])
		if err != nil {
			return err
		}
		defer f.Close()
		input = f
	}

	var data interface{}
	decoder := json.NewDecoder(input)
	if err := decoder.Decode(&data); err != nil {
		return err
	}

	result, err := extract(data, query)
	if err != nil {
		return err
	}

	// Print result
	switch v := result.(type) {
	case string:
		// jq -r behavior? tests often want raw strings
		// If strict JSON output needed, tests usually don't use -r.
		// Use JSON encoding for safety unless it's a simple string and we decide to be raw-by-default?
		// "nft -j list ruleset | jq ..."
		// check usage in tests.
		// t/01-sanity/nftables_test.sh:
		// handles_count=$(echo "$json" | jq '.nftables | length')
		// ruleset_family=$(echo "$json" | jq -r '.nftables[1].table.family')
		b, _ := json.Marshal(v)
		fmt.Println(string(b))
	default:
		b, _ := json.MarshalIndent(v, "", "  ")
		fmt.Println(string(b))
	}

	return nil
}

func extract(data interface{}, query string) (interface{}, error) {
	if query == "." {
		return data, nil
	}

	parts := strings.Split(strings.TrimPrefix(query, "."), ".")
	current := data

	for _, part := range parts {
		if part == "" {
			continue
		}

		// Handle array: [0]
		// Not implemented yet

		m, ok := current.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("cannot index non-object with %s", part)
		}

		val, exists := m[part]
		if !exists {
			return nil, nil // null
		}
		current = val
	}

	return current, nil
}
