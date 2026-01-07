package network

import (
	"bytes"
	"embed"
	"log"
	"strings"
	"sync"
)

//go:embed assets/oui.db.gz
var ouiAsset embed.FS

var (
	ouiDB *OUIDB
	mu    sync.RWMutex
)

// InitOUI loads the OUI database from the embedded asset
func InitOUI() {
	mu.Lock()
	defer mu.Unlock()

	// Try to load from embedded asset
	f, err := ouiAsset.Open("assets/oui.db.gz")
	if err != nil {
		// Only warn if we expect it to be there. In early dev, it might be missing.
		log.Printf("[OUI] Warning: Embedded OUI database not found: %v", err)
		return
	}
	defer f.Close()

	db, err := LoadCompactDB(f)
	if err != nil {
		log.Printf("[OUI] Error loading embedded OUI DB: %v", err)
		return
	}

	ouiDB = db
	log.Printf("[OUI] Loaded %d vendor prefixes", len(db.Entries))
}

// LoadFromBytes allows loading a DB from a byte slice (e.g. from state file)
func LoadFromBytes(data []byte) error {
	mu.Lock()
	defer mu.Unlock()

	db, err := LoadCompactDB(bytes.NewReader(data))
	if err != nil {
		return err
	}
	ouiDB = db
	return nil
}

// LookupVendor returns the manufacturer for a MAC address.
// Returns "Random MAC" for locally administered (random) addresses.
func LookupVendor(mac string) string {
	mu.RLock()
	defer mu.RUnlock()

	if ouiDB == nil {
		return ""
	}

	// Normalize to raw hex "001122334455"
	// Remove all delimiters
	raw := strings.ReplaceAll(mac, ":", "")
	raw = strings.ReplaceAll(raw, "-", "")
	raw = strings.ReplaceAll(raw, ".", "")

	if len(raw) < 6 {
		return ""
	}

	raw = strings.ToUpper(raw)

	// Check for locally administered (random) MAC address
	// The second hex character indicates this: if bit 1 is set, it's locally administered
	// This means the second character is 2, 6, A, or E
	if len(raw) >= 2 {
		secondChar := raw[1]
		if secondChar == '2' || secondChar == '6' || secondChar == 'A' || secondChar == 'E' {
			return "Random MAC"
		}
	}

	// Longest Prefix Match Strategy

	// 1. Try MA-S / OUI-36 (36 bits = 9 hex chars)
	if len(raw) >= 9 {
		if entry, ok := ouiDB.Entries[raw[:9]]; ok {
			return entry.Manufacturer
		}
	}

	// 2. Try MA-M / OUI-28 (28 bits = 7 hex chars)
	if len(raw) >= 7 {
		if entry, ok := ouiDB.Entries[raw[:7]]; ok {
			return entry.Manufacturer
		}
	}

	// 3. Try OUI / MA-L (24 bits = 6 hex chars)
	if len(raw) >= 6 {
		if entry, ok := ouiDB.Entries[raw[:6]]; ok {
			return entry.Manufacturer
		}
	}

	return ""
}
