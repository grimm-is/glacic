package firewall

import (
	"fmt"
	"net"
	"os"
	"sync"

	"github.com/oschwald/geoip2-golang"
)

// GeoIPManager handles country lookups from MaxMind databases.
type GeoIPManager struct {
	mu     sync.RWMutex
	reader *geoip2.Reader
	path   string
}

// NewGeoIPManager creates a new GeoIP manager with the specified database path.
// If path is empty, uses the default location.
func NewGeoIPManager(dbPath string) (*GeoIPManager, error) {
	if dbPath == "" {
		dbPath = "/var/lib/glacic/geoip/GeoLite2-Country.mmdb"
	}

	// Check if database exists
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("GeoIP database not found at %s", dbPath)
	}

	reader, err := geoip2.Open(dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open GeoIP database: %w", err)
	}

	return &GeoIPManager{
		reader: reader,
		path:   dbPath,
	}, nil
}

// LookupCountry returns the ISO 3166-1 alpha-2 country code for the given IP.
// Returns empty string if lookup fails or country not found.
func (g *GeoIPManager) LookupCountry(ip net.IP) (string, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if g.reader == nil {
		return "", fmt.Errorf("GeoIP database not loaded")
	}

	record, err := g.reader.Country(ip)
	if err != nil {
		return "", fmt.Errorf("lookup failed for %s: %w", ip, err)
	}

	return record.Country.IsoCode, nil
}

// Close releases the database resources.
func (g *GeoIPManager) Close() error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.reader != nil {
		return g.reader.Close()
	}
	return nil
}

// Reload reopens the database (for updates).
func (g *GeoIPManager) Reload() error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.reader != nil {
		g.reader.Close()
	}

	reader, err := geoip2.Open(g.path)
	if err != nil {
		return fmt.Errorf("failed to reload GeoIP database: %w", err)
	}

	g.reader = reader
	return nil
}

// DatabasePath returns the path to the loaded database.
func (g *GeoIPManager) DatabasePath() string {
	return g.path
}
