package ctlplane

import (
	"testing"
)

// MockIPSetService is a partial mock for testing RPC calls
type MockIPSetService struct {
	// Embed real service structure if needed, or better, use an interface?
	// The server holds *firewall.IPSetService struct pointer.
	// So we can't easily interface-mock it unless we refactor Server to use an interface.
	// However, firewall.IPSetService has public methods we can call.
	// For this test, we might need a real IPSetService with a mock IPSetManager/Executor?
	// But firewall package isn't mocked here easily.
	//
	// Alternative: Use a real IPSetService with a mock StateStore and "noop" executor?
	// IPSetService.NewIPSetService uses a real executor? No, it initializes one.
	//
	// If we just want to test RPC wiring, we can pass a nil service and check for error "service not available",
	// OR we assume unit tests for IPSetService exist elsewhere and we just test that
	// calling the RPC method invokes the service method.
	//
	// Given we integrated `ipsetService` into `Server` struct directly, we should
	// try to construct a dummy service.
}

func TestServer_RPC_IPSet(t *testing.T) {
	// Setup minimalist server
	server := &Server{}

	// Test Case 1: Service Not Initialized
	t.Run("ServiceNotInitialized", func(t *testing.T) {
		args := &Empty{}
		reply := &ListIPSetsReply{}

		if err := server.ListIPSets(args, reply); err != nil {
			// RPC shouldn't error, but return error in reply?
			// The method signature is `func ... error`.
			// Our implementation:
			// if s.ipsetService == nil { reply.Error = "..."; return nil }
			// So err should be nil, reply.Error should be set.
			t.Fatalf("ListIPSets RPC call failed: %v", err)
		}

		if reply.Error != "IPSet service not available" {
			t.Errorf("Expected 'IPSet service not available', got '%s'", reply.Error)
		}
	})

	// Test Case 2: Service Initialized (with nil dependencies potentially causing panic if called deep?)
	// We can manually inject a nil service to simulate "initialized but broken" or just
	// to verification that logic reaches the service call.
	// Since `firewall.NewIPSetService` requires dependencies, verifying full integration
	// here is hard without full mocks.
	//
	// HOWEVER, we can just Verify method signatures match and wiring exists.
	// The previous test confirms the "guard clause" works.
	//
	// Let's try to verify GetIPSet args passing.
	t.Run("GetIPSet_Guard", func(t *testing.T) {
		args := &GetIPSetArgs{Name: "foo"}
		reply := &GetIPSetReply{}
		server.GetIPSet(args, reply)
		if reply.Error != "IPSet service not available" {
			t.Errorf("Expected error, got %s", reply.Error)
		}
	})

	t.Run("RefreshIPSet_Guard", func(t *testing.T) {
		args := &RefreshIPSetArgs{Name: "foo"}
		reply := &Empty{}
		err := server.RefreshIPSet(args, reply)
		if err == nil {
			t.Error("Expected error for missing service")
		}
		if err != nil && err.Error() != "IPSet service not available" {
			t.Errorf("Expected 'IPSet service not available', got '%v'", err)
		}
	})

	t.Run("GetIPSetElements_Guard", func(t *testing.T) {
		args := &GetIPSetElementsArgs{Name: "blocked_ips"}
		reply := &GetIPSetElementsReply{}
		server.GetIPSetElements(args, reply)
		if reply.Error != "IPSet service not available" {
			t.Errorf("Expected 'IPSet service not available', got '%s'", reply.Error)
		}
	})
}
