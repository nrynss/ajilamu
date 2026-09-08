package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/nrynss/ajilamu/internal/api"
	"github.com/nrynss/ajilamu/internal/config"
	"github.com/nrynss/ajilamu/internal/fixtures"
	"github.com/nrynss/ajilamu/internal/ledger"
)

const shutdownTimeout = 15 * time.Second

// The drain and the flush split shutdownTimeout into separate budgets. A slow
// in-flight request can consume the drain budget without starving the flush.
const (
	drainTimeout = 10 * time.Second
	flushTimeout = shutdownTimeout - drainTimeout
)

func main() {
	if err := run(); err != nil {
		slog.Error("Ajilamu stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	dub, err := fixtures.LoadDub()
	if err != nil {
		return fmt.Errorf("load fixture workspace: %w", err)
	}
	var eventLedger *ledger.Client
	var ledgerFlusher api.LedgerFlusher
	if cfg.RequireClickHouse() == nil {
		eventLedger, err = ledger.New(cfg, filepath.Join(cfg.DataDir, "ledger-queue"))
		if err != nil {
			return fmt.Errorf("open ledger: %w", err)
		}
		defer eventLedger.Close()
		ledgerFlusher = eventLedger
	}

	server, err := api.NewServer(cfg, api.ServerOptions{
		FrontendRoot: frontendRoot(cfg),
		Ledger:       ledgerFlusher,
		Upload:       api.NewUploadHandler(filepath.Join(cfg.DataDir, "uploads")),
		Index: api.IndexHandler([]api.DubSummary{{
			ID:               dub.ID,
			Title:            dub.Title,
			Languages:        languageCodes(dub),
			Readiness:        dub.Readiness,
			TotalNanodollars: dub.Total.TotalNanodollars,
			CreatedAt:        dub.CreatedAt,
			UpdatedAt:        dub.UpdatedAt,
		}}),
		// T7.4a lands persistence. Until then the handler answers 503.
		Config: api.ConfigHandler(nil),
	})
	if err != nil {
		return err
	}

	listener, err := net.Listen("tcp", ":"+cfg.Port)
	if err != nil {
		return fmt.Errorf("listen on port %s: %w", cfg.Port, err)
	}
	defer listener.Close()

	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(listener) }()
	slog.Info("Ajilamu listening", "address", listener.Addr().String(), "env", cfg.Env)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	select {
	case err := <-serveErr:
		if errors.Is(err, net.ErrClosed) || errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		drainCtx, cancelDrain := context.WithTimeout(context.Background(), drainTimeout)
		drainErr := server.Shutdown(drainCtx)
		cancelDrain()
		if drainErr != nil {
			slog.Error("HTTP drain did not finish", "error", drainErr)
		}
		flushCtx, cancelFlush := context.WithTimeout(context.Background(), flushTimeout)
		flushErr := server.FlushLedger(flushCtx)
		cancelFlush()
		if err := <-serveErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		if drainErr != nil {
			return fmt.Errorf("drain HTTP requests: %w", drainErr)
		}
		return flushErr
	}
}

// frontendRoot resolves the static build directory. AJILAMU_FRONTEND_DIR wins
// when set. Otherwise the entrypoint discovers a build beside its working
// directory.
func frontendRoot(cfg *config.Config) string {
	if cfg.FrontendDir != "" {
		return cfg.FrontendDir
	}
	return findFrontendRoot()
}

func findFrontendRoot() string {
	for _, candidate := range []string{
		filepath.Join("web", "build"),
		filepath.Join("web", "dist"),
	} {
		if info, err := os.Stat(filepath.Join(candidate, "index.html")); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return ""
}

func languageCodes(dub *api.Dub) []string {
	languages := make([]string, 0, len(dub.Languages))
	for _, track := range dub.Languages {
		languages = append(languages, track.Language)
	}
	return languages
}
