package vpn

import (
	"encoding/hex"
	"fmt"
	"log"
	"net"

	"grimm.is/glacic/internal/config"
	"grimm.is/glacic/internal/network"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

// Configure6to4 sets up 6to4 tunnels based on config.
func Configure6to4(cfg *config.Config) error {
	if cfg.VPN == nil {
		return nil
	}

	for _, tunnel := range cfg.VPN.SixToFour {
		if !tunnel.Enabled {
			continue
		}

		if err := setup6to4Tunnel(tunnel); err != nil {
			log.Printf("Error setting up 6to4 tunnel %s: %v", tunnel.Name, err)
		}
	}
	return nil
}

func setup6to4Tunnel(cfg config.SixToFourConfig) error {
	// 1. Get IPv4 address of the physical interface (WAN)
	link, err := network.DefaultNetlinker.LinkByName(cfg.Interface)
	if err != nil {
		return fmt.Errorf("interface %s not found: %w", cfg.Interface, err)
	}

	addrs, err := network.DefaultNetlinker.AddrList(link, unix.AF_INET)
	if err != nil || len(addrs) == 0 {
		return fmt.Errorf("no IPv4 address found on %s", cfg.Interface)
	}

	// Use the first global unicast IPv4
	var publicIP net.IP
	for _, addr := range addrs {
		if addr.IP.IsGlobalUnicast() {
			publicIP = addr.IP
			break
		}
	}

	if publicIP == nil {
		return fmt.Errorf("no global IPv4 address on %s", cfg.Interface)
	}

	// 2. Calculate 6to4 Prefix: 2002:WWXX:YYZZ::/48
	// WWXX:YYZZ is the hex representation of the IPv4 address
	prefix := make([]byte, 16)
	prefix[0] = 0x20
	prefix[1] = 0x02
	copy(prefix[2:], publicIP.To4())

	// Create net.IPNet for the tunnel address (usually ::1/16)
	// Address: 2002:WWXX:YYZZ::1/16
	tunnelIP := make(net.IP, 16)
	copy(tunnelIP, prefix)
	tunnelIP[15] = 1

	log.Printf("[6to4] Detected Public IP: %s, 6to4 Prefix: %x", publicIP, prefix[:6])

	// 3. Create 'sit' tunnel interface
	tunnelName := "tun6to4" // could differ if multiple

	// Check if exists
	existing, err := network.DefaultNetlinker.LinkByName(tunnelName)
	if err == nil {
		// Verify if it's the same? Or just recreate. Recreating is safer for dynamic IP changes.
		network.DefaultNetlinker.LinkDel(existing)
	}

	sit := &netlink.Iptun{
		LinkAttrs: netlink.LinkAttrs{
			Name: tunnelName,
			MTU:  1480, // Default 6to4 MTU
		},
		Ttl:   64,
		Local: publicIP,
	}

	if cfg.MTU > 0 {
		sit.LinkAttrs.MTU = cfg.MTU
	}

	if err := network.DefaultNetlinker.LinkAdd(sit); err != nil {
		return fmt.Errorf("failed to create sit interface: %w", err)
	}

	// 4. Assign Address
	// ip -6 addr add 2002:WWXX:YYZZ::1/16 dev tun6to4

	// Note: 6to4 usually uses /16 on the tunnel interface itself to route all 2002::/16 traffic via it?
	// Actually, usually you assign specific /48 subnet ID 0 to it, so /16 is fine.

	addr := &netlink.Addr{
		IPNet: &net.IPNet{
			IP:   tunnelIP,
			Mask: net.CIDRMask(16, 128),
		},
	}

	tunLink, _ := network.DefaultNetlinker.LinkByName(tunnelName)
	if err := network.DefaultNetlinker.AddrAdd(tunLink, addr); err != nil {
		return fmt.Errorf("failed to add address to tunnel: %w", err)
	}

	if err := network.DefaultNetlinker.LinkSetUp(tunLink); err != nil {
		return fmt.Errorf("failed to bring up tunnel: %w", err)
	}

	// 5. Add Default Route (2002:c058:6301::1 is the anycast relay, deprecated?)
	// Modern 6to4 relay is 192.88.99.1 which corresponds to 2002:c058:6301::1
	// Route ::/0 via ::192.88.99.1 on device tun6to4

	relayIP := net.ParseIP("::192.88.99.1")
	route := &netlink.Route{
		Dst:       &net.IPNet{IP: net.IPv6zero, Mask: net.CIDRMask(0, 128)}, // ::/0
		Gw:        relayIP,
		LinkIndex: tunLink.Attrs().Index,
	}

	if err := network.DefaultNetlinker.RouteAdd(route); err != nil {
		return fmt.Errorf("failed to add default route: %w", err)
	}

	log.Printf("[6to4] Tunnel configured successfully on %s", tunnelName)
	return nil
}

// Get6to4Prefix returns the calculated 2002::/48 prefix for a given public IP.
func Get6to4Prefix(ip net.IP) string {
	v4 := ip.To4()
	if v4 == nil {
		return ""
	}
	return fmt.Sprintf("2002:%s::/48", hex.EncodeToString(v4))
}
