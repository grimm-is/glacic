//go:build linux

package firewall

import (
	"context"
	"log"
	"time"

	"grimm.is/glacic/internal/brand"
	"grimm.is/glacic/internal/config"

	"github.com/google/nftables"
)

// MonitorIntegrity starts a background loop to check for external firewall changes.
// Since we don't have easy access to async netlink events with the current library structure without
// blocking the connection, we use a polling approach on the ruleset generation ID.
// MonitorIntegrity starts a background loop to check for external firewall changes.
func (m *Manager) MonitorIntegrity(ctx context.Context, cfg *config.Config) {
	// Check config from manager state if available, or initial cfg
	m.mu.RLock()
	enabled := m.monitorEnabled
	m.mu.RUnlock()

	if !enabled {
		return
	}

	log.Printf("[Firewall] Integrity monitoring enabled.")

	// Create a dedicated connection for monitoring to avoid interference with the main connection
	conn, err := nftables.New()
	if err != nil {
		log.Printf("[Firewall] Monitor failed to create nftables connection: %v", err)
		return
	}
	monitorConn := NewRealNFTablesConn(conn)
	// Note: google/nftables Conn does not expose Close(). It uses a netlink socket that will be closed
	// when the struct is garbage collected if not referenced, or we trust the OS.
	// Re-using it prevents FD exhaustion.

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	// Initial synchronization
	genID, err := m.getRulesetGenID(monitorConn)
	if err == nil {
		m.mu.Lock()
		// If expectedGenID is 0, we assume current state is valid
		if m.expectedGenID == 0 {
			m.expectedGenID = genID
		}
		m.mu.Unlock()
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Check if still enabled
			m.mu.RLock()
			if !m.monitorEnabled {
				m.mu.RUnlock()
				continue
			}
			expected := m.expectedGenID
			targetConfig := m.currentConfig
			m.mu.RUnlock()

			// If targetConfig is nil, use initial cfg
			if targetConfig == nil {
				targetConfig = FromGlobalConfig(cfg)
			}

			currentGenID, err := m.getRulesetGenID(monitorConn)
			if err != nil {
				log.Printf("[Firewall] Monitor error: %v - attempting reconnection", err)

				// Attempt to recreate connection
				newConn, connErr := nftables.New()
				if connErr != nil {
					log.Printf("[Firewall] Failed to recreate monitor connection: %v", connErr)
				} else {
					monitorConn = NewRealNFTablesConn(newConn)
					log.Printf("[Firewall] Monitor connection re-established")
				}
				continue
			}

			if currentGenID != expected {
				log.Printf("[Firewall] !TAMPER DETECTION! Ruleset generation changed (%d -> %d). Reverting...", expected, currentGenID)

				// Cleanup unknown tables (enforce strict ownership)
				m.cleanupExternalTables(monitorConn)

				// Re-apply configuration
				// This call will internally lock and update expectedGenID
				if err := m.ApplyConfig(targetConfig); err != nil {
					log.Printf("[Firewall] Failed to revert changes: %v", err)
					// We failed to revert. expectedGenID remains same.
					// Loop will try again next tick.
				} else {
					log.Printf("[Firewall] Integrity restored.")
					// ApplyConfig updated expectedGenID.
					
					// Call restore callback if set, to sync other services (e.g. DNS dynamic sets)
					m.mu.RLock()
					callback := m.restoreCallback
					m.mu.RUnlock()
					if callback != nil {
						go callback() // Run async to not block monitor loop
					}
				}
			}
		}
	}
}

// cleanupExternalTables removes any table that is not the managed glacic table.
func (m *Manager) cleanupExternalTables(c NFTablesConn) {
	tables, err := c.ListTables()
	if err != nil {
		log.Printf("[Firewall] Failed to list tables for cleanup: %v", err)
		return
	}

	for _, t := range tables {
		if t.Name != brand.LowerName {
			log.Printf("[Firewall] Removing unauthorized table: %s", t.Name)
			c.DelTable(t)
		}
	}
	if err := c.Flush(); err != nil {
		log.Printf("[Firewall] Failed to flush table cleanup: %v", err)
	}
}

// getRulesetGenID returns the current generation ID of the nftables ruleset.
func (m *Manager) getRulesetGenID(c NFTablesConn) (uint32, error) {
	tables, err := c.ListTables()
	if err != nil {
		return 0, err
	}

	var hash uint32
	hash += uint32(len(tables))

	for _, t := range tables {
		if t.Name == brand.LowerName {
			// Get chains first, then get rules per chain
			// Passing nil chain to GetRules causes nil pointer dereference
			chains, err := c.ListChainsOfTableFamily(t.Family)
			if err != nil {
				continue
			}
			for _, chain := range chains {
				if chain.Table.Name != t.Name {
					continue
				}
				rules, err := c.GetRules(t, chain)
				if err != nil {
					continue
				}
				hash += uint32(len(rules))
				for _, r := range rules {
					hash += uint32(r.Handle)
				}
			}
		}
	}

	return hash, nil
}
