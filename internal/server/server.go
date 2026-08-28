// Package server exposes the LinkedIn scraping engine over HTTP.
//
// Endpoints:
//
//	GET /v1/profile?url=<linkedin-url>   profile/company/school JSON
//	GET /healthz                         liveness probe
package server

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"linkedin-profile-api/internal/linkedin"
)

// Server serves the JSON API. The linkedin.Client is built once at startup
// and shared by all handlers: it is read-only after construction (safe for
// concurrent use) and its http.Client keeps LinkedIn connections warm.
type Server struct {
	client *linkedin.Client
	cache  *cache
	// fetchSem bounds how many LinkedIn fetches run at once. It's the
	// account-protection layer: our one LinkedIn session is the scarce
	// resource, not CPU/goroutines.
	fetchSem chan struct{}
	// flights coalesces concurrent same-URL requests into one upstream fetch.
	flights *flightGroup
}

// New returns a Server backed by the given LinkedIn client.
func New(client *linkedin.Client) *Server {
	return &Server{
		client:   client,
		cache:    newCache(),
		fetchSem: make(chan struct{}, 4),
		flights:  &flightGroup{},
	}
}

// Handler builds the routing table (Go 1.22+ method patterns) wrapped in
// the logging middleware.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/profile", s.handleProfile)
	mux.HandleFunc("GET /healthz", s.handleHealth)
	return logRequests(mux)
}

// ListenAndServe runs the HTTP server until SIGINT/SIGTERM, then shuts down
// gracefully (10s for in-flight requests to finish). addr is like ":8080".
func (s *Server) ListenAndServe(addr string) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		// A profile fetch fans out to ~11 upstream calls, max 3 concurrent,
		// each retried up to 3× on transport failure — worst case ≈ 2 minutes.
		// WriteTimeout must exceed that.
		WriteTimeout: 120 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		log.Printf("linkedin-profile-api listening on %s", addr)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		log.Println("shutting down...")
		shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutCtx)
	case err := <-errCh:
		return err
	}
}

// statusWriter records the response status so the logging middleware can
// report it.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// logRequests logs one line per request: method path status duration.
func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		log.Printf("%s %s %d %s",
			r.Method, r.URL.Path, sw.status, time.Since(start).Round(time.Millisecond))
	})
}
