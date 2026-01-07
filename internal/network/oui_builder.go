package network

import (
	"bufio"
	"compress/gzip"
	"encoding/gob"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

// OUIDB represents our compact database
type OUIDB struct {
	Entries map[string]OUIEntry // Prefix (e.g., "00:11:22") -> Entry
	Updated time.Time
}

type OUIEntry struct {
	Manufacturer string
	Country      string
}

// IEEEOUISource is the URL for the MA-L (OUI) registry
const IEEEOUISource = "https://standards-oui.ieee.org/oui/oui.txt"

// IEEEMAMSource is the URL for the MA-M (OUI-28) registry
const IEEEMAMSource = "https://standards-oui.ieee.org/oui28/mam.txt"

// IEEEMASSource is the URL for the MA-S (OUI-36) registry
const IEEEMASSource = "https://standards-oui.ieee.org/oui36/oui36.txt"

// IEEEIABSource is the URL for the MA-M (IAB) registry
const IEEEIABSource = "https://standards-oui.ieee.org/iab/iab.txt"

// Parser Regex for IEEE format:
// 00-00-5E   (hex)		USC INFORMATION SCIENCES INST
// 00005E     (base 16)		USC INFORMATION SCIENCES INST
var hexLineRegex = regexp.MustCompile(`^([0-9A-F]{2})-([0-9A-F]{2})-([0-9A-F]{2})([-0-9A-F]*)\s+\(hex\)\s+(.+)$`)

// BuildOUIDB downloads and parses IEEE OUI data into a compact DB
func BuildOUIDB() (*OUIDB, error) {
	db := &OUIDB{
		Entries: make(map[string]OUIEntry),
		Updated: time.Now(),
	}

	sources := []string{IEEEOUISource, IEEEMAMSource, IEEEMASSource, IEEEIABSource}

	for _, url := range sources {
		if err := fetchAndParse(url, db); err != nil {
			// Log warning but allow partial success?
			// For a builder tool, maybe better to fail?
			// Let's wrap and return for now.
			return nil, fmt.Errorf("failed to process %s: %w", url, err)
		}
	}

	return db, nil
}

func fetchAndParse(url string, db *OUIDB) error {
	client := &http.Client{Timeout: 60 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	// IEEE blocks requests without a User-Agent
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Glacic-OUI-Builder/1.0)")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("bad status: %s", resp.Status)
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		// Look for hex line: "00-11-22   (hex)   Manufacturer Name"
		// Regex modified to accept optional extra parts: ([-0-9A-F]*)
		matches := hexLineRegex.FindStringSubmatch(line)
		if len(matches) == 6 {
			// prefix parts: 1, 2, 3. Extra: 4 (e.g. "-33-44")

			// Base Prefix: XX:XX:XX
			rawPrefix := matches[1] + matches[2] + matches[3]
			extra := matches[4]
			if extra != "" {
				// remove all hyphens
				extra = strings.ReplaceAll(extra, "-", "")
				rawPrefix += extra
			}

			// We store raw hex to handle variable length cleanly
			// 001122 (24 bits)
			// 0011223 (28 bits, first nibble of next byte?)
			// Wait, MA-M is 28 bits (3.5 bytes).
			// MA-S is 36 bits (4.5 bytes).
			// Hex representation in file usually aligns to nibbles.

			// Let's check MA-M file format example:
			// "00-55-DA-9     (hex)   Manufacturer" (28 bits) -> matches[4] would be "-9"

			manufacturer := strings.TrimSpace(matches[5])

			db.Entries[rawPrefix] = OUIEntry{
				Manufacturer: manufacturer,
				Country:      "",
			}
		}
	}

	return scanner.Err()
}

// SaveCompactDB saves the DB to a gzipped file
func (db *OUIDB) Save(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	zw := gzip.NewWriter(f)
	defer zw.Close()

	enc := gob.NewEncoder(zw)
	return enc.Encode(db)
}

// LoadCompactDB loads the DB from a gzipped file/stream
func LoadCompactDB(r io.Reader) (*OUIDB, error) {
	zr, err := gzip.NewReader(r)
	if err != nil {
		return nil, err
	}
	defer zr.Close()

	var db OUIDB
	dec := gob.NewDecoder(zr)
	if err := dec.Decode(&db); err != nil {
		return nil, err
	}
	return &db, nil
}
