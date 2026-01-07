package monitor

import (
	"fmt"
	"sync"
	"time"

	"grimm.is/glacic/internal/config"
	"grimm.is/glacic/internal/logging"

	probing "github.com/prometheus-community/pro-bing"
)

// Start checks monitors in the background.
func Start(logger *logging.Logger, routes []config.Route, wg *sync.WaitGroup, isTestMode bool) {
	if logger == nil {
		logger = logging.New(logging.DefaultConfig())
	}
	for _, r := range routes {
		if r.MonitorIP != "" {
			if isTestMode {
				wg.Add(1)
			}
			go monitorRoute(logger, r, wg, isTestMode)
		}
	}
}

func monitorRoute(logger *logging.Logger, r config.Route, wg *sync.WaitGroup, isTestMode bool) {
	if isTestMode {
		defer wg.Done()
	}

	logger.Info("Starting monitoring", "route", r.Name, "target", r.MonitorIP)

	if isTestMode {
		// In test mode, perform a single ping and exit
		err := checkPing(r.MonitorIP)
		if err != nil {
			logger.Warn("ALERT: Route is DOWN", "route", r.Name, "target", r.MonitorIP, "error", err)
		} else {
			logger.Info("Route is UP (single check in test mode)", "route", r.Name, "target", r.MonitorIP)
		}
		return
	}

	// Normal continuous monitoring
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		err := checkPing(r.MonitorIP)
		if err != nil {
			logger.Warn("ALERT: Route is DOWN", "route", r.Name, "target", r.MonitorIP, "error", err)
		} else {
			// Optional: Log successful pings only occasionally or on status change to avoid noise.
			// For this demo/test, we'll stay silent on success or maybe log every Nth time.
			// fmt.Printf("[Monitor] Route %s is UP\n", r.Name)
		}
	}
}

var CheckPingFunc = func(ip string) error {
	pinger, err := probing.NewPinger(ip)
	if err != nil {
		return fmt.Errorf("failed to create pinger: %w", err)
	}

	pinger.Count = 1
	pinger.Timeout = 1 * time.Second
	pinger.SetPrivileged(false)

	err = pinger.Run()
	if err != nil {
		return err
	}

	if pinger.Statistics().PacketsRecv == 0 {
		return fmt.Errorf("packet loss")
	}
	return nil
}

func checkPing(ip string) error {
	return CheckPingFunc(ip)
}
