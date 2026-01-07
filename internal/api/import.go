package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"grimm.is/glacic/internal/clock"

	imports "grimm.is/glacic/internal/import"

	"grimm.is/glacic/internal/brand"

	"github.com/google/uuid"
)

// ImportSession holds the state of an ongoing import wizard session.
type ImportSession struct {
	ID        string
	CreatedAt time.Time
	Filename  string
	FileType  string
	Result    *imports.ImportResult
	Mappings  map[string]string
}

// Session store (in-memory for now)
var (
	importSessions = make(map[string]*ImportSession)
	sessionMu      sync.RWMutex
)

// handleImportUpload handles file upload and initial analysis.
// POST /api/import/upload
func (s *Server) handleImportUpload(w http.ResponseWriter, r *http.Request) {
	//Limit upload size
	r.ParseMultipartForm(10 << 20) // 10 MB

	file, header, err := r.FormFile("config_file")
	if err != nil {
		http.Error(w, "Failed to get file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Save to temp file in state dir (to ensure permissions)
	tempDir := filepath.Join(brand.GetStateDir(), "imports")
	if err := os.MkdirAll(tempDir, 0700); err != nil {
		http.Error(w, fmt.Sprintf("Failed to create temp dir: %v", err), http.StatusInternalServerError)
		return
	}

	// Save to temp file securely
	// Use os.CreateTemp to generate a random filename
	// Pattern: "import_<timestamp>_<filename>" but made safe
	// We use just the base filename to avoid directory traversal issues in the pattern
	dst, err := os.CreateTemp(tempDir, fmt.Sprintf("import_%d_*_%s", clock.Now().Unix(), filepath.Base(header.Filename)))
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to save file: %v", err), http.StatusInternalServerError)
		return
	}
	tempPath := dst.Name()
	defer dst.Close()

	if _, err := io.Copy(dst, file); err != nil {
		http.Error(w, "Failed to write file", http.StatusInternalServerError)
		return
	}

	// Detect type (simple extension check or content sniff)
	// For now, assume xml = pfsense, rsc = mikrotik
	var result *imports.ImportResult
	var fileType string

	ext := strings.ToLower(filepath.Ext(header.Filename))
	switch ext {
	case ".xml":
		fileType = "pfsense"
		result, err = imports.ParsePfSenseBackup(tempPath)
	case ".rsc":
		fileType = "mikrotik"
		cfg, err := imports.ParseMikroTikExport(tempPath)
		if err == nil {
			result = cfg.ToImportResult()
		}
	default:
		// Generic detection not implemented - only pfSense (.xml) and MikroTik (.rsc) supported
		err = fmt.Errorf("unsupported file type: %s (supported: .xml for pfSense, .rsc for MikroTik)", ext)
	}

	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to parse config: %v", err), http.StatusBadRequest)
		return
	}

	// Create session
	sessionID := uuid.New().String()
	session := &ImportSession{
		ID:        sessionID,
		CreatedAt: clock.Now(),
		Filename:  header.Filename,
		FileType:  fileType,
		Result:    result,
		Mappings:  make(map[string]string),
	}

	sessionMu.Lock()
	importSessions[sessionID] = session
	sessionMu.Unlock()

	// Clean up temp file (parsed data is in memory)
	os.Remove(tempPath)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"session_id": sessionID,
		"file_type":  fileType,
		"result":     result,
	})
}

// handleImportConfig generates a config preview based on mappings.
// POST /api/import/:id/config
func (s *Server) handleImportConfig(w http.ResponseWriter, r *http.Request) {
	// Extract session ID
	pathParts := strings.Split(r.URL.Path, "/")
	if len(pathParts) < 4 {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	sessionID := pathParts[3]

	sessionMu.RLock()
	session, ok := importSessions[sessionID]
	sessionMu.RUnlock()

	if !ok {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}

	// Parse body for mappings
	var req struct {
		Mappings map[string]string `json:"mappings"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Update session mappings
	sessionMu.Lock()
	session.Mappings = req.Mappings
	sessionMu.Unlock()

	// Generate config
	cfg := session.Result.ToConfig(session.Mappings)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(cfg)
}
