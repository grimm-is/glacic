package ctlplane

import (
	"log"
	"time"

	"grimm.is/glacic/internal/audit"
)

// Global audit store - set by Server on startup
var auditStore *audit.Store

// SetAuditStore sets the global audit store for the control plane.
func SetAuditStore(store *audit.Store) {
	auditStore = store
}

// auditLog logs a security-relevant event.
// If an audit store is configured, persists to SQLite.
// Always logs to standard log output for immediate visibility.
func auditLog(action string, args interface{}) {
	// Always log to stdout for immediate visibility
	log.Printf("[AUDIT] Action=%s User=API Args=%v", action, args)

	// Persist to SQLite if store is configured
	if auditStore != nil {
		evt := audit.Event{
			Timestamp: time.Now(),
			User:      "system",
			Action:    action,
			Resource:  "ctlplane",
			Status:    0,
			Details: map[string]any{
				"args": args,
			},
		}
		if err := auditStore.Write(evt); err != nil {
			log.Printf("[AUDIT] Failed to persist event: %v", err)
		}
	}
}

// AuditEvent logs a structured audit event with full context.
func AuditEvent(user, session, action, resource string, status int, ip string, details map[string]any) {
	log.Printf("[AUDIT] Action=%s User=%s Resource=%s Status=%d", action, user, resource, status)

	if auditStore != nil {
		evt := audit.Event{
			Timestamp: time.Now(),
			User:      user,
			Session:   session,
			Action:    action,
			Resource:  resource,
			Status:    status,
			IP:        ip,
			Details:   details,
		}
		if err := auditStore.Write(evt); err != nil {
			log.Printf("[AUDIT] Failed to persist event: %v", err)
		}
	}
}
