package ctlplane

import (
	"log"
)

// auditLog logs a security-relevant event.
// Format: [AUDIT] Action=<Method> User=API Args=<Summary>
func auditLog(action string, args interface{}) {
	log.Printf("[AUDIT] Action=%s User=API Args=%v", action, args)
}
