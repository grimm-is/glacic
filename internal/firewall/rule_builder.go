//go:build linux
// +build linux

package firewall

import (
	"net"
	"strconv"
	"strings"

	"github.com/google/nftables/expr"
)

const (
	// IP Header Constants
	IPv6HeaderLen = 16
	IPv4HeaderLen = 4 // Base length for payload matching

	// IPv6 Header Offsets (RFC 2460)
	IPv6SrcOffset = 8
	IPv6DstOffset = 24

	// IPv4 Header Offsets (RFC 791)
	IPv4SrcOffset = 12
	IPv4DstOffset = 16
)

// buildIPMatch builds expressions to match source or destination IP/CIDR (IPv4 or IPv6)
func (m *Manager) buildIPMatch(cidr string, isSrc bool) []expr.Any {
	var exprs []expr.Any

	// Parse CIDR
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		// Try parsing as single IP
		ip := net.ParseIP(cidr)
		if ip == nil {
			return exprs // Invalid IP
		}
		if ip4 := ip.To4(); ip4 != nil {
			ipNet = &net.IPNet{IP: ip4, Mask: net.CIDRMask(32, 32)}
		} else {
			ipNet = &net.IPNet{IP: ip, Mask: net.CIDRMask(128, 128)}
		}
	}

	isIPv6 := ipNet.IP.To4() == nil

	// Add meta check to ensure we match the correct IP version
	// This is important in 'inet' table to avoid matching IPv6 rules on IPv4 packets
	if isIPv6 {
		exprs = append(exprs, &expr.Meta{Key: expr.MetaKeyNFPROTO, Register: 1})
		exprs = append(exprs, &expr.Cmp{
			Op:       expr.CmpOpEq,
			Register: 1,
			Data:     []byte{byte(ProtoIPv6)},
		})
	} else {
		exprs = append(exprs, &expr.Meta{Key: expr.MetaKeyNFPROTO, Register: 1})
		exprs = append(exprs, &expr.Cmp{
			Op:       expr.CmpOpEq,
			Register: 1,
			Data:     []byte{byte(ProtoIPv4)},
		})
	}

	// Offsets
	var offset uint32
	var lenBytes uint32
	var payloadBase expr.PayloadBase

	if isIPv6 {
		lenBytes = IPv6HeaderLen
		payloadBase = expr.PayloadBaseNetworkHeader
		if isSrc {
			offset = IPv6SrcOffset
		} else {
			offset = IPv6DstOffset
		}
	} else {
		lenBytes = IPv4HeaderLen
		payloadBase = expr.PayloadBaseNetworkHeader
		if isSrc {
			offset = IPv4SrcOffset
		} else {
			offset = IPv4DstOffset
		}
	}

	// Load IP from packet
	exprs = append(exprs, &expr.Payload{
		DestRegister: 1,
		Base:         payloadBase,
		Offset:       offset,
		Len:          lenBytes,
	})

	// Calculate mask
	mask := ipNet.Mask

	// Apply netmask if not a full host match
	ones, bits := mask.Size()
	if ones < bits {
		// Apply mask to loaded IP
		exprs = append(exprs, &expr.Bitwise{
			SourceRegister: 1,
			DestRegister:   1,
			Len:            lenBytes,
			Mask:           mask,
			Xor:            make([]byte, lenBytes),
		})
	}

	// Compare with network address
	targetIP := ipNet.IP.Mask(mask)
	if isIPv6 {
		targetIP = targetIP.To16()
	} else {
		targetIP = targetIP.To4()
	}

	exprs = append(exprs, &expr.Cmp{
		Op:       expr.CmpOpEq,
		Register: 1,
		Data:     targetIP,
	})

	return exprs
}

// buildSetMatch builds expressions to match against an nftables set.
func (m *Manager) buildSetMatch(setName string, isSrc bool, ipsetTypes map[string]string) []expr.Any {
	setType := ipsetTypes[setName]
	if setType == "" {
		setType = "ipv4_addr" // Default fallback
	}

	isIPv6 := setType == "ipv6_addr"

	// Offsets
	var offset uint32
	var lenBytes uint32
	var payloadBase expr.PayloadBase

	if isIPv6 {
		lenBytes = IPv6HeaderLen
		payloadBase = expr.PayloadBaseNetworkHeader
		if isSrc {
			offset = IPv6SrcOffset
		} else {
			offset = IPv6DstOffset
		}
	} else {
		lenBytes = IPv4HeaderLen
		payloadBase = expr.PayloadBaseNetworkHeader
		if isSrc {
			offset = IPv4SrcOffset
		} else {
			offset = IPv4DstOffset
		}
	}

	return []expr.Any{
		// Load IP address into register 1
		&expr.Payload{
			DestRegister: 1,
			Base:         payloadBase,
			Offset:       offset,
			Len:          lenBytes,
		},
		// Lookup in named set
		&expr.Lookup{
			SourceRegister: 1,
			SetName:        setName,
		},
	}
}

// buildLimitExpr builds rate limiting expressions
// Format: "10/second", "100/minute", "1000/hour"
func (m *Manager) buildLimitExpr(limit string) []expr.Any {
	parts := strings.Split(limit, "/")
	if len(parts) != 2 {
		return nil
	}

	rate, err := strconv.ParseUint(parts[0], 10, 32)
	if err != nil || rate == 0 {
		return nil
	}

	var unit expr.LimitTime
	switch strings.ToLower(parts[1]) {
	case "second", "sec", "s":
		unit = expr.LimitTimeSecond
	case "minute", "min", "m":
		unit = expr.LimitTimeMinute
	case "hour", "h":
		unit = expr.LimitTimeHour
	case "day", "d":
		unit = expr.LimitTimeDay
	default:
		return nil
	}

	return []expr.Any{
		&expr.Limit{
			Type:  expr.LimitTypePkts,
			Rate:  uint64(rate),
			Unit:  unit,
			Burst: uint32(rate), // Allow burst equal to rate
		},
	}
}
