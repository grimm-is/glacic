package ctlplane

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/rpc"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"grimm.is/glacic/internal/brand"
	"grimm.is/glacic/internal/config"
	"grimm.is/glacic/internal/device"
	"grimm.is/glacic/internal/firewall"
	"grimm.is/glacic/internal/learning"
	"grimm.is/glacic/internal/logging"
	"grimm.is/glacic/internal/network"
	"grimm.is/glacic/internal/scheduler"
	"grimm.is/glacic/internal/services"
	"grimm.is/glacic/internal/services/dhcp"
	"grimm.is/glacic/internal/services/discovery"
	"grimm.is/glacic/internal/services/lldp"
	"grimm.is/glacic/internal/services/scanner"
	"grimm.is/glacic/internal/state"
	"grimm.is/glacic/internal/upgrade"
)

// Server is the privileged control plane RPC server
type Server struct {
	config        *config.Config
	configFile    string
	hclConfig     *config.ConfigFile    // For HCL round-trip editing
	backupManager *config.BackupManager // For versioned backups
	nflogReader   LogReader             // For netfilter log capture (interface)
	sniReader     LogReader             // For SNI log capture (Group 100)
	nfqueueReader *NFQueueReader        // For inline packet inspection (learning mode)
	scheduler     *scheduler.Scheduler  // For scheduled tasks
	listener      net.Listener          // The RPC listener (for upgrade handoff)

	// Service references
	stateStore state.Store
	upgradeMgr *upgrade.Manager

	// Sub-managers
	networkManager      *NetworkManager
	networkSafeApply    *NetworkSafeApplyManager
	systemManager       *SystemManager
	serviceOrchestrator ServiceManager
	firewallManager     *firewall.Manager // Injected firewall manager
	ipsetService        *firewall.IPSetService
	learningService     *learning.Service
	dhcpService         *dhcp.Service
	lldpService         *lldp.Service
	learningEngine      *learning.Engine
	policyRouting       *network.PolicyRoutingManager
	uplinkManager       *network.UplinkManager
	deviceManager       *device.Manager
	scannerService      *scanner.Scanner
	deviceCollector     *discovery.Collector
	netLib              network.NetworkManager // Injected network library

	// Notification hub for broadcasting to all consumers
	notifyHub *NotificationHub

	// Disarm hook to stop monitors (watchdog, auto-restart) in the main process
	disarmFunc func()

	// Concurrency protection for config structure (Critical!)
	mu sync.RWMutex
}

// SetDisarmFunc sets the function to call when an upgrade is initiated.
// This allows the main process to stop monitors (watchdog, API restarter)
// to prevent race conditions during handoff.
func (s *Server) SetDisarmFunc(f func()) {
	s.disarmFunc = f
}

// verifyUpgradeBinary verifies the upgrade binary checksum.
// Variable for testability only.
var verifyUpgradeBinary = func(path, expectedChecksum string) error {
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("upgrade binary not found at %s: %v", path, err)
	}
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("failed to open upgrade binary: %v", err)
	}
	defer f.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, f); err != nil {
		return fmt.Errorf("failed to calculate checksum: %v", err)
	}
	calculatedChecksum := hex.EncodeToString(hasher.Sum(nil))

	if expectedChecksum == "" {
		log.Printf("[CTL] Warning: Upgrade binary verification skipped (no checksum provided)")
		return nil
	}

	if expectedChecksum != calculatedChecksum {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expectedChecksum, calculatedChecksum)
	}
	return nil
}

// NewServer creates a new control plane server
func NewServer(cfg *config.Config, configFile string, netLib network.NetworkManager) *Server {
	nm := NewNetworkManager(cfg)
	s := &Server{
		config:              cfg,
		configFile:          configFile,
		netLib:              netLib,
		backupManager:       config.NewBackupManager(configFile, 20),
		nflogReader:         NewNFLogReader(10000, 0),
		sniReader:           NewNFLogReader(1000, 100),
		networkManager:      nm,
		networkSafeApply:    NewNetworkSafeApplyManager(nm),
		systemManager:       NewSystemManager(configFile),
		serviceOrchestrator: NewServiceOrchestrator(),
		policyRouting:       network.NewPolicyRoutingManager(),
		uplinkManager:       network.NewUplinkManager(),
		scannerService:      scanner.New(logging.WithComponent("scanner"), scanner.Config{Timeout: 5 * time.Second, Concurrency: 50}),
		notifyHub:           NewNotificationHub(100),
		scheduler:           scheduler.New(logging.WithComponent("scheduler")),
	}

	// Start nflog reader (runs in background)
	if err := s.nflogReader.Start(); err != nil {
		log.Printf("[CTL] Warning: failed to start nflog reader: %v", err)
	}
	if err := s.sniReader.Start(); err != nil {
		log.Printf("[CTL] Warning: failed to start sni reader: %v", err)
	}

	// Start Scheduler
	s.scheduler.Start()

	return s
}

// RegisterService adds a service to the control plane
func (s *Server) RegisterService(svc services.Service) {
	s.serviceOrchestrator.RegisterService(svc)
}

// SetFirewallManager injects the firewall manager
func (s *Server) SetFirewallManager(mgr *firewall.Manager) {
	s.firewallManager = mgr
}

// SetStateStore injects the state store
func (s *Server) SetStateStore(store state.Store) {
	s.stateStore = store
}

// SetUpgradeManager injects the upgrade manager
func (s *Server) SetUpgradeManager(mgr *upgrade.Manager) {
	s.upgradeMgr = mgr
}

// SetIPSetService injects the IPSet service
func (s *Server) SetIPSetService(svc *firewall.IPSetService) {
	s.ipsetService = svc
}

// SetDHCPService injects the DHCP service
func (s *Server) SetDHCPService(svc *dhcp.Service) {
	s.dhcpService = svc
}

// SetUplinkManager injects the Uplink manager
func (s *Server) SetUplinkManager(mgr *network.UplinkManager) {
	s.uplinkManager = mgr
}

// SetLogReader injects a custom log reader (for testing)
func (s *Server) SetLogReader(reader LogReader) {
	s.nflogReader = reader
}

// SubscribeNFLog returns a channel that receives nflog entries
func (s *Server) SubscribeNFLog() <-chan NFLogEntry {
	if s.nflogReader != nil {
		return s.nflogReader.Subscribe()
	}
	// Return empty channel if no reader
	ch := make(chan NFLogEntry)
	close(ch)
	return ch
}

// SetLearningService injects the learning service and starts log forwarding
func (s *Server) SetLearningService(svc *learning.Service) {
	s.learningService = svc

	// Check if inline mode is enabled (uses nfqueue instead of nflog)
	if s.config != nil && s.config.RuleLearning != nil && s.config.RuleLearning.InlineMode {
		s.startInlineLearning(svc)
		return
	}

	// Default: async mode using nflog
	s.startAsyncLearning(svc)
}

// startInlineLearning uses nfqueue for synchronous packet inspection.
// This fixes the "first packet" problem by holding packets until a verdict is returned.
func (s *Server) startInlineLearning(svc *learning.Service) {
	group := uint16(100) // Default group
	if s.config.RuleLearning.LogGroup > 0 {
		group = uint16(s.config.RuleLearning.LogGroup)
	}

	s.nfqueueReader = NewNFQueueReader(group)
	s.nfqueueReader.SetVerdictFunc(func(entry NFLogEntry) bool {
		// Convert NFLogEntry to PacketInfo
		pkt := learning.PacketInfo{
			SrcMAC:    entry.SrcMAC,
			SrcIP:     entry.SrcIP,
			DstIP:     entry.DstIP,
			DstPort:   int(entry.DstPort),
			Protocol:  entry.Protocol,
			Interface: entry.InDevName,
		}
		if pkt.SrcMAC == "" {
			pkt.SrcMAC = entry.HwAddr
		}
		if pkt.Interface == "" {
			pkt.Interface = entry.InDev
		}

		// Get verdict from learning engine synchronously
		verdict, err := svc.Engine().ProcessPacket(&pkt)
		if err != nil {
			log.Printf("[CTL] NFQueue verdict error: %v (accepting packet)", err)
			return true // Fail-open: accept on error
		}
		return verdict
	})

	if err := s.nfqueueReader.Start(); err != nil {
		log.Printf("[CTL] Warning: failed to start nfqueue reader: %v", err)
		log.Printf("[CTL] Falling back to async nflog mode")
		s.startAsyncLearning(svc)
		return
	}

	log.Printf("[CTL] Learning service started in INLINE mode (nfqueue group %d)", group)
}

// startAsyncLearning uses nflog for async packet logging (original behavior).
// The first packet of a new flow may be dropped before the allow rule is added.
func (s *Server) startAsyncLearning(svc *learning.Service) {
	bridge := func(reader LogReader) {
		if reader == nil {
			return
		}
		sub := reader.Subscribe()
		for entry := range sub {
			// Convert NFLogEntry to PacketInfo
			pkt := learning.PacketInfo{
				SrcMAC:    entry.HwAddr, // Use HwAddr as source mac if SrcMAC is empty? entry.SrcMAC is better if available
				SrcIP:     entry.SrcIP,
				DstIP:     entry.DstIP,
				DstPort:   int(entry.DstPort),
				Protocol:  entry.Protocol,
				Interface: entry.InDevName, // Use resolved name
			}
			// Fallback for Interface if empty
			if pkt.Interface == "" {
				pkt.Interface = entry.InDev
			}
			// Fallback for SrcMAC
			if entry.SrcMAC != "" {
				pkt.SrcMAC = entry.SrcMAC
			}

			// Parse policy from prefix if present (e.g. "LEARN:policy:")
			if strings.HasPrefix(entry.Prefix, "LEARN:") {
				parts := strings.Split(entry.Prefix, ":")
				if len(parts) >= 2 {
					pkt.Policy = parts[1]
				}
			}

			s.learningService.IngestPacket(pkt)
		}
	}

	// Start forwarding logs from both readers
	// Group 0 (Drops/General)
	go bridge(s.nflogReader)
	// Group 100 (Learning/SNI)
	go bridge(s.sniReader)

	log.Printf("[CTL] Learning service started in ASYNC mode (nflog)")
}

// SetLearningEngine injects the learning engine and starts SNI forwarding
func (s *Server) SetLearningEngine(engine *learning.Engine) {
	s.learningEngine = engine

	// Start forwarding SNI logs
	go func() {
		sub := s.sniReader.Subscribe()
		for entry := range sub {
			if sni, ok := entry.Extra["sni"]; ok {
				// Forward to engine
				s.learningEngine.ProcessSNI(entry.HwAddr, entry.SrcIP, entry.DstIP, sni)
			}
		}
	}()
}

// SetLLDPService injects the LLDP service
func (s *Server) SetLLDPService(svc *lldp.Service) {
	s.lldpService = svc
}

// Notify publishes a notification to all clients via the notification hub
func (s *Server) Notify(ntype NotificationType, title, message string) {
	if s.notifyHub != nil {
		s.notifyHub.Publish(ntype, title, message)
	}
}

// SetDeviceManager injects the device manager
func (s *Server) SetDeviceManager(mgr *device.Manager) {
	s.deviceManager = mgr
}

// SetDeviceCollector injects the device collector for network discovery
func (s *Server) SetDeviceCollector(collector *discovery.Collector) {
	s.deviceCollector = collector
}

// GetNotifications returns notifications since a given ID (RPC method)
func (s *Server) GetNotifications(args *GetNotificationsArgs, reply *GetNotificationsReply) error {
	if s.notifyHub == nil {
		reply.Notifications = []Notification{}
		return nil
	}

	reply.Notifications = s.notifyHub.GetSince(args.SinceID)
	reply.LastID = s.notifyHub.LastID()
	return nil
}

// GetConfigDiff returns the config diff
func (s *Server) GetConfigDiff(args *Empty, reply *GetConfigDiffReply) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.hclConfig == nil {
		return fmt.Errorf("hcl config manager not initialized")
	}

	reply.Diff = s.hclConfig.Diff()
	return nil
}

// GetStatus returns the current system status
func (s *Server) GetStatus(args *Empty, reply *GetStatusReply) error {
	reply.Status = s.systemManager.GetStatus()
	return nil
}

// GetConfig returns the current configuration
func (s *Server) GetConfig(args *Empty, reply *GetConfigReply) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.config == nil {
		return fmt.Errorf("no configuration loaded")
	}
	reply.Config = *s.config
	return nil
}

// GetInterfaces returns the status of all configured interfaces
func (s *Server) GetInterfaces(args *Empty, reply *GetInterfacesReply) error {
	interfaces, err := s.networkManager.GetInterfaces()
	if err != nil {
		return err
	}
	reply.Interfaces = interfaces
	return nil
}

// Upgrade initiates a hot binary upgrade
func (s *Server) Upgrade(args *UpgradeArgs, reply *UpgradeReply) error {
	if s.upgradeMgr == nil {
		reply.Error = "upgrade manager not initialized"
		return nil
	}

	// Security: Hardcoded upgrade path to prevent RCE
	// The new binary must be placed at this location by the update mechanism
	upgradeBinaryPath := filepath.Join("/usr/sbin", brand.BinaryName+"_new")

	// Verify binary checksum
	if err := verifyUpgradeBinary(upgradeBinaryPath, args.Checksum); err != nil {
		log.Printf("[CTL] Security Alert: Upgrade verification failed: %v", err)
		auditLog("Upgrade", fmt.Sprintf("binary=%s status=verification_failed error=%q", upgradeBinaryPath, err.Error()))
		reply.Error = err.Error()
		return nil
	}

	log.Printf("[CTL] Initiating upgrade to %s (checksum verified)", upgradeBinaryPath)
	auditLog("Upgrade", fmt.Sprintf("binary=%s checksum=%s", upgradeBinaryPath, args.Checksum))

	// Disarm monitors (watchdog, API auto-restart) in the main process
	// This prevents the old process from fighting with the new process (e.g. restoring PID file, restarting API)
	if s.disarmFunc != nil {
		log.Printf("[CTL] Disarming monitors for upgrade...")
		s.disarmFunc()
	}

	// Context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// State collection: Simplified approach - new process loads from disk.
	// - DHCP leases: persisted in state.db, new process loads on startup
	// - DNS cache: ephemeral, rebuilds quickly from upstream queries
	// - Conntrack: kernel maintains across userspace restarts
	// The upgrade system still handles FD handoff for zero-downtime.
	s.upgradeMgr.SetStateCollectors(nil, nil, nil)

	if err := s.upgradeMgr.InitiateUpgrade(ctx, upgradeBinaryPath, s.config, s.configFile); err != nil {
		log.Printf("[CTL] Upgrade failed: %v", err)
		reply.Success = false
		reply.Error = err.Error()
		return nil
	}

	// Release Netlink resources (NFLOG) immediately so the new process can bind them.
	// The new process will start its own server shortly after receiving the RPC response.
	// If we don't release them now, the new process might fail to bind with "operation not permitted".
	if s.nflogReader != nil {
		log.Printf("[CTL] Releasing NFLOG reader for upgrade...")
		s.nflogReader.Stop()
	}
	if s.sniReader != nil {
		log.Printf("[CTL] Releasing SNI reader for upgrade...")
		s.sniReader.Stop()
	}
	if s.nfqueueReader != nil {
		log.Printf("[CTL] Releasing NFQUEUE reader for upgrade...")
		s.nfqueueReader.Stop()
	}

	log.Printf("[CTL] Upgrade negotiation complete. Scheduled exit.")
	reply.Success = true

	// Exit after a brief delay to allow RPC reply to flush
	// Exit gracefully by sending SIGTERM to self
	// This ensures RunCtl's context is canceled and child processes (API) are cleaned up
	// Exit after a brief delay to allow RPC reply to flush
	// Exit gracefully by sending SIGTERM to self
	// This ensures RunCtl's context is canceled and child processes (API) are cleaned up
	go func() {
		// Extended delay (5s) to allow new process to stabilize and detach fully before we exit
		// This helps prevents supervisor/cgroup cleanup race conditions
		time.Sleep(5 * time.Second)
		p, err := os.FindProcess(os.Getpid())
		if err == nil {
			p.Signal(syscall.SIGTERM)
		} else {
			// Fallback if we can't signal ourselves (shouldn't happen)
			log.Printf("[CTL] Failed to find own process for signal: %v. Forcing exit.", err)
			os.Exit(0)
		}
	}()

	return nil
}

// StageBinary receives binary data from the API server and stages it for upgrade.
// This is needed because the API server runs in a chroot and can't write to /usr/sbin.
func (s *Server) StageBinary(args *StageBinaryArgs, reply *StageBinaryReply) error {
	log.Printf("[CTL] Receiving binary for staging (%d bytes, arch: %s)", len(args.Data), args.Arch)

	// Verify architecture matches this system
	localArch := "linux/" + runtime.GOARCH
	if args.Arch != localArch {
		reply.Error = fmt.Sprintf("architecture mismatch: binary is %s but this system is %s", args.Arch, localArch)
		return nil
	}

	// Verify checksum
	hasher := sha256.New()
	hasher.Write(args.Data)
	actualChecksum := hex.EncodeToString(hasher.Sum(nil))

	if args.Checksum != actualChecksum {
		reply.Error = fmt.Sprintf("checksum mismatch: expected %s, got %s", args.Checksum, actualChecksum)
		return nil
	}

	// Stage to canonical path
	stagingPath := filepath.Join("/usr/sbin", brand.BinaryName+"_new")

	if err := os.WriteFile(stagingPath, args.Data, 0755); err != nil {
		reply.Error = fmt.Sprintf("failed to write binary: %v", err)
		return nil
	}

	log.Printf("[CTL] Staged binary at %s (checksum: %s)", stagingPath, actualChecksum[:16]+"...")
	auditLog("StageBinary", fmt.Sprintf("path=%s size=%d checksum=%s arch=%s", stagingPath, len(args.Data), actualChecksum[:16], args.Arch))

	reply.Success = true
	reply.Path = stagingPath
	return nil
}

// GetServices returns the status of all services
func (s *Server) GetServices(args *Empty, reply *GetServicesReply) error {
	reply.Services = s.serviceOrchestrator.GetServicesStatus()
	return nil
}

func (s *Server) GetDHCPLeases(args *Empty, reply *GetDHCPLeasesReply) error {
	reply.Leases = []DHCPLease{}
	if s.dhcpService == nil {
		return nil
	}

	leases := s.dhcpService.GetLeases()
	for _, lease := range leases {
		reply.Leases = append(reply.Leases, DHCPLease{
			// Interface:  "", // Not stored in lease currently, could infer from subnet?
			IPAddress: lease.IP.String(),
			MAC:       lease.MAC,
			// SubnetMask: "",
			// Router:     "",
			// DNSServers: nil,
			// LeaseTime:  "",
			// ObtainedAt: lease.LeaseStart, // Need to add LeaseStart to Lease struct?
			ExpiresAt: lease.Expiration,
			Hostname:  lease.Hostname,
		})
	}
	return nil

	/* Legacy Logic Removed
	leases := s.netLib.GetDHCPLeases()
	if leases == nil {
		return nil
	}

	for ifaceName, lease := range leases {
		var dnsServers []string
		for _, dns := range lease.DNSServers {
			dnsServers = append(dnsServers, dns.String())
		}

		reply.Leases = append(reply.Leases, DHCPLease{
			Interface:  ifaceName,
			IPAddress:  lease.IPAddress.String(),
			MAC:        lease.MAC,
			SubnetMask: net.IP(lease.SubnetMask).String(),
			Router:     lease.Router.String(),
			DNSServers: dnsServers,
			LeaseTime:  lease.LeaseTime.String(),
			ObtainedAt: lease.ObtainedAt,
			ExpiresAt:  lease.ObtainedAt.Add(lease.LeaseTime),
			Hostname:   lease.Hostname,
		})
	}
	*/

	// Enrich with Device Info
	// Use index to modify slice inplace
	if s.deviceManager != nil {
		for i := range reply.Leases {
			mac := reply.Leases[i].MAC
			if mac != "" {
				info := s.deviceManager.GetDevice(mac)
				reply.Leases[i].Vendor = info.Vendor
				if info.Device != nil {
					reply.Leases[i].Alias = info.Device.Alias
					reply.Leases[i].Tags = info.Device.Tags
					reply.Leases[i].DeviceID = info.Device.ID
					reply.Leases[i].Owner = info.Device.Owner
					reply.Leases[i].Type = info.Device.Type
				}
			}
		}
	}
	return nil
}

// GetTopology returns discovered LLDP neighbors and full network graph
func (s *Server) GetTopology(args *Empty, reply *GetTopologyReply) error {
	// 1. LLDP Neighbors (Backward Compatibility & Data Source)
	if s.lldpService != nil {
		neighbors := s.lldpService.GetNeighbors()
		reply.Neighbors = make([]TopologyNeighbor, len(neighbors))
		now := time.Now()
		for i, n := range neighbors {
			if n.Info == nil {
				reply.Neighbors[i] = TopologyNeighbor{
					Interface:       n.Interface,
					SystemName:      "Unknown",
					SystemDesc:      "Invalid LLDP Data",
					LastSeenSeconds: int(now.Sub(n.LastSeen).Seconds()),
				}
				continue
			}
			reply.Neighbors[i] = TopologyNeighbor{
				Interface:       n.Interface,
				ChassisID:       n.Info.ChassisID,
				PortID:          n.Info.PortID,
				SystemName:      n.Info.SystemName,
				SystemDesc:      n.Info.SystemDesc,
				LastSeenSeconds: int(now.Sub(n.LastSeen).Seconds()),
			}

			if s.deviceManager != nil && n.Info.ChassisID != "" {
				info := s.deviceManager.GetDevice(n.Info.ChassisID)
				reply.Neighbors[i].Vendor = info.Vendor
				if info.Device != nil {
					reply.Neighbors[i].Alias = info.Device.Alias
				}
			}
		}
	} else {
		reply.Neighbors = []TopologyNeighbor{}
	}

	// 2. Build Topology Graph
	graph := TopologyGraph{
		Nodes: []TopologyNode{},
		Links: []TopologyLink{},
	}

	// Helper to track unique nodes to avoid duplicates
	addedNodes := make(map[string]bool)

	// Root Router Node
	routerID := "router-0"
	routerIP := "Unknown IP"
	// Try to find a primary IP
	if s.config != nil && len(s.config.Interfaces) > 0 && len(s.config.Interfaces[0].IPv4) > 0 {
		routerIP = s.config.Interfaces[0].IPv4[0]
	}

	graph.Nodes = append(graph.Nodes, TopologyNode{
		ID:          routerID,
		Label:       brand.Name,
		Type:        "router",
		Group:       1,
		IP:          routerIP,
		Icon:        "router",
		Description: "Gateway",
	})
	addedNodes[routerID] = true

	// Interfaces (Inferred Switches)
	if s.config != nil {
		for _, iface := range s.config.Interfaces {
			// Skip loopback and VPN interfaces
			if iface.Name == "lo" || strings.HasPrefix(iface.Name, "wg") {
				continue
			}
			nodeID := "sw-" + iface.Name
			if !addedNodes[nodeID] {
				ip := ""
				if len(iface.IPv4) > 0 {
					ip = iface.IPv4[0]
				}
				graph.Nodes = append(graph.Nodes, TopologyNode{
					ID:          nodeID,
					Label:       iface.Name,
					Type:        "switch",
					Group:       2,
					IP:          ip,
					Icon:        "settings_ethernet",
					Description: iface.Description,
				})
				graph.Links = append(graph.Links, TopologyLink{Source: routerID, Target: nodeID})
				addedNodes[nodeID] = true
			}
		}
	}

	// Devices (from Collector)
	if s.deviceCollector != nil {
		devices := s.deviceCollector.GetDevices()
		for _, dev := range devices {
			if dev.Interface == "" || dev.Interface == "lo" {
				continue
			}

			// Ensure interface node exists (even if not in config, e.g. unconfigured bridged port)
			swID := "sw-" + dev.Interface
			if !addedNodes[swID] {
				graph.Nodes = append(graph.Nodes, TopologyNode{
					ID:    swID,
					Label: dev.Interface,
					Type:  "switch",
					Group: 2,
					Icon:  "settings_ethernet",
				})
				graph.Links = append(graph.Links, TopologyLink{Source: routerID, Target: swID})
				addedNodes[swID] = true
			}

			// Create Device Node
			nodeID := "dev-" + dev.MAC
			label := dev.Alias
			if label == "" {
				label = dev.Hostname
			}
			if label == "" {
				label = dev.Vendor
			}
			if label == "" {
				label = dev.MAC
			}

			devType := "device"
			// Improve type inference based on Collector data
			if dev.DeviceType != "" {
				devType = dev.DeviceType
			} else if dev.IsGateway {
				devType = "cloud" // Visual distinction for gateways
			}

			ip := ""
			if len(dev.IPs) > 0 {
				ip = dev.IPs[0]
			}

			if !addedNodes[nodeID] {
				graph.Nodes = append(graph.Nodes, TopologyNode{
					ID:          nodeID,
					Label:       label,
					Type:        devType,
					Group:       3,
					IP:          ip,
					Icon:        devType,         // Frontend will map this to detailed icon
					Description: dev.DeviceModel, // e.g. "Chromecast Ultra"
				})
				// Link device to its interface switch
				graph.Links = append(graph.Links, TopologyLink{Source: swID, Target: nodeID})
				addedNodes[nodeID] = true
			}
		}
	}

	// Sort graph nodes: Group 1 (Router), then Group 2 (Switches), then Group 3 (Devices sorted by Label)
	sort.Slice(graph.Nodes, func(i, j int) bool {
		n1, n2 := graph.Nodes[i], graph.Nodes[j]
		if n1.Group != n2.Group {
			return n1.Group < n2.Group
		}
		return strings.ToLower(n1.Label) < strings.ToLower(n2.Label)
	})

	reply.Graph = graph
	return nil
}

// GetNetworkDevices returns all discovered devices on the network
func (s *Server) GetNetworkDevices(args *Empty, reply *GetNetworkDevicesReply) error {
	if s.deviceCollector == nil {
		// Return empty list if collector not initialized
		reply.Devices = []NetworkDevice{}
		return nil
	}

	seenDevices := s.deviceCollector.GetDevices()
	reply.Devices = make([]NetworkDevice, len(seenDevices))

	for i, dev := range seenDevices {
		reply.Devices[i] = NetworkDevice{
			MAC:             dev.MAC,
			IPs:             dev.IPs,
			Interface:       dev.Interface,
			FirstSeen:       dev.FirstSeen.Unix(),
			LastSeen:        dev.LastSeen.Unix(),
			Hostname:        dev.Hostname,
			Vendor:          dev.Vendor,
			Alias:           dev.Alias,
			HopCount:        dev.HopCount,
			Flags:           dev.Flags,
			PacketCount:     dev.PacketCount,
			MDNSServices:    dev.MDNSServices,
			MDNSHostname:    dev.MDNSHostname,
			MDNSTXTRecords:  dev.MDNSTXTRecords,
			DHCPFingerprint: dev.DHCPFingerprint,
			DHCPVendorClass: dev.DHCPVendorClass,
			DHCPHostname:    dev.DHCPHostname,
			DHCPClientID:    dev.DHCPClientID,
			DHCPOptions:     dev.DHCPOptions,
			DeviceType:      dev.DeviceType,
			DeviceModel:     dev.DeviceModel,
		}
	}

	// Sort devices for consistent UI
	sort.Slice(reply.Devices, func(i, j int) bool {
		d1, d2 := reply.Devices[i], reply.Devices[j]
		n1 := d1.Alias
		if n1 == "" {
			n1 = d1.Hostname
		}
		if n1 == "" {
			n1 = d1.MAC
		}
		n2 := d2.Alias
		if n2 == "" {
			n2 = d2.Hostname
		}
		if n2 == "" {
			n2 = d2.MAC
		}
		return strings.ToLower(n1) < strings.ToLower(n2)
	})

	return nil
}

// --- Device Identity Management ---

// UpdateDeviceIdentity updates a device identity
func (s *Server) UpdateDeviceIdentity(args *UpdateDeviceIdentityArgs, reply *UpdateDeviceIdentityReply) error {
	if s.deviceManager == nil {
		reply.Error = "device manager not initialized"
		return nil
	}

	id, err := s.deviceManager.UpdateIdentity(args.ID, args.Alias, args.Owner, args.Type, args.Tags)
	if err != nil {
		if args.ID == "" {
			// Create new identity if ID is missing (requires Alias essentially, or we default it)
			// But arguments allow Alias to be nil.
			// If Alias is nil, we can't create with meaningful name.
			if args.Alias == nil {
				reply.Error = "Alias required to create new identity"
				return nil
			}

			// Create with basic info
			owner := ""
			if args.Owner != nil {
				owner = *args.Owner
			}
			dtype := ""
			if args.Type != nil {
				dtype = *args.Type
			}

			id, err = s.deviceManager.CreateIdentity(*args.Alias, owner, dtype)
			if err != nil {
				reply.Error = err.Error()
				return nil
			}

			// Apply tags if present (CreateIdentity doesn't handle tags yet)
			if len(args.Tags) > 0 {
				if _, err := s.deviceManager.UpdateIdentity(id.ID, nil, nil, nil, args.Tags); err != nil {
					log.Printf("[CTL] Warning: failed to set tags for new identity %s: %v", id.ID, err)
				} else {
					// Use updated object
					updated, _ := s.deviceManager.UpdateIdentity(id.ID, nil, nil, nil, nil) // Get latest
					if updated != nil {
						id = updated
					}
				}
			}
		} else {
			reply.Error = err.Error()
			return nil
		}
	}
	reply.Identity = id
	return nil
}

// LinkMAC links a MAC address to a device identity
func (s *Server) LinkMAC(args *LinkMACArgs, reply *Empty) error {
	if s.deviceManager == nil {
		return fmt.Errorf("device manager not initialized")
	}
	return s.deviceManager.LinkMAC(args.MAC, args.IdentityID)
}

// UnlinkMAC removes a MAC address link
func (s *Server) UnlinkMAC(args *UnlinkMACArgs, reply *Empty) error {
	if s.deviceManager == nil {
		return fmt.Errorf("device manager not initialized")
	}
	return s.deviceManager.UnlinkMAC(args.MAC)
}

// ApplyConfig applies a new configuration (RPC endpoint)
func (s *Server) ApplyConfig(args *ApplyConfigArgs, reply *Empty) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Calculate config hash for audit log
	h := sha256.New()
	if s.hclConfig != nil {
		h.Write([]byte(s.hclConfig.GetRawHCL()))
	} else {
		// Fallback to hashing the struct representation
		h.Write([]byte(fmt.Sprintf("%v", args.Config)))
	}
	hash := hex.EncodeToString(h.Sum(nil))

	log.Printf("[CTL] Applying new configuration via RPC...")
	if args.Config.Features != nil {
		log.Printf("[CTL] DEBUG RPC Features: QoS=%v Threat=%v", args.Config.Features.QoS, args.Config.Features.ThreatIntel)
	} else {
		log.Printf("[CTL] DEBUG RPC Features is nil")
	}
	auditLog("ApplyConfig", fmt.Sprintf("hash=%s count_ifaces=%d", hash[:8], len(args.Config.Interfaces)))

	return s.reloadConfigInternal(&args.Config)
}

// ReloadConfig reloads the configuration from the given struct (Internal/Signal use)
func (s *Server) ReloadConfig(newCfg *config.Config) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	log.Printf("[CTL] Reloading configuration internally...")
	return s.reloadConfigInternal(newCfg)
}

// reloadConfigInternal contains the core logic for applying a config.
// Caller must hold the mutex.
func (s *Server) reloadConfigInternal(newCfg *config.Config) error {
	// Track critical errors (subsystems that must succeed for a valid state)
	var criticalErrors []string

	// 1. Update config reference
	s.config = newCfg
	if s.hclConfig != nil {
		s.hclConfig.Config = newCfg
	}
	if s.networkManager != nil {
		s.networkManager.UpdateConfig(newCfg)
	}

	// 2. Apply Network Settings
	// Auto-enable IP forwarding if API sandbox is active.
	// The sandbox architecture requires forwarding to route traffic to 169.254.255.2.
	// This prevents new users from being locked out of the Web UI.
	ipForwarding := newCfg.IPForwarding
	if newCfg.API != nil && newCfg.API.Enabled && !newCfg.API.DisableSandbox {
		if !ipForwarding {
			log.Printf("[CTL] Auto-enabling IP forwarding (required for API sandbox)")
		}
		ipForwarding = true
	}
	if err := s.netLib.SetIPForwarding(ipForwarding); err != nil {
		log.Printf("[CTL] Error setting IP forwarding: %v", err)
		// Non-critical: log but continue
	}
	s.netLib.SetupLoopback()

	// Re-apply interfaces
	for _, iface := range newCfg.Interfaces {
		if err := s.netLib.ApplyInterface(iface); err != nil {
			log.Printf("[CTL] Error applying interface %s: %v", iface.Name, err)
			// Interface errors are non-critical for config apply (may be transient)
		}
	}

	// Apply Policy Routing (Tables & Rules) - CRITICAL
	if err := s.policyRouting.Reload(newCfg.RoutingTables, newCfg.PolicyRoutes); err != nil {
		log.Printf("[CTL] Error applying policy routing: %v", err)
		s.Notify(NotifyWarning, "Policy Routing Error", fmt.Sprintf("Failed to apply: %v", err))
		criticalErrors = append(criticalErrors, fmt.Sprintf("policy routing: %v", err))
	}

	// Apply Multi-WAN Policy Rules (if enabled)
	if newCfg.MultiWAN != nil && newCfg.MultiWAN.Enabled {
		var wanConfigs []network.WANConfig
		for _, link := range newCfg.MultiWAN.Connections {
			if !link.Enabled {
				continue
			}
			wanConfigs = append(wanConfigs, network.WANConfig{
				Name:      link.Name,
				Interface: link.Interface,
				Gateway:   link.Gateway,
				Weight:    link.Weight,
				Priority:  link.Priority,
				Enabled:   true,
			})
		}
		if len(wanConfigs) > 0 {
			if err := s.policyRouting.SetupMultiWAN(wanConfigs); err != nil {
				log.Printf("[CTL] Error applying Multi-WAN routing: %v", err)
				s.Notify(NotifyWarning, "Multi-WAN Error", fmt.Sprintf("Failed to apply: %v", err))
				criticalErrors = append(criticalErrors, fmt.Sprintf("multi-wan: %v", err))
			} else {
				log.Printf("[CTL] Applied Multi-WAN routing for %d uplinks", len(wanConfigs))
			}
		}
	}

	// Apply Uplink Groups (Multi-WAN & Health Checking)
	uplinkGroups := newCfg.UplinkGroups

	// Auto-generate UplinkGroup from MultiWAN config if enabled (Backward Consistency / Simplified Config)
	if newCfg.MultiWAN != nil && newCfg.MultiWAN.Enabled {
		// Log that we are auto-generating the group
		log.Printf("[CTL] Generating 'multi_wan' uplink group from simplified MultiWAN config")

		// Create a default group
		defaultGroup := config.UplinkGroup{
			Name:           "multi_wan",
			Enabled:        true,
			FailoverMode:   "graceful", // Default behavior
			SourceNetworks: []string{"0.0.0.0/0"},
			HealthCheck:    newCfg.MultiWAN.HealthCheck,
		}

		// Map Mode
		if newCfg.MultiWAN.Mode == "loadbalance" {
			defaultGroup.LoadBalanceMode = "weighted"
		} else {
			defaultGroup.FailoverMode = "graceful"
		}

		for _, conn := range newCfg.MultiWAN.Connections {
			// Skip disabled connections
			if !conn.Enabled {
				continue
			}
			defaultGroup.Uplinks = append(defaultGroup.Uplinks, config.UplinkDef{
				Name:      conn.Name,
				Interface: conn.Interface,
				Gateway:   conn.Gateway,
				Weight:    conn.Weight,
				Tier:      conn.Priority, // Map priority to tier (0 is highest priority)
				Enabled:   conn.Enabled,
				Type:      "wan",
			})
		}
		uplinkGroups = append(uplinkGroups, defaultGroup)
	}

	if err := s.uplinkManager.Reload(uplinkGroups); err != nil {
		log.Printf("[CTL] Error reloading uplink manager: %v", err)
		s.Notify(NotifyWarning, "Uplink Config Error", fmt.Sprintf("Failed to apply: %v", err))
		// Uplink is non-critical for basic operation
	} else {
		// Set notification callback
		s.uplinkManager.SetHealthCallback(func(uplink *network.Uplink, healthy bool) {
			status := "UP"
			if !healthy {
				status = "DOWN"
			}
			s.Notify(NotifyInfo, "Uplink Status Change", fmt.Sprintf("Uplink %s is now %s", uplink.Name, status))
		})

		// Determine health check parameters
		interval := 5 * time.Second
		targets := []string{"8.8.8.8", "1.1.1.1"}

		if newCfg.MultiWAN != nil && newCfg.MultiWAN.HealthCheck != nil {
			if newCfg.MultiWAN.HealthCheck.Interval > 0 {
				interval = time.Duration(newCfg.MultiWAN.HealthCheck.Interval) * time.Second
			}
			if len(newCfg.MultiWAN.HealthCheck.Targets) > 0 {
				targets = newCfg.MultiWAN.HealthCheck.Targets
			}
		}

		// Start health checking
		s.uplinkManager.StartHealthChecking(interval, targets)
	}

	// 3. Apply Config to all services - CRITICAL (includes firewall)
	result := s.serviceOrchestrator.ReloadAll(newCfg)
	if !result.Success {
		for svc, errMsg := range result.FailedServices {
			log.Printf("[CTL] Service %s reload failed: %s", svc, errMsg)
			// Firewall failure is critical
			if svc == "firewall" || svc == "Firewall" {
				criticalErrors = append(criticalErrors, fmt.Sprintf("firewall: %s", errMsg))
			}
		}
		// Notify about partial failure
		s.Notify(NotifyWarning, "Configuration Applied", "Some services failed to reload")
	} else {
		// Notify success
		s.Notify(NotifySuccess, "Configuration Applied", "Firewall rules have been updated")
	}

	// 4. Sync Scheduled Rules (non-critical)
	if err := s.syncScheduledRules(newCfg); err != nil {
		log.Printf("[CTL] Warning: Failed to sync scheduled rules: %v", err)
		s.Notify(NotifyWarning, "Scheduler Error", fmt.Sprintf("Failed to sync rules: %v", err))
	}

	// 5. Sync IPSet Updates (non-critical)
	if err := s.syncIPSetTasks(newCfg); err != nil {
		log.Printf("[CTL] Warning: Failed to sync ipset tasks: %v", err)
	}

	// 6. Sync UID Routes (non-critical)
	if err := s.netLib.ApplyUIDRoutes(newCfg.UIDRouting); err != nil {
		log.Printf("[CTL] Warning: Failed to apply UID routes: %v", err)
	}

	// Return aggregated critical errors
	if len(criticalErrors) > 0 {
		log.Printf("[CTL] Configuration applied with critical errors: %v", criticalErrors)
		return fmt.Errorf("critical failures: %s", strings.Join(criticalErrors, "; "))
	}

	log.Printf("[CTL] Configuration applied successfully")
	return nil
}

// syncScheduledRules updates the scheduler with rules from config
func (s *Server) syncScheduledRules(cfg *config.Config) error {
	if s.scheduler == nil {
		return nil
	}
	if s.firewallManager == nil {
		log.Printf("[CTL] Skipping scheduled rules sync - firewall manager not initialized")
		return nil
	}

	// 1. Identify current rule tasks (prefix "rule_")
	currentTasks := s.scheduler.GetStatus()
	for _, task := range currentTasks {
		if strings.HasPrefix(task.ID, "rule_") {
			// Remove existing rule tasks found.
			// Future optimization: only remove changed tasks.
			// Current approach (wipe & recreate) ensures state consistency and is
			// acceptable since scheduled rules are typically few in number.
			s.scheduler.RemoveTask(task.ID)
		}
	}

	// 2. Add new tasks
	for _, rule := range cfg.ScheduledRules {
		if !rule.Enabled {
			continue
		}

		// Start Task
		startSchedule, err := scheduler.Cron(rule.Schedule)
		if err != nil {
			log.Printf("[CTL] Invalid schedule for rule %s: %v", rule.Name, err)
			continue
		}

		startTask := &scheduler.Task{
			ID:          fmt.Sprintf("rule_%s_start", rule.Name),
			Name:        fmt.Sprintf("Enable Rule: %s", rule.Name),
			Description: fmt.Sprintf("Enables firewall rule %s", rule.Name),
			Schedule:    startSchedule,
			Enabled:     true,
			Func: func(ctx context.Context) error {
				// We pass the rule object by value closure? Yes.
				// ApplyScheduledRule with enabled=true
				return s.firewallManager.ApplyScheduledRule(rule, true)
			},
		}
		if err := s.scheduler.AddTask(startTask); err != nil {
			log.Printf("[CTL] Failed to add start task for rule %s: %v", rule.Name, err)
		}

		// End Task (if present)
		if rule.EndSchedule != "" {
			endSchedule, err := scheduler.Cron(rule.EndSchedule)
			if err != nil {
				log.Printf("[CTL] Invalid end schedule for rule %s: %v", rule.Name, err)
				continue
			}

			endTask := &scheduler.Task{
				ID:          fmt.Sprintf("rule_%s_end", rule.Name),
				Name:        fmt.Sprintf("Disable Rule: %s", rule.Name),
				Description: fmt.Sprintf("Disables firewall rule %s", rule.Name),
				Schedule:    endSchedule,
				Enabled:     true,
				Func: func(ctx context.Context) error {
					return s.firewallManager.ApplyScheduledRule(rule, false)
				},
			}
			if err := s.scheduler.AddTask(endTask); err != nil {
				log.Printf("[CTL] Failed to add end task for rule %s: %v", rule.Name, err)
			}
		}
	}
	return nil
}

// RestartService restarts a specific service
func (s *Server) RestartService(args *RestartServiceArgs, reply *Empty) error {
	return s.serviceOrchestrator.RestartService(args.ServiceName)
}

// Reboot reboots the system
func (s *Server) Reboot(args *Empty, reply *Empty) error {
	return s.systemManager.Reboot()
}

// UpdateConfig updates the server's config reference (called by daemon)
func (s *Server) UpdateConfig(cfg *config.Config) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config = cfg
}

// --- Interface Management Methods ---

// GetAvailableInterfaces returns all physical interfaces available for configuration
func (s *Server) GetAvailableInterfaces(args *Empty, reply *GetAvailableInterfacesReply) error {
	// Uses internal ctlplane manager which uses direct netlink
	interfaces, err := s.networkManager.GetAvailableInterfaces()
	if err != nil {
		return err
	}
	reply.Interfaces = interfaces
	return nil
}

// UpdateInterface updates an interface's configuration
func (s *Server) UpdateInterface(args *UpdateInterfaceArgs, reply *UpdateInterfaceReply) error {
	if err := s.networkManager.UpdateInterface(args); err != nil {
		reply.Error = err.Error()
	} else {
		reply.Success = true
	}
	// We need to sync back the config change Server side?
	// The NetworkManager writes directly to the shared config pointer.
	// So s.config is already updated if NetworkManager holds *config.Config.
	// Yes, NewNetworkManager(cfg) passes the pointer.
	return nil
}

// CreateVLAN creates a VLAN interface
func (s *Server) CreateVLAN(args *CreateVLANArgs, reply *CreateVLANReply) error {
	name, err := s.networkManager.CreateVLAN(args)
	if err != nil {
		reply.Error = err.Error()
	} else {
		reply.Success = true
		reply.InterfaceName = name
	}
	return nil
}

// DeleteVLAN deletes a VLAN interface
func (s *Server) DeleteVLAN(args *DeleteVLANArgs, reply *UpdateInterfaceReply) error {
	if err := s.networkManager.DeleteVLAN(args.InterfaceName); err != nil {
		reply.Error = err.Error()
	} else {
		reply.Success = true
	}
	return nil
}

// CreateBond creates a bonded interface
func (s *Server) CreateBond(args *CreateBondArgs, reply *CreateBondReply) error {
	if err := s.networkManager.CreateBond(args); err != nil {
		reply.Error = err.Error()
	} else {
		reply.Success = true
	}
	return nil
}

// DeleteBond deletes a bonded interface
func (s *Server) DeleteBond(args *DeleteBondArgs, reply *UpdateInterfaceReply) error {
	if err := s.networkManager.DeleteBond(args); err != nil {
		reply.Error = err.Error()
	} else {
		reply.Success = true
	}
	return nil
}

// SafeApplyInterface applies interface config with rollback protection
func (s *Server) SafeApplyInterface(args *SafeApplyInterfaceArgs, reply *firewall.ApplyResult) error {
	// Construct SafeApplyConfig from args
	safeCfg := &firewall.SafeApplyConfig{
		PingTargets:         args.PingTargets,
		PingTimeout:         time.Duration(args.PingTimeoutSeconds) * time.Second,
		RollbackDelay:       time.Duration(args.RollbackDelaySeconds) * time.Second,
		RequireConfirmation: args.RequireConfirmation,
	}
	if safeCfg.PingTimeout == 0 {
		safeCfg.PingTimeout = 5 * time.Second
	}
	if safeCfg.RollbackDelay == 0 {
		safeCfg.RollbackDelay = 30 * time.Second
	}

	result, err := s.networkSafeApply.ApplyInterfaceConfig(args.UpdateArgs, args.ClientIP, safeCfg)
	if err != nil {
		// If error occurs, we still return it but reply might be partial?
		// RPC error handling: if we return error, reply content is ignored usually?
		return err
	}

	*reply = *result
	return nil
}

// ConfirmApplyInterface confirms a pending interface apply
func (s *Server) ConfirmApplyInterface(args *ConfirmApplyArgs, reply *Empty) error {
	return s.networkSafeApply.ConfirmApply(args.PendingID)
}

// CancelApplyInterface cancels a pending interface apply
func (s *Server) CancelApplyInterface(args *CancelApplyArgs, reply *Empty) error {
	return s.networkSafeApply.CancelApply(args.ApplyID)
}

// --- HCL Editing Methods (Advanced Mode) ---

// GetRawHCL returns the entire config file as raw HCL
func (s *Server) GetRawHCL(args *Empty, reply *GetRawHCLReply) error {
	if err := s.ensureHCLConfig(); err != nil {
		return err
	}

	reply.HCL = s.hclConfig.GetRawHCL()
	reply.Path = s.configFile
	reply.Sections = s.hclConfig.ListSections()

	if info, err := os.Stat(s.configFile); err == nil {
		reply.LastModified = info.ModTime().Format(time.RFC3339)
	}

	return nil
}

// GetSectionHCL returns a specific section as raw HCL
func (s *Server) GetSectionHCL(args *GetSectionHCLArgs, reply *GetSectionHCLReply) error {
	if err := s.ensureHCLConfig(); err != nil {
		reply.Error = err.Error()
		return nil
	}

	var hcl string
	var err error

	if len(args.Labels) > 0 {
		hcl, err = s.hclConfig.GetSectionByLabel(args.SectionType, args.Labels)
	} else {
		hcl, err = s.hclConfig.GetSection(args.SectionType)
	}

	if err != nil {
		reply.Error = err.Error()
		return nil
	}

	reply.HCL = hcl
	return nil
}

// SetRawHCL replaces the entire config with new HCL
func (s *Server) SetRawHCL(args *SetRawHCLArgs, reply *SetRawHCLReply) error {
	if err := s.ensureHCLConfig(); err != nil {
		reply.Error = err.Error()
		return nil
	}

	// Capture previous state for heavy services
	s.mu.RLock()
	oldDHCP := s.config.DHCP != nil && s.config.DHCP.Enabled
	oldDNS := s.config.DNSServer != nil && s.config.DNSServer.Enabled
	s.mu.RUnlock()

	if err := s.hclConfig.SetRawHCL(args.HCL); err != nil {
		reply.Error = err.Error()
		return nil
	}

	// Update the in-memory config
	s.mu.Lock()
	s.config = s.hclConfig.Config
	// Trigger full reload of system state (nftables, services, etc)
	if err := s.reloadConfigInternal(s.config); err != nil {
		s.mu.Unlock()
		reply.Error = fmt.Sprintf("failed to apply HCL config: %v", err)
		return nil
	}
	s.mu.Unlock()

	reply.Success = true

	// Check new state to see if heavy services were disabled
	s.mu.RLock()
	newDHCP := s.config.DHCP != nil && s.config.DHCP.Enabled
	newDNS := s.config.DNSServer != nil && s.config.DNSServer.Enabled
	s.mu.RUnlock()

	if (oldDHCP && !newDHCP) || (oldDNS && !newDNS) {
		reply.RestartHint = "Heavy services (DHCP/DNS) were disabled. Run 'firewall upgrade --self' to reclaim memory."
	}
	return nil
}

// SetSectionHCL replaces a specific section with new HCL
func (s *Server) SetSectionHCL(args *SetSectionHCLArgs, reply *SetSectionHCLReply) error {
	if err := s.ensureHCLConfig(); err != nil {
		reply.Error = err.Error()
		return nil
	}

	var err error
	if len(args.Labels) > 0 {
		err = s.hclConfig.SetSectionByLabel(args.SectionType, args.Labels, args.HCL)
	} else {
		err = s.hclConfig.SetSection(args.SectionType, args.HCL)
	}

	if err != nil {
		reply.Error = err.Error()
		return nil
	}

	// Update the in-memory config
	s.mu.Lock()
	s.config = s.hclConfig.Config
	s.mu.Unlock()
	reply.Success = true
	return nil
}

// DeleteSection removes a specific section from the configuration
func (s *Server) DeleteSection(args *DeleteSectionArgs, reply *DeleteSectionReply) error {
	if err := s.ensureHCLConfig(); err != nil {
		reply.Error = err.Error()
		return nil
	}

	err := s.hclConfig.RemoveSection(args.SectionType)
	if err != nil {
		reply.Error = err.Error()
		return nil
	}

	// Update the in-memory config
	s.mu.Lock()
	s.config = s.hclConfig.Config
	s.mu.Unlock()
	reply.Success = true
	return nil
}

// DeleteSectionByLabel removes a specific labeled section from the configuration
func (s *Server) DeleteSectionByLabel(args *DeleteSectionByLabelArgs, reply *DeleteSectionReply) error {
	if err := s.ensureHCLConfig(); err != nil {
		reply.Error = err.Error()
		return nil
	}

	err := s.hclConfig.RemoveSectionByLabel(args.SectionType, args.Labels)
	if err != nil {
		reply.Error = err.Error()
		return nil
	}

	// Update the in-memory config
	s.mu.Lock()
	s.config = s.hclConfig.Config
	s.mu.Unlock()
	reply.Success = true
	return nil
}

// ValidateHCL validates HCL without applying it
func (s *Server) ValidateHCL(args *ValidateHCLArgs, reply *ValidateHCLReply) error {
	diags, err := config.ParseHCLWithDiagnostics(args.HCL)
	reply.Diagnostics = diags

	if err != nil {
		reply.Valid = false
		reply.Error = err.Error()
	} else {
		reply.Valid = true
	}

	return nil
}

// TriggerTask manually triggers a scheduled task
func (s *Server) TriggerTask(args *TriggerTaskArgs, reply *TriggerTaskReply) error {
	if s.scheduler == nil {
		reply.Error = "scheduler is not initialized or enabled"
		return nil
	}

	err := s.scheduler.RunTask(args.TaskName)
	if err != nil {
		reply.Error = err.Error()
	} else {
		reply.Success = true
		reply.Message = fmt.Sprintf("Task %s triggered", args.TaskName)
	}
	return nil
}

// SaveConfig saves the current config to disk
func (s *Server) SaveConfig(args *Empty, reply *SaveConfigReply) error {
	if err := s.ensureHCLConfig(); err != nil {
		reply.Error = err.Error()
		return nil
	}

	// Sync the current in-memory config to the HCL AST
	// This ensures API changes to s.config are reflected in the HCL
	s.mu.RLock()
	s.hclConfig.Config = s.config
	s.mu.RUnlock()

	// Update HCL AST from config struct (preserves comments for unchanged sections)
	if err := s.hclConfig.SyncConfigToHCL(); err != nil {
		reply.Error = fmt.Sprintf("failed to sync config to HCL: %v", err)
		return nil
	}

	if err := s.hclConfig.Save(); err != nil {
		reply.Error = err.Error()
		return nil
	}

	reply.Success = true
	reply.BackupPath = s.configFile + ".bak"
	return nil
}

// ensureHCLConfig loads the HCL config file if not already loaded
func (s *Server) ensureHCLConfig() error {
	if s.hclConfig != nil {
		return nil
	}

	cf, err := config.LoadConfigFile(s.configFile)
	if err != nil {
		return fmt.Errorf("failed to load HCL config: %w", err)
	}

	s.hclConfig = cf
	return nil
}

// --- Backup Management Methods ---

// ListBackups returns all available config backups
func (s *Server) ListBackups(args *Empty, reply *ListBackupsReply) error {
	backups, err := s.backupManager.ListBackups()
	if err != nil {
		reply.Error = err.Error()
		return nil
	}

	reply.Backups = make([]BackupInfo, len(backups))
	for i, b := range backups {
		reply.Backups[i] = BackupInfo{
			Version:     b.Version,
			Timestamp:   b.Timestamp.Format(time.RFC3339),
			Description: b.Description,
			Size:        b.Size,
			IsAuto:      b.IsAuto,
			Pinned:      b.Pinned,
		}
	}
	reply.MaxBackups = s.backupManager.GetMaxBackups()

	return nil
}

// CreateBackup creates a new manual backup
func (s *Server) CreateBackup(args *CreateBackupArgs, reply *CreateBackupReply) error {
	var backup *config.BackupInfo
	var err error

	if args.Pinned {
		backup, err = s.backupManager.CreatePinnedBackup(args.Description)
	} else {
		backup, err = s.backupManager.CreateBackup(args.Description, false)
	}

	if err != nil {
		reply.Error = err.Error()
		return nil
	}

	reply.Success = true
	reply.Backup = BackupInfo{
		Version:     backup.Version,
		Timestamp:   backup.Timestamp.Format(time.RFC3339),
		Description: backup.Description,
		Size:        backup.Size,
		IsAuto:      backup.IsAuto,
		Pinned:      backup.Pinned,
	}

	return nil
}

// RestoreBackup restores a specific backup version
func (s *Server) RestoreBackup(args *RestoreBackupArgs, reply *RestoreBackupReply) error {
	if err := s.backupManager.RestoreBackup(args.Version); err != nil {
		reply.Error = err.Error()
		return nil
	}

	// Reload the config
	cf, err := config.LoadConfigFile(s.configFile)
	if err != nil {
		reply.Error = fmt.Sprintf("restored but failed to reload: %v", err)
		return nil
	}

	s.mu.Lock()
	s.config = cf.Config
	s.hclConfig = cf
	s.mu.Unlock()

	// CRITICAL FIX: "Restore Desync" - Apply the restored configuration
	if err := s.ApplyConfig(&ApplyConfigArgs{Config: *s.config}, &Empty{}); err != nil {
		reply.Error = fmt.Sprintf("restored but failed to apply: %v", err)
		return nil
	}

	reply.Success = true
	reply.Message = fmt.Sprintf("Restored backup version %d and applied configuration", args.Version)
	return nil
}

// GetBackupContent returns the content of a specific backup
func (s *Server) GetBackupContent(args *GetBackupContentArgs, reply *GetBackupContentReply) error {
	content, err := s.backupManager.GetBackupContent(args.Version)
	if err != nil {
		reply.Error = err.Error()
		return nil
	}

	reply.Content = string(content)
	return nil
}

// --- Learning Firewall RPCs ---

// GetLearningRules returns pending rules
func (s *Server) GetLearningRules(args *GetLearningRulesArgs, reply *GetLearningRulesReply) error {
	if s.learningService == nil {
		// Just return empty list if disabled
		reply.Rules = []*learning.PendingRule{}
		return nil
	}

	rules, err := s.learningService.GetPendingRules(args.Status)
	if err != nil {
		reply.Error = err.Error()
		return nil
	}
	reply.Rules = rules
	return nil
}

// GetLearningRule returns a specific rule
func (s *Server) GetLearningRule(args *GetLearningRuleArgs, reply *GetLearningRuleReply) error {
	if s.learningService == nil {
		reply.Error = "Learning service not enabled"
		return nil
	}

	rule, err := s.learningService.GetPendingRule(args.ID)
	if err != nil {
		reply.Error = err.Error()
		return nil
	}
	reply.Rule = rule
	return nil
}

// ApproveRule approves a pending rule
func (s *Server) ApproveRule(args *LearningRuleActionArgs, reply *LearningRuleActionReply) error {
	if s.learningService == nil {
		reply.Error = "Learning service not enabled"
		return nil
	}

	rule, err := s.learningService.ApproveRule(args.ID, args.User)
	if err != nil {
		reply.Error = err.Error()
		return nil
	}
	reply.Success = true
	reply.Rule = rule
	return nil
}

// DenyRule denies a pending rule
func (s *Server) DenyRule(args *LearningRuleActionArgs, reply *LearningRuleActionReply) error {
	if s.learningService == nil {
		reply.Error = "Learning service not enabled"
		return nil
	}

	rule, err := s.learningService.DenyRule(args.ID, args.User)
	if err != nil {
		reply.Error = err.Error()
		return nil
	}
	reply.Success = true
	reply.Rule = rule
	return nil
}

// IgnoreRule ignores a pending rule
func (s *Server) IgnoreRule(args *LearningRuleActionArgs, reply *LearningRuleActionReply) error {
	if s.learningService == nil {
		reply.Error = "Learning service not enabled"
		return nil
	}

	rule, err := s.learningService.IgnoreRule(args.ID)
	if err != nil {
		reply.Error = err.Error()
		return nil
	}
	reply.Success = true
	reply.Rule = rule
	return nil
}

// DeleteRule deletes a pending rule
func (s *Server) DeleteRule(args *LearningRuleActionArgs, reply *LearningRuleActionReply) error {
	if s.learningService == nil {
		reply.Error = "Learning service not enabled"
		return nil
	}

	err := s.learningService.DeleteRule(args.ID)
	if err != nil {
		reply.Error = err.Error()
		return nil
	}
	reply.Success = true
	return nil
}

// GetLearningStats returns learning statistics
func (s *Server) GetLearningStats(args *Empty, reply *GetLearningStatsReply) error {
	if s.learningService == nil {
		reply.Stats = map[string]interface{}{"enabled": false}
		return nil
	}

	stats, err := s.learningService.GetStats()
	if err != nil {
		reply.Error = err.Error()
		return nil
	}
	reply.Stats = stats
	return nil
}

// PinBackup sets or clears the pinned status of a backup
func (s *Server) PinBackup(args *PinBackupArgs, reply *PinBackupReply) error {
	var err error
	if args.Pinned {
		err = s.backupManager.PinBackup(args.Version)
	} else {
		err = s.backupManager.UnpinBackup(args.Version)
	}

	if err != nil {
		reply.Error = err.Error()
		return nil
	}

	reply.Success = true
	return nil
}

// SetMaxBackups updates the maximum number of auto-backups to retain
func (s *Server) SetMaxBackups(args *SetMaxBackupsArgs, reply *SetMaxBackupsReply) error {
	if args.MaxBackups < 1 {
		reply.Error = "max_backups must be at least 1"
		return nil
	}

	s.backupManager.SetMaxBackups(args.MaxBackups)
	reply.Success = true
	return nil
}

// Start starts the RPC server on the Unix socket
func (s *Server) Start() error {
	// Remove existing socket if present
	os.Remove(SocketPath)

	// Create Unix socket listener
	listener, err := net.Listen("unix", SocketPath)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", SocketPath, err)
	}

	// Set socket permissions to allow unprivileged API process (nobody)
	// We use 0666 because the process ownership might be root:root, and nobody is not in group root.
	// This allows any local user to connect, which is acceptable for this appliance appliance.
	if err := os.Chmod(SocketPath, 0666); err != nil {
		listener.Close()
		return fmt.Errorf("failed to set socket permissions: %w", err)
	}

	return s.StartWithListener(listener)
}

// StartWithListener starts the RPC server with an existing listener
func (s *Server) StartWithListener(listener net.Listener) error {
	// Store listener for upgrade handoff
	s.listener = listener

	// Register RPC service
	if err := rpc.Register(s); err != nil {
		// Ignore "service already defined" error if restarting/reusing
		if err.Error() != "rpc: service already defined: ctlplane.Server" {
			return fmt.Errorf("failed to register RPC service: %w", err)
		}
	}

	// Initialize and start scheduler
	if err := s.startScheduler(); err != nil {
		log.Printf("[CTL] Warning: failed to start scheduler: %v", err)
	}

	log.Printf("[CTL] Control plane listening on %s", listener.Addr())

	// Accept connections in background
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				// If the listener is closed, we exit
				if errors.Is(err, net.ErrClosed) {
					return
				}
				log.Printf("[CTL] Accept error: %v", err)
				return
			}
			go func() {
				defer func() {
					if r := recover(); r != nil {
						log.Printf("[CTL] CRITICAL: RPC connection handler panicked: %v", r)
					}
				}()
				rpc.ServeConn(conn)
			}()
		}
	}()

	return nil
}

// GetListener returns the control plane listener for upgrade handoff
func (s *Server) GetListener() net.Listener {
	return s.listener
}

// startScheduler initializes the task scheduler and registers scheduled tasks.
func (s *Server) startScheduler() error {
	// Only start scheduler if enabled in config
	s.mu.RLock()
	schedulerConfig := s.config.Scheduler
	s.mu.RUnlock()

	if schedulerConfig == nil || !schedulerConfig.Enabled {
		log.Printf("[CTL] Scheduler disabled in configuration")
		return nil
	}

	// Create task registry with function bindings
	registry := &scheduler.TaskRegistry{
		ConfigPath:  s.configFile,
		BackupDir:   s.getBackupDir(),
		GetConfig:   func() *config.Config { return s.config },
		ApplyConfig: s.applyConfig,
		RefreshIPSets: func() error {
			return s.refreshIPSets()
		},
		RefreshDNS: func() error {
			return s.refreshDNSBlocklists()
		},
	}

	// Register IPSet update task
	if s.config.Scheduler.IPSetRefreshHours > 0 {
		interval := time.Duration(s.config.Scheduler.IPSetRefreshHours) * time.Hour
		task := scheduler.NewIPSetUpdateTask(registry, interval)
		s.scheduler.AddTask(task)
		log.Printf("[CTL] Registered IPSet update task (interval: %v)", interval)
	}

	// Register DNS blocklist update task
	if s.config.Scheduler.DNSRefreshHours > 0 {
		interval := time.Duration(s.config.Scheduler.DNSRefreshHours) * time.Hour
		task := scheduler.NewDNSBlocklistUpdateTask(registry, interval)
		s.scheduler.AddTask(task)
		log.Printf("[CTL] Registered DNS blocklist update task (interval: %v)", interval)
	}

	// Register backup task if enabled
	if s.config.Scheduler.BackupEnabled {
		// Parse cron schedule or use default (2:00 AM daily)
		var schedule scheduler.Schedule
		if s.config.Scheduler.BackupSchedule != "" {
			cronSchedule, err := scheduler.Cron(s.config.Scheduler.BackupSchedule)
			if err != nil {
				log.Printf("[CTL] Invalid backup schedule '%s', using default: %v",
					s.config.Scheduler.BackupSchedule, err)
				schedule = scheduler.Daily(2, 0) // Default: 2:00 AM
			} else {
				schedule = cronSchedule
			}
		} else {
			schedule = scheduler.Daily(2, 0) // Default: 2:00 AM
		}

		keepCount := s.config.Scheduler.BackupRetentionDays
		if keepCount <= 0 {
			keepCount = 7 // Default: keep 7 days of backups
		}

		task := scheduler.NewConfigBackupTask(registry, schedule, keepCount)
		s.scheduler.AddTask(task)
		log.Printf("[CTL] Registered config backup task (keep: %d backups)", keepCount)
	}

	status := s.scheduler.GetStatus()
	log.Printf("[CTL] Scheduler initialized with %d tasks", len(status))

	return nil
}

// getBackupDir returns the backup directory path from config or default.
func (s *Server) getBackupDir() string {
	if s.config.Scheduler != nil && s.config.Scheduler.BackupDir != "" {
		return s.config.Scheduler.BackupDir
	}
	return filepath.Join(brand.GetStateDir(), "backups") // Default backup directory
}

// applyConfig applies a new configuration (used by scheduled tasks).
func (s *Server) applyConfig(cfg *config.Config) error {
	// Apply the configuration through the orchestrator
	result := s.serviceOrchestrator.ReloadAll(cfg)
	if !result.Success {
		// Collect failed services for error message
		var failedList []string
		for svc := range result.FailedServices {
			failedList = append(failedList, svc)
		}
		return fmt.Errorf("failed to apply config: services failed: %v", failedList)
	}

	// Update our stored config
	s.config = cfg
	log.Printf("[CTL] Applied configuration from scheduled task")
	return nil
}

// refreshIPSets refreshes all IPSets from their configured sources.
func (s *Server) refreshIPSets() error {
	if s.ipsetService == nil {
		return fmt.Errorf("IPSet service not initialized")
	}

	if s.config == nil {
		return fmt.Errorf("no configuration loaded")
	}

	log.Printf("[CTL] Refreshing IPSets from configured sources")

	var errorList []string

	for _, ipset := range s.config.IPSets {
		if !ipset.AutoUpdate {
			continue
		}

		// Use the IPSetService to update (it handles downloading and atomic reloading)
		if err := s.ipsetService.ForceUpdate(ipset.Name); err != nil {
			log.Printf("[CTL] Failed to update IPSet %s: %v", ipset.Name, err)
			errorList = append(errorList, fmt.Sprintf("%s: %v", ipset.Name, err))
		} else {
			log.Printf("[CTL] Successfully updated IPSet %s", ipset.Name)
		}
	}

	if len(errorList) > 0 {
		return fmt.Errorf("some IPSet updates failed: %s", strings.Join(errorList, "; "))
	}

	log.Printf("[CTL] IPSet refresh completed")
	return nil
}

// --- IPSet Management RPC Methods ---

// ListIPSets returns all IPSet metadata
func (s *Server) ListIPSets(args *Empty, reply *ListIPSetsReply) error {
	if s.ipsetService == nil {
		reply.Error = "IPSet service not available"
		return nil
	}
	ipsets, err := s.ipsetService.ListIPSets()
	if err != nil {
		reply.Error = err.Error()
		return nil
	}
	reply.IPSets = ipsets
	return nil
}

// GetIPSet returns metadata for a specific IPSet
func (s *Server) GetIPSet(args *GetIPSetArgs, reply *GetIPSetReply) error {
	if s.ipsetService == nil {
		reply.Error = "IPSet service not available"
		return nil
	}
	meta, err := s.ipsetService.GetMetadata(args.Name)
	if err != nil {
		reply.Error = err.Error()
		return nil
	}
	reply.Metadata = meta
	return nil
}

// RefreshIPSet forces an update of an IPSet
func (s *Server) RefreshIPSet(args *RefreshIPSetArgs, reply *Empty) error {
	if s.ipsetService == nil {
		return fmt.Errorf("IPSet service not available")
	}
	return s.ipsetService.ForceUpdate(args.Name)
}

// GetIPSetElements returns the elements in an IPSet
func (s *Server) GetIPSetElements(args *GetIPSetElementsArgs, reply *GetIPSetElementsReply) error {
	if s.ipsetService == nil {
		reply.Error = "IPSet service not available"
		return nil
	}
	elements, err := s.ipsetService.GetSetElements(args.Name)
	if err != nil {
		reply.Error = err.Error()
		return nil
	}
	reply.Elements = elements
	return nil
}

// AddIPSetEntry adds an entry to an IPSet
func (s *Server) AddIPSetEntry(args *AddIPSetEntryArgs, reply *AddIPSetEntryReply) error {
	if s.ipsetService == nil {
		reply.Error = "IPSet service not available"
		return nil
	}
	if err := s.ipsetService.AddEntry(args.Name, args.IP); err != nil {
		reply.Error = err.Error()
		return nil
	}
	reply.Success = true
	return nil
}

// RemoveIPSetEntry removes an entry from an IPSet
func (s *Server) RemoveIPSetEntry(args *RemoveIPSetEntryArgs, reply *RemoveIPSetEntryReply) error {
	if s.ipsetService == nil {
		reply.Error = "IPSet service not available"
		return nil
	}
	if err := s.ipsetService.RemoveEntry(args.Name, args.IP); err != nil {
		reply.Error = err.Error()
		return nil
	}
	reply.Success = true
	return nil
}

// CheckIPSetEntry checks if an entry exists in an IPSet
func (s *Server) CheckIPSetEntry(args *CheckIPSetEntryArgs, reply *CheckIPSetEntryReply) error {
	if s.ipsetService == nil {
		reply.Error = "IPSet service not available"
		return nil
	}
	exists, err := s.ipsetService.CheckEntry(args.Name, args.IP)
	if err != nil {
		reply.Error = err.Error()
		return nil
	}
	reply.Exists = exists
	return nil
}

// GetIPSetCacheInfo returns information about the IPSet cache
func (s *Server) GetIPSetCacheInfo(args *Empty, reply *GetIPSetCacheInfoReply) error {
	if s.ipsetService == nil {
		reply.Info = nil
		return nil
	}
	info, err := s.ipsetService.GetCacheInfo()
	if err != nil {
		// Log warning but return partial info (or nil)
		log.Printf("[CTL] Failed to get IPSet cache info: %v", err)
	}
	reply.Info = info
	return nil
}

// ClearIPSetCache clears the IPSet cache
func (s *Server) ClearIPSetCache(args *Empty, reply *Empty) error {
	if s.ipsetService == nil {
		return fmt.Errorf("IPSet service not available")
	}
	return s.ipsetService.ClearCache()
}

// refreshDNSBlocklists refreshes DNS blocklists from their configured sources.
func (s *Server) refreshDNSBlocklists() error {
	svc, ok := s.serviceOrchestrator.GetService("DNS")
	if !ok {
		return fmt.Errorf("DNS service not initialized")
	}

	if s.config == nil || s.config.DNSServer == nil {
		return fmt.Errorf("no DNS configuration loaded")
	}

	log.Printf("[CTL] Refreshing DNS blocklists from configured sources")

	for _, blocklist := range s.config.DNSServer.Blocklists {
		if !blocklist.Enabled {
			continue
		}

		if blocklist.URL != "" {
			log.Printf("[CTL] Refreshing DNS blocklist %s from URL %s", blocklist.Name, blocklist.URL)
		}
	}

	// Restart DNS service to reload blocklists
	// Simply reloading configuration should suffice as it triggers logic
	if _, err := svc.Reload(s.config); err != nil {
		log.Printf("[CTL] Warning: failed to reload DNS during blocklist refresh: %v", err)
	}

	log.Printf("[CTL] DNS blocklist refresh completed")
	return nil
}

func getHostname() string {
	h, _ := os.Hostname()
	return h
}

// SystemReboot reboots the system
func (s *Server) SystemReboot(args *SystemRebootArgs, reply *SystemRebootReply) error {
	log.Printf("[CTL] System reboot requested (Force: %v)", args.Force)

	// In a real scenario, we might want to delay slightly to allow the response to return
	go func() {
		time.Sleep(1 * time.Second)
		if err := exec.Command("reboot").Run(); err != nil {
			log.Printf("[CTL] Failed to reboot: %v", err)
			// fallback to force
			if args.Force {
				exec.Command("reboot", "-f").Run()
			}
		}
	}()

	reply.Success = true
	reply.Message = "System is rebooting..."
	return nil
}

// GetSystemStats returns system resource usage statistics
func (s *Server) GetSystemStats(args *Empty, reply *GetSystemStatsReply) error {
	stats := SystemStats{}

	// Basic Uptime (using clock package or syscall)
	// For simplicity and cross-platform compilation, we'll use a placeholder or conditional.
	// In production (Linux), we'd read /proc/uptime
	if uptime, err := os.ReadFile("/proc/uptime"); err == nil {
		var u float64
		fmt.Sscanf(string(uptime), "%f", &u)
		stats.Uptime = uint64(u)
	}

	// Load Avg
	if loadavg, err := os.ReadFile("/proc/loadavg"); err == nil {
		var l1, l5, l15 float64
		fmt.Sscanf(string(loadavg), "%f %f %f", &l1, &l5, &l15)
		stats.LoadAverage = l1
	}

	// Memory
	// Parse /proc/meminfo
	if meminfo, err := os.ReadFile("/proc/meminfo"); err == nil {
		lines := strings.Split(string(meminfo), "\n")
		for _, line := range lines {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				key := strings.TrimSuffix(fields[0], ":")
				val := parseSize(fields[1]) * 1024 // assuming kB

				switch key {
				case "MemTotal":
					stats.MemoryTotal = val
				case "MemAvailable":
					// used = total - available
					stats.MemoryUsed = stats.MemoryTotal - val // deferred calc
				}
			}
		}
		if stats.MemoryUsed == 0 && stats.MemoryTotal > 0 {
			// If MemAvailable wasn't found (older kernels), try MemFree + Buffers + Cached
			// Simplified: just leave it or use MemFree
		} else if stats.MemoryTotal > 0 {
			stats.MemoryUsed = stats.MemoryTotal - (stats.MemoryTotal - stats.MemoryUsed)
			// Wait, the logic above: used = total - available (where 'available' was put in Used temp?)
			// No, simpler:
			// stats.MemoryUsed is currently (Total - Available)
			// So it is correct.
		}
	}

	// Disk Usage (Root partition)
	var stat syscall.Statfs_t
	if err := syscall.Statfs("/", &stat); err == nil {
		stats.DiskTotal = stat.Blocks * uint64(stat.Bsize)
		stats.DiskUsed = (stat.Blocks - stat.Bfree) * uint64(stat.Bsize)
	}

	reply.Stats = stats
	return nil
}

func parseSize(s string) uint64 {
	var val uint64
	fmt.Sscanf(s, "%d", &val)
	return val
}

// --- Scan Network Methods ---

// StartScanNetwork starts a network scan asynchronously
func (s *Server) StartScanNetwork(args *StartScanNetworkArgs, reply *StartScanNetworkReply) error {
	if s.scannerService.IsScanning() {
		reply.Error = "scan already in progress"
		return nil
	}

	timeout := time.Duration(args.TimeoutSeconds) * time.Second
	if args.TimeoutSeconds == 0 {
		timeout = 5 * time.Minute
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		_, _ = s.scannerService.ScanNetwork(ctx, args.CIDR)
	}()

	reply.Success = true
	return nil
}

// GetScanStatus returns the current scan status and last result metadata
func (s *Server) GetScanStatus(args *Empty, reply *GetScanStatusReply) error {
	reply.Scanning = s.scannerService.IsScanning()
	reply.LastResult = s.scannerService.LastResult()
	return nil
}

// GetScanResult returns the full last scan result
func (s *Server) GetScanResult(args *Empty, reply *GetScanResultReply) error {
	reply.Result = s.scannerService.LastResult()
	return nil
}

// GetCommonPorts returns list of common ports
func (s *Server) GetCommonPorts(args *Empty, reply *GetCommonPortsReply) error {
	reply.Ports = scanner.GetCommonPorts()
	return nil
}

// ScanHost scans a specific host
func (s *Server) ScanHost(args *ScanHostArgs, reply *ScanHostReply) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := s.scannerService.ScanHost(ctx, args.IP)
	if err != nil {
		reply.Error = err.Error()
		return nil
	}
	reply.Result = result
	return nil
}

// EnsureTagIPSet ensures an IPSet exists for a given tag
func (s *Server) EnsureTagIPSet(tag string) (string, error) {
	// Sanitize tag to be a valid IPSet name
	// Allowed: alphanumeric, underscore, dash, dot
	safeTag := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' {
			return r
		}
		return '_'
	}, tag)

	ipsetName := "tag_" + safeTag

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.config == nil {
		return "", fmt.Errorf("config not loaded")
	}

	// Check if exists
	for _, set := range s.config.IPSets {
		if set.Name == ipsetName {
			return ipsetName, nil
		}
	}

	log.Printf("[CTL] Creating new IPSet for tag: %s (%s)", tag, ipsetName)

	// Add to Config
	newSet := config.IPSet{
		Name:          ipsetName,
		Type:          "ipv4_addr",
		Description:   "Auto-generated for tag: " + tag,
		MatchOnSource: true,
	}
	s.config.IPSets = append(s.config.IPSets, newSet)

	// Persist to disk
	if s.hclConfig != nil {
		// Update the HCL wrapper's config pointer if it drifted
		s.hclConfig.Config = s.config
		if err := s.hclConfig.Save(); err != nil {
			log.Printf("[CTL] Error saving config after adding tag IPSet: %v", err)
		}
	}

	// Create in Kernel immediately
	if s.ipsetService != nil {
		if err := s.ipsetService.GetIPSetManager().CreateSet(ipsetName, "ipv4_addr"); err != nil {
			// If it fails, we return error (though config is already saved)
			// It might fail if it already exists in kernel but not config?
			// We can ignore specific errors or just log.
			log.Printf("[CTL] Warning: failed to create kernel ipset %s: %v", ipsetName, err)
		}
	}

	return ipsetName, nil
}

// --- Wake-on-LAN ---

// WakeOnLAN sends a magic packet to wake up a device
func (s *Server) WakeOnLAN(args *WakeOnLANArgs, reply *WakeOnLANReply) error {
	if args.MAC == "" {
		reply.Error = "MAC address is required"
		return nil
	}

	mac, err := net.ParseMAC(args.MAC)
	if err != nil {
		reply.Error = fmt.Sprintf("invalid MAC address: %v", err)
		return nil
	}

	packet := make([]byte, 102)
	// 6 bytes of 0xFF
	for i := 0; i < 6; i++ {
		packet[i] = 0xFF
	}
	// 16 repetitions of MAC
	for i := 0; i < 16; i++ {
		copy(packet[6+i*6:], mac)
	}

	// Send to broadcast address on specific interface or global broadcast
	addr := &net.UDPAddr{
		IP:   net.IPv4bcast,
		Port: 9,
	}

	var conn *net.UDPConn
	if args.Interface != "" {
		_, err := net.InterfaceByName(args.Interface)
		if err != nil {
			reply.Error = fmt.Sprintf("interface %s not found: %v", args.Interface, err)
			return nil
		}

		// Bind to interface requires more complex socket setup or sending via specific IP
		// For simplicity, we'll try to DialUDP with local address if possible,
		// but UDP broadcast usually just works.
		// However, correct WOL usually requires binding to the interface.
		// Let's use standard net.DialUDP.
		conn, err = net.DialUDP("udp", nil, addr)
	} else {
		conn, err = net.DialUDP("udp", nil, addr)
	}

	if err != nil {
		reply.Error = fmt.Sprintf("failed to dial UDP: %v", err)
		return nil
	}
	defer conn.Close()

	_, err = conn.Write(packet)
	if err != nil {
		reply.Error = fmt.Sprintf("failed to send magic packet: %v", err)
		return nil
	}

	reply.Success = true
	auditLog("WakeOnLAN", fmt.Sprintf("mac=%s interface=%s", args.MAC, args.Interface))
	return nil
}

// --- Ping (Connectivity Verification) ---

// Ping pings a target IP address to verify connectivity
// Mitigation: OWASP A03:2021-Injection - Target is validated as IP address
func (s *Server) Ping(args *PingArgs, reply *PingReply) error {
	// Validate target is a valid IP address to prevent command injection
	ip := net.ParseIP(args.Target)
	if ip == nil {
		reply.Error = "invalid IP address"
		reply.Reachable = false
		return nil
	}

	timeout := args.TimeoutSeconds
	if timeout <= 0 {
		timeout = 5
	}

	// Use ping command with count=1 and timeout
	// -c 1: send one packet
	// -W: timeout in seconds (Linux)
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout+1)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "ping", "-c", "1", "-W", fmt.Sprintf("%d", timeout), ip.String())
	start := time.Now()
	err := cmd.Run()
	rtt := time.Since(start)

	if err != nil {
		// Ping failed - host unreachable
		reply.Reachable = false
		if ctx.Err() == context.DeadlineExceeded {
			reply.Error = "ping timeout"
		} else {
			reply.Error = "host unreachable"
		}
		return nil
	}

	reply.Reachable = true
	reply.RTTMs = int(rtt.Milliseconds())
	return nil
}

// syncIPSetTasks updates the scheduler with IPSet update tasks
func (s *Server) syncIPSetTasks(cfg *config.Config) error {
	if s.scheduler == nil || s.ipsetService == nil {
		return nil
	}

	// 1. Identify current ipset tasks (prefix "ipset_")
	currentTasks := s.scheduler.GetStatus()
	for _, task := range currentTasks {
		if strings.HasPrefix(task.ID, "ipset_") {
			s.scheduler.RemoveTask(task.ID)
		}
	}

	// 2. Add new tasks
	for _, ipset := range cfg.IPSets {
		if !ipset.AutoUpdate || ipset.RefreshHours <= 0 {
			continue
		}

		// Only schedule if it has a source
		if ipset.FireHOLList == "" && ipset.URL == "" {
			continue
		}

		// Calculate schedule based on RefreshHours
		interval := time.Duration(ipset.RefreshHours) * time.Hour
		sched := scheduler.Every(interval)

		task := &scheduler.Task{
			ID:          fmt.Sprintf("ipset_%s", ipset.Name),
			Name:        fmt.Sprintf("Update IPSet: %s", ipset.Name),
			Description: fmt.Sprintf("Updates IPSet %s from external source", ipset.Name),
			Schedule:    sched,
			Enabled:     true,
			RunOnStart:  true,
			Func: func(ctx context.Context) error {
				return s.ipsetService.ForceUpdate(ipset.Name)
			},
		}

		if err := s.scheduler.AddTask(task); err != nil {
			log.Printf("[CTL] Failed to add ipset task for %s: %v", ipset.Name, err)
		}
	}
	return nil
}

// --- Safe Mode Operations ---

// IsInSafeMode checks if safe mode is currently active.
func (s *Server) IsInSafeMode(args *Empty, reply *SafeModeStatusReply) error {
	if s.firewallManager != nil {
		reply.InSafeMode = s.firewallManager.IsInSafeMode()
	}
	return nil
}

// EnterSafeMode activates safe mode (emergency lockdown).
func (s *Server) EnterSafeMode(args *Empty, reply *Empty) error {
	if s.firewallManager == nil {
		return fmt.Errorf("firewall manager not initialized")
	}
	log.Printf("[CTL] SAFE MODE activated by RPC request")
	s.Notify(NotifyWarning, "Safe Mode", "Safe Mode has been activated - forwarding disabled")
	return s.firewallManager.ApplySafeMode()
}

// ExitSafeMode deactivates safe mode and restores normal operation.
func (s *Server) ExitSafeMode(args *Empty, reply *Empty) error {
	if s.firewallManager == nil {
		return fmt.Errorf("firewall manager not initialized")
	}
	log.Printf("[CTL] Safe mode deactivated by RPC request")
	s.Notify(NotifyInfo, "Safe Mode", "Safe Mode has been deactivated - normal operation resumed")
	return s.firewallManager.ExitSafeMode()
}
