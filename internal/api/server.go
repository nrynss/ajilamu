package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptrace"
	"sync/atomic"
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

// Ledger readiness outcomes. The route reports exactly one of these.
const (
	ledgerStatusReady         = "ready"
	ledgerStatusWaking        = "waking"
	ledgerStatusUnreachable   = "unreachable"
	ledgerStatusMisconfigured = "misconfigured"
)

// writeLedgerReady answers the readiness route as JSON. Only ready is a 200,
// and detail carries the missing credential when the config is incomplete.
func writeLedgerReady(w http.ResponseWriter, status, detail string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if status != ledgerStatusReady {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	_ = json.NewEncoder(w).Encode(struct {
		Status string `json:"status"`
		Detail string `json:"detail,omitempty"`
	}{Status: status, Detail: detail})
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
			writeLedgerReady(w, ledgerStatusMisconfigured, err.Error())
			return
		}
		writeLedgerReady(w, probeClickHouse(r.Context(), cfg), "")
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

// probeDialTimeout bounds connection establishment for the readiness probe.
// A refused or unresolved host fails here instead of waiting out the budget.
var probeDialTimeout = 2 * time.Second

// probeResponseBudget bounds the whole readiness probe. Measured against the
// live ClickHouse Cloud instance on 2026-09-08: a warm ping answered in under
// a second, but the first ping on a cold route took 13 seconds. This budget
// covers that cold ping with margin, so a waking service reports ready. An
// idle authenticated call needed more than 25 seconds, so that case reports
// waking rather than claiming ClickHouse did not answer.
var probeResponseBudget = 15 * time.Second

// probeClickHouse observes the ClickHouse HTTP interface that the durable
// ledger writes through. It reports ready, waking, or unreachable so the
// route can tell a healthy service that is waking from a broken one.
func probeClickHouse(ctx context.Context, cfg *config.Config) string {
	scheme := "http"
	if cfg.ClickHouseSecure {
		scheme = "https"
	}

	// A response timeout and a dial timeout look alike, so record whether the
	// TCP connection came up. Waking means the budget expired after connect.
	// Every other post-connect failure is unreachable, so a wrong scheme or a
	// reset cannot report waking forever.
	var connected atomic.Bool
	trace := &httptrace.ClientTrace{
		ConnectDone: func(_, _ string, err error) {
			if err == nil {
				connected.Store(true)
			}
		},
	}
	ctx, cancel := context.WithTimeout(httptrace.WithClientTrace(ctx, trace), probeResponseBudget)
	defer cancel()

	// Keep-alives off forces one dial per probe, which keeps the
	// connected-against-not decision deterministic. Redirects stay unfollowed
	// so one probe never opens a second connection.
	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Transport: &http.Transport{
			DialContext:       (&net.Dialer{Timeout: probeDialTimeout}).DialContext,
			DisableKeepAlives: true,
		},
	}

	probeURL := fmt.Sprintf("%s://%s:%d/ping", scheme, cfg.ClickHouseHost, cfg.ClickHousePort)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, probeURL, nil)
	if err != nil {
		return ledgerStatusUnreachable
	}
	resp, err := client.Do(req)
	if err != nil {
		if connected.Load() && probeTimedOut(err) {
			return ledgerStatusWaking
		}
		return ledgerStatusUnreachable
	}
	// The status line answers the probe, so the body stays unread. A stalled
	// /ping body therefore cannot hold the route for the whole budget.
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return ledgerStatusUnreachable
	}
	return ledgerStatusReady
}

// probeTimedOut reports whether a probe failure ran out of time. Waking means
// the response budget expired, never that an error followed a connection.
func probeTimedOut(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var timeout net.Error
	return errors.As(err, &timeout) && timeout.Timeout()
}
