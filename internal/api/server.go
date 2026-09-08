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

// HistoryReader reads ledger history for the workspace routes.
// It names api wire types only, so this package never imports internal/ledger.
type HistoryReader interface {
	// ListCommits returns every commit of one dub, oldest first.
	ListCommits(ctx context.Context, dubID string) ([]Commit, error)
	// TimelineAt returns the timeline snapshot at one commit.
	TimelineAt(ctx context.Context, dubID, language, commitID string) ([]TimelineEntry, error)
	// CompareBranches returns metrics for two heads of one language track.
	CompareBranches(ctx context.Context, dubID, language, commitA, commitB string) (BranchComparison, error)
}

// ServerOptions supplies dependencies owned by other API tasks.
type ServerOptions struct {
	FrontendRoot     string
	Ledger           LedgerFlusher
	Index            http.Handler
	History          HistoryReader
	Workspace        WorkspaceReader
	Project          ProjectLookup
	Config           http.Handler
	ConfigPresence   http.Handler
	Languages        http.Handler
	LanguagesRefresh http.Handler
	Upload           http.Handler
	Sample           http.Handler
	Runner           PipelineRunner
	// Rerender re-runs one dialogue line through the fit loop.
	Rerender   LineRenderer
	Recorder   RunRecorder
	StorageDir string
	Logger     *slog.Logger
}

// Server owns the HTTP mux and coordinates HTTP draining with ledger flushing.
type Server struct {
	cfg    *config.Config
	http   *http.Server
	ledger LedgerFlusher
	runs   *runRegistry
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

// methodNotAllowed answers a route that exists for another method.
func methodNotAllowed(allow string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Allow", allow)
		http.Error(w, "Method not allowed.", http.StatusMethodNotAllowed)
	})
}

// ConfigPresence reports which write-only credentials exist. It never carries
// a credential value, so the read route cannot leak one.
type ConfigPresence struct {
	VoiceKey       bool `json:"voice_key_set"`
	TranslationKey bool `json:"translation_key_set"`
}

// ConfigPresenceHandler serves the credential presence read route. report must
// never return a credential value. A nil report answers 503, matching
// ConfigHandler, so an unwired store cannot claim a setting exists.
func ConfigPresenceHandler(report func() (ConfigPresence, error)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "Method not allowed.", http.StatusMethodNotAllowed)
			return
		}
		if report == nil {
			http.Error(w, "Settings are unavailable.", http.StatusServiceUnavailable)
			return
		}
		presence, err := report()
		if err != nil {
			http.Error(w, "Settings could not be read.", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(presence)
	})
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
	if options.ConfigPresence != nil {
		mux.Handle("GET /api/config", options.ConfigPresence)
	}
	if options.Languages != nil {
		mux.Handle("GET /api/languages", options.Languages)
	}
	if options.LanguagesRefresh != nil {
		mux.Handle("POST /api/languages/refresh", options.LanguagesRefresh)
	}
	if options.Upload != nil {
		mux.Handle("POST /api/dubs/new", options.Upload)
	}
	if options.Sample != nil {
		mux.Handle("POST /api/dubs/sample", options.Sample)
	}
	mux.Handle("GET /api/dubs/{id}/history", HistoryHandlerFrom(options.History, logger))
	mux.Handle("GET /api/dubs/{id}/timeline", TimelineHandlerFrom(options.History, logger))
	mux.Handle("GET /api/dubs/{id}/branches", BranchCompareHandlerFrom(options.History, logger))
	// The creation endpoints are POST only. Their names are reserved, so the
	// workspace wildcard must not serve them as project ids.
	mux.Handle("GET /api/dubs/new", methodNotAllowed(http.MethodPost))
	mux.Handle("GET /api/dubs/sample", methodNotAllowed(http.MethodPost))
	runs := newRunRegistry(options.Runner, options.Recorder, logger)
	mux.Handle("GET /api/dubs/{id}", WorkspaceHandlerFrom(options.Workspace, options.History, options.Project, runs.active, logger))
	mux.Handle("POST /api/dubs/{id}/run", RunStartHandler(runs, options.StorageDir))
	mux.Handle("POST /api/dubs/{id}/run/cancel", RunCancelHandler(runs))
	mux.Handle("GET /api/dubs/{id}/events", EventsHandler(runs))
	mux.Handle("POST /api/dubs/{id}/lines/{segment}/rerender", RerenderHandler(options.Rerender, options.Recorder, options.History, options.StorageDir, runs.active, logger))
	mux.Handle("POST /api/editor/commands/preview", NewCommandPreviewHandler())
	mux.Handle("/api/", http.NotFoundHandler())
	mux.Handle("/", frontend)

	return &Server{
		cfg:    cfg,
		ledger: options.Ledger,
		runs:   runs,
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

// Shutdown stops new connections and drains in-flight requests. It cancels
// every active run first, so an event stream closes and the drain can finish,
// and it waits a bounded time for those runs to stop before it returns. The
// caller must still call FlushLedger, even when the drain exhausts its deadline.
func (s *Server) Shutdown(ctx context.Context) error {
	if s == nil || s.http == nil {
		return nil
	}
	if s.runs != nil {
		s.runs.cancelAll()
	}
	err := s.http.Shutdown(ctx)
	if s.runs != nil {
		s.runs.wait(ctx)
	}
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
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
