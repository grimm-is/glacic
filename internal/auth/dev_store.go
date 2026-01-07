// Package auth provides user authentication and session management.
// This file contains the AuthStore interface and DevStore implementation.
package auth

import (
	"time"
)

// AuthStore is the interface for user authentication and session management.
// Both Store (production) and DevStore (dev/testing) implement this.
type AuthStore interface {
	// HasUsers returns true if any users exist
	HasUsers() bool

	// CreateUser creates a new user
	CreateUser(username, password string, role Role) error

	// Authenticate validates credentials and returns a session
	Authenticate(username, password string) (*Session, error)

	// ValidateSession checks if a session token is valid
	ValidateSession(token string) (*User, error)

	// Logout invalidates a session
	Logout(token string) error

	// GetUser returns a user by username
	GetUser(username string) (*User, error)

	// ListUsers returns all users (without password hashes)
	ListUsers() []*User

	// UpdatePassword changes a user's password
	UpdatePassword(username, newPassword string) error

	// UpdateRole changes a user's role
	UpdateRole(username string, role Role) error

	// DeleteUser removes a user
	DeleteUser(username string) error
}

// DevStore is a dev/test auth store that auto-authenticates with full permissions.
// Used when require_auth = false in config, eliminating need for duplicate routes.
type DevStore struct {
	devUser *User
}

// NewDevStore creates a dev auth store with a pre-authenticated admin user.
func NewDevStore() *DevStore {
	return &DevStore{
		devUser: &User{
			Username:  "dev",
			Role:      RoleAdmin,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
	}
}

// HasUsers always returns true - dev user exists
func (d *DevStore) HasUsers() bool {
	return true
}

// CreateUser is a no-op in dev mode
func (d *DevStore) CreateUser(username, password string, role Role) error {
	return nil
}

// Authenticate always succeeds with dev session
func (d *DevStore) Authenticate(username, password string) (*Session, error) {
	return &Session{
		Token:     "dev-session-token",
		Username:  d.devUser.Username,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}, nil
}

// ValidateSession always returns the dev user
func (d *DevStore) ValidateSession(token string) (*User, error) {
	return d.devUser, nil
}

// Logout is a no-op in dev mode
func (d *DevStore) Logout(token string) error {
	return nil
}

// GetUser returns the dev user
func (d *DevStore) GetUser(username string) (*User, error) {
	return d.devUser, nil
}

// ListUsers returns just the dev user
func (d *DevStore) ListUsers() []*User {
	return []*User{d.devUser}
}

// UpdatePassword is a no-op in dev mode
func (d *DevStore) UpdatePassword(username, newPassword string) error {
	return nil
}

// UpdateRole is a no-op in dev mode
func (d *DevStore) UpdateRole(username string, role Role) error {
	return nil
}

// DeleteUser is a no-op in dev mode
func (d *DevStore) DeleteUser(username string) error {
	return nil
}

// Verify interface compliance at compile time
var _ AuthStore = (*Store)(nil)
var _ AuthStore = (*DevStore)(nil)
