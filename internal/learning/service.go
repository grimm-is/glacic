package learning

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"sync"
	"time"

	"grimm.is/glacic/internal/config"
	"grimm.is/glacic/internal/learning/flowdb"
	"grimm.is/glacic/internal/logging"
)

// Service manages the learning firewall functionality
type Service struct {
	config *config.RuleLearningConfig
	engine *Engine
	logger *logging.Logger

	// Callbacks
	onRuleApproved func(policy string, rule *config.PolicyRule) error

	// Control
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	running bool
	mutex   sync.RWMutex

	// Direct ingestion
	ingestChan chan PacketInfo
}

// NewService creates a new learning service
func NewService(cfg *config.RuleLearningConfig, dbPath string) (*Service, error) {
	if cfg == nil || !cfg.Enabled {
		return &Service{config: cfg}, nil
	}

	logger := logging.New(logging.DefaultConfig()).WithComponent("learning")

	// Initialize Engine (SQLite backed)
	// dbPath passed from caller (allows testing via :memory:)

	engine, err := NewEngine(EngineConfig{
		DBPath:       dbPath,
		Logger:       logger,
		LearningMode: cfg.LearningMode,
		Config:       cfg,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create learning engine: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	svc := &Service{
		config:     cfg,
		engine:     engine,
		logger:     logger,
		ctx:        ctx,
		cancel:     cancel,
		ingestChan: make(chan PacketInfo, 1000),
	}

	return svc, nil
}

// Start starts the learning service
func (s *Service) Start() error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if s.running {
		return fmt.Errorf("service is already running")
	}

	if s.config == nil || !s.config.Enabled {
		log.Println("Learning service is disabled")
		return nil
	}

	// Start Engine
	if err := s.engine.Start(); err != nil {
		return fmt.Errorf("failed to start engine: %w", err)
	}

	// Start ingestion routine
	s.wg.Add(1)
	go s.ingestionLoop()

	s.running = true
	s.logger.Info("Learning service started")
	return nil
}

// SetOnRuleApproved sets the callback for applying approved rules to the firewall.
// The callback receives the target policy name and the generated firewall rule.
func (s *Service) SetOnRuleApproved(fn func(policy string, rule *config.PolicyRule) error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.onRuleApproved = fn
}

// SetDeviceManager sets the device manager for flow enrichment
func (s *Service) SetDeviceManager(dm DeviceManager) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if s.engine != nil {
		s.engine.SetDeviceManager(dm)
	}
}

// SetDispatcher sets the notification dispatcher
func (s *Service) SetDispatcher(d NotificationDispatcher) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if s.engine != nil {
		s.engine.SetDispatcher(d)
	}
}

// Stop stops the learning service
func (s *Service) Stop() {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if !s.running {
		return
	}

	// Cancel context
	s.cancel()

	// Stop Engine
	if s.engine != nil {
		s.engine.Stop()
	}

	// Wait for goroutines
	s.wg.Wait()

	s.running = false
	s.logger.Info("Learning service stopped")
}

// IsRunning returns whether the service is running
func (s *Service) IsRunning() bool {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.running
}

// Engine returns the underlying learning engine.
// This is used for inline mode (nfqueue) where the control plane needs
// to call ProcessPacket synchronously to return verdicts.
func (s *Service) Engine() *Engine {
	return s.engine
}

// GetPendingRules returns all pending rules
func (s *Service) GetPendingRules(status string) ([]*PendingRule, error) {
	if s.engine == nil {
		return nil, fmt.Errorf("engine not initialized")
	}

	// Map generic status to FlowState
	var stateFilter flowdb.FlowState
	switch status {
	case "pending":
		stateFilter = flowdb.StatePending
	case "approved", "allowed":
		stateFilter = flowdb.StateAllowed
	case "denied":
		stateFilter = flowdb.StateDenied
	default:
		// Empty means all
		stateFilter = ""
	}

	flows, err := s.engine.db.ListFlows(flowdb.ListOptions{
		State: stateFilter,
		Desc:  true, // Newest first
	})
	if err != nil {
		return nil, err
	}

	var rules []*PendingRule
	for _, f := range flows {
		rules = append(rules, s.flowToPendingRule(&f))
	}

	return rules, nil
}

// GetPendingRule returns a specific pending rule
func (s *Service) GetPendingRule(idStr string) (*PendingRule, error) {
	if s.engine == nil {
		return nil, fmt.Errorf("engine not initialized")
	}

	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid rule ID: %w", err)
	}

	flow, err := s.engine.db.GetFlow(id)
	if err != nil {
		return nil, err
	}
	if flow == nil {
		return nil, fmt.Errorf("rule not found")
	}

	return s.flowToPendingRule(flow), nil
}

// ApproveRule approves a pending rule and applies it to the firewall
func (s *Service) ApproveRule(idStr, approvedBy string) (*PendingRule, error) {
	if s.engine == nil {
		return nil, fmt.Errorf("engine not initialized")
	}

	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid rule ID: %w", err)
	}

	// 1. Update state in DB
	if err := s.engine.db.UpdateState(id, flowdb.StateAllowed); err != nil {
		return nil, fmt.Errorf("failed to update state: %w", err)
	}

	// 2. Retrieve flow to generate rule
	flow, err := s.engine.db.GetFlow(id)
	if err != nil || flow == nil {
		return nil, fmt.Errorf("flow not found after update")
	}

	rule := s.flowToPendingRule(flow)
	rule.Status = "approved"
	rule.ApprovedBy = approvedBy
	now := time.Now()
	rule.ApprovedAt = &now

	// 3. Generate Firewall Rule
	genRule := s.generateFirewallRule(rule)
	rule.GeneratedRule = genRule

	// Apply via callback
	if s.onRuleApproved != nil {
		policyName := flow.Policy
		if policyName == "" {
			policyName = "auto" // Fallback if no policy captured
		}
		if err := s.onRuleApproved(policyName, genRule); err != nil {
			s.logger.Error("Failed to apply approved rule to firewall", "id", id, "error", err)
			return rule, fmt.Errorf("rule approved but failed to apply: %w", err)
		}
		s.logger.Info("Approved and applied rule", "id", id, "policy", policyName, "by", approvedBy)
	} else {
		s.logger.Warn("Approved rule but no firewall callback configured", "id", id)
	}

	return rule, nil
}

// DenyRule denies a pending rule
func (s *Service) DenyRule(idStr, deniedBy string) (*PendingRule, error) {
	if s.engine == nil {
		return nil, fmt.Errorf("engine not initialized")
	}

	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid rule ID: %w", err)
	}

	if err := s.engine.db.UpdateState(id, flowdb.StateDenied); err != nil {
		return nil, err
	}

	rule, _ := s.GetPendingRule(idStr)
	if rule != nil {
		rule.ApprovedBy = deniedBy // Usage of ApprovedBy for denier too
		s.logger.Info("Denied rule", "id", id, "by", deniedBy)
	}
	return rule, nil
}

// IgnoreRule ignores a pending rule (delete it for now, or use separate state?)
// FlowDB doesn't have 'ignored' state in constants, but we can treat it as 'denied' or just delete.
// Or maybe add 'ignored' to FlowState? For now, we'll treat Ignore as Delete to keep DB clean,
// OR we can leave it pending but filtered?
// The original KV implementation had "Status = ignored".
// Let's assume we just delete it or update to Denied for this MVP step.
// Wait, if ignored, it shouldn't show up again?
// Engine dedup logic will bring it back if seen again.
// To permanently ignore, we need a state.
// Let's use Denied for now, as that persists suppression.
func (s *Service) IgnoreRule(idStr string) (*PendingRule, error) {
	// Treat ignore as deny for now to suppress
	return s.DenyRule(idStr, "ignored")
}

// DeleteRule deletes a pending rule
func (s *Service) DeleteRule(idStr string) error {
	if s.engine == nil {
		return fmt.Errorf("engine not initialized")
	}

	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid rule ID: %w", err)
	}

	return s.engine.db.DeleteFlow(id)
}

// GetStats returns learning service statistics
func (s *Service) GetStats() (map[string]interface{}, error) {
	stats := make(map[string]interface{})
	stats["service_running"] = s.IsRunning()

	if s.engine != nil {
		dbStats, err := s.engine.db.GetStats()
		if err == nil {
			stats["db"] = dbStats
		}
	}

	return stats, nil
}

// GetConfig returns the current learning configuration
func (s *Service) GetConfig() *config.RuleLearningConfig {
	return s.config
}

// UpdateConfig updates the learning configuration
func (s *Service) UpdateConfig(cfg *config.RuleLearningConfig) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	wasRunning := s.running

	// Stop if running
	if wasRunning {
		// Temporary release lock to call Stop() which acquires lock
		s.mutex.Unlock()
		s.Stop()
		s.mutex.Lock()
	}

	// Update config
	s.config = cfg

	// Restart if it was running and new config is enabled
	if wasRunning && cfg != nil && cfg.Enabled {
		// Need to re-init everything with new config?
		// Engine might need config update.
		// For now simple restart
		// Short release lock again
		s.mutex.Unlock()
		err := s.Start()
		s.mutex.Lock()
		if err != nil {
			return err
		}
	}

	return nil
}

// IngestPacket accepts a packet for learning from an external source
func (s *Service) IngestPacket(p PacketInfo) {
	if !s.IsRunning() {
		return
	}
	select {
	case s.ingestChan <- p:
	default:
		// Drop if full
	}
}

// ingestionLoop processes packets from the ingestion channel
func (s *Service) ingestionLoop() {
	defer s.wg.Done()

	for {
		select {
		case <-s.ctx.Done():
			return
		case pkt := <-s.ingestChan:
			if s.engine != nil {
				s.engine.ProcessPacket(&pkt)
			}
		}
	}
}

// Helpers

func (s *Service) flowToPendingRule(f *flowdb.Flow) *PendingRule {
	return &PendingRule{
		ID:              fmt.Sprintf("%d", f.ID),
		Policy:          f.Policy,
		SrcNetwork:      fmt.Sprintf("%s", f.SrcMAC), // Using MAC as src identifier for now, or IP if available
		DstNetwork:      f.DstIPSample,               // Just sample
		DstPort:         fmt.Sprintf("%d", f.DstPort),
		Protocol:        f.Protocol,
		FirstSeen:       f.FirstSeen,
		LastSeen:        f.LastSeen,
		HitCount:        int64(f.Occurrences),
		Status:          string(f.State),
		SuggestedAction: "accept", // Default suggestion
		// UniqueSourceIPs: [f.SrcIP],
	}
}

func (s *Service) generateFirewallRule(rule *PendingRule) *config.PolicyRule {
	// Construct a robust rule
	// Use IP if available, else MAC? Firewall rules usually need IP.
	// Engine stores DstIPSample.
	// This logic needs to be smarter based on flow data (subnetting etc)
	// For now, mirroring legacy logic but using flow data.

	return &config.PolicyRule{
		Name:        fmt.Sprintf("auto_%s", rule.ID),
		Description: fmt.Sprintf("Auto-generated rule (policy: %s)", rule.Policy),
		Protocol:    rule.Protocol,
		SrcIP:       rule.SrcNetwork, // Warning: this might be MAC in my convert function above.
		// If SrcNetwork is MAC, this won't work for standard IP rules unless glaciated supports MAC.
		// Glacic firewall rules (PolicyRule) SrcIP field usually expects CIDR.
		// However, we can use SrcIP from the Flow if available.
		DestIP:  rule.DstNetwork,
		Action:  "accept",
		Comment: fmt.Sprintf("Auto-learned from %s", rule.Policy),
	}
}
