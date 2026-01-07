// Package auth provides user authentication and session management.
package auth

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"grimm.is/glacic/internal/clock"
	"os"
	"path/filepath"
	"sync"
	"time"

	"grimm.is/glacic/internal/brand"

	"golang.org/x/crypto/bcrypt"
)

// Role defines user permission levels
type Role string

const (
	RoleAdmin    Role = "admin"    // Full access, user management
	RoleOperator Role = "operator" // View & modify config, restart services
	RoleViewer   Role = "viewer"   // Read-only dashboard access
)

// User represents a system user
type User struct {
	Username  string    `json:"username"`
	Hash      string    `json:"hash"` // bcrypt hash
	Role      Role      `json:"role"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Session represents an active login session
type Session struct {
	Token     string    `json:"token"`
	Username  string    `json:"username"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

// Store manages users and sessions
type Store struct {
	path     string
	users    map[string]*User
	sessions map[string]*Session
	mu       sync.RWMutex
}

// AuthData is the persisted auth state
type AuthData struct {
	Users    map[string]*User    `json:"users"`
	Sessions map[string]*Session `json:"sessions"`
}

// DefaultAuthPath is the default location for auth data
// Uses DefaultStateDir which should be writable by the firewall service
var DefaultAuthPath = filepath.Join(brand.GetStateDir(), "auth.json")

// NewStore creates a new auth store
func NewStore(path string) (*Store, error) {
	if path == "" {
		path = DefaultAuthPath
	}

	s := &Store{
		path:     path,
		users:    make(map[string]*User),
		sessions: make(map[string]*Session),
	}

	// Try to load existing data
	if err := s.load(); err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	return s, nil
}

// load reads auth data from disk
func (s *Store) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}

	var authData AuthData
	if err := json.Unmarshal(data, &authData); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if authData.Users != nil {
		s.users = authData.Users
	}
	if authData.Sessions != nil {
		s.sessions = authData.Sessions
	}

	// Clean expired sessions
	now := clock.Now()
	for token, sess := range s.sessions {
		if sess.ExpiresAt.Before(now) {
			delete(s.sessions, token)
		}
	}

	return nil
}

// save writes auth data to disk
// MUST be called while NOT holding the lock (will acquire RLock internally)
func (s *Store) save() error {
	s.mu.RLock()
	authData := AuthData{
		Users:    s.users,
		Sessions: s.sessions,
	}
	s.mu.RUnlock()

	data, err := json.MarshalIndent(authData, "", "  ")
	if err != nil {
		return err
	}

	// Ensure directory exists
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	// Write atomically
	tmpPath := s.path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmpPath, s.path)
}

// saveLocked writes auth data to disk
// MUST be called while holding the write lock
func (s *Store) saveLocked() error {
	authData := AuthData{
		Users:    s.users,
		Sessions: s.sessions,
	}

	data, err := json.MarshalIndent(authData, "", "  ")
	if err != nil {
		return err
	}

	// Ensure directory exists
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	// Write atomically
	tmpPath := s.path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmpPath, s.path)
}

// HasUsers returns true if any users exist
func (s *Store) HasUsers() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.users) > 0
}

// CreateUser creates a new user
func (s *Store) CreateUser(username, password string, role Role) error {
	if username == "" || password == "" {
		return errors.New("username and password required")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.users[username]; exists {
		return errors.New("user already exists")
	}

	now := clock.Now()
	s.users[username] = &User{
		Username:  username,
		Hash:      string(hash),
		Role:      role,
		CreatedAt: now,
		UpdatedAt: now,
	}

	return s.saveLocked()
}

// Authenticate validates credentials and returns a session
func (s *Store) Authenticate(username, password string) (*Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	user, exists := s.users[username]
	if !exists {
		return nil, errors.New("invalid credentials")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Hash), []byte(password)); err != nil {
		return nil, errors.New("invalid credentials")
	}

	// Generate session token
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, err
	}
	token := hex.EncodeToString(tokenBytes)

	session := &Session{
		Token:     token,
		Username:  username,
		CreatedAt: clock.Now(),
		ExpiresAt: clock.Now().Add(24 * time.Hour),
	}

	s.sessions[token] = session

	if err := s.saveLocked(); err != nil {
		return nil, err
	}

	return session, nil
}

// ValidateSession checks if a session token is valid
func (s *Store) ValidateSession(token string) (*User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// SECURITY: If no users exist, reject ALL sessions
	// This prevents stale session cookies from authenticating after a clean rebuild
	if len(s.users) == 0 {
		return nil, errors.New("no users configured")
	}

	session, exists := s.sessions[token]
	if !exists {
		return nil, errors.New("invalid session")
	}

	if session.ExpiresAt.Before(clock.Now()) {
		return nil, errors.New("session expired")
	}

	user, exists := s.users[session.Username]
	if !exists {
		return nil, errors.New("user not found")
	}

	return user, nil
}

// Logout invalidates a session
func (s *Store) Logout(token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.sessions, token)
	return s.saveLocked()
}

// GetUser returns a user by username
func (s *Store) GetUser(username string) (*User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	user, exists := s.users[username]
	if !exists {
		return nil, errors.New("user not found")
	}
	return user, nil
}

// ListUsers returns all users (without password hashes)
func (s *Store) ListUsers() []*User {
	s.mu.RLock()
	defer s.mu.RUnlock()

	users := make([]*User, 0, len(s.users))
	for _, u := range s.users {
		// Return copy without hash
		users = append(users, &User{
			Username:  u.Username,
			Role:      u.Role,
			CreatedAt: u.CreatedAt,
			UpdatedAt: u.UpdatedAt,
		})
	}
	return users
}

// UpdatePassword changes a user's password
func (s *Store) UpdatePassword(username, newPassword string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	user, exists := s.users[username]
	if !exists {
		return errors.New("user not found")
	}

	user.Hash = string(hash)
	user.UpdatedAt = clock.Now()

	return s.saveLocked()
}

// UpdateRole changes a user's role
func (s *Store) UpdateRole(username string, role Role) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	user, exists := s.users[username]
	if !exists {
		return errors.New("user not found")
	}

	// Don't allow demoting the last admin
	if user.Role == RoleAdmin && role != RoleAdmin {
		adminCount := 0
		for _, u := range s.users {
			if u.Role == RoleAdmin {
				adminCount++
			}
		}
		if adminCount <= 1 {
			return errors.New("cannot demote last admin user")
		}
	}

	user.Role = role
	user.UpdatedAt = clock.Now()

	return s.saveLocked()
}

// DeleteUser removes a user
func (s *Store) DeleteUser(username string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.users[username]; !exists {
		return errors.New("user not found")
	}

	// Don't allow deleting last admin
	adminCount := 0
	for _, u := range s.users {
		if u.Role == RoleAdmin {
			adminCount++
		}
	}
	if s.users[username].Role == RoleAdmin && adminCount <= 1 {
		return errors.New("cannot delete last admin user")
	}

	delete(s.users, username)

	// Also delete their sessions
	for token, sess := range s.sessions {
		if sess.Username == username {
			delete(s.sessions, token)
		}
	}

	return s.saveLocked()
}

// CanAccess checks if a role has permission for an action
func (r Role) CanAccess(action string) bool {
	switch action {
	case "view":
		return true // All roles can view
	case "modify":
		return r == RoleAdmin || r == RoleOperator
	case "admin":
		return r == RoleAdmin
	default:
		return false
	}
}
