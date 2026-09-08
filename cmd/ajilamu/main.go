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
	var history api.HistoryReader
	if cfg.RequireClickHouse() == nil {
		eventLedger, err = ledger.New(cfg, filepath.Join(cfg.DataDir, "ledger-queue"))
		if err != nil {
			return fmt.Errorf("open ledger: %w", err)
		}
		defer eventLedger.Close()
		ledgerFlusher = eventLedger
		history = newHistoryReader(eventLedger)
	}

	uploadDir := filepath.Join(cfg.DataDir, "uploads")
	fixtureSummary := api.DubSummary{
		ID:               dub.ID,
		Title:            dub.Title,
		Languages:        languageCodes(dub),
		Readiness:        dub.Readiness,
		TotalNanodollars: dub.Total.TotalNanodollars,
		CreatedAt:        dub.CreatedAt,
		UpdatedAt:        dub.UpdatedAt,
	}
	server, err := api.NewServer(cfg, api.ServerOptions{
		FrontendRoot: frontendRoot(cfg),
		Ledger:       ledgerFlusher,
		History:      history,
		Upload:       api.NewUploadHandler(uploadDir),
		Sample:       api.NewSampleHandler(uploadDir),
		Index: api.IndexHandlerFrom(func() []api.DubSummary {
			return append([]api.DubSummary{fixtureSummary}, api.ListUploadSummaries(uploadDir)...)
		}),
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

// historyReader adapts the ledger client to the api read interface.
type historyReader struct {
	client *ledger.Client
}

// The adapter satisfies the routes without internal/api importing internal/ledger.
var _ api.HistoryReader = (*historyReader)(nil)

// newHistoryReader returns a reader for client.
// A nil client returns a nil interface, so a typed nil never reaches the routes.
func newHistoryReader(client *ledger.Client) api.HistoryReader {
	if client == nil {
		return nil
	}
	return &historyReader{client: client}
}

// ListCommits returns the dub's commits as wire commits.
func (h *historyReader) ListCommits(ctx context.Context, dubID string) ([]api.Commit, error) {
	rows, err := h.client.ListCommits(ctx, dubID)
	if err != nil {
		return nil, err
	}
	return commitHistory(rows), nil
}

// TimelineAt returns the commit's timeline as wire entries.
func (h *historyReader) TimelineAt(ctx context.Context, dubID, language, commitID string) ([]api.TimelineEntry, error) {
	segments, err := h.client.TimelineAt(ctx, dubID, language, commitID)
	if err != nil {
		return nil, err
	}
	return timelineEntries(segments), nil
}

// CompareBranches returns both heads as a wire comparison.
func (h *historyReader) CompareBranches(ctx context.Context, dubID, language, commitA, commitB string) (api.BranchComparison, error) {
	compare, err := h.client.CompareBranches(ctx, dubID, language, commitA, commitB)
	if err != nil {
		return api.BranchComparison{}, err
	}
	return branchComparison(compare), nil
}

// commitHistory maps ledger rows onto wire commits.
// VersionSeq becomes VersionNumber because the wire counts along the chain.
func commitHistory(rows []ledger.CommitHistoryRow) []api.Commit {
	commits := make([]api.Commit, 0, len(rows))
	for _, row := range rows {
		commits = append(commits, api.Commit{
			CommitID:       row.CommitID,
			ParentCommitID: row.ParentCommitID,
			VersionNumber:  int(row.VersionSeq),
			CreatedAt:      row.CreatedAt,
			Action:         row.Action,
			Author:         row.Author,
			Instruction:    row.Instruction,
		})
	}
	return commits
}

// timelineEntries maps ledger segments onto wire timeline entries.
func timelineEntries(segments []ledger.TimelineSegment) []api.TimelineEntry {
	entries := make([]api.TimelineEntry, 0, len(segments))
	for _, segment := range segments {
		entries = append(entries, api.TimelineEntry{
			SegmentIndex: int(segment.SegmentIndex),
			StartMs:      segment.StartMs,
			EndMs:        segment.EndMs,
			Speaker:      segment.Speaker,
			Emotion:      segment.Emotion,
			SourceText:   segment.SourceText,
			Text:         segment.Text,
			TakeID:       segment.TakeID,
			VersionSeq:   segment.VersionSeq,
		})
	}
	return entries
}

// branchComparison maps both ledger heads onto the wire comparison.
func branchComparison(compare ledger.BranchCompare) api.BranchComparison {
	return api.BranchComparison{A: branchSummary(compare.A), B: branchSummary(compare.B)}
}

// branchSummary maps one ledger head onto the wire summary.
// AttributedCostUSD stays a decimal string, so the exact value survives transport.
func branchSummary(view ledger.BranchView) api.BranchSummary {
	return api.BranchSummary{
		CommitID:          view.CommitID,
		Branch:            view.Branch,
		SlotMs:            view.SlotMs,
		TakeCount:         view.TakeCount,
		AttributedCostUSD: view.AttributedCostUSD,
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
