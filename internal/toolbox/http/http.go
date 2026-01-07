// Package http provides a simple HTTP file server for tests.
package http

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
)

func Run(args []string) error {
	fs := flag.NewFlagSet("http", flag.ExitOnError)
	port := fs.Int("p", 8080, "port to listen on")
	dir := fs.String("d", ".", "directory to serve")

	if err := fs.Parse(args); err != nil {
		return err
	}

	addr := fmt.Sprintf(":%d", *port)

	server := &http.Server{
		Addr:    addr,
		Handler: http.FileServer(http.Dir(*dir)),
	}

	// Handle graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
		<-sigCh
		server.Close()
	}()

	fmt.Fprintf(os.Stderr, "Serving %s on http://127.0.0.1%s\n", *dir, addr)

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("http server error: %w", err)
	}

	return nil
}
