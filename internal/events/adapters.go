// Package events adapters wire existing Glacic event sources to the EventHub.
// This provides a unified event stream for UI, stats, and learning engine.
package events

import (
	"net"

	"grimm.is/glacic/internal/device"
)

// ──────────────────────────────────────────────────────────────────────────────
// DHCP Adapter
// ──────────────────────────────────────────────────────────────────────────────

// DHCPAdapter implements dhcp.LeaseListener and publishes to EventHub.
type DHCPAdapter struct {
	hub    *Hub
	device *device.Manager // For vendor lookup
}

// NewDHCPAdapter creates a new DHCP adapter.
func NewDHCPAdapter(hub *Hub, deviceMgr *device.Manager) *DHCPAdapter {
	return &DHCPAdapter{
		hub:    hub,
		device: deviceMgr,
	}
}

// OnLease implements dhcp.LeaseListener.
func (a *DHCPAdapter) OnLease(mac string, ip net.IP, hostname string) {
	var vendor string
	if a.device != nil {
		info := a.device.GetDevice(mac)
		vendor = info.Vendor
	}

	a.hub.Publish(Event{
		Type:   EventDHCPLease,
		Source: "dhcp",
		Data: DHCPLeaseData{
			MAC:      mac,
			IP:       ip.String(),
			Hostname: hostname,
			Vendor:   vendor,
		},
	})
}

// OnLeaseExpired implements dhcp.ExpirationListener.
func (a *DHCPAdapter) OnLeaseExpired(mac string, ip net.IP, hostname string) {
	a.hub.Publish(Event{
		Type:   EventDHCPExpire,
		Source: "dhcp",
		Data: DHCPLeaseData{
			MAC:      mac,
			IP:       ip.String(),
			Hostname: hostname,
		},
	})
}

// ──────────────────────────────────────────────────────────────────────────────
// NFLog Adapter
// ──────────────────────────────────────────────────────────────────────────────

// NFLogAdapter subscribes to nflog entries and publishes to EventHub.
type NFLogAdapter struct {
	hub *Hub
}

// NewNFLogAdapter creates a new NFLog adapter.
func NewNFLogAdapter(hub *Hub) *NFLogAdapter {
	return &NFLogAdapter{hub: hub}
}

// HandleEntry processes an nflog entry and publishes an event.
// Call this from ctlplane.NFLogReader's subscriber.
func (a *NFLogAdapter) HandleEntry(ruleID, srcIP, dstIP, protocol, action string, srcPort, dstPort uint16, bytes uint64) {
	a.hub.Publish(Event{
		Type:   EventNFTMatch,
		Source: "nflog",
		Data: NFTMatchData{
			RuleID:   ruleID,
			SrcIP:    srcIP,
			DstIP:    dstIP,
			SrcPort:  srcPort,
			DstPort:  dstPort,
			Protocol: protocol,
			Action:   action,
			Bytes:    bytes,
		},
	})
}

// ──────────────────────────────────────────────────────────────────────────────
// Stats Collector Adapter
// ──────────────────────────────────────────────────────────────────────────────

// StatsAdapter wraps the stats collector to publish counter updates.
type StatsAdapter struct {
	hub *Hub
}

// NewStatsAdapter creates a new stats adapter.
func NewStatsAdapter(hub *Hub) *StatsAdapter {
	return &StatsAdapter{hub: hub}
}

// OnCounterUpdate emits an event when a rule counter is updated.
func (a *StatsAdapter) OnCounterUpdate(ruleID string, packets, bytes uint64) {
	a.hub.Publish(Event{
		Type:   EventNFTCounter,
		Source: "stats",
		Data: NFTCounterData{
			RuleID:  ruleID,
			Packets: packets,
			Bytes:   bytes,
		},
	})
}

// ──────────────────────────────────────────────────────────────────────────────
// WebSocket Bridge
// ──────────────────────────────────────────────────────────────────────────────

// WSBridge forwards events to the WebSocket manager for live UI updates.
// It subscribes to the EventHub and translates events to WS topics.
type WSBridge struct {
	hub       *Hub
	publisher func(topic string, data any) // WSManager.Publish
	stop      chan struct{}
}

// NewWSBridge creates a bridge from EventHub to WebSocket.
func NewWSBridge(hub *Hub, wsPublish func(topic string, data any)) *WSBridge {
	return &WSBridge{
		hub:       hub,
		publisher: wsPublish,
		stop:      make(chan struct{}),
	}
}

// Start begins forwarding events to WebSocket clients.
func (b *WSBridge) Start() {
	// Subscribe to events that should go to UI
	events := b.hub.Subscribe(256,
		EventDHCPLease,
		EventDHCPExpire,
		EventDeviceSeen,
		EventNFTCounter,
		EventFlowNew,
		EventFlowApproved,
	)

	go func() {
		for {
			select {
			case <-b.stop:
				return
			case e := <-events:
				// Map event types to WS topics
				topic := eventTypeToWSTopic(e.Type)
				if topic != "" {
					b.publisher(topic, e)
				}
			}
		}
	}()
}

// Stop stops the bridge.
func (b *WSBridge) Stop() {
	close(b.stop)
}

// eventTypeToWSTopic maps event types to WebSocket topic names.
func eventTypeToWSTopic(t EventType) string {
	switch t {
	case EventDHCPLease, EventDHCPExpire:
		return "leases"
	case EventDeviceSeen, EventDeviceNew:
		return "devices"
	case EventNFTCounter:
		return "stats"
	case EventFlowNew, EventFlowApproved:
		return "learning"
	default:
		return ""
	}
}
