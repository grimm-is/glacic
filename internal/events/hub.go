package events

import (
	"sync"
	"time"
)

// Hub is the central event bus for Glacic.
// It provides pub/sub semantics with typed events and non-blocking fan-out.
type Hub struct {
	mu   sync.RWMutex
	subs map[EventType][]chan Event

	// Global subscribers receive all events
	global []chan Event

	// Metrics
	published uint64
	dropped   uint64
}

// NewHub creates a new event hub.
func NewHub() *Hub {
	return &Hub{
		subs: make(map[EventType][]chan Event),
	}
}

// Publish sends an event to all subscribers of that event type.
// This is non-blocking - if a subscriber's channel is full, the event is dropped.
func (h *Hub) Publish(e Event) {
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now()
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	h.published++

	// Send to type-specific subscribers
	for _, ch := range h.subs[e.Type] {
		select {
		case ch <- e:
		default:
			h.dropped++
		}
	}

	// Send to global subscribers
	for _, ch := range h.global {
		select {
		case ch <- e:
		default:
			h.dropped++
		}
	}
}

// PublishAsync sends an event in a goroutine (fire-and-forget).
func (h *Hub) PublishAsync(e Event) {
	go h.Publish(e)
}

// Subscribe returns a channel that receives events of the specified types.
// If no types are specified, subscribes to all events.
// The caller is responsible for draining the channel to avoid drops.
func (h *Hub) Subscribe(bufSize int, types ...EventType) <-chan Event {
	if bufSize <= 0 {
		bufSize = 256
	}

	ch := make(chan Event, bufSize)

	h.mu.Lock()
	defer h.mu.Unlock()

	if len(types) == 0 {
		// Global subscription
		h.global = append(h.global, ch)
	} else {
		for _, t := range types {
			h.subs[t] = append(h.subs[t], ch)
		}
	}

	return ch
}

// Unsubscribe removes a channel from all subscriptions.
// The channel is NOT closed by this method.
func (h *Hub) Unsubscribe(ch <-chan Event) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Remove from global
	h.global = removeFromSlice(h.global, ch)

	// Remove from type-specific
	for t, subs := range h.subs {
		h.subs[t] = removeFromSlice(subs, ch)
	}
}

// Stats returns publish/drop counts for monitoring.
func (h *Hub) Stats() (published, dropped uint64) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.published, h.dropped
}

// removeFromSlice removes a channel from a slice of channels.
func removeFromSlice(slice []chan Event, target <-chan Event) []chan Event {
	result := make([]chan Event, 0, len(slice))
	for _, ch := range slice {
		if ch != target {
			result = append(result, ch)
		}
	}
	return result
}

// ──────────────────────────────────────────────────────────────────────────────
// Convenience Methods
// ──────────────────────────────────────────────────────────────────────────────

// EmitDHCPLease publishes a DHCP lease event.
func (h *Hub) EmitDHCPLease(mac, ip, hostname, vendor string) {
	h.Publish(Event{
		Type:   EventDHCPLease,
		Source: "dhcp",
		Data: DHCPLeaseData{
			MAC:      mac,
			IP:       ip,
			Hostname: hostname,
			Vendor:   vendor,
		},
	})
}

// EmitNFTCounter publishes a firewall counter update.
func (h *Hub) EmitNFTCounter(ruleID string, packets, bytes uint64) {
	h.Publish(Event{
		Type:   EventNFTCounter,
		Source: "nft",
		Data: NFTCounterData{
			RuleID:  ruleID,
			Packets: packets,
			Bytes:   bytes,
		},
	})
}

// EmitDeviceSeen publishes a device discovery event.
func (h *Hub) EmitDeviceSeen(mac, ip, hostname, vendor, iface, method string) {
	h.Publish(Event{
		Type:   EventDeviceSeen,
		Source: "discovery",
		Data: DeviceSeenData{
			MAC:       mac,
			IP:        ip,
			Hostname:  hostname,
			Vendor:    vendor,
			Interface: iface,
			Method:    method,
		},
	})
}
