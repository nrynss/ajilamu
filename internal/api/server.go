package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/nrynss/ajilamu/internal/config"
)

// LedgerFlusher is the shutdown boundary required by the durable ledger.
// Pending returns the retained event count if flushing runs out of time.
type LedgerFlusher interface {
	Flush(context.Context) error
	Pending() (int, error)
}

// ServerOptions supplies dependencies owned by other API tasks.
type ServerOptions struct {
	FrontendRoot string
	Ledger       LedgerFlusher
	Index        http.Handler
	Config       http.Handler
	Upload       http.Handler
	Sample       http.Handler
	Logger       *slog.Logger
}

// Server owns the HTTP mux and coordinates HTTP draining with ledger flushing.
type Server struct {
	cfg    *config.Config
	http   *http.Server
	ledger LedgerFlusher
	logger *slog.Logger
}

// NewServer mounts the cross-cutting API routes and the static workspace.
// Feature handlers remain independently owned and arrive through options.
func NewServer(cfg *config.Config, options ServerOptions) (*Server, error) {
	if cfg == nil {
		return nil, errors.New("server config is nil")
	}
	logger := options.Logger
	if logger == nil {
		logger = slog.Default()
	}

	frontend := FrontendUnavailable()
	if options.FrontendRoot != "" {
		var err error
		frontend, err = NewStaticHandler(options.FrontendRoot)
		if err != nil {
			return nil, fmt.Errorf("serve frontend: %w", err)
		}
	} else if cfg.Env == "production" {
		return nil, errors.New("ENV=production requires a frontend build: set AJILAMU_FRONTEND_DIR or run npm --prefix web run build")
	}

	mux := http.NewServeMux()
	mux.Handle("GET /api/healthz", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	mux.Handle("GET /api/ledger/ready", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := cfg.RequireClickHouse(); err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		if err := probeClickHouse(r.Context(), cfg); err != nil {
			http.Error(w, "ClickHouse did not answer a ping.", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write([]byte(`{"status":"ready"}`))
	}))
	if options.Index != nil {
		mux.Handle("GET /api/dubs", options.Index)
	}
	if options.Config != nil {
		mux.Handle("POST /api/config", options.Config)
	}
	if options.Upload != nil {
		mux.Handle("POST /api/dubs/new", options.Upload)
	}
	if options.Sample != nil {
		mux.Handle("POST /api/dubs/sample", options.Sample)
	}
	mux.Handle("/api/", http.NotFoundHandler())
	mux.Handle("/", frontend)

	return &Server{
		cfg:    cfg,
		ledger: options.Ledger,
		logger: logger,
		http: &http.Server{
			Handler:           mux,
			ReadHeaderTimeout: 5 * time.Second,
			IdleTimeout:       60 * time.Second,
		},
	}, nil
}

// Handler exposes the mux for black-box HTTP tests and local embedding.
func (s *Server) Handler() http.Handler {
	return s.http.Handler
}

// Serve accepts connections on listener until Shutdown closes the server.
func (s *Server) Serve(listener net.Listener) error {
	return s.http.Serve(listener)
}

// Shutdown stops new connections and drains in-flight requests. The caller
// must still call FlushLedger, even when the drain exhausts its deadline.
func (s *Server) Shutdown(ctx context.Context) error {
	if s == nil || s.http == nil {
		return nil
	}
	if err := s.http.Shutdown(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// FlushLedger flushes retained ledger events before process exit. It runs on
// its own deadline, independent of the HTTP drain. A failed flush logs the
// pending count, and a finished flush logs completion. Without a ledger it is
// a no-op.
func (s *Server) FlushLedger(ctx context.Context) error {
	if s == nil || s.ledger == nil {
		return nil
	}
	if err := s.ledger.Flush(ctx); err != nil {
		remaining, pendingErr := s.ledger.Pending()
		if pendingErr != nil {
			s.logger.Error("ledger flush failed and pending count is unavailable", "error", err, "pending_error", pendingErr)
		} else {
			s.logger.Error("ledger flush timed out or failed", "error", err, "pending", remaining)
		}
		return fmt.Errorf("flush ledger: %w", err)
	}
	s.logger.Info("ledger flush finished")
	return nil
}

// clickHouseProbeClient bounds the readiness probe so a partitioned ledger
// fails the route quickly instead of hanging it.
var clickHouseProbeClient = &http.Client{Timeout: 3 * time.Second}

// probeClickHouse observes the ClickHouse HTTP interface that the durable
// ledger writes through. Readiness requires a live answer, not just settings.
func probeClickHouse(ctx context.Context, cfg *config.Config) error {
	scheme := "http"
	if cfg.ClickHouseSecure {
		scheme = "https"
	}
	probeURL := fmt.Sprintf("%s://%s:%d/ping", scheme, cfg.ClickHouseHost, cfg.ClickHousePort)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, probeURL, nil)
	if err != nil {
		return fmt.Errorf("build ClickHouse ping: %w", err)
	}
	resp, err := clickHouseProbeClient.Do(req)
	if err != nil {
		return fmt.Errorf("ping ClickHouse: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("ClickHouse ping returned %s", resp.Status)
	}
	return nil
}
