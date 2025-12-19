package firewall

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/mock"
)

func TestIPSetManager_ReloadSet_Injection(t *testing.T) {
	mockRunner := new(MockCommandRunner)
	manager := NewIPSetManager("glacic")
	manager.SetRunner(mockRunner)

	// Happy Path
	validNames := []string{"safe_set", "set-123", "ExampleSet"}
	for _, name := range validNames {
		// Expect call to nft
		// Note: ReloadSet calls runNftScript which calls runner.RunInput
		// We mock RunInput to return nil
		// RunInput(input, name, args...)
		mockRunner.On("RunInput", mock.Anything, "nft", "-f", "-").Return(nil)

		err := manager.ReloadSet(name, []string{"1.1.1.1"})
		if err != nil {
			t.Errorf("ReloadSet failed for valid name %s: %v", name, err)
		}
	}

	// Sad Path - Injection Attempts
	invalidNames := []string{
		"set; rm -rf /",
		"set$(whoami)",
		"set|cat /etc/passwd",
		"set space",
		"set/slash",
	}

	for _, name := range invalidNames {
		err := manager.ReloadSet(name, []string{"1.1.1.1"})
		if err == nil {
			t.Errorf("ReloadSet should have failed for invalid name: %s", name)
		} else if !strings.Contains(err.Error(), "invalid set name") {
			// Ensure it failed for the right reason (validation), not some other error
			t.Errorf("Expected invalid ipset name error for %s, got: %v", name, err)
		}
	}
}

func TestIPSetManager_CreateSet_Injection(t *testing.T) {
	mockRunner := new(MockCommandRunner)
	manager := NewIPSetManager("glacic")
	manager.SetRunner(mockRunner)

	// Happy Path - use mock.Anything for variadic args
	mockRunner.On("Run", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
	if err := manager.CreateSet("valid_set", "ipv4_addr"); err != nil {
		t.Errorf("CreateSet failed for valid name: %v", err)
	}

	// Sad Path
	invalidName := "set;reboot"
	if err := manager.CreateSet(invalidName, "ipv4_addr"); err == nil {
		t.Error("CreateSet should have failed for invalid name")
	}
}
