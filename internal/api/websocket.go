package api

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"grimm.is/glacic/internal/ctlplane"
	"grimm.is/glacic/internal/logging"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	// Mitigation: OWASP A01:2021-Broken Access Control (Cross-Site WebSocket Hijacking)
	// Enforce same-origin policy for WebSocket upgrades
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			// No origin header (e.g., same-origin requests from browser, or non-browser clients)
			return true
		}
		host := r.Host
		// Allow if origin host matches request host (same-origin)
		// Origin format: scheme://host[:port]
		// Host format: host[:port]
		// We compare just the host:port portion
		if len(origin) > 7 && origin[:7] == "http://" {
			return origin[7:] == host
		}
		if len(origin) > 8 && origin[:8] == "https://" {
			return origin[8:] == host
		}
		return false
	},
}

type ServiceStatus struct {
	Status string `json:"status"` // online, offline
	Uptime string `json:"uptime"`
}

// WSMessage is a topic-based message sent to clients
type WSMessage struct {
	Topic string `json:"topic"`
	Data  any    `json:"data"`
}

// wsClient represents a connected WebSocket client with subscriptions
type wsClient struct {
	conn   *websocket.Conn
	topics map[string]bool
	send   chan []byte
}

// WSManager handles websocket connections with topic-based pub/sub
type WSManager struct {
	clients    map[*wsClient]bool
	register   chan *wsClient
	unregister chan *wsClient
	mutex      sync.RWMutex
	client     ctlplane.ControlPlaneClient // RPC client to fetch status
}

func NewWSManager(client ctlplane.ControlPlaneClient) *WSManager {
	manager := &WSManager{
		clients:    make(map[*wsClient]bool),
		register:   make(chan *wsClient),
		unregister: make(chan *wsClient),
		client:     client,
	}
	go manager.run()
	go manager.statusLoop()
	return manager
}

func (m *WSManager) run() {
	for {
		select {
		case client := <-m.register:
			m.mutex.Lock()
			m.clients[client] = true
			m.mutex.Unlock()
		case client := <-m.unregister:
			m.mutex.Lock()
			if _, ok := m.clients[client]; ok {
				delete(m.clients, client)
				close(client.send)
				client.conn.Close()
			}
			m.mutex.Unlock()
		}
	}
}

// Publish sends a message to all clients subscribed to the given topic
func (m *WSManager) Publish(topic string, data any) {
	msg := WSMessage{Topic: topic, Data: data}
	msgBytes, err := json.Marshal(msg)
	if err != nil {
		return
	}

	m.mutex.RLock()
	defer m.mutex.RUnlock()

	for client := range m.clients {
		// Check if client is subscribed to this topic (or "status" is always sent for backwards compat)
		if topic == "status" || client.topics[topic] {
			select {
			case client.send <- msgBytes:
			default:
				// Client buffer full, skip
			}
		}
	}
}

// NotificationType defines the type of notification
type NotificationType string

const (
	NotifySuccess NotificationType = "success"
	NotifyError   NotificationType = "error"
	NotifyWarning NotificationType = "warning"
	NotifyInfo    NotificationType = "info"
)

// Notification represents a user-facing notification
type Notification struct {
	Type    NotificationType `json:"type"`
	Title   string           `json:"title"`
	Message string           `json:"message"`
	Time    int64            `json:"time"`
}

// PublishNotification sends a user notification to all subscribed clients
func (m *WSManager) PublishNotification(ntype NotificationType, title, message string) {
	m.Publish("notification", Notification{
		Type:    ntype,
		Title:   title,
		Message: message,
		Time:    time.Now().Unix(),
	})
}

// statusLoop polls status, logs, and stats and publishes them
func (m *WSManager) statusLoop() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	// Track last log timestamp for incremental updates
	var lastLogTime string
	// Track last notification ID for cursoring
	var lastNotifyID int64

	for range ticker.C {
		if m.client == nil {
			continue
		}

		// Publish status (always sent for backwards compat)
		status, err := m.client.GetStatus()
		if err != nil {
			logging.APILog("error", "Failed to fetch status for WS: %v", err)
		} else {
			wsStatus := ServiceStatus{
				Status: "online",
				Uptime: status.Uptime,
			}
			m.Publish("status", wsStatus)
		}

		// Publish logs (only if clients are subscribed)
		if m.hasSubscribers("logs") {
			args := &ctlplane.GetLogsArgs{
				Limit: 50,
				Since: lastLogTime,
			}
			reply, err := m.client.GetLogs(args)
			if err == nil && reply != nil && len(reply.Entries) > 0 {
				m.Publish("logs", reply.Entries)
				// Update last seen timestamp
				if len(reply.Entries) > 0 {
					lastLogTime = reply.Entries[0].Timestamp
				}
			}
		}

		// Publish stats (only if clients are subscribed)
		if m.hasSubscribers("stats") {
			stats, err := m.client.GetSystemStats()
			if err == nil && stats != nil {
				m.Publish("stats", stats)
			}
		}

		// Publish notifications from control plane (always poll, forward to subscribed clients)
		if m.hasSubscribers("notification") {
			notifs, newLastID, err := m.client.GetNotifications(lastNotifyID)
			if err == nil && len(notifs) > 0 {
				for _, n := range notifs {
					m.Publish("notification", n)
				}
				lastNotifyID = newLastID
			}
		}
	}
}

// hasSubscribers checks if any client is subscribed to the given topic
func (m *WSManager) hasSubscribers(topic string) bool {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	for client := range m.clients {
		if client.topics[topic] {
			return true
		}
	}
	return false
}

// readPump handles incoming messages from a client (subscriptions)
func (c *wsClient) readPump(m *WSManager) {
	defer func() {
		m.unregister <- c
	}()

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			break
		}

		// Parse subscription message
		var msg struct {
			Action string   `json:"action"`
			Topics []string `json:"topics"`
		}
		if err := json.Unmarshal(message, &msg); err != nil {
			continue
		}

		switch msg.Action {
		case "subscribe":
			for _, topic := range msg.Topics {
				c.topics[topic] = true
			}
		case "unsubscribe":
			for _, topic := range msg.Topics {
				delete(c.topics, topic)
			}
		}
	}
}

// writePump sends messages to the client
func (c *wsClient) writePump() {
	defer c.conn.Close()

	for message := range c.send {
		if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
			break
		}
	}
}

// handleStatusWS handles the websocket connection
func (s *Server) handleStatusWS(w http.ResponseWriter, r *http.Request) {
	// Check authentication
	authorized := false
	var apiKey string

	// 1. Check API Key (CLI/Remote access)
	// Mitigation: OWASP A07:2021-Identification and Authentication Failures
	if s.Config.API != nil {
		apiKey = r.Header.Get("X-API-Key")
		if apiKey == "" {
			apiKey = r.URL.Query().Get("api_key")
		}

		if apiKey != "" {
			// Check Bootstrap Key
			if s.Config.API.BootstrapKey != "" && apiKey == s.Config.API.BootstrapKey {
				authorized = true
			}

			// Check Configured Keys
			if !authorized {
				for _, k := range s.Config.API.Keys {
					if k.Enabled && k.Key == apiKey {
						authorized = true
						break
					}
				}
			}
		}
	}

	// 2. Check Session (Web UI access)
	if !authorized && s.authStore != nil && s.authStore.HasUsers() {
		cookie, err := r.Cookie("session")
		if err == nil {
			if _, err := s.authStore.ValidateSession(cookie.Value); err == nil {
				authorized = true
			}
		}
	}

	// 3. Enforce Auth
	// If auth is required (Users exist OR API Key configured) and not authorized, deny
	usersExist := s.authStore != nil && s.authStore.HasUsers()
	apiConfigured := s.Config.API != nil && (s.Config.API.RequireAuth || len(s.Config.API.Keys) > 0 || s.Config.API.BootstrapKey != "")

	if (usersExist || apiConfigured) && !authorized {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if s.wsManager == nil {
		http.Error(w, "Websockets not enabled", http.StatusServiceUnavailable)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		logging.APILog("error", "Failed to upgrade WS: %v", err)
		return
	}

	client := &wsClient{
		conn:   conn,
		topics: make(map[string]bool),
		send:   make(chan []byte, 256),
	}

	s.wsManager.register <- client

	go client.writePump()
	go client.readPump(s.wsManager)
}
