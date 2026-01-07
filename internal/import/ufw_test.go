package imports

import (
	"os"
	"testing"
)

func TestParseUFWRules(t *testing.T) {
	// UFW 'status numbered' output often used for parsing
	// Or /etc/ufw/user.rules

	// Assuming ParseUFWRules takes the file content of user.rules
	content := `
*filter
:ufw-user-input - [0:0]
:ufw-user-output - [0:0]
:ufw-user-forward - [0:0]
### tuple ### allow tcp 22 0.0.0.0/0 any 0.0.0.0/0 in
-A ufw-user-input -p tcp --dport 22 -j ACCEPT
### tuple ### allow udp 53 0.0.0.0/0 any 0.0.0.0/0 in
-A ufw-user-input -p udp --dport 53 -j ACCEPT
COMMIT
`
	tmpFile, err := os.CreateTemp("", "ufw-rules-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())
	if _, err := tmpFile.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()

	result, err := ParseUFWRules(tmpFile.Name())
	if err != nil {
		t.Fatalf("ParseUFWRules failed: %v", err)
	}

	// Verify through ImportResult which standardizes rules
	res := result.ToImportResult()

	if len(res.FilterRules) < 2 {
		t.Errorf("Expected at least 2 rules, got %d", len(res.FilterRules))
	}

	ssh := res.FilterRules[0]
	if ssh.Protocol != "tcp" || ssh.DestPort != "22" || ssh.Action != "accept" {
		t.Error("SSH rule parsing failed")
	}
}

func TestParseUFWStatus(t *testing.T) {
	// If the parser supports 'ufw status' output
	// Placeholder for future status parsing test
}
