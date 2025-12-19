package ctlplane

import (
	"sync"
	"time"
)

// NotificationType represents the type of notification
type NotificationType string

const (
	NotifySuccess NotificationType = "success"
	NotifyError   NotificationType = "error"
	NotifyWarning NotificationType = "warning"
	NotifyInfo    NotificationType = "info"
)

// Notification represents a user-facing notification
type Notification struct {
	ID      int64            `json:"id"`
	Type    NotificationType `json:"type"`
	Title   string           `json:"title"`
	Message string           `json:"message"`
	Time    time.Time        `json:"time"`
}

// NotificationHub manages a ring buffer of notifications for broadcasting
type NotificationHub struct {
	mu            sync.RWMutex
	notifications []Notification
	nextID        int64
	maxSize       int
}

// NewNotificationHub creates a new notification hub with the given max size
func NewNotificationHub(maxSize int) *NotificationHub {
	if maxSize <= 0 {
		maxSize = 100
	}
	return &NotificationHub{
		notifications: make([]Notification, 0, maxSize),
		maxSize:       maxSize,
		nextID:        1,
	}
}

// Publish adds a new notification to the hub
func (h *NotificationHub) Publish(ntype NotificationType, title, message string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	n := Notification{
		ID:      h.nextID,
		Type:    ntype,
		Title:   title,
		Message: message,
		Time:    time.Now(),
	}
	h.nextID++

	// Ring buffer: remove oldest if at capacity
	if len(h.notifications) >= h.maxSize {
		h.notifications = h.notifications[1:]
	}
	h.notifications = append(h.notifications, n)
}

// GetSince returns all notifications with ID greater than the given ID
// Returns empty slice if no new notifications
func (h *NotificationHub) GetSince(sinceID int64) []Notification {
	h.mu.RLock()
	defer h.mu.RUnlock()

	var result []Notification
	for _, n := range h.notifications {
		if n.ID > sinceID {
			result = append(result, n)
		}
	}
	return result
}

// GetAll returns all notifications in the buffer
func (h *NotificationHub) GetAll() []Notification {
	h.mu.RLock()
	defer h.mu.RUnlock()

	result := make([]Notification, len(h.notifications))
	copy(result, h.notifications)
	return result
}

// LastID returns the ID of the most recent notification (0 if none)
func (h *NotificationHub) LastID() int64 {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if len(h.notifications) == 0 {
		return 0
	}
	return h.notifications[len(h.notifications)-1].ID
}
