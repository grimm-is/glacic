package learning

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"grimm.is/glacic/internal/clock"
	"grimm.is/glacic/internal/logging"
	"grimm.is/glacic/internal/network"

	"grimm.is/glacic/internal/brand"
	"grimm.is/glacic/internal/config"
	"grimm.is/glacic/internal/device"
	"grimm.is/glacic/internal/learning/flowdb"
)

// DeviceManager abstracts the device manager
type DeviceManager interface {
	GetDevice(mac string) device.DeviceInfo
}

// NotificationDispatcher abstracts the notification system
type NotificationDispatcher interface {
	SendSimple(title, message, level string)
}

// Engine is the main learning engine that coordinates flow learning and DNS correlation
type Engine struct {
	mu           sync.RWMutex
	config       *config.RuleLearningConfig
	db           *flowdb.DB
	flowCache    *FlowCache // In-memory LRU cache for fast packet processing
	dnsCache     *DNSSnoopCache
	logger       *logging.Logger
	learningMode bool

	// Callbacks for firewall rule application
	onFlowAllowed func(flow *flowdb.Flow) error
	onFlowDenied  func(flow *flowdb.Flow) error
	// Callback for new flow notifications (learning mode off only)
	onNewFlow func(flow *flowdb.Flow)

	// Port scan detection: track unique ports per source MAC
	portScanTracker map[string]*portScanState
	portScanMu      sync.Mutex

	// Dependencies
	deviceManager DeviceManager
	dispatcher    NotificationDispatcher

	// Background workers
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// portScanState tracks potential port scans from a source
type portScanState struct {
	uniquePorts    map[int]bool
	firstSeen      time.Time
	lastNotify     time.Time
	lastScanNotify time.Time
}

// EngineConfig holds configuration for the learning engine
type EngineConfig struct {
	DBPath       string // Path to SQLite database (use ":memory:" for in-memory)
	Logger       *logging.Logger
	LearningMode bool
	Config       *config.RuleLearningConfig
}

// NewEngine creates a new learning engine
func NewEngine(cfg EngineConfig) (*Engine, error) {
	if cfg.DBPath == "" {
		cfg.DBPath = ":memory:"
	}

	if cfg.Logger == nil {
		cfg.Logger = logging.Default()
	}

	// Open dedicated flow database
	db, err := flowdb.Open(cfg.DBPath, cfg.Logger)
	if err != nil {
		return nil, fmt.Errorf("failed to open flow database: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Default cache size: 10k entries
	cacheSize := 10000
	if cfg.Config != nil && cfg.Config.CacheSize > 0 {
		cacheSize = cfg.Config.CacheSize
	}

	engine := &Engine{
		config:          cfg.Config,
		db:              db,
		flowCache:       NewFlowCache(cacheSize),
		dnsCache:        NewDNSSnoopCache(cfg.Logger, 10000),
		logger:          cfg.Logger.WithComponent("learning_engine"),
		learningMode:    cfg.LearningMode,
		portScanTracker: make(map[string]*portScanState),
		ctx:             ctx,
		cancel:          cancel,
	}

	return engine, nil
}

// Start starts the learning engine background workers
func (e *Engine) Start() error {
	e.logger.Info("starting "+brand.Name+" learning engine", "learning_mode", e.learningMode)

	// Start cleanup worker
	e.wg.Add(1)
	go e.cleanupWorker()

	// Start reverse DNS worker
	e.wg.Add(1)
	go e.reverseDNSWorker()

	// Start DB flush worker for cache write-back
	e.wg.Add(1)
	go e.dbFlushWorker()

	return nil
}

// Stop stops the learning engine
func (e *Engine) Stop() {
	e.logger.Info("stopping " + brand.Name + " learning engine")
	e.cancel()
	e.dnsCache.Stop()
	e.wg.Wait()
	if e.db != nil {
		e.db.Close()
	}
}

// SetLearningMode enables or disables learning mode
func (e *Engine) SetLearningMode(enabled bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.learningMode = enabled
	e.logger.Info("learning mode changed", "enabled", enabled)
}

// IsLearningMode returns whether learning mode is active
func (e *Engine) IsLearningMode() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.learningMode
}

// SetFlowCallbacks sets callbacks for flow state changes
func (e *Engine) SetFlowCallbacks(onAllowed, onDenied func(flow *flowdb.Flow) error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.onFlowAllowed = onAllowed
	e.onFlowDenied = onDenied
}

// SetNewFlowCallback sets a callback for new pending flows (notifications).
// This is only called when learning mode is OFF and a new flow is detected.
func (e *Engine) SetNewFlowCallback(fn func(flow *flowdb.Flow)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.onNewFlow = fn
}

// SetDeviceManager sets the device manager dependency
func (e *Engine) SetDeviceManager(dm DeviceManager) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.deviceManager = dm
}

// SetDispatcher sets the notification dispatcher
func (e *Engine) SetDispatcher(d NotificationDispatcher) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.dispatcher = d
}

// isPortScan checks if the source is doing a port scan (many unique ports in short time)
// Returns true if this looks like a port scan and should be suppressed
const (
	portScanThreshold = 10               // Number of unique ports before considered a scan
	portScanWindow    = 5 * time.Minute  // Time window for port scan detection
	notifyRateLimit   = 30 * time.Second // Min time between notifications per source
	scanNotifyLimit   = 1 * time.Hour    // Min time between scan alerts per source
)

func (e *Engine) isPortScan(srcMAC string, dstPort int) bool {
	e.portScanMu.Lock()
	defer e.portScanMu.Unlock()

	now := clock.Now()

	state, exists := e.portScanTracker[srcMAC]
	if !exists {
		state = &portScanState{
			uniquePorts: make(map[int]bool),
			firstSeen:   now,
		}
		e.portScanTracker[srcMAC] = state
	}

	// Reset if outside detection window
	if now.Sub(state.firstSeen) > portScanWindow {
		state.uniquePorts = make(map[int]bool)
		state.firstSeen = now
	}

	state.uniquePorts[dstPort] = true

	return len(state.uniquePorts) >= portScanThreshold
}

// shouldNotify checks if we should send a notification for this source
func (e *Engine) shouldNotify(srcMAC string) bool {
	e.portScanMu.Lock()
	defer e.portScanMu.Unlock()

	now := clock.Now()
	state, exists := e.portScanTracker[srcMAC]
	if !exists {
		return true
	}

	// Rate limit notifications per source
	if now.Sub(state.lastNotify) < notifyRateLimit {
		return false
	}

	state.lastNotify = now
	return true
}

// shouldNotifyScan checks if we should send a scan alert for this source
func (e *Engine) shouldNotifyScan(srcMAC string) bool {
	e.portScanMu.Lock()
	defer e.portScanMu.Unlock()

	now := clock.Now()
	state, exists := e.portScanTracker[srcMAC]
	// If state doesn't exist, it can't be a scan (needs history)
	if !exists {
		return false
	}

	if now.Sub(state.lastScanNotify) < scanNotifyLimit {
		return false
	}

	state.lastScanNotify = now
	return true
}

// ProcessPacket handles a packet that doesn't match existing firewall rules
// Returns the verdict: true = accept, false = drop
func (e *Engine) ProcessPacket(pkt *PacketInfo) (bool, error) {
	if pkt == nil {
		return false, fmt.Errorf("packet info required")
	}

	e.mu.RLock()
	learningMode := e.learningMode
	e.mu.RUnlock()

	// FAST PATH: Check in-memory cache first (no I/O)
	if entry, ok := e.flowCache.Get(pkt.SrcMAC, pkt.Protocol, pkt.DstPort); ok {
		// Update stats in cached entry
		entry.Flow.LastSeen = clock.Now()
		entry.Flow.Occurrences++
		entry.Flow.SrcIP = pkt.SrcIP
		entry.Flow.DstIPSample = pkt.DstIP
		entry.Dirty = true // Mark for async DB write
		return entry.Verdict, nil
	}

	// SLOW PATH: Cache miss - query database
	existingFlow, err := e.db.FindFlow(pkt.SrcMAC, pkt.Protocol, pkt.DstPort)
	if err != nil {
		return false, fmt.Errorf("failed to find flow: %w", err)
	}

	if existingFlow != nil {
		// Update existing flow
		existingFlow.SrcIP = pkt.SrcIP
		existingFlow.DstIPSample = pkt.DstIP
		if err := e.db.UpsertFlow(existingFlow); err != nil {
			e.logger.Error("failed to update flow", "error", err)
		}

		// Compute verdict based on state
		var verdict bool
		switch existingFlow.State {
		case flowdb.StateAllowed:
			verdict = true
		case flowdb.StateDenied:
			verdict = false
		default: // pending
			verdict = learningMode
		}

		// Populate cache for future packets
		e.flowCache.Put(pkt.SrcMAC, pkt.Protocol, pkt.DstPort, &FlowCacheEntry{
			Flow:    existingFlow,
			Verdict: verdict,
		})

		return verdict, nil
	}

	// Create new flow
	newFlow := &flowdb.Flow{
		SrcMAC:             pkt.SrcMAC,
		SrcIP:              pkt.SrcIP,
		SrcHostname:        pkt.SrcHostname,
		Protocol:           pkt.Protocol,
		DstPort:            pkt.DstPort,
		DstIPSample:        pkt.DstIP,
		Policy:             pkt.Policy,
		LearningModeActive: learningMode,
	}

	// Enrich with device info
	if e.deviceManager != nil {
		info := e.deviceManager.GetDevice(pkt.SrcMAC)
		newFlow.Vendor = info.Vendor
		if info.Device != nil {
			newFlow.DeviceID = info.Device.ID
		}
	} else {
		// Fallback to simple vendor lookup if no manager (though manager usually wraps it)
		newFlow.Vendor = network.LookupVendor(pkt.SrcMAC)
	}

	// Set initial state based on learning mode
	if learningMode {
		newFlow.State = flowdb.StateAllowed
	} else {
		newFlow.State = flowdb.StatePending
	}

	if err := e.db.UpsertFlow(newFlow); err != nil {
		return false, fmt.Errorf("failed to save new flow: %w", err)
	}

	e.logger.Info("new flow detected",
		"src_mac", pkt.SrcMAC,
		"src_ip", pkt.SrcIP,
		"protocol", pkt.Protocol,
		"dst_port", pkt.DstPort,
		"dst_ip", pkt.DstIP,
		"state", newFlow.State,
	)

	// Add to cache
	e.flowCache.Put(pkt.SrcMAC, pkt.Protocol, pkt.DstPort, &FlowCacheEntry{
		Flow:    newFlow,
		Verdict: learningMode,
	})

	// Enrich with DNS context
	go e.enrichFlowWithDNS(newFlow.ID, pkt.DstIP)

	// If learning mode and auto-allowed, trigger callback
	if learningMode && e.onFlowAllowed != nil {
		go func() {
			if err := e.onFlowAllowed(newFlow); err != nil {
				e.logger.Error("failed to apply allowed flow rule", "error", err)
			}
		}()
	}

	// If learning mode OFF, notify about new pending flow (unless port scan)
	if !learningMode {
		// Check for port scan before notifying
		if e.isPortScan(pkt.SrcMAC, pkt.DstPort) {
			e.logger.Debug("suppressing notification - port scan detected",
				"src_mac", pkt.SrcMAC,
				"src_ip", pkt.SrcIP,
			)
			// Trigger Port Scan Alert
			if e.dispatcher != nil && e.shouldNotifyScan(pkt.SrcMAC) {
				msg := fmt.Sprintf("Port scan detected from %s (%s) targeting %s", pkt.SrcIP, pkt.SrcMAC, pkt.DstIP)
				go e.dispatcher.SendSimple("Port Scan Detected", msg, "warning")
			}
		} else if e.shouldNotify(pkt.SrcMAC) {
			// Trigger New Flow Alert
			if e.onNewFlow != nil {
				go e.onNewFlow(newFlow)
			}
			if e.dispatcher != nil {
				msg := fmt.Sprintf("New flow detected: %s (%s) -> %s:%d (%s)",
					pkt.SrcIP, pkt.SrcMAC, pkt.DstIP, pkt.DstPort, pkt.Protocol)
				go e.dispatcher.SendSimple("New Flow Detected", msg, "info")
			}
		}
	}

	return learningMode, nil
}

// ProcessSNI handles SNI hint discovered from separate inspection process
func (e *Engine) ProcessSNI(srcMAC, srcIP, dstIP, sni string) {
	if sni == "" {
		return
	}

	// 1. Identify App from Signatures
	appName := IdentifyApp(sni)
	if appName == "" {
		// Fallback: If IdentifyApp returns empty, maybe use the domain itself as a hint
		// But IdentifyApp is preferred.
		// We can still add the SNI as a domain hint.
	}

	// 2. Identify Vendor from MAC
	vendor := network.LookupVendor(srcMAC)

	// 3. Find/Update Flow and Annotate
	// We need to find the flow. But SNI packets are TCP port 443 specific.
	// We assume destination port 443.
	// We might not have the flow in DB yet if the packet hook hasn't run or is racing.
	// We'll try to find it.

	e.mu.RLock()
	// Note: We use 443 as default for SNI.
	flow, err := e.db.FindFlow(srcMAC, "TCP", 443)
	e.mu.RUnlock()

	if err != nil {
		e.logger.Error("failed to find flow for SNI annotation", "error", err)
		return
	}

	if flow != nil {
		updates := false
		if appName != "" && flow.App == "" {
			flow.App = appName
			updates = true
		}
		if vendor != "" && flow.Vendor == "" {
			flow.Vendor = vendor
			updates = true
		}

		if updates {
			if err := e.db.UpsertFlow(flow); err != nil {
				e.logger.Error("failed to update flow with annotations", "error", err)
			}
		}

		// Add Domain Hint
		hint := &flowdb.DomainHint{
			FlowID:     flow.ID,
			Domain:     sni,
			Confidence: 100, // SNI is high confidence
			Source:     flowdb.SourceSNIPeek,
			DetectedAt: clock.Now(),
		}
		if err := e.db.AddHint(hint); err != nil {
			e.logger.Error("failed to add SNI hint", "error", err)
		}
	} else {
		// Flow doesn't exist yet? Create it?
		// Usually ProcessPacket creates it. If we are faster than ProcessPacket, we can try to create it.
		// But we needSrcIP/DstIP which we have.
		// Let's create it.
		newFlow := &flowdb.Flow{
			SrcMAC:             srcMAC,
			SrcIP:              srcIP,
			Protocol:           "TCP",
			DstPort:            443,
			DstIPSample:        dstIP,
			LearningModeActive: e.IsLearningMode(),
			App:                appName,
			Vendor:             vendor,
			State:              flowdb.StatePending,
		}
		if newFlow.LearningModeActive {
			newFlow.State = flowdb.StateAllowed
		}

		if err := e.db.UpsertFlow(newFlow); err != nil {
			e.logger.Error("failed to create flow from SNI", "error", err)
			return
		}

		// Add Hint
		hint := &flowdb.DomainHint{
			FlowID:     newFlow.ID,
			Domain:     sni,
			Confidence: 100,
			Source:     flowdb.SourceSNIPeek,
			DetectedAt: clock.Now(),
		}
		e.db.AddHint(hint)
	}
}

// enrichFlowWithDNS adds domain hints to a flow
func (e *Engine) enrichFlowWithDNS(flowID int64, dstIP string) {
	// Check DNS cache first
	if domain, source, ok := e.dnsCache.GetWithSource(dstIP); ok {
		confidence := 80 // DNS snoop confidence
		if source == flowdb.SourceSNIPeek {
			confidence = 100
		}
		hint := &flowdb.DomainHint{
			FlowID:     flowID,
			Domain:     domain,
			Confidence: confidence,
			Source:     flowdb.HintSource(source),
			DetectedAt: clock.Now(),
		}
		if err := e.db.AddHint(hint); err != nil {
			e.logger.Error("failed to add domain hint", "error", err)
		}
		return
	}

	// Queue for reverse DNS lookup
	e.queueReverseDNS(flowID, dstIP)
}

// Reverse DNS queue
var reverseDNSQueue = make(chan reverseDNSRequest, 1000)

type reverseDNSRequest struct {
	flowID int64
	ip     string
}

func (e *Engine) queueReverseDNS(flowID int64, ip string) {
	select {
	case reverseDNSQueue <- reverseDNSRequest{flowID: flowID, ip: ip}:
	default:
		// Queue full, skip
		e.logger.Debug("reverse DNS queue full, skipping", "ip", ip)
	}
}

func (e *Engine) reverseDNSWorker() {
	defer e.wg.Done()

	for {
		select {
		case <-e.ctx.Done():
			return
		case req := <-reverseDNSQueue:
			domain, err := e.dnsCache.LookupReverse(req.ip)
			if err != nil {
				e.logger.Debug("reverse DNS lookup failed", "ip", req.ip, "error", err)
				continue
			}

			if domain != "" {
				hint := &flowdb.DomainHint{
					FlowID:     req.flowID,
					Domain:     domain,
					Confidence: 20, // Reverse DNS confidence
					Source:     flowdb.SourceReverse,
					DetectedAt: clock.Now(),
				}
				if err := e.db.AddHint(hint); err != nil {
					e.logger.Error("failed to add reverse DNS hint", "error", err)
				}
			}
		}
	}
}

// AllowFlow marks a flow as allowed and triggers firewall rule creation
func (e *Engine) AllowFlow(flowID int64) error {
	if err := e.db.UpdateState(flowID, flowdb.StateAllowed); err != nil {
		return fmt.Errorf("failed to allow flow: %w", err)
	}

	// Invalidate cache so next packet gets fresh state
	e.flowCache.InvalidateByID(flowID)

	flow, err := e.db.GetFlow(flowID)
	if err != nil {
		return fmt.Errorf("failed to get flow: %w", err)
	}

	e.logger.Info("flow allowed",
		"id", flowID,
		"src_mac", flow.SrcMAC,
		"protocol", flow.Protocol,
		"dst_port", flow.DstPort,
	)

	// Trigger callback to add firewall rule
	if e.onFlowAllowed != nil {
		return e.onFlowAllowed(flow)
	}

	return nil
}

// FreezeFlow is an alias for AllowFlow (legacy API)
func (e *Engine) FreezeFlow(flowID int64) error {
	return e.AllowFlow(flowID)
}

// DenyFlow marks a flow as blocked and triggers firewall rule creation
func (e *Engine) DenyFlow(flowID int64) error {
	if err := e.db.UpdateState(flowID, flowdb.StateDenied); err != nil {
		return fmt.Errorf("failed to deny flow: %w", err)
	}

	// Invalidate cache so next packet gets fresh state
	e.flowCache.InvalidateByID(flowID)

	flow, err := e.db.GetFlow(flowID)
	if err != nil {
		return fmt.Errorf("failed to get flow: %w", err)
	}

	e.logger.Info("flow denied",
		"id", flowID,
		"src_mac", flow.SrcMAC,
		"protocol", flow.Protocol,
		"dst_port", flow.DstPort,
	)

	// Trigger callback to add firewall rule
	if e.onFlowDenied != nil {
		return e.onFlowDenied(flow)
	}

	return nil
}

// BurnFlow is an alias for DenyFlow (legacy API)
func (e *Engine) BurnFlow(flowID int64) error {
	return e.DenyFlow(flowID)
}

// AllowWithScrutiny allows a flow but enables extra logging and sets a review reminder
func (e *Engine) AllowWithScrutiny(flowID int64, reviewAfter time.Duration) error {
	if err := e.db.UpdateState(flowID, flowdb.StateAllowed); err != nil {
		return fmt.Errorf("failed to allow flow: %w", err)
	}

	if err := e.db.SetScrutiny(flowID, true, reviewAfter); err != nil {
		return fmt.Errorf("failed to set scrutiny: %w", err)
	}

	flow, err := e.db.GetFlow(flowID)
	if err != nil {
		return fmt.Errorf("failed to get flow: %w", err)
	}

	e.logger.Info("flow allowed with scrutiny",
		"id", flowID,
		"src_mac", flow.SrcMAC,
		"protocol", flow.Protocol,
		"dst_port", flow.DstPort,
		"review_after", reviewAfter,
	)

	if e.onFlowAllowed != nil {
		return e.onFlowAllowed(flow)
	}

	return nil
}

// GetScrutinyDue returns flows that need review
func (e *Engine) GetScrutinyDue() ([]flowdb.Flow, error) {
	return e.db.GetScrutinyDue()
}

// GetPendingFlows returns flows awaiting triage
func (e *Engine) GetPendingFlows(limit int) ([]flowdb.FlowWithHints, error) {
	return e.db.ListFlowsWithHints(flowdb.ListOptions{
		State: flowdb.StatePending,
		Limit: limit,
		Desc:  true,
	})
}

// GetAllFlows returns all flows with optional state filter
func (e *Engine) GetAllFlows(state string, limit int) ([]flowdb.FlowWithHints, error) {
	return e.db.ListFlowsWithHints(flowdb.ListOptions{
		State: flowdb.FlowState(state),
		Limit: limit,
		Desc:  true,
	})
}

// GetFlow returns a single flow with hints
func (e *Engine) GetFlow(id int64) (*flowdb.FlowWithHints, error) {
	return e.db.GetFlowWithHints(id)
}

// ListFlows returns flows with full options (search, pagination)
func (e *Engine) ListFlows(opts flowdb.ListOptions) ([]flowdb.FlowWithHints, error) {
	return e.db.ListFlowsWithHints(opts)
}

// DeleteFlow removes a flow
func (e *Engine) DeleteFlow(id int64) error {
	return e.db.DeleteFlow(id)
}

// GetStats returns engine statistics
func (e *Engine) GetStats() (map[string]int64, error) {
	stats, err := e.db.GetStats()
	if err != nil {
		return nil, err
	}

	result := map[string]int64{
		"total_flows":        stats.TotalFlows,
		"pending_flows":      stats.PendingFlows,
		"allowed_flows":      stats.AllowedFlows,
		"denied_flows":       stats.DeniedFlows,
		"scrutiny_flows":     stats.ScrutinyFlows,
		"total_occurrences":  stats.TotalOccurrences,
		"total_domain_hints": stats.TotalDomainHints,
	}

	// Add DNS cache stats
	dnsStats := e.dnsCache.Stats()
	for k, v := range dnsStats {
		result["dns_cache_"+k] = v
	}

	// Add flow cache stats
	cacheHits, cacheMisses, cacheSize := e.flowCache.Stats()
	result["flow_cache_hits"] = int64(cacheHits)
	result["flow_cache_misses"] = int64(cacheMisses)
	result["flow_cache_size"] = int64(cacheSize)

	// Add learning mode status
	if e.IsLearningMode() {
		result["learning_mode"] = 1
	} else {
		result["learning_mode"] = 0
	}

	return result, nil
}

// HandleDNSResponse processes a DNS response for correlation
func (e *Engine) HandleDNSResponse(question string, answerIP net.IP, ttl uint32) {
	e.dnsCache.HandleDNSResponse(question, answerIP, ttl)
}

// cleanupWorker periodically cleans up old data
func (e *Engine) cleanupWorker() {
	defer e.wg.Done()

	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-e.ctx.Done():
			return
		case <-ticker.C:
			retentionDays := 30
			if e.config != nil && e.config.RetentionDays > 0 {
				retentionDays = e.config.RetentionDays
			}

			deleted, err := e.db.Cleanup(retentionDays)
			if err != nil {
				e.logger.Error("failed to cleanup old flows", "error", err)
			} else if deleted > 0 {
				e.logger.Info("cleaned up old flows", "deleted", deleted)
			}
		}
	}
}

// dbFlushWorker periodically flushes dirty cache entries to the database.
// This implements write-back caching for flow updates.
func (e *Engine) dbFlushWorker() {
	defer e.wg.Done()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-e.ctx.Done():
			// Final flush on shutdown
			e.flushDirtyEntries()
			return
		case <-ticker.C:
			e.flushDirtyEntries()
		}
	}
}

// flushDirtyEntries writes all dirty cache entries to the database.
func (e *Engine) flushDirtyEntries() {
	dirtyFlows := e.flowCache.FlushDirty()
	if len(dirtyFlows) == 0 {
		return
	}

	for _, flow := range dirtyFlows {
		if err := e.db.UpsertFlow(flow); err != nil {
			e.logger.Error("failed to flush flow to database", "flow_id", flow.ID, "error", err)
		}
	}

	e.logger.Debug("flushed dirty flows to database", "count", len(dirtyFlows))
}

// AllowAllPending allows all pending flows at once
func (e *Engine) AllowAllPending() (int64, error) {
	return e.db.AllowAllPending()
}

// FreezeAllPending is an alias for AllowAllPending (legacy API)
func (e *Engine) FreezeAllPending() (int64, error) {
	return e.AllowAllPending()
}

// PacketInfo contains information about a packet for learning
type PacketInfo struct {
	SrcMAC      string
	SrcIP       string
	SrcHostname string
	DstIP       string
	DstPort     int
	Protocol    string // "tcp" or "udp"
	Interface   string
	Policy      string // Firewall policy name
}
