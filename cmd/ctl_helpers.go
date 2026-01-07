package cmd

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/insomniacslk/dhcp/dhcpv4"

	"grimm.is/glacic/internal/brand"
	"grimm.is/glacic/internal/config"
	"grimm.is/glacic/internal/ctlplane"
	"grimm.is/glacic/internal/device"
	fw "grimm.is/glacic/internal/firewall"
	"grimm.is/glacic/internal/health"
	"grimm.is/glacic/internal/learning"
	"grimm.is/glacic/internal/logging"
	"grimm.is/glacic/internal/network"
	"grimm.is/glacic/internal/notification"
	"grimm.is/glacic/internal/qos"
	"grimm.is/glacic/internal/routing"
	"grimm.is/glacic/internal/services/ddns"
	"grimm.is/glacic/internal/services/dhcp"
	"grimm.is/glacic/internal/services/discovery"
	"grimm.is/glacic/internal/services/dns"
	"grimm.is/glacic/internal/services/hostmanager"
	"grimm.is/glacic/internal/services/lldp"
	"grimm.is/glacic/internal/services/mdns"
	"grimm.is/glacic/internal/services/ntp"
	"grimm.is/glacic/internal/services/ra"
	"grimm.is/glacic/internal/services/threatintel"
	"grimm.is/glacic/internal/services/upnp"
	"grimm.is/glacic/internal/state"
	"grimm.is/glacic/internal/upgrade"
	"grimm.is/glacic/internal/vpn"
)

// ctlServices holds all initialized services for the control plane.
type ctlServices struct {
	stateStore      state.Store
	netMgr          *network.Manager
	fwMgr           *fw.Manager
	dnsSvc          *dns.Service
	dhcpSvc         *dhcp.Service
	qosMgr          *qos.Manager
	polMgr          *network.PolicyRoutingManager
	lldpSvc         *lldp.Service
	deviceCollector *discovery.Collector
	deviceMgr       *device.Manager
	learningSvc     *learning.Service
	ctlServer       *ctlplane.Server
	upgradeMgr      *upgrade.Manager
	dispatcher      *notification.Dispatcher
	uplinkManager   *network.UplinkManager
	nflogReader     *ctlplane.NFLogReader
	mdnsSvc         *mdns.Reflector
	ntpSvc          *ntp.Service
	dhcpSniffer     *dhcp.Sniffer

	// Cleanup functions to call on shutdown
	cleanupFuncs []func()
}

// addCleanup registers a cleanup function to be called on shutdown.
func (s *ctlServices) addCleanup(fn func()) {
	s.cleanupFuncs = append(s.cleanupFuncs, fn)
}

// Shutdown calls all registered cleanup functions in reverse order.
func (s *ctlServices) Shutdown() {
	for i := len(s.cleanupFuncs) - 1; i >= 0; i-- {
		s.cleanupFuncs[i]()
	}
}

// initializeLogging sets up logging and captures stdio.
func initializeCtlLogging() {
	logFile := "/var/log/glacic/glacic.log"
	if env := os.Getenv("GLACIC_LOG_FILE"); env != "" {
		if env == "stdout" || env == "stderr" {
			logFile = ""
		} else {
			logFile = env
		}
	}
	logging.CaptureStdio(logFile)
	logging.RedirectStdLog()
}

// setupPIDFile creates and manages the PID file with watchdog.
func setupPIDFile(monitorsCtx context.Context) (cleanup func(), err error) {
	runDir := brand.GetRunDir()
	if err := os.MkdirAll(runDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create run directory: %w", err)
	}
	pidFile := filepath.Join(runDir, brand.LowerName+".pid")

	writePID := func() error {
		return os.WriteFile(pidFile, []byte(fmt.Sprintf("%d", os.Getpid())), 0644)
	}

	if err := writePID(); err != nil {
		return nil, fmt.Errorf("failed to write PID file: %w", err)
	}

	// Start watchdog
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-monitorsCtx.Done():
				return
			case <-ticker.C:
				data, err := os.ReadFile(pidFile)
				if err != nil || strings.TrimSpace(string(data)) != fmt.Sprintf("%d", os.Getpid()) {
					if err := writePID(); err != nil {
						logging.Error(fmt.Sprintf("Failed to restore PID file: %v", err))
					} else {
						logging.Info("Restoring PID file (detected missing or invalid)")
					}
				}
			}
		}
	}()

	cleanup = func() {
		if data, err := os.ReadFile(pidFile); err == nil {
			if strings.TrimSpace(string(data)) == fmt.Sprintf("%d", os.Getpid()) {
				os.Remove(pidFile)
			}
		}
	}
	return cleanup, nil
}

// loadConfiguration handles config loading with crash loop protection.
func loadConfiguration(rtCfg *CtlRuntimeConfig) (*config.Config, error) {
	// Note: Config file existence is checked in RunCtl before logging starts

	trackerPath := brand.GetStateDir()
	if rtCfg.StateDir != "" {
		trackerPath = rtCfg.StateDir
	}
	crashTracker := health.NewCrashTracker(trackerPath)

	// Skip crash loop check during upgrade
	if rtCfg.IsUpgrade {
		logging.Info("Skipping crash loop check (upgrade in progress)")
	} else {
		safeMode, err := crashTracker.CheckCrashLoop()
		if err != nil {
			logging.Warn(fmt.Sprintf("Warning: failed to check crash loop: %v", err))
		}
		if safeMode {
			logging.Error("!!! CRASH LOOP DETECTED - ENTERING SAFE MODE !!!")
			return &config.Config{
				SchemaVersion: "SAFE_MODE",
				Interfaces:    []config.Interface{{Name: "lo", IPv4: []string{"127.0.0.1/8"}}},
				IPForwarding:  false,
			}, nil
		}
		crashTracker.StartStabilityTimer()
	}

	result, err := config.LoadFileWithOptions(rtCfg.ConfigFile, config.DefaultLoadOptions())
	if err != nil {
		// Normal load failed - try forgiving parse
		logging.Warn(fmt.Sprintf("Normal config load failed: %v", err))
		logging.Info("Attempting forgiving parse to salvage configuration...")

		data, readErr := os.ReadFile(rtCfg.ConfigFile)
		if readErr != nil {
			return nil, fmt.Errorf("failed to read configuration file: %w", readErr)
		}

		forgiving := config.LoadForgiving(data, rtCfg.ConfigFile)
		if forgiving.FatalError != nil {
			logging.Error(fmt.Sprintf("Forgiving parse also failed: %v", forgiving.FatalError))
			logging.Warn("Using minimal safe mode configuration")
		} else if forgiving.HadErrors {
			logging.Warn("Configuration salvaged with errors - some blocks were skipped")
			for _, skipped := range forgiving.SkippedBlocks {
				logging.Warn(fmt.Sprintf("  Skipped lines %d-%d: %s", skipped.StartLine, skipped.EndLine, skipped.Reason))
			}
			logging.Info("Use 'glacic config diff' or Web UI to see full details")
		}

		// Store the forgiving result for later API access
		// TODO: Make this accessible via API/ctlplane
		return forgiving.Config, nil
	}

	if result.WasMigrated {
		logging.Info(fmt.Sprintf("Configuration migrated from schema %s to %s",
			result.OriginalVersion, result.CurrentVersion))
	}
	logging.Info(fmt.Sprintf("Configuration loaded (schema version %s)", result.Config.SchemaVersion))

	// Save safe mode hints for future recovery scenarios
	if err := config.SaveSafeModeHints(result.Config); err != nil {
		logging.Warn(fmt.Sprintf("Could not save safe mode hints: %v", err))
	}

	return result.Config, nil
}

// configureSyslog sets up syslog if enabled in config.
func configureSyslog(cfg *config.Config) {
	if cfg.Syslog == nil || !cfg.Syslog.Enabled {
		return
	}

	syslogCfg := logging.SyslogConfig{
		Enabled:  true,
		Host:     cfg.Syslog.Host,
		Port:     cfg.Syslog.Port,
		Protocol: cfg.Syslog.Protocol,
		Tag:      cfg.Syslog.Tag,
		Facility: cfg.Syslog.Facility,
	}

	writer, err := logging.NewSyslogWriter(syslogCfg)
	if err != nil {
		logging.Error(fmt.Sprintf("Failed to initialize syslog: %v", err))
		return
	}

	logging.Info(fmt.Sprintf("Syslog enabled (host: %s:%d)", syslogCfg.Host, syslogCfg.Port))
	multiOut := io.MultiWriter(os.Stderr, writer)

	logCfg := logging.DefaultConfig()
	logCfg.Output = multiOut
	logCfg.Level = logging.LevelInfo

	logging.SetDefault(logging.New(logCfg))
	logging.Info("Logging switched to include Syslog")
}

// initializeStateStore creates and configures the state store.
func initializeStateStore(rtCfg *CtlRuntimeConfig, cfg *config.Config) (state.Store, error) {
	dbPath := filepath.Join(brand.GetStateDir(), "state.db")
	if rtCfg.StateDir != "" {
		dbPath = filepath.Join(rtCfg.StateDir, "state.db")
	} else if cfg.StateDir != "" {
		dbPath = filepath.Join(cfg.StateDir, "state.db")
	}

	if rtCfg.TestMode {
		dbPath = ":memory:"
	}

	stateOpts := state.DefaultOptions(dbPath)
	stateStore, err := state.NewSQLiteStore(stateOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize state store: %w", err)
	}

	// Wire clock anchor save to state writes
	stateStore.OnWrite = SaveClockAnchor

	return stateStore, nil
}

// configureReplication sets up state replication if configured.
func configureReplication(cfg *config.Config, stateStore state.Store) func() {
	if cfg.Replication == nil {
		return func() {}
	}

	// Replication requires SQLiteStore
	sqlStore, ok := stateStore.(*state.SQLiteStore)
	if !ok {
		logging.Warn("Replication requires SQLite store, skipping")
		return func() {}
	}

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

	replicator := state.NewReplicator(sqlStore, repCfg, logger)
	if err := replicator.Start(); err != nil {
		logging.Error(fmt.Sprintf("Failed to start replication: %v", err))
		return func() {}
	}

	logging.Info(fmt.Sprintf("Replication started in %s mode", mode))
	return replicator.Stop
}

// initializeNetworkStack sets up the network manager and applies interface config.
func initializeNetworkStack(cfg *config.Config) (*network.Manager, error) {
	netMgr := network.NewManager()

	if err := netMgr.SetIPForwarding(cfg.IPForwarding); err != nil {
		return nil, fmt.Errorf("error setting IP forwarding: %w", err)
	}
	logging.Info(fmt.Sprintf("IP Forwarding set to: %v", cfg.IPForwarding))

	if err := netMgr.SetupLoopback(); err != nil {
		logging.Warn(fmt.Sprintf("Warning: failed to setup loopback: %v", err))
	}

	netMgr.InitializeDHCPClientManager(cfg.DHCP)

	for _, iface := range cfg.Interfaces {
		logging.Info(fmt.Sprintf(" applying configuration for interface: %s", iface.Name))
		if err := netMgr.ApplyInterface(iface); err != nil {
			logging.Error(fmt.Sprintf("Error applying interface %s: %v", iface.Name, err))
		} else {
			logging.Info(fmt.Sprintf("Successfully applied interface %s", iface.Name))
		}
	}

	// Apply sysctl tuning (router optimizations)
	applySysctlTuning(cfg)

	return netMgr, nil
}

// applyStaticRoutes applies static routes after interfaces are configured.
func applyStaticRoutes(cfg *config.Config, netMgr *network.Manager) error {
	if len(cfg.Routes) == 0 {
		return nil
	}

	// Wait for DHCP on default route interface if needed
	for _, r := range cfg.Routes {
		if r.Destination == "0.0.0.0/0" && r.Interface != "" {
			for _, ic := range cfg.Interfaces {
				if ic.Name == r.Interface && ic.DHCP {
					logging.Info(fmt.Sprintf("Waiting for IP address on %s (DHCP) before applying static routes...", ic.Name))
					if err := netMgr.WaitForLinkIP(ic.Name, 10); err != nil {
						return fmt.Errorf("error waiting for IP on %s: %w", ic.Name, err)
					}
					logging.Info(fmt.Sprintf("IP address found on %s. Proceeding with static routes.", ic.Name))
				}
			}
		}
	}

	if err := netMgr.ApplyStaticRoutes(cfg.Routes); err != nil {
		return fmt.Errorf("error applying static routes: %w", err)
	}
	logging.Info("Static routes applied.")
	return nil
}

// initializeCoreServices creates the core services (DNS, DHCP, Firewall, etc.)
func initializeCoreServices(ctx context.Context, cfg *config.Config, netMgr *network.Manager, stateStore state.Store) (*ctlServices, error) {
	services := &ctlServices{
		stateStore: stateStore,
		netMgr:     netMgr,
	}

	// Syslog Forwarding
	if cfg.Syslog != nil && cfg.Syslog.Enabled {
		syslogCfg := logging.SyslogConfig{
			Enabled:  true,
			Host:     cfg.Syslog.Host,
			Port:     cfg.Syslog.Port,
			Protocol: cfg.Syslog.Protocol,
			Tag:      cfg.Syslog.Tag,
			Facility: cfg.Syslog.Facility,
		}
		syslogWriter, err := logging.NewSyslogWriter(syslogCfg)
		if err != nil {
			logging.Warn(fmt.Sprintf("Failed to connect to syslog server: %v", err))
		} else {
			// Combine stderr and syslog writers
			multiWriter := logging.MultiWriter(os.Stderr, syslogWriter)
			logCfg := logging.DefaultConfig()
			logCfg.Output = multiWriter
			logging.SetDefault(logging.New(logCfg))
			logging.Info("Syslog forwarding enabled", "host", cfg.Syslog.Host, "port", cfg.Syslog.Port)
		}
	}

	// DNS Service
	// DNS Service
	dnsLogger := logging.WithComponent("dns")
	services.dnsSvc = dns.NewService(cfg, dnsLogger)
	netMgr.SetDNSUpdater(services.dnsSvc)

	// Initialize if either legacy or new config is present
	shouldInitDNS := (cfg.DNSServer != nil && cfg.DNSServer.Enabled) || (cfg.DNS != nil)

	if shouldInitDNS {
		logging.Info("DNS Server config present, initializing...")
		if _, err := services.dnsSvc.Reload(cfg); err != nil {
			logging.Error(fmt.Sprintf("Error initializing DNS service: %v", err))
		} else if services.dnsSvc.IsRunning() {
			logging.Info("DNS Service started.")
		}
	}

	// Upgrade Manager
	services.upgradeMgr = upgrade.NewManager(logging.WithComponent("upgrade"))

	// Inject UpgradeManager into DNS service (created earlier)
	if services.dnsSvc != nil {
		services.dnsSvc.SetUpgradeManager(services.upgradeMgr)
	}

	// DHCP Service
	services.dhcpSvc = dhcp.NewService(services.dnsSvc, stateStore)
	services.dhcpSvc.SetUpgradeManager(services.upgradeMgr)
	if cfg.DHCP != nil {
		if _, err := services.dhcpSvc.Reload(cfg); err != nil {
			logging.Error(fmt.Sprintf("Error initializing DHCP service: %v", err))
		} else if services.dhcpSvc.IsRunning() {
			logging.Info("DHCP Service started.")
		}

		// Device Profiling Strategy:
		// 1. If builtin DHCP server is running, hook into it directly (avoids port conflict).
		// 2. If external/import mode, start standalone sniffer.

		useStandaloneSniffer := false
		if cfg.DHCP.Mode == "external" || cfg.DHCP.Mode == "import" {
			useStandaloneSniffer = true
		}

		if !useStandaloneSniffer {
			// Hook into internal DHCP server
			services.dhcpSvc.SetPacketListener(func(pkt *dhcpv4.DHCPv4, iface string, src net.Addr) {
				// Guard: deviceCollector is initialized later in initializeDeviceServices
				if services.deviceCollector == nil {
					return
				}
				event := dhcp.ExtractEvent(pkt, iface, src)
				services.deviceCollector.DHCPEvents() <- discovery.DHCPEvent{
					Timestamp:   event.Timestamp,
					ClientMAC:   event.ClientMAC,
					Interface:   event.Interface,
					Hostname:    event.Hostname,
					Fingerprint: event.Fingerprint,
					VendorClass: event.VendorClass,
					ClientID:    event.ClientID,
					Options:     event.Options,
				}
			})
			logging.Info("DHCP device profiling enabled (internal hook)")
		} else {
			// Start standalone sniffer
			var snifferIfaces []string
			for _, scope := range cfg.DHCP.Scopes {
				if scope.Interface != "" {
					snifferIfaces = append(snifferIfaces, scope.Interface)
				}
			}

			if len(snifferIfaces) > 0 {
				services.dhcpSniffer = dhcp.NewSniffer(dhcp.SnifferConfig{
					Enabled:    true,
					Interfaces: snifferIfaces,
				})
				if err := services.dhcpSniffer.Start(ctx); err != nil {
					logging.Error(fmt.Sprintf("Error starting DHCP sniffer: %v", err))
				} else {
					logging.Info(fmt.Sprintf("DHCP Sniffer started on %v", snifferIfaces))
					services.addCleanup(services.dhcpSniffer.Stop)
				}
			}
		}
	}

	// VPN Services
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
				services.addCleanup(vpnMgr.Stop)
			}
		}
	}

	// Policy Routing
	services.polMgr = network.NewPolicyRoutingManager()
	if err := services.polMgr.Reload(cfg.RoutingTables, cfg.PolicyRoutes); err != nil {
		logging.Error(fmt.Sprintf("Error applying policy routing: %v", err))
	} else {
		logging.Info("Policy routes applied.")
	}

	// Firewall setup complete

	// Firewall
	fwLogger := logging.WithComponent("firewall")
	fwMgr, err := fw.NewManager(fwLogger, "")
	if err != nil {
		logging.Error(fmt.Sprintf("Error initializing firewall manager: %v", err))
	} else {
		services.fwMgr = fwMgr

		// BOOT TO SAFE MODE FIRST
		// Apply minimal safe mode rules immediately to ensure a secure baseline.
		// This protects the system even if full config application fails.
		fwMgr.PreRenderSafeMode(fw.FromGlobalConfig(cfg))
		if err := fwMgr.ApplySafeMode(); err != nil {
			logging.Error(fmt.Sprintf("Error applying safe mode: %v", err))
			// Continue anyway - we'll try to apply full config
		} else {
			logging.Info("Safe mode applied (secure baseline).")
		}

		// Now apply the full configuration
		if err := fwMgr.ApplyConfig(fw.FromGlobalConfig(cfg)); err != nil {
			logging.Error(fmt.Sprintf("Error applying firewall rules: %v", err))
			// System remains in safe mode - still accessible via LAN
			logging.Warn("Firewall config failed - system remains in safe mode")
		} else {
			logging.Info("Firewall rules applied.")
			
			// Wire DNS Service implementation
			// We do this after fwMgr is created but it can be done before ApplyConfig theoretically.
			if services.dnsSvc != nil {
				services.dnsSvc.SetFirewall(fwMgr)
				// SYNC FIREWALL: Re-authorize cached IPs to persist dynamic sets
				// This is critical for the "Smart Flush" strategy.
				services.dnsSvc.SyncFirewall()
				
				// Set callback for integrity restore events
				fwMgr.SetIntegrityRestoreCallback(services.dnsSvc.SyncFirewall)
			}
		}
	}

	// Apply Multi-WAN Policy Rules (via UplinkManager)
	// Must be done AFTER firewall is initialized because it uses nftables chains
	services.uplinkManager = network.NewUplinkManager()

	// Ensure required nftables chains exist (UplinkManager fallback mode expects them)
	// mark_prerouting: for connection marking
	// nat_postrouting: for SNAT
	exec.Command("nft", "add", "table", "inet", "glacic").Run()
	exec.Command("nft", "add", "chain", "inet", "glacic", "mark_prerouting", "{ type filter hook prerouting priority -150; policy accept; }").Run()
	exec.Command("nft", "add", "chain", "inet", "glacic", "nat_postrouting", "{ type nat hook postrouting priority 100; policy accept; }").Run()

	// Convert MultiWAN config to UplinkGroups
	var uplinkGroups []config.UplinkGroup

	if cfg.MultiWAN != nil && cfg.MultiWAN.Enabled {
		// Create a synthetic UplinkGroup for MultiWAN
		groupName := "multi_wan"
		group := config.UplinkGroup{
			Name:            groupName,
			Enabled:         true,
			SourceNetworks:  []string{"0.0.0.0/0"}, // Match all traffic by default for MultiWAN
			FailoverMode:    "graceful",
			LoadBalanceMode: "none",
		}

		if cfg.MultiWAN.Mode == "loadbalance" {
			group.LoadBalanceMode = "weighted"
		} else if cfg.MultiWAN.Mode == "both" {
			group.LoadBalanceMode = "weighted"
			group.FailoverMode = "graceful"
		}

		// Health Check
		if cfg.MultiWAN.HealthCheck != nil {
			group.HealthCheck = cfg.MultiWAN.HealthCheck
		}

		// Uplinks
		for _, link := range cfg.MultiWAN.Connections {
			if !link.Enabled {
				continue
			}
			uplink := config.UplinkDef{
				Name:      link.Name,
				Interface: link.Interface,
				Gateway:   link.Gateway,
				Weight:    link.Weight,
				Type:      "wan",
				Enabled:   true,
			}
			group.Uplinks = append(group.Uplinks, uplink)
		}

		uplinkGroups = append(uplinkGroups, group)
	}

	// Also include native UplinkGroups if any
	if len(cfg.UplinkGroups) > 0 {
		uplinkGroups = append(uplinkGroups, cfg.UplinkGroups...)
	}

	if err := services.uplinkManager.Reload(uplinkGroups); err != nil {
		logging.Error(fmt.Sprintf("Error initializing UplinkManager: %v", err))
	} else {
		// Set notification callback
		services.uplinkManager.SetHealthCallback(func(uplink *network.Uplink, healthy bool) {
			status := "UP"
			if !healthy {
				status = "DOWN"
			}
			logging.Info(fmt.Sprintf("[Uplink] %s is now %s", uplink.Name, status))
		})

		// Start health checking
		// Use fast interval for responsiveness (critical for tests)
		services.uplinkManager.StartHealthChecking(1*time.Second, []string{"8.8.8.8"}) // Default backup targets
		logging.Info("UplinkManager initialized.")
	}

	// QoS
	services.qosMgr = qos.NewManager(logging.WithComponent("qos"))
	if err := services.qosMgr.ApplyConfig(cfg); err != nil {
		logging.Error(fmt.Sprintf("Error applying QoS policy: %v", err))
	} else {
		logging.Info("QoS policies applied.")
	}

	return services, nil
}

// initializeAdditionalServices creates secondary services (DDNS, Threat Intel, etc.)
func initializeAdditionalServices(ctx context.Context, cfg *config.Config, services *ctlServices) {
	// DDNS
	if cfg.DDNS != nil && cfg.DDNS.Enabled {
		ddnsSvc := ddns.NewService(logging.WithComponent("DDNS"))
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

	// HostManager (Dynamic DNS Objects)
	// Always initialize, it will check config for relevant IPSets
	hmSvc := hostmanager.New(cfg, logging.WithComponent("hostmanager"))
	if err := hmSvc.Start(); err != nil {
		logging.Error(fmt.Sprintf("Error starting HostManager: %v", err))
	} else {
		// Only log success if we actually have active sets? 
		// The service logs "Starting" and "No DNS-based IPSets" internally.
		// We add cleanup here.
		services.addCleanup(func() { hmSvc.Stop() })
	}

	// Threat Intel
	if cfg.ThreatIntel != nil && cfg.ThreatIntel.Enabled {
		tiSvc := threatintel.NewService(cfg.ThreatIntel, services.dnsSvc, nil)
		if tiSvc != nil {
			tiSvc.Start()
			logging.Info("Threat Intel Service started.")
		}
	}

	// FRR Dynamic Routing
	if cfg.FRR != nil {
		if err := routing.ConfigureFRR(cfg.FRR); err != nil {
			logging.Error(fmt.Sprintf("Error configuring FRR: %v", err))
		} else {
			logging.Info("Dynamic Routing (FRR) configured.")
		}
	}

	// 6to4 Tunnels
	if cfg.VPN != nil && len(cfg.VPN.SixToFour) > 0 {
		logging.Info("Configuring 6to4 tunnels...")
		go func() {
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

	// RA Service
	for _, iface := range cfg.Interfaces {
		if iface.RA {
			raSvc := ra.NewService(cfg)
			raSvc.Start()
			logging.Info("IPv6 RA Service started.")
			break
		}
	}

	// mDNS Reflector
	mdnsEnabled := false
	var mdnsIfaces []string

	if cfg.MDNS != nil && cfg.MDNS.Enabled {
		mdnsEnabled = true
		mdnsIfaces = cfg.MDNS.Interfaces
	} else if cfg.MDNS == nil {
		// Auto-enable on DHCP serving interfaces (LAN)
		// This ensures device discovery works out of the box
		if cfg.DHCP != nil && cfg.DHCP.Enabled {
			seen := make(map[string]bool)
			for _, scope := range cfg.DHCP.Scopes {
				if scope.Interface != "" && !seen[scope.Interface] {
					mdnsIfaces = append(mdnsIfaces, scope.Interface)
					seen[scope.Interface] = true
				}
			}
			if len(mdnsIfaces) > 0 {
				mdnsEnabled = true
				logging.Info("Auto-enabling mDNS on LAN interfaces", "interfaces", mdnsIfaces)
			}
		}
	}

	if mdnsEnabled {
		services.mdnsSvc = mdns.NewReflector(mdns.Config{
			Enabled:    true,
			Interfaces: mdnsIfaces,
		}, logging.WithComponent("mdns"))
		services.mdnsSvc.SetUpgradeManager(services.upgradeMgr)
		// Defer startup to initializeDeviceServices so we can wire the profiling callback first
	}

	// UPnP
	if cfg.UPnP != nil && cfg.UPnP.Enabled {
		upnpSvc := upnp.NewService(upnp.Config{
			Enabled:       cfg.UPnP.Enabled,
			ExternalIntf:  cfg.UPnP.ExternalIntf,
			InternalIntfs: cfg.UPnP.InternalIntfs,
			SecureMode:    cfg.UPnP.SecureMode,
		}, services.fwMgr)
		upnpSvc.SetUpgradeManager(services.upgradeMgr)
		if err := upnpSvc.Start(ctx); err != nil {
			logging.Error(fmt.Sprintf("Error starting UPnP service: %v", err))
		} else {
			logging.Info("UPnP Service started.")
			services.addCleanup(upnpSvc.Stop)
		}
	}

	// NTP Service
	if cfg.NTP != nil && cfg.NTP.Enabled {
		services.ntpSvc = ntp.NewService(logging.WithComponent("ntp"))
		services.ntpSvc.SetUpgradeManager(services.upgradeMgr)
		// Reload to apply config and start
		if _, err := services.ntpSvc.Reload(cfg); err != nil {
			logging.Error(fmt.Sprintf("Error starting NTP service: %v", err))
		} else {
			logging.Info("NTP Service started.")
			services.addCleanup(func() { services.ntpSvc.Stop(ctx) })
		}
	}

	// Notification Dispatcher
	if cfg.Notifications != nil {
		services.dispatcher = notification.NewDispatcher(cfg.Notifications, logging.WithComponent("notification"))
	}
}

// initializeDeviceServices sets up device management and discovery.
func initializeDeviceServices(ctx context.Context, cfg *config.Config, services *ctlServices) {
	// Device Manager
	network.InitOUI()
	devMgr, err := device.NewManager(services.stateStore, network.LookupVendor)
	if err != nil {
		logging.Error(fmt.Sprintf("Failed to initialize device manager: %v", err))
	} else {
		services.deviceMgr = devMgr
		services.ctlServer.SetDeviceManager(devMgr)
	}

	// IPSet Service
	iplistCacheDir := filepath.Join(brand.GetStateDir(), "iplists")
	ipsetService := fw.NewIPSetService(brand.LowerName, iplistCacheDir, services.stateStore, logging.WithComponent("ipsets"))
	services.ctlServer.SetIPSetService(ipsetService)

	// LLDP Service
	services.lldpSvc = lldp.NewService()
	services.lldpSvc.Start()
	services.ctlServer.SetLLDPService(services.lldpSvc)
	services.addCleanup(services.lldpSvc.Stop)

	for _, iface := range cfg.Interfaces {
		if iface.Name != "lo" && !strings.HasPrefix(iface.Name, "wg") {
			if err := services.lldpSvc.AddInterface(iface.Name); err != nil {
				logging.Warn(fmt.Sprintf("Warning: failed to start LLDP listener on %s: %v", iface.Name, err))
			}
		}
	}

	// NFLog Reader - captures packet events for device discovery
	services.nflogReader = ctlplane.NewNFLogReader(10000, ctlplane.NFLogGroup)
	if err := services.nflogReader.Start(); err != nil {
		logging.Warn(fmt.Sprintf("Warning: failed to start NFLog reader: %v", err))
	} else {
		logging.Info("NFLog reader started (device discovery active)")
		services.ctlServer.SetLogReader(services.nflogReader)
		services.addCleanup(services.nflogReader.Stop)
	}

	// Device Discovery Collector
	services.deviceCollector = discovery.NewCollector(func(dev *discovery.SeenDevice) {
		if services.deviceMgr != nil {
			info := services.deviceMgr.GetDevice(dev.MAC)
			dev.Vendor = info.Vendor
			if info.Device != nil {
				dev.Alias = info.Device.Alias
			}
		}
	}, filepath.Join(brand.GetStateDir(), "discovery.json"))
	services.deviceCollector.Start()
	services.ctlServer.SetDeviceCollector(services.deviceCollector)
	services.addCleanup(services.deviceCollector.Stop)

	// Wire mDNS events to device collector (if mDNS is enabled)
	if services.mdnsSvc != nil {
		services.mdnsSvc.SetEventCallback(func(parsed *mdns.ParsedMDNS) {
			services.deviceCollector.MDNSEvents() <- discovery.MDNSEvent{
				Timestamp:  time.Now(),
				SrcMAC:     parsed.SrcMAC,
				SrcIP:      parsed.SrcIP,
				Interface:  parsed.Interface,
				Hostname:   parsed.Hostname,
				Services:   parsed.Services,
				TXTRecords: parsed.TXTRecords,
			}
		})
		logging.Info("mDNS device profiling enabled")
	}

	// Start mDNS Reflector (now that callback is wired)
	if services.mdnsSvc != nil {
		if err := services.mdnsSvc.Start(ctx); err != nil {
			logging.Error(fmt.Sprintf("Error starting mDNS reflector: %v", err))
		} else {
			logging.Info("mDNS Reflector Service started.")
			services.addCleanup(services.mdnsSvc.Stop)
		}
	}

	// Wire DHCP events to device collector (if sniffer is enabled)
	if services.dhcpSniffer != nil {
		services.dhcpSniffer.SetEventCallback(func(event dhcp.SnifferEvent) {
			// Guard: deviceCollector may not be initialized yet
			if services.deviceCollector == nil {
				return
			}
			services.deviceCollector.DHCPEvents() <- discovery.DHCPEvent{
				Timestamp:   event.Timestamp,
				ClientMAC:   event.ClientMAC,
				Interface:   event.Interface,
				Hostname:    event.Hostname,
				Fingerprint: event.Fingerprint,
				VendorClass: event.VendorClass,
				ClientID:    event.ClientID,
				Options:     event.Options,
			}
		})
		logging.Info("DHCP device profiling enabled (sniffer)")
	}

	// Forward nflog entries to device collector
	go func() {
		sub := services.ctlServer.SubscribeNFLog()
		for entry := range sub {
			services.deviceCollector.Events() <- discovery.PacketEvent{
				Timestamp: entry.Timestamp,
				HwAddr:    entry.HwAddr,
				SrcIP:     entry.SrcIP,
				DstIP:     entry.DstIP,
				InDev:     entry.InDev,
				InDevName: entry.InDevName,
				Protocol:  entry.Protocol,
				SrcPort:   entry.SrcPort,
				DstPort:   entry.DstPort,
			}
		}
	}()
}

// initializeLearningService sets up the rule learning engine.
func initializeLearningService(cfg *config.Config, services *ctlServices) {
	if cfg.RuleLearning == nil || !cfg.RuleLearning.Enabled {
		return
	}

	dbPath := filepath.Join(brand.GetStateDir(), "flow.db")
	learningSvc, err := learning.NewService(cfg.RuleLearning, dbPath)
	if err != nil {
		logging.Error(fmt.Sprintf("Error initializing learning service: %v", err))
		return
	}

	if err := learningSvc.Start(); err != nil {
		logging.Error(fmt.Sprintf("Error starting learning service: %v", err))
		return
	}

	logging.Info("Learning Service started.")
	services.learningSvc = learningSvc
	services.ctlServer.SetLearningService(learningSvc)

	if services.deviceMgr != nil {
		learningSvc.SetDeviceManager(services.deviceMgr)
	}
	if services.dispatcher != nil {
		learningSvc.SetDispatcher(services.dispatcher)
	}

	services.addCleanup(learningSvc.Stop)

	// Bridge NFLog -> Learning Service
	go func() {
		logging.Info("Bridging NFLog events to Learning Service")
		sub := services.ctlServer.SubscribeNFLog()
		for entry := range sub {
			policy := ""
			if strings.HasPrefix(entry.Prefix, "LEARN:") {
				parts := strings.Split(entry.Prefix, ":")
				if len(parts) >= 2 {
					policy = parts[1]
				}
			}

			pkt := learning.PacketInfo{
				SrcMAC:    entry.SrcMAC,
				SrcIP:     entry.SrcIP,
				DstIP:     entry.DstIP,
				DstPort:   int(entry.DstPort),
				Protocol:  strings.ToLower(entry.Protocol),
				Interface: entry.InDevName,
				Policy:    policy,
			}
			learningSvc.IngestPacket(pkt)
		}
	}()
}

// startControlPlaneServer starts the RPC server with optional inherited listener.
func startControlPlaneServer(cfg *config.Config, configFile string, netMgr *network.Manager, services *ctlServices, listeners map[string]interface{}) error {
	services.ctlServer = ctlplane.NewServer(cfg, configFile, netMgr)
	services.ctlServer.SetUpgradeManager(services.upgradeMgr)

	if listeners != nil && listeners["ctl"] != nil {
		logging.Info("Using injected control plane listener")
		if ctlListener, ok := listeners["ctl"].(net.Listener); ok {
			if err := services.ctlServer.StartWithListener(ctlListener); err != nil {
				return fmt.Errorf("failed to start control plane RPC server with inherited listener: %w", err)
			}
		} else {
			return fmt.Errorf("injected 'ctl' listener is not a net.Listener")
		}
	} else {
		if err := services.ctlServer.Start(); err != nil {
			return fmt.Errorf("failed to start control plane RPC server: %w", err)
		}
	}

	// Register for upgrade handoff
	if ctlListener := services.ctlServer.GetListener(); ctlListener != nil {
		services.upgradeMgr.RegisterListener("ctl", ctlListener)
	}

	// Register services
	if services.fwMgr != nil {
		services.ctlServer.RegisterService(services.fwMgr)
	}
	services.ctlServer.RegisterService(services.dnsSvc)
	services.ctlServer.RegisterService(services.dhcpSvc)
	services.ctlServer.RegisterService(services.dhcpSvc)
	services.ctlServer.SetStateStore(services.stateStore)
	services.ctlServer.SetDHCPService(services.dhcpSvc)
	services.ctlServer.SetUplinkManager(services.uplinkManager)
	services.dhcpSvc.SetLeaseListener(services.ctlServer)

	return nil
}

// applySysctlTuning applies system-level sysctl tuning for router workloads.
func applySysctlTuning(cfg *config.Config) {
	// Skip if no system config
	if cfg.System == nil {
		logging.Info("No system config - using default sysctl profile")
		cfg.System = &config.SystemConfig{
			SysctlProfile: "default",
		}
	}

	// Default to "default" profile if not specified
	profile := cfg.System.SysctlProfile
	if profile == "" {
		profile = "default"
	}

	// Create tuner with hardware detection
	tuner := network.NewSysctlTuner(
		network.SysctlProfile(profile),
		cfg.System.Sysctl,
		logging.WithComponent("sysctl"),
	)

	// Apply tuning
	if err := tuner.Apply(); err != nil {
		logging.Error(fmt.Sprintf("Error applying sysctl tuning: %v", err))
	} else {
		logging.Info(fmt.Sprintf("Applied sysctl tuning (profile: %s)", profile))
	}
}

// runMainEventLoop handles signals and runs the main event loop.
func runMainEventLoop(ctx context.Context, cancel context.CancelFunc, configFile string, ctlServer *ctlplane.Server, wg *sync.WaitGroup) error {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)

	for {
		select {
		case <-ctx.Done():
			return nil
		case sig := <-sigCh:
			switch sig {
			case syscall.SIGHUP:
				logging.Info("Received SIGHUP, reloading configuration...")
				result, err := config.LoadFileWithOptions(configFile, config.DefaultLoadOptions())
				if err != nil {
					logging.Error(fmt.Sprintf("Failed to reload configuration: %v", err))
					continue
				}

				if result.WasMigrated {
					logging.Info(fmt.Sprintf("Reloaded configuration (migrated from %s to %s)",
						result.OriginalVersion, result.CurrentVersion))
				}
				logging.Info(fmt.Sprintf("Reloaded configuration (schema version %s)", result.Config.SchemaVersion))

				if err := ctlServer.ReloadConfig(result.Config); err != nil {
					logging.Error(fmt.Sprintf("Failed to apply reloaded configuration: %v", err))
				}

			case os.Interrupt, syscall.SIGTERM:
				logging.Info("Received signal, shutting down...", "signal", sig)
				cancel()
				logging.Info("Waiting for services to stop...")
				wg.Wait()
				return nil
			}
		}
	}
}

// runTestMode runs the control plane in test mode and exits.
// It outputs the current nftables ruleset and returns immediately.
func runTestMode(cfg *config.Config, netMgr *network.Manager) error {
	// Wait for IP if routes depend on DHCP
	if len(cfg.Routes) > 0 {
		netMgr.WaitForLinkIP("eth0", 10)
	}

	logging.Info("Test mode: Firewall rules applied successfully")

	// Output the ruleset for verification
	logging.Info("--- NFTABLES RULESET ---")
	cmd := exec.Command("nft", "list", "ruleset")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		logging.Error(fmt.Sprintf("Failed to list ruleset: %v (nft binary might be missing)", err))
	}
	logging.Info("------------------------")

	// Return nil to trigger deferred cleanup and exit
	return nil
}

// applyServiceDefaults applies default values to configuration for services
// that should be enabled by default (like mDNS discovery).
func applyServiceDefaults(cfg *config.Config) {
	// mDNS Reflector / Discovery
	// Default to enabled for discovery if not specified
	if cfg.MDNS == nil {
		var ifaces []string
		for _, iface := range cfg.Interfaces {
			if iface.Name != "lo" && !strings.HasPrefix(iface.Name, "wg") {
				ifaces = append(ifaces, iface.Name)
			}
		}
		if len(ifaces) > 0 {
			cfg.MDNS = &config.MDNSConfig{
				Enabled:    true,
				Interfaces: ifaces,
			}
			logging.Info("mDNS enabled by default for discovery", "interfaces", ifaces)
		}
	}
}
