// cmd/api-dev/main.go - Development API server for UI iteration
//
// This runs the API server in "mock mode" without needing:
// - nftables/netfilter (no root required)
// - Control plane socket
// - VM environment
//
// Usage: go run ./cmd/api-dev
// Then:  cd ui && npm run dev
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// MockConfig represents the application configuration
var mockConfig = map[string]interface{}{
	"ip_forwarding": true,
	"zones": []map[string]string{
		{"name": "WAN", "color": "red", "description": "External/Internet"},
		{"name": "LAN", "color": "green", "description": "Local Network"},
		{"name": "DMZ", "color": "orange", "description": "Demilitarized Zone"},
		{"name": "Guest", "color": "purple", "description": "Guest Network"},
	},
	"interfaces": []map[string]interface{}{
		{"Name": "eth0", "Zone": "WAN", "IPv4": []string{}, "DHCP": true, "Description": "Internet Connection"},
		{"Name": "eth1", "Zone": "LAN", "IPv4": []string{"192.168.1.1/24"}, "Description": "Local Network"},
		{"Name": "eth2", "Zone": "DMZ", "IPv4": []string{"10.0.0.1/24"}, "Description": "DMZ Network"},
	},
	"policies": []map[string]interface{}{
		{"from": "LAN", "to": "WAN", "rules": []map[string]interface{}{{"action": "accept", "name": "Allow outbound"}}},
		{"from": "LAN", "to": "Firewall", "rules": []map[string]interface{}{{"action": "accept", "protocol": "tcp", "dest_port": 22, "name": "SSH"}}},
		{"from": "WAN", "to": "LAN", "rules": []map[string]interface{}{{"action": "drop", "name": "Block inbound"}}},
	},
	"nat": []map[string]interface{}{
		{"type": "masquerade", "interface": "eth0"},
		{"type": "dnat", "protocol": "tcp", "destination": "443", "to_address": "10.0.0.10", "to_port": "443", "description": "Web Server"},
	},
	"ipsets": []map[string]interface{}{
		{"name": "firehol_level1", "firehol_list": "firehol_level1", "auto_update": true, "action": "drop", "apply_to": "input"},
		{"name": "trusted_ips", "entries": []string{"192.168.1.0/24", "10.0.0.0/8"}, "type": "ipv4_addr"},
	},
	"dhcp": map[string]interface{}{
		"enabled": true,
		"scopes": []map[string]interface{}{
			{"name": "LAN", "interface": "eth1", "range_start": "192.168.1.100", "range_end": "192.168.1.200", "router": "192.168.1.1"},
		},
	},
	"dns_server": map[string]interface{}{
		"enabled":    true,
		"listen_on":  []string{"192.168.1.1"},
		"forwarders": []string{"1.1.1.1", "8.8.8.8"},
	},
	"wireguard": []interface{}{},
	"features": map[string]bool{
		"network_learning": true,
		"qos":              true,
	},
}

var sessions = sync.Map{}

// WebSocket clients
var wsClients = make(map[*websocket.Conn]map[string]bool)
var wsMutex sync.RWMutex

func main() {
	mux := http.NewServeMux()

	// Auth endpoints
	mux.HandleFunc("/api/auth/status", handleAuthStatus)
	mux.HandleFunc("/api/auth/login", handleLogin)
	mux.HandleFunc("/api/auth/logout", handleLogout)
	mux.HandleFunc("/api/setup/create-admin", handleCreateAdmin)

	// Config endpoints
	mux.HandleFunc("/api/status", handleStatus)
	mux.HandleFunc("/api/config", handleConfig)
	mux.HandleFunc("/api/config/apply", handleApplyConfig)
	mux.HandleFunc("/api/config/policies", handlePolicies)
	mux.HandleFunc("/api/config/nat", handleNAT)
	mux.HandleFunc("/api/config/ipsets", handleIPSets)
	mux.HandleFunc("/api/config/dhcp", handleDHCP)
	mux.HandleFunc("/api/config/dns", handleDNS)
	mux.HandleFunc("/api/config/routes", handleRoutes)
	mux.HandleFunc("/api/config/zones", handleZones)
	mux.HandleFunc("/api/config/qos", handleQoS)
	mux.HandleFunc("/api/brand", handleBrand)

	// Services
	mux.HandleFunc("/api/services", handleServices)
	mux.HandleFunc("/api/leases", handleLeases)
	mux.HandleFunc("/api/interfaces", handleInterfaces)
	mux.HandleFunc("/api/interfaces/available", handleAvailableInterfaces)
	mux.HandleFunc("/api/interfaces/update", handleUpdateInterface)
	mux.HandleFunc("/api/traffic", handleTraffic)
	mux.HandleFunc("/api/system/routes", handleSystemRoutes)

	// Scanner endpoints
	mux.HandleFunc("/api/scanner/status", handleScannerStatus)
	mux.HandleFunc("/api/scanner/result", handleScannerResult)
	mux.HandleFunc("/api/scanner/network", handleScanNetwork)
	mux.HandleFunc("/api/scanner/host", handleScanHost)
	mux.HandleFunc("/api/scanner/ports", handleScannerPorts)

	// Logging
	mux.HandleFunc("/api/logs", handleLogs)
	mux.HandleFunc("/api/logs/sources", handleLogSources)
	mux.HandleFunc("/api/logs/stats", handleLogStats)
	mux.HandleFunc("/api/logs/stream", handleLogStream)

	// Monitoring
	mux.HandleFunc("/api/monitoring/overview", handleMonitoringOverview)

	// Batch endpoint (critical for UI)
	mux.HandleFunc("/api/batch", handleBatch)

	// User management
	mux.HandleFunc("/api/users", handleUsers)

	// System
	mux.HandleFunc("/api/system/reboot", handleReboot)

	// Config sections
	mux.HandleFunc("/api/config/protection", handleProtection)
	mux.HandleFunc("/api/config/protections", handleProtection)
	mux.HandleFunc("/api/config/vpn", handleVPN)
	mux.HandleFunc("/api/config/hcl", handleHCL)
	mux.HandleFunc("/api/config/hcl/section", handleHCLSection)
	mux.HandleFunc("/api/config/hcl/validate", handleHCLValidate)
	mux.HandleFunc("/api/config/hcl/save", handleHCLSave)
	mux.HandleFunc("/api/topology", handleTopology)

	// Learning
	mux.HandleFunc("/api/learning/stats", handleLearningStats)
	mux.HandleFunc("/api/learning/rules", handleLearningRules)

	// IPSet status
	mux.HandleFunc("/api/ipsets/status", handleIPSetStatus)
	mux.HandleFunc("/api/ipsets/refresh", handleIPSetRefresh)

	// Backups
	mux.HandleFunc("/api/backups", handleBackups)
	mux.HandleFunc("/api/backups/create", handleBackupCreate)
	mux.HandleFunc("/api/backups/restore", handleBackupRestore)
	mux.HandleFunc("/api/backups/content", handleBackupContent)
	mux.HandleFunc("/api/backups/pin", handleBackupPin)

	// VLAN/Bonds
	mux.HandleFunc("/api/vlans", handleVLAN)
	mux.HandleFunc("/api/bonds", handleBonds)

	// WebSocket for real-time updates
	mux.HandleFunc("/api/ws", handleWebSocket)
	mux.HandleFunc("/api/ws/status", handleWebSocket) // UI connects to this path

	// Start background goroutines for mock events
	go generateMockNotifications()
	go generateMockLogs()

	Printer.Println("\n🚀 Mock API Server")
	Printer.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	Printer.Println("API:  http://localhost:8080")
	Printer.Println("Auth: admin / admin123")
	Printer.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	Printer.Println("\nStart UI: cd ui && npm run dev")
	Printer.Println("")

	// OpenAPI & Docs
	mux.HandleFunc("/api/openapi.yaml", handleOpenAPI)
	mux.HandleFunc("/api/docs", handleSwaggerUI)

	log.Fatal(http.ListenAndServe(":8080", corsMiddleware(mux)))
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == "OPTIONS" {
			w.WriteHeader(204)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func sendJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

// ========== Auth Handlers ==========

func handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	token := getCookie(r, "session")
	_, authenticated := sessions.Load(token)
	sendJSON(w, map[string]interface{}{
		"authenticated":  authenticated,
		"username":       "admin",
		"setup_required": false,
	})
}

func handleLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	json.NewDecoder(r.Body).Decode(&body)

	if body.Username == "admin" && body.Password == "admin123" {
		token := fmt.Sprintf("%d", time.Now().UnixNano())
		sessions.Store(token, body.Username)
		http.SetCookie(w, &http.Cookie{Name: "session", Value: token, Path: "/"})
		sendJSON(w, map[string]interface{}{"authenticated": true, "username": body.Username})
	} else {
		w.WriteHeader(401)
		sendJSON(w, map[string]string{"error": "Invalid credentials"})
	}
}

func handleLogout(w http.ResponseWriter, r *http.Request) {
	token := getCookie(r, "session")
	sessions.Delete(token)
	http.SetCookie(w, &http.Cookie{Name: "session", Value: "", Path: "/", MaxAge: -1})
	sendJSON(w, map[string]bool{"success": true})
}

func handleCreateAdmin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	json.NewDecoder(r.Body).Decode(&body)

	token := fmt.Sprintf("%d", time.Now().UnixNano())
	sessions.Store(token, body.Username)
	http.SetCookie(w, &http.Cookie{Name: "session", Value: token, Path: "/"})
	sendJSON(w, map[string]interface{}{"authenticated": true, "username": body.Username})
}

// ========== Config Handlers ==========

func handleStatus(w http.ResponseWriter, r *http.Request) {
	sendJSON(w, map[string]interface{}{
		"status":  "online",
		"uptime":  "2d 5h 32m",
		"version": "0.1.0-dev",
	})
}

func handleConfig(w http.ResponseWriter, r *http.Request) {
	sendJSON(w, mockConfig)
}

func handleApplyConfig(w http.ResponseWriter, r *http.Request) {
	log.Println("Config applied")
	sendJSON(w, map[string]bool{"success": true})
}

func handlePolicies(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		sendJSON(w, mockConfig["policies"])
	} else if r.Method == "POST" {
		var policies []interface{}
		json.NewDecoder(r.Body).Decode(&policies)
		mockConfig["policies"] = policies
		log.Printf("Updated policies: %d rules", len(policies))
		sendJSON(w, map[string]bool{"success": true})
	}
}

func handleNAT(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		sendJSON(w, mockConfig["nat"])
	} else if r.Method == "POST" {
		var nat []interface{}
		json.NewDecoder(r.Body).Decode(&nat)
		mockConfig["nat"] = nat
		sendJSON(w, map[string]bool{"success": true})
	}
}

func handleIPSets(w http.ResponseWriter, r *http.Request) {
	sendJSON(w, mockConfig["ipsets"])
}

func handleDHCP(w http.ResponseWriter, r *http.Request) {
	sendJSON(w, mockConfig["dhcp"])
}

func handleDNS(w http.ResponseWriter, r *http.Request) {
	sendJSON(w, mockConfig["dns_server"])
}

func handleRoutes(w http.ResponseWriter, r *http.Request) {
	sendJSON(w, []interface{}{})
}

func handleZones(w http.ResponseWriter, r *http.Request) {
	sendJSON(w, mockConfig["zones"])
}

func handleQoS(w http.ResponseWriter, r *http.Request) {
	sendJSON(w, map[string]interface{}{"enabled": false})
}

func handleBrand(w http.ResponseWriter, r *http.Request) {
	sendJSON(w, map[string]interface{}{
		"name":        "Glacic (Dev)",
		"lowerName":   "glacic",
		"tagline":     "Development Mode",
		"binaryName":  "glacic",
		"serviceName": "glacic",
	})
}

func handleServices(w http.ResponseWriter, r *http.Request) {
	sendJSON(w, []map[string]interface{}{
		{"name": "ssh", "description": "Secure Shell", "ports": []map[string]interface{}{{"port": 22, "protocol": "tcp"}}},
		{"name": "http", "description": "HTTP Web", "ports": []map[string]interface{}{{"port": 80, "protocol": "tcp"}}},
		{"name": "https", "description": "HTTPS Web", "ports": []map[string]interface{}{{"port": 443, "protocol": "tcp"}}},
		{"name": "dns", "description": "DNS", "ports": []map[string]interface{}{{"port": 53, "protocol": "udp"}}},
	})
}

func handleLeases(w http.ResponseWriter, r *http.Request) {
	sendJSON(w, []map[string]interface{}{
		{"ip_address": "192.168.1.101", "mac": "00:11:22:33:44:55", "hostname": "laptop", "expires": time.Now().Add(2 * time.Hour).Format(time.RFC3339)},
		{"ip_address": "192.168.1.102", "mac": "00:11:22:33:44:56", "hostname": "phone", "expires": time.Now().Add(6 * time.Hour).Format(time.RFC3339)},
	})
}

func handleInterfaces(w http.ResponseWriter, r *http.Request) {
	sendJSON(w, []map[string]interface{}{
		{"name": "eth0", "mac": "00:11:22:33:44:50", "zone": "WAN", "ipv4": []string{}, "status": "up"},
		{"name": "eth1", "mac": "00:11:22:33:44:51", "zone": "LAN", "ipv4": []string{"192.168.1.1/24"}, "status": "up"},
		{"name": "eth2", "mac": "00:11:22:33:44:52", "zone": "DMZ", "ipv4": []string{"10.0.0.1/24"}, "status": "up"},
		{"name": "eth3", "mac": "00:11:22:33:44:53", "zone": "", "ipv4": []string{}, "status": "down"},
	})
}

func handleAvailableInterfaces(w http.ResponseWriter, r *http.Request) {
	sendJSON(w, []map[string]interface{}{
		{"name": "eth3", "mac": "00:11:22:33:44:57", "assigned": false},
		{"name": "eth4", "mac": "00:11:22:33:44:58", "assigned": false},
	})
}

func handleUpdateInterface(w http.ResponseWriter, r *http.Request) {
	sendJSON(w, map[string]bool{"success": true})
}

func handleTraffic(w http.ResponseWriter, r *http.Request) {
	sendJSON(w, map[string]interface{}{
		"interfaces": map[string]interface{}{
			"eth0": map[string]int64{"rx_bytes": rand.Int63n(1000000000), "tx_bytes": rand.Int63n(500000000)},
			"eth1": map[string]int64{"rx_bytes": rand.Int63n(500000000), "tx_bytes": rand.Int63n(200000000)},
		},
	})
}

func handleSystemRoutes(w http.ResponseWriter, r *http.Request) {
	sendJSON(w, map[string]interface{}{
		"routes": []map[string]interface{}{
			{"destination": "0.0.0.0/0", "gateway": "203.0.113.1", "interface": "eth0", "metric": 100},
			{"destination": "192.168.1.0/24", "gateway": "", "interface": "eth1", "metric": 0},
		},
	})
}

// ========== Scanner Handlers ==========

var scannerScanning = false
var scannerResult = map[string]interface{}{
	"network":     "192.168.1.0/24",
	"total_hosts": 254,
	"duration_ms": 12500,
	"started_at":  time.Now().Add(-10 * time.Minute).Format(time.RFC3339),
	"hosts": []map[string]interface{}{
		{
			"ip":       "192.168.1.1",
			"hostname": "router.local",
			"open_ports": []map[string]interface{}{
				{"port": 22, "name": "SSH", "description": "Secure Shell"},
				{"port": 80, "name": "HTTP", "description": "Web Server"},
				{"port": 443, "name": "HTTPS", "description": "Secure Web Server"},
			},
		},
		{
			"ip":       "192.168.1.10",
			"hostname": "nas.local",
			"open_ports": []map[string]interface{}{
				{"port": 445, "name": "SMB", "description": "Windows File Sharing"},
				{"port": 8080, "name": "HTTP-Alt", "description": "Alt Web Server"},
			},
		},
		{
			"ip":       "192.168.1.50",
			"hostname": "plex.local",
			"open_ports": []map[string]interface{}{
				{"port": 32400, "name": "Plex", "description": "Plex Media Server"},
			},
		},
	},
}

func handleScannerStatus(w http.ResponseWriter, r *http.Request) {
	sendJSON(w, map[string]interface{}{
		"scanning": scannerScanning,
		"last_scan": map[string]interface{}{
			"network":             "192.168.1.0/24",
			"total_hosts":         254,
			"hosts_with_services": 3,
			"duration_ms":         12500,
			"started_at":          time.Now().Add(-10 * time.Minute).Format(time.RFC3339),
		},
	})
}

func handleScannerResult(w http.ResponseWriter, r *http.Request) {
	sendJSON(w, scannerResult)
}

func handleScanNetwork(w http.ResponseWriter, r *http.Request) {
	if scannerScanning {
		w.WriteHeader(409)
		sendJSON(w, map[string]string{"error": "Scan already in progress"})
		return
	}

	var req struct {
		CIDR string `json:"cidr"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	// Simulate async scan
	scannerScanning = true
	go func() {
		time.Sleep(5 * time.Second)
		scannerScanning = false
		log.Printf("Mock network scan complete: %s", req.CIDR)
	}()

	w.WriteHeader(202)
	sendJSON(w, map[string]interface{}{
		"status":  "started",
		"network": req.CIDR,
		"message": "Network scan started",
	})
}

func handleScanHost(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IP string `json:"ip"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	sendJSON(w, map[string]interface{}{
		"ip":       req.IP,
		"hostname": "host.local",
		"open_ports": []map[string]interface{}{
			{"port": 22, "name": "SSH", "description": "Secure Shell"},
		},
		"scan_duration_ms": 1200,
	})
}

func handleScannerPorts(w http.ResponseWriter, r *http.Request) {
	sendJSON(w, []map[string]interface{}{
		{"number": 22, "name": "SSH", "description": "Secure Shell"},
		{"number": 80, "name": "HTTP", "description": "Web Server"},
		{"number": 443, "name": "HTTPS", "description": "Secure Web Server"},
		{"number": 445, "name": "SMB", "description": "Windows File Sharing"},
		{"number": 3306, "name": "MySQL", "description": "MySQL Database"},
		{"number": 5432, "name": "PostgreSQL", "description": "PostgreSQL Database"},
		{"number": 8080, "name": "HTTP-Alt", "description": "Alt Web Server"},
		{"number": 32400, "name": "Plex", "description": "Plex Media Server"},
	})
}

// ========== Logging Handlers ==========

var mockLogs []map[string]interface{}
var logMutex sync.RWMutex

func handleLogs(w http.ResponseWriter, r *http.Request) {
	logMutex.RLock()
	defer logMutex.RUnlock()

	// Parse query params
	source := r.URL.Query().Get("source")
	level := r.URL.Query().Get("level")
	search := strings.ToLower(r.URL.Query().Get("search"))

	var filtered []map[string]interface{}
	for _, l := range mockLogs {
		if source != "" {
			sources := strings.Split(source, ",")
			found := false
			for _, s := range sources {
				if l["source"] == s {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		if level != "" {
			levels := map[string]int{"debug": 0, "info": 1, "warn": 2, "error": 3}
			reqLevel, ok := levels[strings.ToLower(level)]
			logLevel, ok2 := levels[strings.ToLower(l["level"].(string))]

			if ok && ok2 {
				if logLevel < reqLevel {
					continue
				}
			} else if l["level"] != level {
				continue
			}
		}
		if search != "" {
			msg, _ := l["message"].(string)
			src, _ := l["source"].(string)
			if !strings.Contains(strings.ToLower(msg), search) && !strings.Contains(strings.ToLower(src), search) {
				continue
			}
		}
		filtered = append(filtered, l)
	}

	sendJSON(w, filtered)
}

func handleLogSources(w http.ResponseWriter, r *http.Request) {
	sendJSON(w, []map[string]string{
		{"id": "syslog", "name": "System"},
		{"id": "nftables", "name": "Firewall"},
		{"id": "dhcp", "name": "DHCP"},
		{"id": "dns", "name": "DNS"},
		{"id": "api", "name": "API"},
		{"id": "firewall", "name": "Filter"},
	})
}

func handleLogStats(w http.ResponseWriter, r *http.Request) {
	sendJSON(w, map[string]interface{}{
		"total":     len(mockLogs),
		"by_source": map[string]int{"syslog": 50, "nftables": 100, "dhcp": 20, "dns": 80},
		"by_level":  map[string]int{"info": 200, "warn": 30, "error": 5},
	})
}

func handleLogStream(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", 500)
		return
	}

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			entry := generateLogEntry()
			data, _ := json.Marshal(entry)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}

// ========== WebSocket Handler ==========

func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade failed: %v", err)
		return
	}
	defer conn.Close()

	wsMutex.Lock()
	wsClients[conn] = make(map[string]bool)
	wsMutex.Unlock()

	defer func() {
		wsMutex.Lock()
		delete(wsClients, conn)
		wsMutex.Unlock()
	}()

	// Read subscription messages
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			break
		}

		var req struct {
			Action string   `json:"action"`
			Topics []string `json:"topics"`
		}
		if err := json.Unmarshal(msg, &req); err != nil {
			continue
		}

		wsMutex.Lock()
		topics := wsClients[conn]
		if req.Action == "subscribe" {
			for _, t := range req.Topics {
				topics[t] = true
			}
		} else if req.Action == "unsubscribe" {
			for _, t := range req.Topics {
				delete(topics, t)
			}
		}
		wsMutex.Unlock()
	}
}

func broadcastWS(topic string, data interface{}) {
	msg := map[string]interface{}{
		"topic": topic,
		"data":  data,
	}
	msgJSON, _ := json.Marshal(msg)

	wsMutex.RLock()
	defer wsMutex.RUnlock()

	for conn, topics := range wsClients {
		if topics[topic] {
			conn.WriteMessage(websocket.TextMessage, msgJSON)
		}
	}
}

// ========== Mock Event Generators ==========

func generateMockNotifications() {
	notifications := []map[string]interface{}{
		{"type": "info", "title": "System Started", "message": "Firewall services initialized successfully"},
		{"type": "warning", "title": "High Traffic", "message": "eth0 experiencing high bandwidth usage"},
		{"type": "info", "title": "DHCP Lease", "message": "New lease issued to 192.168.1.105"},
		{"type": "warning", "title": "Port Scan Detected", "message": "Multiple connection attempts from 203.0.113.45"},
		{"type": "info", "title": "Config Applied", "message": "Firewall rules updated successfully"},
		{"type": "error", "title": "DNS Resolution Failed", "message": "Unable to reach upstream DNS 8.8.8.8"},
	}

	time.Sleep(3 * time.Second) // Initial delay

	for {
		time.Sleep(time.Duration(10+rand.Intn(20)) * time.Second)
		notif := notifications[rand.Intn(len(notifications))]
		notif["timestamp"] = time.Now().Format(time.RFC3339)
		notif["id"] = fmt.Sprintf("notif-%d", time.Now().UnixNano())

		broadcastWS("notifications", notif)
		log.Printf("Notification: %s", notif["title"])
	}
}

func generateMockLogs() {
	time.Sleep(1 * time.Second)

	// Generate initial log batch
	for i := 0; i < 50; i++ {
		entry := generateLogEntry()
		entry["timestamp"] = time.Now().Add(-time.Duration(50-i) * time.Minute).Format(time.RFC3339)

		logMutex.Lock()
		mockLogs = append(mockLogs, entry)
		logMutex.Unlock()
	}

	// Generate new logs periodically
	for {
		time.Sleep(time.Duration(3+rand.Intn(5)) * time.Second)
		entry := generateLogEntry()

		logMutex.Lock()
		mockLogs = append(mockLogs, entry)
		if len(mockLogs) > 500 {
			mockLogs = mockLogs[1:]
		}
		logMutex.Unlock()

		broadcastWS("logs", []interface{}{entry})
	}
}

func generateLogEntry() map[string]interface{} {
	sources := []string{"syslog", "nftables", "dhcp", "dns", "api", "firewall"}
	levels := []string{"info", "info", "info", "warn", "debug"}
	messages := []string{
		"Connection established from 192.168.1.100:54321",
		"DNS query: google.com -> 142.250.80.46",
		"DHCP: DISCOVER from 00:11:22:33:44:55",
		"Rule matched: LAN->WAN accept",
		"Dropped packet: WAN->LAN tcp:22",
		"ICMP echo request to 8.8.8.8",
		"New flow: tcp 192.168.1.50:443 -> 203.0.113.10:54000",
		"Rate limit exceeded for 192.168.1.200",
	}

	return map[string]interface{}{
		"timestamp": time.Now().Format(time.RFC3339),
		"source":    sources[rand.Intn(len(sources))],
		"level":     levels[rand.Intn(len(levels))],
		"message":   messages[rand.Intn(len(messages))],
	}
}

// ========== Topology Handlers ==========

func handleTopology(w http.ResponseWriter, r *http.Request) {
	sendJSON(w, map[string]interface{}{
		"neighbors": []map[string]interface{}{
			{
				"interface":         "eth0",
				"chassis_id":        "74:83:c2:d0:e1:f2",
				"port_id":           "Port 1",
				"system_name":       "USW-Pro-24-PoE",
				"system_desc":       "UniFi Switch Pro 24 PoE",
				"last_seen_seconds": 5,
			},
			{
				"interface":         "eth1",
				"chassis_id":        "e0:63:da:11:22:33",
				"port_id":           "eth0",
				"system_name":       "U6-Pro-LivingRoom",
				"system_desc":       "U6-Pro",
				"last_seen_seconds": 12,
			},
			{
				"interface":         "eth2",
				"chassis_id":        "00:11:32:44:55:66",
				"port_id":           "bond0",
				"system_name":       "Synology-NAS",
				"system_desc":       "Synology DS920+",
				"last_seen_seconds": 3,
			},
		},
	})
}

// ========== Learning Handlers ==========

func handleLearningStats(w http.ResponseWriter, r *http.Request) {
	sendJSON(w, map[string]interface{}{
		"stats": map[string]interface{}{
			"total_flows":        12543,
			"active_flows":       45,
			"apps_identified":    18,
			"vendors_identified": 12,
			"top_apps": []map[string]interface{}{
				{"name": "Netflix", "count": 5230},
				{"name": "YouTube", "count": 3102},
				{"name": "Spotify", "count": 1450},
				{"name": "Zoom", "count": 890},
				{"name": "Slack", "count": 450},
			},
			"top_vendors": []map[string]interface{}{
				{"name": "Apple", "count": 15},
				{"name": "Ubiquiti", "count": 8},
				{"name": "Espressif", "count": 12},
			},
		},
	})
}

func handleLearningRules(w http.ResponseWriter, r *http.Request) {
	sendJSON(w, map[string]interface{}{
		"rules": []map[string]interface{}{
			{
				"id":        "rule-1",
				"src_ip":    "192.168.1.55",
				"dst_ip":    "142.250.0.0/24",
				"dst_port":  443,
				"protocol":  "tcp",
				"app_id":    "YouTube",
				"timestamp": time.Now().Add(-5 * time.Minute).Format(time.RFC3339),
			},
			{
				"id":        "rule-2",
				"src_ip":    "192.168.1.102",
				"dst_ip":    "104.16.0.0/12",
				"dst_port":  443,
				"protocol":  "tcp",
				"app_id":    "Netflix",
				"timestamp": time.Now().Add(-15 * time.Minute).Format(time.RFC3339),
			},
		},
	})
}

// ========== Docs Handlers ==========

func handleOpenAPI(w http.ResponseWriter, r *http.Request) {
	// Read the generated spec file
	// In dev mode, we read from the file system
	content, err := os.ReadFile("internal/api/spec/openapi.yaml")
	if err != nil {
		http.Error(w, "Spec not found (run cmd/gen-docs): "+err.Error(), 404)
		return
	}
	w.Header().Set("Content-Type", "text/yaml")
	w.Write(content)
}

func handleSwaggerUI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <meta name="description" content="SwaggerUI" />
    <title>Glacic Firewall API</title>
    <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5.11.0/swagger-ui.css" />
</head>
<body>
<div id="swagger-ui"></div>
<script src="https://unpkg.com/swagger-ui-dist@5.11.0/swagger-ui-bundle.js" crossorigin></script>
<script>
    window.onload = () => {
        window.ui = SwaggerUIBundle({
            url: '/api/openapi.yaml',
            dom_id: '#swagger-ui',
        });
    };
</script>
</body>
</html>`)
}

// ========== Helpers ==========

func getCookie(r *http.Request, name string) string {
	cookie, err := r.Cookie(name)
	if err != nil {
		return ""
	}
	return cookie.Value
}

// ========== Batch Handler ==========

func handleBatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", 405)
		return
	}

	var requests []struct {
		Endpoint string `json:"endpoint"`
		Method   string `json:"method"`
	}
	json.NewDecoder(r.Body).Decode(&requests)

	responses := make([]map[string]interface{}, len(requests))
	for i, req := range requests {
		// Simulate responses for common endpoints
		var body interface{}
		status := 200

		switch req.Endpoint {
		case "/status":
			body = map[string]interface{}{"status": "online", "uptime": "2d 5h 32m", "version": "0.1.0-dev"}
		case "/config":
			body = mockConfig
		case "/leases":
			body = []map[string]interface{}{
				{"ip_address": "192.168.1.101", "mac": "00:11:22:33:44:55", "hostname": "laptop"},
				{"ip_address": "192.168.1.102", "mac": "00:11:22:33:44:56", "hostname": "phone"},
			}
		case "/traffic":
			body = map[string]interface{}{
				"interfaces": map[string]interface{}{
					"eth0": map[string]int64{"rx_bytes": rand.Int63n(1000000000), "tx_bytes": rand.Int63n(500000000)},
					"eth1": map[string]int64{"rx_bytes": rand.Int63n(500000000), "tx_bytes": rand.Int63n(200000000)},
				},
			}
		default:
			body = map[string]interface{}{}
		}

		responses[i] = map[string]interface{}{
			"status": status,
			"body":   body,
		}
	}

	sendJSON(w, responses)
}

// ========== Additional Handlers ==========

func handleMonitoringOverview(w http.ResponseWriter, r *http.Request) {
	sendJSON(w, map[string]interface{}{
		"system": map[string]interface{}{
			"uptime_seconds": 86400 + rand.Intn(86400),
			"load_average":   []float64{0.5 + rand.Float64(), 0.4 + rand.Float64(), 0.3 + rand.Float64()},
			"cpu_percent":    15.0 + rand.Float64()*30,
			"memory": map[string]interface{}{
				"total":   8589934592,
				"used":    3221225472 + rand.Int63n(1073741824),
				"free":    4294967296,
				"percent": 40.0 + rand.Float64()*20,
			},
			"disk": map[string]interface{}{
				"total":   107374182400,
				"used":    32212254720,
				"free":    75161927680,
				"percent": 30.0,
			},
		},
		"network": map[string]interface{}{
			"interfaces": map[string]interface{}{
				"eth0": map[string]int64{
					"rx_bytes":   rand.Int63n(10000000000),
					"tx_bytes":   rand.Int63n(5000000000),
					"rx_packets": rand.Int63n(10000000),
					"tx_packets": rand.Int63n(5000000),
					"rx_bps":     rand.Int63n(100000000),
					"tx_bps":     rand.Int63n(50000000),
				},
				"eth1": map[string]int64{
					"rx_bytes":   rand.Int63n(5000000000),
					"tx_bytes":   rand.Int63n(2000000000),
					"rx_packets": rand.Int63n(5000000),
					"tx_packets": rand.Int63n(2000000),
					"rx_bps":     rand.Int63n(50000000),
					"tx_bps":     rand.Int63n(20000000),
				},
			},
			"connections": map[string]int{
				"established": 50 + rand.Intn(100),
				"listening":   10,
				"time_wait":   rand.Intn(50),
			},
		},
		"firewall": map[string]interface{}{
			"rules_loaded":    true,
			"packets_matched": rand.Int63n(1000000),
			"packets_dropped": rand.Int63n(10000),
		},
		"services": map[string]interface{}{
			"dhcp":     map[string]string{"status": "running", "leases": "5"},
			"dns":      map[string]string{"status": "running", "queries": "1234"},
			"firewall": map[string]string{"status": "running"},
		},
	})
}

func handleUsers(w http.ResponseWriter, r *http.Request) {
	sendJSON(w, []map[string]interface{}{
		{"username": "admin", "created_at": time.Now().Add(-24 * time.Hour).Format(time.RFC3339)},
	})
}

func handleReboot(w http.ResponseWriter, r *http.Request) {
	log.Println("Reboot requested (mock)")
	sendJSON(w, map[string]bool{"success": true})
}

func handleProtection(w http.ResponseWriter, r *http.Request) {
	sendJSON(w, map[string]interface{}{
		"enabled":              true,
		"anti_spoofing":        true,
		"bogon_filter":         true,
		"syn_flood_protection": true,
	})
}

func handleVPN(w http.ResponseWriter, r *http.Request) {
	sendJSON(w, map[string]interface{}{
		"wireguard": []interface{}{},
	})
}

func handleHCL(w http.ResponseWriter, r *http.Request) {
	sendJSON(w, map[string]interface{}{
		"content": "# Mock HCL Config\n\nzone \"WAN\" {\n  color = \"red\"\n}\n\nzone \"LAN\" {\n  color = \"green\"\n}\n",
	})
}

func handleHCLSection(w http.ResponseWriter, r *http.Request) {
	sendJSON(w, map[string]interface{}{
		"content": "# Section content\n",
	})
}

func handleHCLValidate(w http.ResponseWriter, r *http.Request) {
	sendJSON(w, map[string]interface{}{
		"valid":  true,
		"errors": []interface{}{},
	})
}

func handleHCLSave(w http.ResponseWriter, r *http.Request) {
	sendJSON(w, map[string]bool{"success": true})
}

func handleIPSetStatus(w http.ResponseWriter, r *http.Request) {
	sendJSON(w, map[string]interface{}{
		"sets": []map[string]interface{}{
			{"name": "firehol_level1", "entries": 5000, "last_update": time.Now().Add(-1 * time.Hour).Format(time.RFC3339)},
			{"name": "trusted_ips", "entries": 10, "last_update": time.Now().Add(-24 * time.Hour).Format(time.RFC3339)},
		},
	})
}

func handleIPSetRefresh(w http.ResponseWriter, r *http.Request) {
	sendJSON(w, map[string]bool{"success": true})
}

func handleBackups(w http.ResponseWriter, r *http.Request) {
	sendJSON(w, []map[string]interface{}{
		{"version": "2024-12-12T10:00:00Z", "size": 1024, "pinned": false},
		{"version": "2024-12-11T10:00:00Z", "size": 1000, "pinned": true},
	})
}

func handleBackupCreate(w http.ResponseWriter, r *http.Request) {
	sendJSON(w, map[string]interface{}{
		"success": true,
		"version": time.Now().Format(time.RFC3339),
	})
}

func handleBackupRestore(w http.ResponseWriter, r *http.Request) {
	sendJSON(w, map[string]bool{"success": true})
}

func handleBackupContent(w http.ResponseWriter, r *http.Request) {
	sendJSON(w, map[string]interface{}{
		"content": "# Backup content\nzone \"WAN\" {}\n",
	})
}

func handleBackupPin(w http.ResponseWriter, r *http.Request) {
	sendJSON(w, map[string]bool{"success": true})
}

func handleVLAN(w http.ResponseWriter, r *http.Request) {
	sendJSON(w, map[string]bool{"success": true})
}

func handleBonds(w http.ResponseWriter, r *http.Request) {
	sendJSON(w, map[string]bool{"success": true})
}
