package api

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"time"
)

// accessLogWriter wraps http.ResponseWriter to capture the status code
type accessLogWriter struct {
	http.ResponseWriter
	status int
	size   int
}

func (rw *accessLogWriter) WriteHeader(status int) {
	rw.status = status
	rw.ResponseWriter.WriteHeader(status)
}

func (rw *accessLogWriter) Write(b []byte) (int, error) {
	if rw.status == 0 {
		rw.status = http.StatusOK
	}
	size, err := rw.ResponseWriter.Write(b)
	rw.size += size
	return size, err
}

func (rw *accessLogWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := rw.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	return h.Hijack()
}

// AccessLogger middleware logs all HTTP requests
func AccessLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		fmt.Fprintf(os.Stderr, "[DEBUG] AccessLogger: Request start %s %s\n", r.Method, r.URL.Path)
		// log.Printf("[DEBUG] AccessLogger: Request start %s %s", r.Method, r.URL.Path)

		rw := &accessLogWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(rw, r)

		duration := time.Since(start)

		// Log format: [ACCESS] Method Path IP Status Size Duration
		log.Printf("[ACCESS] %s %s %s %d %d %s",
			r.Method,
			r.URL.Path,
			r.RemoteAddr,
			rw.status,
			rw.size,
			duration,
		)
	})
}
