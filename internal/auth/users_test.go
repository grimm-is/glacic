package auth

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func tempAuthPath(t *testing.T) string {
	dir := t.TempDir()
	return filepath.Join(dir, "auth.json")
}

func TestNewStore(t *testing.T) {
	path := tempAuthPath(t)
	store, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	if store == nil {
		t.Fatal("NewStore returned nil")
	}
}

func TestCreateUser(t *testing.T) {
	path := tempAuthPath(t)
	store, _ := NewStore(path)

	err := store.CreateUser("admin", "password123", RoleAdmin)
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	// Verify user exists
	user, err := store.GetUser("admin")
	if err != nil {
		t.Fatalf("GetUser failed: %v", err)
	}
	if user.Username != "admin" {
		t.Errorf("Username = %q, want %q", user.Username, "admin")
	}
	if user.Role != RoleAdmin {
		t.Errorf("Role = %q, want %q", user.Role, RoleAdmin)
	}
}

func TestCreateUserDuplicate(t *testing.T) {
	path := tempAuthPath(t)
	store, _ := NewStore(path)

	store.CreateUser("admin", "password123", RoleAdmin)
	err := store.CreateUser("admin", "different", RoleAdmin)
	if err == nil {
		t.Error("Expected error for duplicate user")
	}
}

func TestCreateUserValidation(t *testing.T) {
	path := tempAuthPath(t)
	store, _ := NewStore(path)

	tests := []struct {
		name     string
		username string
		password string
		wantErr  bool
	}{
		{"empty username", "", "password", true},
		{"empty password", "user", "", true},
		{"both empty", "", "", true},
		{"valid", "user", "password", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := store.CreateUser(tt.username, tt.password, RoleViewer)
			if (err != nil) != tt.wantErr {
				t.Errorf("CreateUser() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestAuthenticate(t *testing.T) {
	path := tempAuthPath(t)
	store, _ := NewStore(path)

	store.CreateUser("testuser", "correctpassword", RoleViewer)

	// Test correct password
	session, err := store.Authenticate("testuser", "correctpassword")
	if err != nil {
		t.Fatalf("Authenticate failed: %v", err)
	}
	if session == nil {
		t.Fatal("Session is nil")
	}
	if session.Token == "" {
		t.Error("Session token is empty")
	}
	if session.Username != "testuser" {
		t.Errorf("Session username = %q, want %q", session.Username, "testuser")
	}
}

func TestAuthenticateWrongPassword(t *testing.T) {
	path := tempAuthPath(t)
	store, _ := NewStore(path)

	store.CreateUser("testuser", "correctpassword", RoleViewer)

	_, err := store.Authenticate("testuser", "wrongpassword")
	if err == nil {
		t.Error("Expected error for wrong password")
	}
}

func TestAuthenticateNonexistentUser(t *testing.T) {
	path := tempAuthPath(t)
	store, _ := NewStore(path)

	_, err := store.Authenticate("nonexistent", "password")
	if err == nil {
		t.Error("Expected error for nonexistent user")
	}
}

func TestValidateSession(t *testing.T) {
	path := tempAuthPath(t)
	store, _ := NewStore(path)

	store.CreateUser("testuser", "password", RoleAdmin)
	session, _ := store.Authenticate("testuser", "password")

	user, err := store.ValidateSession(session.Token)
	if err != nil {
		t.Fatalf("ValidateSession failed: %v", err)
	}
	if user.Username != "testuser" {
		t.Errorf("User = %q, want %q", user.Username, "testuser")
	}
}

func TestValidateSessionInvalid(t *testing.T) {
	path := tempAuthPath(t)
	store, _ := NewStore(path)

	_, err := store.ValidateSession("invalid-token")
	if err == nil {
		t.Error("Expected error for invalid token")
	}
}

func TestLogout(t *testing.T) {
	path := tempAuthPath(t)
	store, _ := NewStore(path)

	store.CreateUser("testuser", "password", RoleViewer)
	session, _ := store.Authenticate("testuser", "password")

	err := store.Logout(session.Token)
	if err != nil {
		t.Fatalf("Logout failed: %v", err)
	}

	// Session should no longer be valid
	_, err = store.ValidateSession(session.Token)
	if err == nil {
		t.Error("Session should be invalid after logout")
	}
}

func TestHasUsers(t *testing.T) {
	path := tempAuthPath(t)
	store, _ := NewStore(path)

	if store.HasUsers() {
		t.Error("HasUsers should be false initially")
	}

	store.CreateUser("admin", "password", RoleAdmin)

	if !store.HasUsers() {
		t.Error("HasUsers should be true after creating user")
	}
}

func TestListUsers(t *testing.T) {
	path := tempAuthPath(t)
	store, _ := NewStore(path)

	store.CreateUser("admin", "pass1", RoleAdmin)
	store.CreateUser("viewer", "pass2", RoleViewer)

	users := store.ListUsers()
	if len(users) != 2 {
		t.Errorf("ListUsers returned %d users, want 2", len(users))
	}

	// Verify hashes are not exposed
	for _, u := range users {
		if u.Hash != "" {
			t.Errorf("User %q has exposed hash", u.Username)
		}
	}
}

func TestUpdatePassword(t *testing.T) {
	path := tempAuthPath(t)
	store, _ := NewStore(path)

	store.CreateUser("testuser", "oldpassword", RoleViewer)

	err := store.UpdatePassword("testuser", "newpassword")
	if err != nil {
		t.Fatalf("UpdatePassword failed: %v", err)
	}

	// Old password should fail
	_, err = store.Authenticate("testuser", "oldpassword")
	if err == nil {
		t.Error("Old password should not work")
	}

	// New password should work
	_, err = store.Authenticate("testuser", "newpassword")
	if err != nil {
		t.Errorf("New password failed: %v", err)
	}
}

func TestDeleteUser(t *testing.T) {
	path := tempAuthPath(t)
	store, _ := NewStore(path)

	store.CreateUser("admin", "pass", RoleAdmin)
	store.CreateUser("viewer", "pass", RoleViewer)

	err := store.DeleteUser("viewer")
	if err != nil {
		t.Fatalf("DeleteUser failed: %v", err)
	}

	_, err = store.GetUser("viewer")
	if err == nil {
		t.Error("User should not exist after deletion")
	}
}

func TestDeleteLastAdmin(t *testing.T) {
	path := tempAuthPath(t)
	store, _ := NewStore(path)

	store.CreateUser("admin", "pass", RoleAdmin)

	err := store.DeleteUser("admin")
	if err == nil {
		t.Error("Should not be able to delete last admin")
	}
}

func TestDeleteUserRemovesSessions(t *testing.T) {
	path := tempAuthPath(t)
	store, _ := NewStore(path)

	store.CreateUser("admin", "pass", RoleAdmin)
	store.CreateUser("viewer", "pass", RoleViewer)
	session, _ := store.Authenticate("viewer", "pass")

	store.DeleteUser("viewer")

	_, err := store.ValidateSession(session.Token)
	if err == nil {
		t.Error("Session should be invalid after user deletion")
	}
}

func TestRoleCanAccess(t *testing.T) {
	tests := []struct {
		role   Role
		action string
		want   bool
	}{
		{RoleAdmin, "view", true},
		{RoleAdmin, "modify", true},
		{RoleAdmin, "admin", true},
		{RoleOperator, "view", true},
		{RoleOperator, "modify", true},
		{RoleOperator, "admin", false},
		{RoleViewer, "view", true},
		{RoleViewer, "modify", false},
		{RoleViewer, "admin", false},
		{RoleAdmin, "unknown", false},
	}

	for _, tt := range tests {
		t.Run(string(tt.role)+"_"+tt.action, func(t *testing.T) {
			if got := tt.role.CanAccess(tt.action); got != tt.want {
				t.Errorf("%s.CanAccess(%q) = %v, want %v", tt.role, tt.action, got, tt.want)
			}
		})
	}
}

func TestStorePersistence(t *testing.T) {
	path := tempAuthPath(t)

	// Create store and add user
	store1, _ := NewStore(path)
	store1.CreateUser("persistent", "password", RoleAdmin)

	// Create new store from same path
	store2, _ := NewStore(path)
	user, err := store2.GetUser("persistent")
	if err != nil {
		t.Fatalf("User not persisted: %v", err)
	}
	if user.Username != "persistent" {
		t.Errorf("Username = %q, want %q", user.Username, "persistent")
	}
}

func TestSessionExpiry(t *testing.T) {
	path := tempAuthPath(t)
	store, _ := NewStore(path)

	store.CreateUser("testuser", "password", RoleViewer)
	session, _ := store.Authenticate("testuser", "password")

	// Manually expire the session
	store.mu.Lock()
	store.sessions[session.Token].ExpiresAt = time.Now().Add(-1 * time.Hour)
	store.mu.Unlock()

	_, err := store.ValidateSession(session.Token)
	if err == nil {
		t.Error("Expired session should be invalid")
	}
}

func TestSessionTokenUniqueness(t *testing.T) {
	path := tempAuthPath(t)
	store, _ := NewStore(path)

	store.CreateUser("testuser", "password", RoleViewer)

	tokens := make(map[string]bool)
	for i := 0; i < 10; i++ {
		session, _ := store.Authenticate("testuser", "password")
		if tokens[session.Token] {
			t.Error("Duplicate session token generated")
		}
		tokens[session.Token] = true
	}
}

func TestFilePermissions(t *testing.T) {
	path := tempAuthPath(t)
	store, _ := NewStore(path)
	store.CreateUser("admin", "password", RoleAdmin)

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Failed to stat auth file: %v", err)
	}

	// File should be readable/writable only by owner
	perm := info.Mode().Perm()
	if perm != 0600 {
		t.Errorf("Auth file permissions = %o, want 0600", perm)
	}
}
