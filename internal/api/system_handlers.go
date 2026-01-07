package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
)

// handleSystemReboot triggers a system reboot
// POST /api/system/reboot
func (s *Server) handleSystemReboot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Force bool `json:"force"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err.Error() != "EOF" {
		WriteErrorCtx(w, r, http.StatusBadRequest, "Invalid request body")
		return
	}

	msg, err := s.client.SystemReboot(req.Force)
	if err != nil {
		WriteErrorCtx(w, r, http.StatusInternalServerError, "Failed to reboot system: "+err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": msg,
	})
}

// handleSystemUpgrade triggers a system upgrade via uploaded binary
// POST /api/system/upgrade (multipart/form-data)
// Fields:
//   - binary: the new binary file
//   - checksum: SHA256 hex string for verification
//   - arch: expected architecture (e.g., "linux/arm64")
func (s *Server) handleSystemUpgrade(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check content type - support both multipart upload and trigger-only JSON
	contentType := r.Header.Get("Content-Type")

	// If no binary is being uploaded (JSON or empty body), just trigger local upgrade
	if !strings.HasPrefix(contentType, "multipart/form-data") {
		// Legacy behavior: trigger upgrade with staged binary
		if err := s.client.Upgrade(""); err != nil {
			WriteErrorCtx(w, r, http.StatusInternalServerError, "Upgrade failed: "+err.Error())
			return
		}
		WriteJSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"message": "Upgrade initiated (using staged binary)",
		})
		return
	}

	// Parse multipart form (limit: 100MB)
	const maxUploadSize = 100 << 20 // 100 MB
	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		WriteErrorCtx(w, r, http.StatusBadRequest, "Failed to parse multipart form: "+err.Error())
		return
	}
	defer r.MultipartForm.RemoveAll()

	// Get the binary file
	file, header, err := r.FormFile("binary")
	if err != nil {
		WriteErrorCtx(w, r, http.StatusBadRequest, "Missing 'binary' field: "+err.Error())
		return
	}
	defer file.Close()

	// Get expected checksum
	expectedChecksum := r.FormValue("checksum")
	if expectedChecksum == "" {
		WriteErrorCtx(w, r, http.StatusBadRequest, "Missing 'checksum' field")
		return
	}

	// Get expected architecture
	expectedArch := r.FormValue("arch")
	if expectedArch == "" {
		WriteErrorCtx(w, r, http.StatusBadRequest, "Missing 'arch' field")
		return
	}

	// Read binary into memory (for RPC transport)
	binaryData, err := io.ReadAll(file)
	if err != nil {
		WriteErrorCtx(w, r, http.StatusInternalServerError, "Failed to read binary: "+err.Error())
		return
	}

	// Verify checksum locally before sending to control plane
	hasher := sha256.New()
	hasher.Write(binaryData)
	actualChecksum := hex.EncodeToString(hasher.Sum(nil))
	if actualChecksum != expectedChecksum {
		WriteErrorCtx(w, r, http.StatusBadRequest,
			fmt.Sprintf("Checksum mismatch: expected %s, got %s", expectedChecksum, actualChecksum))
		return
	}

	log.Printf("[API] Staging upgrade binary via RPC: %s (%d bytes, arch: %s)",
		header.Filename, len(binaryData), expectedArch)

	// Stage binary via control plane RPC (which runs as root)
	stageReply, err := s.client.StageBinary(binaryData, actualChecksum, expectedArch)
	if err != nil {
		WriteErrorCtx(w, r, http.StatusInternalServerError, "Failed to stage binary: "+err.Error())
		return
	}

	log.Printf("[API] Binary staged at %s, triggering upgrade...", stageReply.Path)

	// Trigger upgrade via control plane
	if err := s.client.Upgrade(actualChecksum); err != nil {
		WriteErrorCtx(w, r, http.StatusInternalServerError, "Upgrade failed: "+err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success":  true,
		"message":  "Upgrade initiated",
		"checksum": actualChecksum,
		"bytes":    len(binaryData),
	})
}

// handleWakeOnLAN sends a Wake-on-LAN magic packet
// POST /api/system/wol
func (s *Server) handleWakeOnLAN(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteErrorCtx(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req struct {
		MAC       string `json:"mac"`
		Interface string `json:"interface,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteErrorCtx(w, r, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.MAC == "" {
		WriteErrorCtx(w, r, http.StatusBadRequest, "MAC address is required")
		return
	}

	if err := s.client.WakeOnLAN(req.MAC, req.Interface); err != nil {
		WriteErrorCtx(w, r, http.StatusInternalServerError, "Failed to send WOL packet: "+err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// handleSystemStats returns system statistics
// GET /api/system/stats
func (s *Server) handleSystemStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteErrorCtx(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	stats, err := s.client.GetSystemStats()
	if err != nil {
		WriteErrorCtx(w, r, http.StatusInternalServerError, "Failed to get system stats: "+err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"stats": stats,
	})
}

// handleSystemRoutes returns the kernel routing table
// GET /api/system/routes
func (s *Server) handleSystemRoutes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteErrorCtx(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	routes, err := s.client.GetRoutes()
	if err != nil {
		WriteErrorCtx(w, r, http.StatusInternalServerError, "Failed to get routes: "+err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"routes": routes,
	})
}

// handleSafeModeStatus returns safe mode status
// GET /api/system/safe-mode
func (s *Server) handleSafeModeStatus(w http.ResponseWriter, r *http.Request) {
	inSafeMode, err := s.client.IsInSafeMode()
	if err != nil {
		WriteErrorCtx(w, r, http.StatusInternalServerError, "Failed to check safe mode status: "+err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"in_safe_mode": inSafeMode,
	})
}

// handleEnterSafeMode activates safe mode (emergency lockdown)
// POST /api/system/safe-mode
func (s *Server) handleEnterSafeMode(w http.ResponseWriter, r *http.Request) {
	err := s.client.EnterSafeMode()
	if err != nil {
		WriteErrorCtx(w, r, http.StatusInternalServerError, "Failed to enter safe mode: "+err.Error())
		return
	}

	log.Printf("[API] Safe mode activated by user")
	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success":      true,
		"message":      "Safe mode activated - forwarding disabled",
		"in_safe_mode": true,
	})
}

// handleExitSafeMode deactivates safe mode
// DELETE /api/system/safe-mode
func (s *Server) handleExitSafeMode(w http.ResponseWriter, r *http.Request) {
	err := s.client.ExitSafeMode()
	if err != nil {
		WriteErrorCtx(w, r, http.StatusInternalServerError, "Failed to exit safe mode: "+err.Error())
		return
	}

	log.Printf("[API] Safe mode deactivated by user")
	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success":      true,
		"message":      "Safe mode deactivated - normal operation resumed",
		"in_safe_mode": false,
	})
}
