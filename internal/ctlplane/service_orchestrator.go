package ctlplane

import (
	"context"
	"fmt"
	"log"
	"sync"

	"grimm.is/glacic/internal/config"
	"grimm.is/glacic/internal/services"
)

// ServiceManager defines the interface for managing services
type ServiceManager interface {
	RegisterService(svc services.Service)
	GetService(name string) (services.Service, bool)
	GetServicesStatus() []ServiceStatus
	RestartService(name string) error
	ReloadAll(cfg *config.Config) *ReloadResult
	StartAll(ctx context.Context)
	StopAll(ctx context.Context)
}

// ServiceOrchestrator manages the lifecycle of services.
type ServiceOrchestrator struct {
	mu       sync.RWMutex
	services map[string]services.Service
}

// Ensure ServiceOrchestrator implements ServiceManager
var _ ServiceManager = (*ServiceOrchestrator)(nil)

// NewServiceOrchestrator creates a new service orchestrator.
func NewServiceOrchestrator() *ServiceOrchestrator {
	return &ServiceOrchestrator{
		services: make(map[string]services.Service),
	}
}

// RegisterService adds a service to the orchestrator.
func (so *ServiceOrchestrator) RegisterService(svc services.Service) {
	so.mu.Lock()
	defer so.mu.Unlock()
	so.services[svc.Name()] = svc
}

// GetService returns a registered service by name.
func (so *ServiceOrchestrator) GetService(name string) (services.Service, bool) {
	so.mu.RLock()
	defer so.mu.RUnlock()
	svc, ok := so.services[name]
	return svc, ok
}

// GetServicesStatus returns the status of all services.
func (so *ServiceOrchestrator) GetServicesStatus() []ServiceStatus {
	so.mu.RLock()
	defer so.mu.RUnlock()
	var statuses []ServiceStatus
	for name, svc := range so.services {
		status := svc.Status()
		statuses = append(statuses, ServiceStatus{
			Name:    name,
			Running: status.Running,
			Error:   status.Error,
		})
	}
	return statuses
}

// RestartService restarts a specific service.
func (so *ServiceOrchestrator) RestartService(name string) error {
	auditLog("RestartService", fmt.Sprintf("service=%s", name))
	so.mu.RLock()
	svc, ok := so.services[name]
	so.mu.RUnlock()
	if !ok {
		return fmt.Errorf("unknown service: %s", name)
	}

	// Full restart
	if err := svc.Stop(context.Background()); err != nil {
		log.Printf("[ORCH] Warning: failed to stop service %s: %v", name, err)
	}
	if err := svc.Start(context.Background()); err != nil {
		return fmt.Errorf("failed to start service %s: %w", name, err)
	}
	return nil
}

// ReloadResult contains the result of a ReloadAll operation.
type ReloadResult struct {
	Success        bool              // True if all services reloaded successfully
	FailedServices map[string]string // Map of service name to error message
}

// ReloadAll reloads configuration for all services.
// It ensures Firewall is reloaded first.
// Returns a ReloadResult indicating success or partial failure.
func (so *ServiceOrchestrator) ReloadAll(cfg *config.Config) *ReloadResult {
	so.mu.RLock()
	defer so.mu.RUnlock()

	result := &ReloadResult{
		Success:        true,
		FailedServices: make(map[string]string),
	}

	// 1. Reload Firewall first (critical)
	if fw, ok := so.services["Firewall"]; ok {
		if _, err := fw.Reload(cfg); err != nil {
			result.Success = false
			result.FailedServices["Firewall"] = err.Error()
			// Firewall failure is critical, but we continue to try other services
			log.Printf("[ORCH] Critical: failed to reload firewall: %v", err)
		}
	}

	// 2. Reload other services
	for name, svc := range so.services {
		if name == "Firewall" {
			continue
		}
		if _, err := svc.Reload(cfg); err != nil {
			result.Success = false
			result.FailedServices[name] = err.Error()
			log.Printf("[ORCH] Warning: failed to reload service %s: %v", name, err)
		}
	}
	return result
}

// StartAll starts all registered services.
func (so *ServiceOrchestrator) StartAll(ctx context.Context) {
	so.mu.RLock()
	defer so.mu.RUnlock()
	for name, svc := range so.services {
		if err := svc.Start(ctx); err != nil {
			log.Printf("[ORCH] Failed to start service %s: %v", name, err)
		}
	}
}

// StopAll stops all registered services.
func (so *ServiceOrchestrator) StopAll(ctx context.Context) {
	so.mu.RLock()
	defer so.mu.RUnlock()
	for name, svc := range so.services {
		if err := svc.Stop(ctx); err != nil {
			log.Printf("[ORCH] Failed to stop service %s: %v", name, err)
		}
	}
}
