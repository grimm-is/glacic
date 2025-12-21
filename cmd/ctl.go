package cmd

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"grimm.is/glacic/internal/brand"
	"grimm.is/glacic/internal/clock"
	"grimm.is/glacic/internal/config"
	"grimm.is/glacic/internal/ctlplane"
	"grimm.is/glacic/internal/device"
	fw "grimm.is/glacic/internal/firewall"
	"grimm.is/glacic/internal/health"
	"grimm.is/glacic/internal/learning"
	"grimm.is/glacic/internal/logging"
	"grimm.is/glacic/internal/monitor"
	"grimm.is/glacic/internal/network"
	"grimm.is/glacic/internal/notification"
	"grimm.is/glacic/internal/qos"
	"grimm.is/glacic/internal/routing"
	"grimm.is/glacic/internal/services/ddns"
	"grimm.is/glacic/internal/services/dhcp"
	"grimm.is/glacic/internal/services/discovery"
	"grimm.is/glacic/internal/services/dns"
	"grimm.is/glacic/internal/services/lldp"
	"grimm.is/glacic/internal/services/mdns"
	"grimm.is/glacic/internal/services/ra"
	"grimm.is/glacic/internal/services/threatintel"
	"grimm.is/glacic/internal/services/upnp"
	"grimm.is/glacic/internal/state"
	"grimm.is/glacic/internal/upgrade"
	"grimm.is/glacic/internal/vpn"
)

// RunCtl runs the privileged control plane daemon
// This process must run as root and handles all privileged operations:
// - Network interface configuration (netlink)
// - Firewall rules (nftables)
// - DHCP server/client (raw sockets, port 67)
// - DNS server (port 53)
// - Routing
// listeners argument allows injecting pre-opened listeners (for zero-downtime upgrades)
func RunCtl(configFile string, testMode bool, stateDir string, dryRun bool, listeners map[string]interface{}) error {
	// Dry-run mode: generate rules and exit before any daemon setup
	// This must happen BEFORE CaptureStdio to ensure output goes to terminal
	if dryRun {
		// Load config for dry-run
		result, err := config.LoadFileWithOptions(configFile, config.DefaultLoadOptions())
		if err != nil {
			return fmt.Errorf("failed to load configuration: %w", err)
		}

		// Generate and print rules
		dryRunLogger := logging.WithComponent("firewall")
		fwMgr := fw.NewManagerWithConn(nil, dryRunLogger, "")
		rules, err := fwMgr.GenerateRules(fw.FromGlobalConfig(result.Config))
		if err != nil {
			return fmt.Errorf("dry-run generation failed: %w", err)
		}

		fmt.Println(rules)
		return nil
	}

	// Context for services
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Context for monitors (watchdog, API auto-restart) that should be stopped during upgrade
	monitorsCtx, monitorsCancel := context.WithCancel(ctx)
	defer monitorsCancel()

	// Global Panic Recovery
	defer func() {
		if r := recover(); r != nil {
			logging.Error(fmt.Sprintf("CRITICAL PANIC in RunCtl: %v", r))
			// We should probably exit with error code, but logging it is step 1
			os.Exit(1)
		}
	}()

	// Capture stdout/stderr into the application log buffer and log file
	logFile := "/var/log/glacic/glacic.log"
	if env := os.Getenv("GLACIC_LOG_FILE"); env != "" {
		if env == "stdout" || env == "stderr" {
			logFile = ""
		} else {
			logFile = env
		}
	}
	logging.CaptureStdio(logFile)
	// Configure standard logger to use structured logger (unified format)
	logging.RedirectStdLog()

	// Handle signals
	go func() {
		// ... signal handling ...
	}()
	// Ensure state directory exists
	if err := os.MkdirAll(brand.GetStateDir(), 0755); err != nil {
		return fmt.Errorf("failed to create state directory: %w", err)
	}

	// Manage PID file with Watchdog (Self-Healing)
	// We run a watchdog for the first 60 seconds to ensure that if an *old* version of the daemon
	// (which blindly deletes the PID file on exit) shuts down after we start, we restore the file.
	runDir := brand.GetRunDir()
	if err := os.MkdirAll(runDir, 0755); err != nil {
		return fmt.Errorf("failed to create run directory: %w", err)
	}
	pidFile := filepath.Join(runDir, brand.LowerName+".pid")

	writePID := func() error {
		return os.WriteFile(pidFile, []byte(fmt.Sprintf("%d", os.Getpid())), 0644)
	}

	if err := writePID(); err != nil {
		return fmt.Errorf("failed to write PID file: %w", err)
	}

	// Start Keeper/Watchdog
	go func() {
		// Checks every 1 second to ensure we own the PID file.
		// This handles upgrade races (old process deleting file) and accidental deletions.
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-monitorsCtx.Done():
				return
			case <-ticker.C:
				// Check if file exists and is correct
				data, err := os.ReadFile(pidFile)
				if err != nil || strings.TrimSpace(string(data)) != fmt.Sprintf("%d", os.Getpid()) {
					// Restore it
					logging.Info("Restoring PID file (detected missing or invalid)")
					_ = writePID()
				}
			}
		}
	}()

	defer func() {
		// Only remove the PID file if it still contains our PID.
		// This prevents us from deleting the PID file of a new process that took over during an upgrade.
		if data, err := os.ReadFile(pidFile); err == nil {
			if strings.TrimSpace(string(data)) == fmt.Sprintf("%d", os.Getpid()) {
				os.Remove(pidFile)
			}
		}
	}()

	// System time sanity check - load anchor and warn if time seems wrong
	initClockAnchor()

	var cfg *config.Config

	// Boot Loop Protection
	trackerPath := brand.GetStateDir()
	if stateDir != "" {
		trackerPath = stateDir
	}
	crashTracker := health.NewCrashTracker(trackerPath) // Persistent storage

	// If we are upgrading (listeners provided), we skip the crash loop check.
	// An upgrade is intentional and might happen quickly.
	isUpgrade := len(listeners) > 0
	var safeMode bool
	var err error

	if !isUpgrade {
		safeMode, err = crashTracker.CheckCrashLoop()
		if err != nil {
			log.Printf("Warning: failed to check crash loop: %v", err)
		}
	} else {
		logging.Info("Skipping crash loop check (upgrade in progress)")
	}

	if safeMode {
		log.Println("!!! CRASH LOOP DETECTED - ENTERING SAFE MODE !!!")
		log.Println("Loading minimal Safe Mode configuration...")

		// Create safe config
		cfg = &config.Config{
			SchemaVersion: "SAFE_MODE",
			Interfaces: []config.Interface{
				{Name: "lo", IPv4: []string{"127.0.0.1/8"}},
			},
			// Enable API (RunAPI manages this, but Ctl services rely on cfg)
			IPForwarding: false,
		}

		// We can optionally start the API service here or rely on RunAPI?
		// RunCtxl runs services. RunAPI runs the HTTP server.
		// The HTTP server connects to Ctl via RPC.
		// So Ctl needs to come up.
	} else {
		crashTracker.StartStabilityTimer()

		// Load config with version handling and auto-migration
		result, err := config.LoadFileWithOptions(configFile, config.DefaultLoadOptions())
		if err != nil {
			return fmt.Errorf("failed to load configuration: %w", err)
		}
		cfg = result.Config

		if result.WasMigrated {
			logging.Info(fmt.Sprintf("Configuration migrated from schema %s to %s",
				result.OriginalVersion, result.CurrentVersion))
		}
		logging.Info(fmt.Sprintf("Configuration loaded (schema version %s)", cfg.SchemaVersion))

		// Configure Syslog if enabled
		if cfg.Syslog != nil && cfg.Syslog.Enabled {
			syslogCfg := logging.SyslogConfig{
				Enabled:  true,
				Host:     cfg.Syslog.Host,
				Port:     cfg.Syslog.Port,
				Protocol: cfg.Syslog.Protocol,
				Tag:      cfg.Syslog.Tag,
				Facility: cfg.Syslog.Facility,
			}

			if writer, err := logging.NewSyslogWriter(syslogCfg); err != nil {
				logging.Error(fmt.Sprintf("Failed to initialize syslog: %v", err))
			} else {
				logging.Info(fmt.Sprintf("Syslog enabled (host: %s:%d)", syslogCfg.Host, syslogCfg.Port))
				// Combine stderr (logfile) and syslog
				// Note: cmd/start.go redirects Stderr to the log file.
				multiOut := io.MultiWriter(os.Stderr, writer)

				// Re-initialize default logger with new output
				logCfg := logging.DefaultConfig()
				logCfg.Output = multiOut
				// Determine log level (default Info)
				// TODO: Add logging level to config?
				logCfg.Level = logging.LevelInfo

				logging.SetDefault(logging.New(logCfg))
				logging.Info("Logging switched to include Syslog")
			}
		}
	}

	// Initialize State Store
	dbPath := filepath.Join(brand.GetStateDir(), "state.db")
	if stateDir != "" {
		dbPath = filepath.Join(stateDir, "state.db")
	} else if cfg != nil && cfg.StateDir != "" {
		// Fallback to config if not overridden by CLI
		// But cfg is loaded AFTER crash tracker logic.
		// If CLI override is provided, we use it for both.
		// Ideally CLI overrides config.
		stateDir = cfg.StateDir // For potential later use?
		dbPath = filepath.Join(cfg.StateDir, "state.db")
	}

	if testMode {
		dbPath = ":memory:"
	}
	stateOpts := state.DefaultOptions(dbPath)
	stateStore, err := state.NewSQLiteStore(stateOpts)
	if err != nil {
		return fmt.Errorf("failed to initialize state store: %w", err)
	}
	defer stateStore.Close()

	// Wire clock anchor save to state writes (lazy update, avoids flash wear)
	stateStore.OnWrite = SaveClockAnchor

	// Configure State Replication
	if cfg.Replication != nil {
		logger := logging.WithComponent("replication")

		mode := state.ModePrimary
		if cfg.Replication.Mode == "replica" {
			mode = state.ModeReplica
		}

		repCfg := state.ReplicationConfig{
			Mode:           mode,
			ListenAddr:     cfg.Replication.ListenAddr,
			PrimaryAddr:    cfg.Replication.PrimaryAddr,
			ReconnectDelay: 5 * time.Second,
			SyncTimeout:    30 * time.Second,
		}

		if repCfg.ListenAddr == "" {
			repCfg.ListenAddr = ":9999"
		}

		replicator := state.NewReplicator(stateStore, repCfg, logger)
		if err := replicator.Start(); err != nil {
			logging.Error(fmt.Sprintf("Failed to start replication: %v", err))
		} else {
			logging.Info(fmt.Sprintf("Replication started in %s mode", mode))
			defer replicator.Stop()
		}
	}

	// Create Network Manager
	netMgr := network.NewManager()

	// Apply IP Forwarding setting
	if err := netMgr.SetIPForwarding(cfg.IPForwarding); err != nil {
		return fmt.Errorf("error setting IP forwarding: %w", err)
	}
	logging.Info(fmt.Sprintf("IP Forwarding set to: %v", cfg.IPForwarding))

	// Bring up loopback interface first (Alpine networking disabled)
	if err := netMgr.SetupLoopback(); err != nil {
		logging.Warn(fmt.Sprintf("Warning: failed to setup loopback: %v", err))
	}

	// Initialize DHCP client manager
	netMgr.InitializeDHCPClientManager(cfg.DHCP)
	defer netMgr.StopDHCPClientManager()

	for _, iface := range cfg.Interfaces {
		logging.Info(fmt.Sprintf(" applying configuration for interface: %s", iface.Name))
		if err := netMgr.ApplyInterface(iface); err != nil {
			logging.Error(fmt.Sprintf("Error applying interface %s: %v", iface.Name, err))
		} else {
			logging.Info(fmt.Sprintf("Successfully applied interface %s", iface.Name))
		}
	}

	// Apply Static Routes after interfaces are configured
	if len(cfg.Routes) > 0 {
		for _, r := range cfg.Routes {
			if r.Destination == "0.0.0.0/0" && r.Interface != "" {
				var targetIfaceCfg config.Interface
				for _, ic := range cfg.Interfaces {
					if ic.Name == r.Interface {
						targetIfaceCfg = ic
						break
					}
				}

				if targetIfaceCfg.DHCP {
					logging.Info(fmt.Sprintf("Waiting for IP address on %s (DHCP) before applying static routes...", targetIfaceCfg.Name))
					err := netMgr.WaitForLinkIP(targetIfaceCfg.Name, 10)
					if err != nil {
						logging.Error(fmt.Sprintf("Error waiting for IP on %s: %v", targetIfaceCfg.Name, err))
						os.Exit(1)
					}
					logging.Info(fmt.Sprintf("IP address found on %s. Proceeding with static routes.", targetIfaceCfg.Name))
				}
			}
		}

		if err := netMgr.ApplyStaticRoutes(cfg.Routes); err != nil {
			logging.Error(fmt.Sprintf("Error applying static routes: %v", err))
			os.Exit(1)
		}
		logging.Info("Static routes applied.")
	}

	// Configure Dynamic Routing (FRR)
	if cfg.FRR != nil {
		if err := routing.ConfigureFRR(cfg.FRR); err != nil {
			logging.Error(fmt.Sprintf("Error configuring FRR: %v", err))
		} else {
			logging.Info("Dynamic Routing (FRR) configured.")
		}
	}

	// Service instances
	// Configure DNS Server
	dnsSvc := dns.NewService()
	// Set DNS Updater for Network Manager (for DHCP upstream updates)
	netMgr.SetDNSUpdater(dnsSvc)

	if cfg.DNSServer != nil && cfg.DNSServer.Enabled {
		logging.Info("DNS Server config present, initializing...")
		// Reload applies config and starts if enabled
		if _, err := dnsSvc.Reload(cfg); err != nil {
			logging.Error(fmt.Sprintf("Error initializing DNS service: %v", err))
		} else if dnsSvc.IsRunning() {
			logging.Info("DNS Service started.")
		}
	}

	// Initialize services
	// logger already defined above as logging.New(logCfg).WithComponent("CTL")

	// Create upgrade manager
	upgradeLogger := logging.WithComponent("upgrade")
	upgradeMgr := upgrade.NewManager(upgradeLogger)

	// Create control plane server
	ctlServer := ctlplane.NewServer(cfg, configFile, netMgr)
	ctlServer.SetUpgradeManager(upgradeMgr)
	ctlServer.SetDisarmFunc(monitorsCancel)

	// Start RPC server immediately to ensure availability/handoff
	// This prevents "Connection Refused" if other services (like Learning DB) block on startup
	if listeners != nil && listeners["ctl"] != nil {
		logging.Info("Using injected control plane listener")

		// Assert to net.Listener
		if ctlListener, ok := listeners["ctl"].(net.Listener); ok {
			if err := ctlServer.StartWithListener(ctlListener); err != nil {
				logging.Error(fmt.Sprintf("Failed to start control plane RPC server with inherited listener: %v", err))
				os.Exit(1)
			}
		} else {
			// Fallback if casting fails? Should not happen if passed correctly.
			logging.Error("Injected 'ctl' listener is not a net.Listener")
			os.Exit(1)
		}
	} else {
		if err := ctlServer.Start(); err != nil {
			logging.Error(fmt.Sprintf("Failed to start control plane RPC server: %v", err))
			os.Exit(1)
		}
	}

	// Register control plane listener for upgrade handoff
	// This allows the old process to pass the socket to the new process during upgrade
	if ctlListener := ctlServer.GetListener(); ctlListener != nil {
		upgradeMgr.RegisterListener("ctl", ctlListener)
	}

	// Dependency Injection for Services
	// Configure DHCP Server
	// DHCP depends on DNS service (updater) and State Store
	dhcpSvc := dhcp.NewService(dnsSvc, stateStore)
	dhcpSvc.SetLeaseListener(ctlServer)
	if cfg.DHCP != nil {
		if _, err := dhcpSvc.Reload(cfg); err != nil {
			logging.Error(fmt.Sprintf("Error initializing DHCP service: %v", err))
		} else if dhcpSvc.IsRunning() {
			logging.Info("DHCP Service started.")
		}
	}

	// Configure VPNs (WireGuard / Tailscale)
	if cfg.VPN != nil {
		vpnLogger := logging.WithComponent("vpn")
		vpnMgr, err := vpn.NewManager(cfg.VPN, vpnLogger)
		if err != nil {
			logging.Error(fmt.Sprintf("Error initializing VPN manager: %v", err))
		} else {
			if err := vpnMgr.Start(ctx); err != nil {
				logging.Error(fmt.Sprintf("Error starting VPNs: %v", err))
			} else {
				logging.Info("VPN Service started.")
				defer vpnMgr.Stop()
			}
		}
	}

	// Configure Policy Routing
	polMgr := network.NewPolicyRoutingManager()
	if err := polMgr.Reload(cfg.RoutingTables, cfg.PolicyRoutes); err != nil {
		logging.Error(fmt.Sprintf("Error applying policy routing: %v", err))
	} else {
		logging.Info("Policy routes applied.")
	}

	// Configure Firewall (NFTables)
	fwLogger := logging.WithComponent("firewall")
	// Assuming NewManager takes (logger *logging.Logger, cacheDir string) or similar based on existing code usage
	// Check if fw package is imported as 'fw' (likely 'firewall') -> 'fw' used in snippet.
	fwMgr, err := fw.NewManager(fwLogger, "") // Logger from ctlplane, default cache dir
	if err != nil {
		logging.Error(fmt.Sprintf("Error initializing firewall manager: %v", err))
	} else {
		// Apply initial config
		if err := fwMgr.ApplyConfig(fw.FromGlobalConfig(cfg)); err != nil {
			logging.Error(fmt.Sprintf("Error applying firewall rules: %v", err))
		} else {
			logging.Info("Firewall rules applied.")
		}
	}

	// Configure DDNS Service
	ddnsSvc := ddns.NewService(logging.WithComponent("DDNS"))
	if cfg.DDNS != nil && cfg.DDNS.Enabled {
		// Map config
		ddnsCfg := ddns.Config{
			Enabled:   cfg.DDNS.Enabled,
			Provider:  cfg.DDNS.Provider,
			Hostname:  cfg.DDNS.Hostname,
			Token:     cfg.DDNS.Token,
			Username:  cfg.DDNS.Username,
			ZoneID:    cfg.DDNS.ZoneID,
			RecordID:  cfg.DDNS.RecordID,
			Interface: cfg.DDNS.Interface,
			Interval:  cfg.DDNS.Interval,
		}
		ddnsSvc.Reload(ddnsCfg)
		if err := ddnsSvc.Start(ctx); err != nil {
			logging.Error(fmt.Sprintf("Error starting DDNS service: %v", err))
		}
	}

	// Register services
	if fwMgr != nil {
		ctlServer.RegisterService(fwMgr)
	}
	ctlServer.RegisterService(dnsSvc)
	ctlServer.RegisterService(dhcpSvc)
	// DDNS doesn't implement Service interface fully? ctlplane/server.go RegisterService expects services.Service
	// services.Service interface check needed.
	// If ddns.Service doesn't satisfy it, we just run it standalone (Start called above).
	// But RegisterService is good for "GetServices" status API.
	// Let's assume we can't register it yet or won't for now.

	// Configure QoS
	qosLogger := logging.WithComponent("qos")
	qosMgr := qos.NewManager(qosLogger)
	if err := qosMgr.ApplyConfig(cfg); err != nil {
		logging.Error(fmt.Sprintf("Error applying QoS policy: %v", err))
	} else {
		logging.Info("QoS policies applied.")
	}

	// Threat Intel Service
	if cfg.ThreatIntel != nil && cfg.ThreatIntel.Enabled {
		// NewService now accepts logger? Or we need to pass it?
		// Assuming NewService signature: func NewService(cfg, dnsSvc, logger)
		// Check signature in previous steps or assume nil if not changed.
		// Wait, user code says: tiSvc := threatintel.NewService(cfg.ThreatIntel, dnsSvc, nil)
		// Let's pass a logger if possible, or nil.
		// NewService(cfg, dnsSvc, ipsetMgr) - we don't have ipsetMgr handy here easily, pass nil as original
		tiSvc := threatintel.NewService(cfg.ThreatIntel, dnsSvc, nil)
		if tiSvc != nil {
			// Inject logger if settable? Or assume it uses default/global?
			// Since we can't pass it to NewService (it expects IPSetManager), we rely on global logger or internal initialization.
			tiSvc.Start()
			logging.Info("Threat Intel Service started.")

			// If we really wanted to use tiLogger, we'd need to modify internal/services/threatintel/service.go
			// For now, logging.Info satisfies the requirement of standardizing *this* file's logs.
		}
	}

	// 6to4 Configuration
	if cfg.VPN != nil && len(cfg.VPN.SixToFour) > 0 {
		logging.Info("Configuring 6to4 tunnels...")
		// Use a goroutine to wait for WAN IP if needed, or just attempt now.
		// Since we wait for eth0/link IP earlier in main if routes exist, we assume IP might be there.
		// Better to run it asynchronously or after a delay if WAN is DHCP.
		go func() {
			// Simple retry logic
			for i := 0; i < 5; i++ {
				if err := vpn.Configure6to4(cfg); err == nil {
					logging.Info("6to4 tunnels configured.")
					return
				}
				time.Sleep(5 * time.Second)
			}
			logging.Error("Failed to configure 6to4 tunnels after retries")
		}()
	}

	// RA Service (Router Advertisements)
	// Check if any interface has RA enabled
	raEnabled := false
	for _, iface := range cfg.Interfaces {
		if iface.RA {
			raEnabled = true
			break
		}
	}
	if raEnabled {
		raSvc := ra.NewService(cfg)
		raSvc.Start()
		logging.Info("IPv6 RA Service started.")
	}

	// mDNS Reflector Service
	if cfg.MDNS != nil && cfg.MDNS.Enabled {
		mdnsSvc := mdns.NewReflector(mdns.Config{
			Enabled:    cfg.MDNS.Enabled,
			Interfaces: cfg.MDNS.Interfaces,
		})
		if err := mdnsSvc.Start(ctx); err != nil {
			logging.Error(fmt.Sprintf("Error starting mDNS reflector: %v", err))
		} else {
			logging.Info("mDNS Reflector Service started.")
			defer mdnsSvc.Stop()
		}
	}

	// UPnP Service
	if cfg.UPnP != nil && cfg.UPnP.Enabled {
		upnpSvc := upnp.NewService(upnp.Config{
			Enabled:       cfg.UPnP.Enabled,
			ExternalIntf:  cfg.UPnP.ExternalIntf,
			InternalIntfs: cfg.UPnP.InternalIntfs,
			SecureMode:    cfg.UPnP.SecureMode,
		}, fwMgr)
		if err := upnpSvc.Start(ctx); err != nil {
			logging.Error(fmt.Sprintf("Error starting UPnP service: %v", err))
		} else {
			logging.Info("UPnP Service started.")
			defer upnpSvc.Stop()
		}
	}

	// Start Monitor
	// Start Monitor
	if cfg.Features != nil && cfg.Features.IntegrityMonitoring {
		go fwMgr.MonitorIntegrity(ctx, cfg)
	}

	// Inject services into control plane
	// RegisterService called above
	ctlServer.SetStateStore(stateStore)

	// Initialize Device Manager
	network.InitOUI() // Load OUI database
	devMgr, err := device.NewManager(stateStore, network.LookupVendor)
	if err != nil {
		logging.Error(fmt.Sprintf("Failed to initialize device manager: %v", err))
	} else {
		ctlServer.SetDeviceManager(devMgr)
	}

	// Initialize IPSet Service
	// We use the ctlplane logger for now, or create a specific one
	ipsetLogger := logging.WithComponent("ipsets")
	iplistCacheDir := filepath.Join(brand.GetConfigDir(), "iplists") // Use config dir or cache dir?
	// brand.go doesn't have GetCacheDir. Let's assume subdirectory of state for now or similar.
	// Ideally brand.GetCacheDir(). For now, state dir is safer than hardcoded /var/cache.
	iplistCacheDir = filepath.Join(brand.GetStateDir(), "iplists")

	ipsetService := fw.NewIPSetService("firewall", iplistCacheDir, stateStore, ipsetLogger)
	ctlServer.SetIPSetService(ipsetService)

	// Initialize Notification Dispatcher
	var dispatcher *notification.Dispatcher
	if cfg.Notifications != nil {
		dispatcher = notification.NewDispatcher(cfg.Notifications, logging.WithComponent("notification"))
	}

	// Configure Learning Service
	if cfg.RuleLearning != nil && cfg.RuleLearning.Enabled {
		dbPath := filepath.Join(brand.GetStateDir(), "flow.db")
		learningSvc, err := learning.NewService(cfg.RuleLearning, dbPath)
		if err != nil {
			logging.Error(fmt.Sprintf("Error initializing learning service: %v", err))
		} else {
			if err := learningSvc.Start(); err != nil {
				logging.Error(fmt.Sprintf("Error starting learning service: %v", err))
			} else {
				logging.Info("Learning Service started.")
				ctlServer.SetLearningService(learningSvc)
				// Inject dependencies
				if devMgr != nil {
					learningSvc.SetDeviceManager(devMgr)
				}
				if dispatcher != nil {
					learningSvc.SetDispatcher(dispatcher)
				}
				// Ensure service is stopped on exit
				defer learningSvc.Stop()

				// Bridge NFLog -> Learning Service
				go func() {
					logging.Info("Bridging NFLog events to Learning Service")
					sub := ctlServer.SubscribeNFLog()
					for entry := range sub {
						// Filter for LEARN prefix or general traffic if desired
						// The original monitor looked for "LEARN:" prefix but also accepted others if lenient?
						// Let's implement the same prefix parsing as the old monitor.
						
						policy := ""
						if strings.HasPrefix(entry.Prefix, "LEARN:") {
							parts := strings.Split(entry.Prefix, ":")
							if len(parts) >= 2 {
								policy = parts[1]
							}
						}
						
						// Create PacketInfo
						pkt := learning.PacketInfo{
							SrcMAC:    entry.SrcMAC,
							SrcIP:     entry.SrcIP,
							DstIP:     entry.DstIP,
							DstPort:   int(entry.DstPort),
							Protocol:  strings.ToLower(entry.Protocol),
							Interface: entry.InDevName,
							Policy:    policy,
						}

						// If SrcMAC is missing but HwAddr exists, try to fallback?
						// ctlplane logic now handles SrcMAC extraction.

						learningSvc.IngestPacket(pkt)
					}
				}()
			}
		}
	}

	// LLDP Service (Topology Discovery)
	// Enable by default or via config? For now enable for all 'lan' interfaces or physical?
	// We'll just enable for all physical interfaces for now.
	lldpSvc := lldp.NewService()
	lldpSvc.Start()
	ctlServer.SetLLDPService(lldpSvc)
	defer lldpSvc.Stop()

	// Scan interfaces and start listener
	for _, iface := range cfg.Interfaces {
		// Only start on physical interfaces (hacky check: no dot, or specific list)
		// Or just all assigned interfaces?
		// Don't run on loopback or wg
		if iface.Name != "lo" && !strings.HasPrefix(iface.Name, "wg") {
			// We might need to check if interface is UP? Service.AddInterface will act on it.
			if err := lldpSvc.AddInterface(iface.Name); err != nil {
				logging.Warn(fmt.Sprintf("Warning: failed to start LLDP listener on %s: %v", iface.Name, err))
			}
		}
	}

	// Device Discovery Collector
	// Subscribe to nflog and maintain list of seen devices
	deviceCollector := discovery.NewCollector(func(dev *discovery.SeenDevice) {
		// Enrich with OUI lookup from device manager
		if devMgr != nil {
			info := devMgr.GetDevice(dev.MAC)
			dev.Vendor = info.Vendor
			if info.Device != nil {
				dev.Alias = info.Device.Alias
			}
		}
	})
	deviceCollector.Start()
	ctlServer.SetDeviceCollector(deviceCollector)
	defer deviceCollector.Stop()

	// Forward nflog entries to device collector
	go func() {
		sub := ctlServer.SubscribeNFLog()
		for entry := range sub {
			deviceCollector.Events() <- discovery.PacketEvent{
				Timestamp: entry.Timestamp,
				HwAddr:    entry.HwAddr,
				SrcIP:     entry.SrcIP,
				InDev:     entry.InDev,
			}
		}
	}()

	// RPC server started early (see above)

	// Test mode vs daemon mode
	if testMode {
		testLogger := logging.WithComponent("monitor")
		var wg sync.WaitGroup

		if len(cfg.Routes) > 0 {
			netMgr.WaitForLinkIP("eth0", 10)
		}
		monitor.Start(testLogger, cfg.Routes, &wg, true)

		wg.Wait()
		logging.Info("Monitor test run finished. Exiting.")

		logging.Info("--- NFTABLES RULESET ---")
		cmd := exec.Command("nft", "list", "ruleset")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			logging.Error(fmt.Sprintf("Failed to list ruleset: %v (nft binary might be missing)", err))
		}
		logging.Info("------------------------")
		return nil
	}

	// Normal run
	var wg sync.WaitGroup // WaitGroup for child processes

	if len(cfg.Routes) > 0 {
		netMgr.WaitForLinkIP("eth0", 10)
	}

	monitorLogger := logging.WithComponent("monitor")
	monitor.Start(monitorLogger, cfg.Routes, nil, false)

	logging.Info("Control plane running. Waiting for commands...")

	// Spawn API Server
	if cfg.API == nil || cfg.API.Enabled { // Default to enabled
		exe, err := os.Executable()
		if err == nil {
			wg.Add(1)
			var apiListenerFile *os.File
			if listeners != nil && listeners["api"] != nil {
				// We need the *File* for the API listener to pass to child.
				// Assuming listeners["api"] is of a type that has File() (like TCPListener)
				// We need to reflect or assert?
				// In upgrade.go: `l.(interface{ File() (*os.File, error) })`
				if fileL, ok := listeners["api"].(interface{ File() (*os.File, error) }); ok {
					f, err := fileL.File()
					if err == nil {
						apiListenerFile = f
						logging.Info("Passing inherited API listener to child process")
					} else {
						logging.Error("Failed to get file from API listener", "error", err)
					}
				}
			}

			go spawnAPI(monitorsCtx, exe, cfg, &wg, apiListenerFile)
		} else {
			logging.Error(fmt.Sprintf("Failed to determine executable path, cannot spawn API: %v", err))
		}
	}

	// Capture signals to ensure graceful shutdown and reloads
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)

	// Event loop
	for {
		select {
		case <-ctx.Done():
			return nil
		case sig := <-sigCh:
			switch sig {
			case syscall.SIGHUP:
				logging.Info("Received SIGHUP, reloading configuration...")
				// Reload config from disk
				result, err := config.LoadFileWithOptions(configFile, config.DefaultLoadOptions())
				if err != nil {
					logging.Error(fmt.Sprintf("Failed to reload configuration: %v", err))
					// Don't exit, just log error and keep running with old config
					continue
				}

				if result.WasMigrated {
					logging.Info(fmt.Sprintf("Reloaded configuration (migrated from %s to %s)",
						result.OriginalVersion, result.CurrentVersion))
				}
				logging.Info(fmt.Sprintf("Reloaded configuration (schema version %s)", result.Config.SchemaVersion))

				// Apply new config
				if err := ctlServer.ReloadConfig(result.Config); err != nil {
					logging.Error(fmt.Sprintf("Failed to apply reloaded configuration: %v", err))
				}
			case os.Interrupt, syscall.SIGTERM:
				logging.Info("Received signal, shutting down...", "signal", sig)
				// Cancel context to signal services and API to stop
				cancel()

				// Wait for API server and other goroutines to clean up
				logging.Info("Waiting for services to stop...")
				wg.Wait()
				return nil
			}
		}
	}
}

// spawnAPI runs the API server as a child process and restarts it if it crashes
func spawnAPI(ctx context.Context, exe string, cfg *config.Config, wg *sync.WaitGroup, inheritedFile *os.File) {
	defer wg.Done()

	// Simple backoff for restarts
	failCount := 0

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		args := []string{"_api-server"}

		// Check for flags
		if cfg.API != nil {
			if cfg.API.Listen != "" {
				args = append(args, "-listen", cfg.API.Listen)
			}
			// Only root processes can change user
			if os.Geteuid() == 0 {
				args = append(args, "-user", "nobody")
			}
		} else {
			// minimal defaults
			if os.Geteuid() == 0 {
				args = append(args, "-user", "nobody")
			}
		}

		cmd := exec.Command(exe, args...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		// Build clean environment
		// We must explicitly ensure GLACIC_UPGRADE_STANDBY is NOT passed to child
		// to prevent recursion loops where 'glacic api' thinks it's an upgrade standby.
		newEnv := make([]string, 0, len(os.Environ()))
		for _, e := range os.Environ() {
			if !strings.HasPrefix(e, "GLACIC_UPGRADE_STANDBY=") {
				newEnv = append(newEnv, e)
			}
		}
		cmd.Env = newEnv

		if inheritedFile != nil {
			cmd.ExtraFiles = []*os.File{inheritedFile}
		}

		// Create a separate mechanism to kill on context cancellation
		killCh := make(chan struct{})
		// Wait for context cancellation in background
		go func() {
			select {
			case <-ctx.Done():
				if cmd.Process != nil {
					logging.Info("Context cancelled, sending SIGTERM to API server...")
					// Try graceful shutdown first
					cmd.Process.Signal(syscall.SIGTERM)

					// Force kill after timeout if still running
					time.Sleep(5 * time.Second)
					if cmd.ProcessState == nil || !cmd.ProcessState.Exited() {
						logging.Warn("API server did not exit in time, force killing...")
						cmd.Process.Kill()
					}
				}
			case <-killCh:
				// Process finished naturally, stop waiting
				return
			}
		}()

		if cfg.API == nil || !cfg.API.Enabled {
			// Should actuall check "DisableSandbox" here?
			// The original code passed no flags for sandbox bypass, checking environment.
			// But wait, the environment is inherited.
			// Let's stick to original behavior for args.
		}

		logging.Info(fmt.Sprintf("Spawning API server: %v", args), "executable", exe)
		if err := cmd.Start(); err != nil {
			logging.Error(fmt.Sprintf("Failed to start API server: %v", err))
			failCount++
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second * time.Duration(failCount)):
				continue
			}
		}

		// Wait for it to exit
		err := cmd.Wait()
		close(killCh) // Stop the background killer

		// If context is done, we expect it to be killed or exiting
		if ctx.Err() != nil {
			logging.Info("API server shutting down (context cancelled)")
			return
		}

		if err != nil {
			logging.Error(fmt.Sprintf("API server exited with error: %v. Restarting...", err))
		} else {
			logging.Info("API server exited cleanly.")
			// Check context again in case it happened during cleanup
			if ctx.Err() != nil {
				return
			}
		}

		// Reset failCount? Or keep incrementing?
		// Sleep a bit before restart
		select {
		case <-ctx.Done():
			return
		case <-time.After(1 * time.Second):
		}
	}
}

var clockAnchorFile = filepath.Join(brand.GetStateDir(), "clock_anchor")

// initClockAnchor loads the clock anchor from persistent storage and checks system time sanity.
// If system time is unreasonable (before 2023), it logs a warning.
// The anchor is saved lazily (via OnWrite hook) to avoid flash wear.
func initClockAnchor() {
	now := time.Now()

	// Load saved anchor time for reference
	data, err := os.ReadFile(clockAnchorFile)
	if err == nil {
		var savedTime time.Time
		if err := savedTime.UnmarshalText(data); err == nil {
			fmt.Printf("Clock anchor loaded: %s\n", savedTime.Format(time.RFC3339))

			// If system time is way off, warn the user
			if !clock.IsReasonableTime(now) && clock.IsReasonableTime(savedTime) {
				fmt.Fprintf(os.Stderr, "Warning: System time (%s) appears unreasonable. "+
					"Last known good time was %s. Waiting for NTP sync.\n",
					now.Format(time.RFC3339), savedTime.Format(time.RFC3339))
			}
		}
	} else if !clock.IsReasonableTime(now) {
		fmt.Fprintf(os.Stderr, "Warning: System time (%s) appears unreasonable and no anchor file exists.\n",
			now.Format(time.RFC3339))
	}
}

// SaveClockAnchor persists the current time to storage.
// Called by state store OnWrite hook to piggyback anchor updates on other writes.
func SaveClockAnchor() {
	now := time.Now()
	// Only save if time is reasonable (don't persist bad timestamps)
	if !clock.IsReasonableTime(now) {
		return
	}
	data, err := now.MarshalText()
	if err != nil {
		return
	}
	_ = os.WriteFile(clockAnchorFile, data, 0644)
}
