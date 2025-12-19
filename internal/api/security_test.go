package api

import (
	"strings"
	"testing"

	"grimm.is/glacic/internal/firewall"
	"grimm.is/glacic/internal/logging"
)

// MockIPSetService is a partial mock for IPSetService
// Since we can't easily mock the struct method receiver in Go without an interface,
// we'll rely on integration-style testing or we'd need to refactor SecurityManager to take an interface.
// Given strict instructions to "fix logic gaps", and the current architecture uses struct pointers,
// we will have to test with a real IPSetService or refactor to interface.
// However, IPSetService depends on system commands (nft).
// A better approach for unit testing SecurityManager is to interface-ize the dependency.

// Let's verify if we can modify SecurityManager to use an interface.
// For now, to avoid large refactors, we can skip mocking if we can't easily,
// AND proceed if we decide to test the logic flow which we just enabled.
// BUT, testing without mocks for system calls is hard.
// Actually, IPSetService uses IPSetManager which uses a CommandRunner.
// We can mock the CommandRunner!

type mockRunner struct {
	cmds []string
}

func (m *mockRunner) Run(cmd string, args ...string) error {
	m.cmds = append(m.cmds, cmd+" "+strings.Join(args, " "))
	return nil
}

func (m *mockRunner) RunInput(input string, cmd string, args ...string) error {
	m.cmds = append(m.cmds, "INPUT:"+input)
	return nil
}

func (m *mockRunner) Output(cmd string, args ...string) ([]byte, error) {
	return []byte{}, nil
}

func TestSecurityManager_BlockIP(t *testing.T) {
	// Setup
	logger := logging.New(logging.DefaultConfig())
	runner := &mockRunner{}

	// Create IPSetService with mocked runner
	// Note: IPSetService constructor creates new managers internally.
	// We need to access them to inject the runner.
	// This makes unit testing hard without refactoring IPSetService to accept dependencies.
	// We will rely on the fact that we fixed the logic gap (calling the method).
	// But let's verify nil checks at least.

	// Test Nil Service
	smNil := NewSecurityManager(nil, logger) // Nil service
	err := smNil.BlockIP("1.2.3.4", "test")
	if err == nil {
		t.Error("Expected error when service is nil")
	} else if err.Error() != "IPSet service not available" {
		t.Errorf("Unexpected error: %v", err)
	}

	// Test Happy Path
	svc := firewall.NewIPSetService("test_table", "/tmp", nil, logger)
	svc.GetIPSetManager().SetRunner(runner)
	sm := NewSecurityManager(svc, logger)

	err = sm.BlockIP("1.2.3.4", "test")
	if err != nil {
		t.Errorf("BlockIP failed: %v", err)
	}

	// Verify command was run
	found := false
	for _, cmd := range runner.cmds {
		if cmd == "nft add element inet test_table blocked_ips { 1.2.3.4 }" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected blocking command not found in %v", runner.cmds)
	}
}

// Since we cannot easily inject a mock runner into IPSetService without changing its constructor or fields visible,
// We will modify IPSetService to allow injecting dependencies or add a SetRunner method for testing if needed.
// Checking ipset_service.go... it has NewIPSetService but managers are private fields with no setters.
// IPSetManager has SetRunner. IPSetService has ipsetManager field (private).
// So we can't set the runner on IPSetService's manager from outside package firewall.
// BUT, we are in package api.
// We can test this by adding a Test helper in package firewall? No, we want to test api/security.go.
//
// Plan: Just testing the NIL case and the happy path logic flow (assuming service works) is tricky.
// We'll create a test that verifies the logic structure.
