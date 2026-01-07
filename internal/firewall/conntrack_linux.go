//go:build linux

package firewall

import (
	"fmt"

	"github.com/ti-mo/conntrack"
)

// GetConntrackEntries returns active connection tracking entries using netlink API.
func GetConntrackEntries() ([]ConntrackEntry, error) {
	conn, err := conntrack.Dial(nil)
	if err != nil {
		return nil, fmt.Errorf("conntrack dial failed: %w", err)
	}
	defer conn.Close()

	flows, err := conn.Dump(nil)
	if err != nil {
		return nil, fmt.Errorf("conntrack dump failed: %w", err)
	}

	entries := make([]ConntrackEntry, 0, len(flows))
	for _, f := range flows {
		entry := ConntrackEntry{
			Timeout: f.Timeout,
		}

		// Protocol
		if f.TupleOrig.Proto.Protocol != 0 {
			switch f.TupleOrig.Proto.Protocol {
			case 6:
				entry.Protocol = "tcp"
			case 17:
				entry.Protocol = "udp"
			case 1:
				entry.Protocol = "icmp"
			default:
				entry.Protocol = fmt.Sprintf("%d", f.TupleOrig.Proto.Protocol)
			}
		}

		// Source and destination from original tuple
		if f.TupleOrig.IP.SourceAddress.IsValid() {
			entry.SrcIP = f.TupleOrig.IP.SourceAddress.String()
		}
		if f.TupleOrig.IP.DestinationAddress.IsValid() {
			entry.DstIP = f.TupleOrig.IP.DestinationAddress.String()
		}
		entry.SrcPort = f.TupleOrig.Proto.SourcePort
		entry.DstPort = f.TupleOrig.Proto.DestinationPort

		// TCP state (if available)
		if entry.Protocol == "tcp" && f.ProtoInfo.TCP != nil {
			entry.State = tcpStateString(f.ProtoInfo.TCP.State)
		}

		entries = append(entries, entry)
	}

	return entries, nil
}

// tcpStateString converts TCP state constant to string.
func tcpStateString(state uint8) string {
	states := map[uint8]string{
		0:  "NONE",
		1:  "SYN_SENT",
		2:  "SYN_RECV",
		3:  "ESTABLISHED",
		4:  "FIN_WAIT",
		5:  "CLOSE_WAIT",
		6:  "LAST_ACK",
		7:  "TIME_WAIT",
		8:  "CLOSE",
		9:  "SYN_SENT2",
		10: "MAX",
	}
	if s, ok := states[state]; ok {
		return s
	}
	return fmt.Sprintf("UNKNOWN(%d)", state)
}
