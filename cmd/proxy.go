package cmd

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"

	"grimm.is/glacic/internal/brand"
	"grimm.is/glacic/internal/logging"
	"grimm.is/glacic/internal/proxy"
)

// RunProxy runs the userspace proxy
// args should be the arguments after `glacic _proxy`
func RunProxy(args []string) {
	flags := flag.NewFlagSet("_proxy", flag.ExitOnError)
	listenAddr := flags.String("listen", ":8080", "Address to listen on (TCP)")
	targetSock := flags.String("target", "/run/glacic/api/api.sock", "Target Unix socket path")
	dropUser := flags.String("user", "", "User to drop privileges to")
	noChroot := flags.Bool("no-chroot", false, "Skip chroot/sandbox setup")
	tlsCert := flags.String("tls-cert", "", "TLS certificate file (enables HTTPS)")
	tlsKey := flags.String("tls-key", "", "TLS private key file")
	flags.Parse(args)

	// Logging
	logCfg := logging.DefaultConfig()
	logCfg.Output = os.Stderr
	logger := logging.New(logCfg).WithComponent("proxy")
	logging.SetDefault(logger)

	logging.Info("Starting proxy service...", "listen", *listenAddr, "target", *targetSock)

	// Create proxy server
	server := proxy.NewServer(*listenAddr, *targetSock)

	// Configure TLS BEFORE chroot/privilege drop
	// This ensures we can read the cert files from the host filesystem
	if *tlsCert != "" && *tlsKey != "" {
		if err := server.WithTLS(*tlsCert, *tlsKey); err != nil {
			logging.Error("Failed to configure TLS: " + err.Error())
			os.Exit(1)
		}
		logging.Info("TLS enabled for proxy", "cert", *tlsCert)
	}

	// Privilege Dropping
	if *dropUser != "" {
		if uid, gid, err := resolveDropUser(*dropUser); err == nil {
			// Chroot setup - skip if --no-chroot flag is set (for dev/demo mode)
			if syscall.Geteuid() == 0 && !*noChroot {
				jailPath := "/run/" + brand.LowerName + "-proxy-jail"
				if err := setupProxyChroot(jailPath, *targetSock); err != nil {
					logging.Error("Failed to setup proxy chroot: " + err.Error())
					os.Exit(1)
				}
				if err := enterChroot(jailPath); err != nil {
					logging.Error("Failed to enter proxy chroot: " + err.Error())
					os.Exit(1)
				}
				// Adjust target path to be relative to chroot root
				// If we mount /var/run/glacic/api -> /run/api
				// Then target /var/run/glacic/api/api.sock becomes /run/api/api.sock
				// Logic: setupProxyChroot handles the bind mount.
				// We need to know the mapping.
				// Assume targetSock is absolute host path.
				// We map it to fixed path inside jail.
				*targetSock = "/run/api/api.sock" // Simplified for now
				server.SetTargetSock(*targetSock)
			}

			if err := applyPrivileges(uid, gid); err != nil {
				logging.Error("Failed to drop privileges: " + err.Error())
				os.Exit(1)
			}
			logging.Info("Dropped privileges", "user", *dropUser)
		} else {
			logging.Error("Failed to resolve user: " + err.Error())
			os.Exit(1)
		}
	}
    
	// Context for shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		logging.Info("Received signal, shutting down...", "signal", sig)
		cancel()
	}()

	// Start Proxy
	if err := server.Start(ctx, nil); err != nil {
		logging.Error("Proxy failed to start: " + err.Error())
		os.Exit(1)
	}

	// Wait for context
	<-ctx.Done()
	server.Wait()
	logging.Info("Proxy exited.")
}
