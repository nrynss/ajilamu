package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/nrynss/ajilamu/internal/api"
	"github.com/nrynss/ajilamu/internal/assemble"
	"github.com/nrynss/ajilamu/internal/config"
	"github.com/nrynss/ajilamu/internal/cost"
	"github.com/nrynss/ajilamu/internal/fit"
	"github.com/nrynss/ajilamu/internal/fixtures"
	"github.com/nrynss/ajilamu/internal/gemini"
	"github.com/nrynss/ajilamu/internal/ledger"
	"github.com/nrynss/ajilamu/internal/tts"
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
	var workspace api.WorkspaceReader
	var runRecorder api.RunRecorder
	var pipelineRunner api.PipelineRunner
	if cfg.RequireClickHouse() == nil {
		eventLedger, err = ledger.New(cfg, filepath.Join(cfg.DataDir, "ledger-queue"))
		if err != nil {
			return fmt.Errorf("open ledger: %w", err)
		}
		defer eventLedger.Close()
		ledgerFlusher = eventLedger
		history = newHistoryReader(eventLedger)
		workspace = newWorkspaceReader(eventLedger)
		runRecorder = newRunRecorder(eventLedger, cfg.GeminiModel, slog.Default())
	}
	if cfg.RequireGoogleCloud() == nil {
		runner, err := newPipelineRunner(cfg, cost.DefaultRateCard())
		if err != nil {
			slog.Warn("dubbing runs are unavailable", "error", err)
		} else {
			pipelineRunner = runner
		}
	}

	settings, err := config.OpenSettings(cfg.DataDir)
	if err != nil {
		return fmt.Errorf("open settings store: %w", err)
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
		Workspace:    workspace,
		Project:      api.UploadProjectLookup(uploadDir),
		Runner:       pipelineRunner,
		Recorder:     runRecorder,
		StorageDir:   uploadDir,
		Upload:       api.NewUploadHandler(uploadDir),
		Sample:       api.NewSampleHandler(uploadDir),
		Index: api.IndexHandlerFrom(func() []api.DubSummary {
			return append([]api.DubSummary{fixtureSummary}, api.ListUploadSummaries(uploadDir)...)
		}),
		// T7.4a persists submitted credentials under AJILAMU_DATA_DIR. The
		// store never returns a value, so the read route reports presence only.
		Config:         api.ConfigHandler(newConfigSaver(settings)),
		ConfigPresence: api.ConfigPresenceHandler(newConfigPresence(settings)),
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

// newConfigSaver adapts the write-only settings form onto the durable store.
// internal/config cannot import internal/api, so the store cannot take a
// ConfigUpdate. This adapter is the only seam.
func newConfigSaver(store *config.SettingsStore) func(api.ConfigUpdate) error {
	return func(update api.ConfigUpdate) error {
		voiceKey, _ := update.VoiceKey()
		translationKey, _ := update.TranslationKey()
		return store.SaveCredentials(voiceKey, translationKey)
	}
}

// newConfigPresence adapts the store presence onto the read route. It maps
// booleans only, so a credential value never leaves internal/config.
func newConfigPresence(store *config.SettingsStore) func() (api.ConfigPresence, error) {
	return func() (api.ConfigPresence, error) {
		presence, err := store.Presence()
		if err != nil {
			return api.ConfigPresence{}, err
		}
		return api.ConfigPresence{
			VoiceKey:       presence.VoiceKey,
			TranslationKey: presence.TranslationKey,
		}, nil
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

// workspaceReader adapts the ledger client to the api workspace interface.
// The ledger reads already return wire types, so each method forwards.
type workspaceReader struct {
	client *ledger.Client
}

// The adapter satisfies the workspace route without internal/api importing internal/ledger.
var _ api.WorkspaceReader = (*workspaceReader)(nil)

// newWorkspaceReader returns a reader for client.
// A nil client returns a nil interface, so a typed nil never reaches the route.
func newWorkspaceReader(client *ledger.Client) api.WorkspaceReader {
	if client == nil {
		return nil
	}
	return &workspaceReader{client: client}
}

// WorkspaceTakes returns one track per target language.
func (w *workspaceReader) WorkspaceTakes(ctx context.Context, dubID string) ([]api.LanguageTrack, error) {
	return w.client.WorkspaceTakes(ctx, dubID)
}

// WholePassCharges returns the charges no single take owns.
func (w *workspaceReader) WholePassCharges(ctx context.Context, dubID string) ([]api.Charge, error) {
	return w.client.WholePassCharges(ctx, dubID)
}

// RunningTotal returns the exact running sum and what it covers.
func (w *workspaceReader) RunningTotal(ctx context.Context, dubID string) (api.Total, error) {
	return w.client.RunningTotal(ctx, dubID)
}

// Languages returns the target language codes.
func (w *workspaceReader) Languages(ctx context.Context, dubID string) ([]string, error) {
	return w.client.Languages(ctx, dubID)
}

// ProjectMetadata returns the identity and the timestamps.
func (w *workspaceReader) ProjectMetadata(ctx context.Context, dubID string) (api.DubSummary, error) {
	return w.client.ProjectMetadata(ctx, dubID)
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

// findFrontendRoot returns the declared build output, web/build. The probe
// refuses a stale web/dist, because nothing emits there and the adapter
// writes only web/build.
func findFrontendRoot() string {
	const build = "web/build"
	if info, err := os.Stat(filepath.Join(build, "index.html")); err == nil && !info.IsDir() {
		return build
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

// runBranch names the single history branch a run writes.
const runBranch = "main"

// pipelineRunner adapts the fit pipeline and the assembler onto api.PipelineRunner.
// internal/api cannot import internal/fit, so this adapter is the only seam.
// The clients are built once, so no run leaks a gRPC connection. Runs are
// serialized, so the charge router always has exactly one active target.
type pipelineRunner struct {
	segmenter   gemini.Segmenter
	translator  gemini.Translator
	synthesizer tts.Synthesizer
	router      *chargeRouter
	mu          sync.Mutex
}

var _ api.PipelineRunner = (*pipelineRunner)(nil)

// newPipelineRunner builds the production clients. A nil config or an
// incomplete Google Cloud project returns a nil interface, never a typed nil.
func newPipelineRunner(cfg *config.Config, card cost.RateCard) (api.PipelineRunner, error) {
	if cfg == nil || cfg.RequireGoogleCloud() != nil {
		return nil, nil
	}
	router := &chargeRouter{}
	segmenter, err := gemini.NewSegmenter(cfg, router, card, nil)
	if err != nil {
		return nil, fmt.Errorf("open the segmenter: %w", err)
	}
	translator, err := gemini.NewTranslator(cfg, router, card, nil)
	if err != nil {
		return nil, fmt.Errorf("open the translator: %w", err)
	}
	synthesizer, err := tts.NewSynthesizer(cfg, router, card, nil)
	if err != nil {
		return nil, fmt.Errorf("open the synthesizer: %w", err)
	}
	return &pipelineRunner{
		segmenter:   segmenter,
		translator:  translator,
		synthesizer: synthesizer,
		router:      router,
	}, nil
}

// Run segments, translates, synthesizes, fits, and assembles one dub.
func (p *pipelineRunner) Run(ctx context.Context, req api.RunRequest, emit func(api.ProgressEvent)) (api.RunResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	charges := cost.NewLedger()
	p.router.set(charges)
	defer p.router.set(nil)

	source, err := os.ReadFile(req.Source)
	if err != nil {
		return api.RunResult{}, fmt.Errorf("read the source video: %w", err)
	}
	emit(api.ProgressEvent{
		Type:     api.EventProgress,
		Stage:    api.StageSegmenting,
		Sentence: "Reading the source video.",
		Language: req.Language,
	})
	result, err := fit.RunPipeline(ctx, fit.PipelineConfig{
		Segmenter:   p.segmenter,
		InputMedia:  &gemini.Input{Data: source, MIMEType: mediaType(req.Source)},
		Translator:  p.translator,
		Synthesizer: p.synthesizer,
		Language:    req.Language,
		WorkDir:     req.WorkDir,
		Recorder:    charges,
		ProgressFn:  fit.ProgressFunc(emit),
	})
	if err != nil {
		return api.RunResult{}, err
	}
	if err := assembleRun(ctx, req, result, emit); err != nil {
		return api.RunResult{}, err
	}
	return pipelineRunResult(ctx, result, assemble.Peaks)
}

// chargeRouter sends every client charge to the ledger of the active run.
// A run holds the runner mutex, so the target never changes mid-run.
type chargeRouter struct {
	mu     sync.Mutex
	target *cost.Ledger
}

// set points the router at the ledger of the run that is starting or ending.
func (r *chargeRouter) set(target *cost.Ledger) {
	r.mu.Lock()
	r.target = target
	r.mu.Unlock()
}

// Add records one charge on the active run ledger.
func (r *chargeRouter) Add(charge cost.Charge) {
	r.mu.Lock()
	target := r.target
	r.mu.Unlock()
	if target != nil {
		target.Add(charge)
	}
}

// assembleRun builds the bed, places the takes, ducks the mix, and exports the film.
func assembleRun(ctx context.Context, req api.RunRequest, result *fit.PipelineResult, emit func(api.ProgressEvent)) error {
	emit(api.ProgressEvent{
		Type:             api.EventProgress,
		Stage:            api.StageAssembling,
		Sentence:         "Building the background bed from the film audio.",
		Language:         req.Language,
		TotalNanodollars: result.TotalCost,
	})
	bed, err := assemble.BuildBed(ctx, req.Source, req.Music, filepath.Join(req.WorkDir, "bed.wav"))
	if err != nil {
		return fmt.Errorf("build the audio bed: %w", err)
	}
	clips := make([]assemble.Clip, 0, len(result.Lines))
	for _, line := range result.Lines {
		if line.ChosenTake.File == "" {
			continue
		}
		clips = append(clips, assemble.Clip{Segment: line.Segment, File: line.ChosenTake.File})
	}
	emit(api.ProgressEvent{
		Type:             api.EventProgress,
		Stage:            api.StageAssembling,
		Sentence:         fmt.Sprintf("Placing %d takes into their slots.", len(clips)),
		Language:         req.Language,
		TotalNanodollars: result.TotalCost,
	})
	placement, err := bed.Place(ctx, clips, filepath.Join(req.WorkDir, "speech.wav"))
	if err != nil {
		return fmt.Errorf("place the takes: %w", err)
	}
	emit(api.ProgressEvent{
		Type:             api.EventProgress,
		Stage:            api.StageAssembling,
		Sentence:         "Mixing the speech over the bed.",
		Language:         req.Language,
		TotalNanodollars: result.TotalCost,
	})
	mixFile := filepath.Join(req.WorkDir, "mix.wav")
	if err := bed.Duck(ctx, placement.File, mixFile); err != nil {
		return fmt.Errorf("duck the bed under the speech: %w", err)
	}
	emit(api.ProgressEvent{
		Type:             api.EventProgress,
		Stage:            api.StageExporting,
		Sentence:         "Exporting the dubbed film.",
		Language:         req.Language,
		TotalNanodollars: result.TotalCost,
	})
	if err := assemble.Export(ctx, req.Source, mixFile, filepath.Join(req.WorkDir, assemble.DubbedDucked)); err != nil {
		return fmt.Errorf("export the dubbed film: %w", err)
	}
	return nil
}

// peakReader sketches one take file for the timeline.
type peakReader func(ctx context.Context, path string) ([]uint8, error)

// pipelineRunResult maps a fit pipeline result onto the api run result.
// Charges follow their segment. A charge no take owns stays whole-pass until
// Persist attributes it to the first rendered take.
func pipelineRunResult(ctx context.Context, result *fit.PipelineResult, peaks peakReader) (api.RunResult, error) {
	if result == nil {
		return api.RunResult{}, errors.New("pipeline result is nil")
	}
	rendered := make(map[int]struct{}, len(result.Lines))
	for _, line := range result.Lines {
		rendered[line.Segment.ID] = struct{}{}
	}
	bySegment := make(map[int][]cost.Charge, len(rendered))
	var wholePass []cost.Charge
	for _, charge := range result.Charges {
		if _, ok := rendered[charge.TakeID]; ok {
			bySegment[charge.TakeID] = append(bySegment[charge.TakeID], charge)
			continue
		}
		wholePass = append(wholePass, charge)
	}
	out := api.RunResult{
		FlaggedSegments:  result.FlaggedSegments,
		TotalCost:        result.TotalCost,
		WholePassCharges: wholePass,
	}
	for _, line := range result.Lines {
		take := line.ChosenTake
		if take.File == "" {
			continue
		}
		attempt := chosenAttempt(line)
		var waveform []uint8
		if peaks != nil {
			sketch, err := peaks(ctx, take.File)
			if err != nil {
				return api.RunResult{}, fmt.Errorf("sketch take peaks for line %d: %w", line.Segment.ID, err)
			}
			waveform = sketch
		}
		out.Takes = append(out.Takes, api.RunTake{
			Segment:      line.Segment,
			Take:         take,
			Voice:        voiceName(result.Voices, line.Segment.Speaker.Name),
			Repair:       attempt.Repair,
			RepairDetail: attempt.RepairDetail,
			Charges:      bySegment[line.Segment.ID],
			Peaks:        waveform,
		})
		out.Timeline = append(out.Timeline, api.RunSegmentState{
			SegmentIndex: line.Segment.ID,
			StartMs:      line.Segment.StartMs,
			EndMs:        line.Segment.EndMs,
			Speaker:      line.Segment.Speaker.Name,
			Emotion:      line.Segment.Emotion,
			SourceText:   line.Segment.Text,
			Text:         attempt.Text,
		})
	}
	return out, nil
}

// chosenAttempt returns the attempt the line chose.
func chosenAttempt(line fit.LineResult) fit.LineAttempt {
	for _, attempt := range line.Attempts {
		if attempt.Attempt == line.ChosenTake.Attempt {
			return attempt
		}
	}
	return fit.LineAttempt{}
}

// voiceName returns the voice profile assigned to one speaker.
func voiceName(voices map[string]tts.Voice, speaker string) string {
	return voices[speaker].Name
}

// mediaType names the source container for the segmenter.
func mediaType(path string) string {
	if kind := mime.TypeByExtension(filepath.Ext(path)); kind != "" {
		return kind
	}
	return "application/octet-stream"
}

// runRecorder adapts the ledger client onto api.RunRecorder.
type runRecorder struct {
	client   *ledger.Client
	provider string
	logger   *slog.Logger
}

var _ api.RunRecorder = (*runRecorder)(nil)

// newRunRecorder returns nil for a nil client, so a typed nil never reaches a route.
func newRunRecorder(client *ledger.Client, provider string, logger *slog.Logger) api.RunRecorder {
	if client == nil {
		return nil
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &runRecorder{client: client, provider: provider, logger: logger}
}

// Persist writes the run in ledger order. It flushes the durable queue first,
// so a parent commit still queued is delivered before the head read, and again
// at the end, so every row reaches ClickHouse before the run reports done.
func (r *runRecorder) Persist(ctx context.Context, req api.RunRequest, result api.RunResult) error {
	if r.client == nil {
		return errors.New("run recorder has no ledger client")
	}
	if err := r.client.Flush(ctx); err != nil {
		return fmt.Errorf("flush the ledger before the run commit: %w", err)
	}
	commits, err := r.client.ListCommits(ctx, req.DubID)
	if err != nil {
		return fmt.Errorf("read the dub head: %w", err)
	}
	parent := ""
	version := uint64(1)
	if len(commits) > 0 {
		head := commits[len(commits)-1]
		parent = head.CommitID
		version = head.VersionSeq + 1
	}
	commit := ledger.Commit{
		CommitID:       result.CommitID,
		ParentCommitID: parent,
		ProjectID:      result.ProjectID,
		DubID:          req.DubID,
		OwnerID:        result.OwnerID,
		Branch:         runBranch,
		Language:       req.Language,
		VersionSeq:     version,
		Message:        fmt.Sprintf("Rendered %d lines into %s.", len(result.Takes), req.Language),
	}
	if err := r.client.AppendCommit(ctx, commit); err != nil {
		return fmt.Errorf("append the run commit: %w", err)
	}
	action := ledger.Action{
		CommitID:     result.CommitID,
		ProjectID:    result.ProjectID,
		DubID:        req.DubID,
		OwnerID:      result.OwnerID,
		Language:     req.Language,
		SegmentIndex: -1,
		ActionType:   api.ActionTakeRendered,
		Author:       api.AuthorAgent,
	}
	if err := r.client.RecordAction(ctx, action); err != nil {
		return fmt.Errorf("record the run action: %w", err)
	}
	if len(result.Takes) == 0 && len(result.WholePassCharges) > 0 {
		r.logger.Warn("run rendered no takes, so whole-pass charges have no owner",
			"dub_id", req.DubID, "charges", len(result.WholePassCharges))
	}
	for i, take := range result.Takes {
		charges := take.Charges
		if i == 0 && len(result.WholePassCharges) > 0 {
			charges = append(append([]cost.Charge(nil), take.Charges...),
				attributeWholePass(result.WholePassCharges, take.Segment.ID)...)
		}
		attempt := ledger.TakeAttempt{
			TakeID:         take.TakeID,
			CommitID:       result.CommitID,
			ProjectID:      result.ProjectID,
			DubID:          req.DubID,
			OwnerID:        result.OwnerID,
			Language:       req.Language,
			Voice:          take.Voice,
			ChargeProvider: r.provider,
			RepairDetail:   take.RepairDetail,
			Repair:         take.Repair,
			Segment:        take.Segment,
			Take:           take.Take,
			Charges:        charges,
			Peaks:          take.Peaks,
		}
		if err := r.client.RecordTake(ctx, attempt); err != nil {
			return fmt.Errorf("record take %d: %w", take.Segment.ID, err)
		}
	}
	for _, segment := range result.Timeline {
		snapshot := ledger.TimelineSegment{
			CommitID:     result.CommitID,
			ProjectID:    result.ProjectID,
			DubID:        req.DubID,
			OwnerID:      result.OwnerID,
			Language:     req.Language,
			VersionSeq:   version,
			SegmentIndex: int32(segment.SegmentIndex),
			StartMs:      segment.StartMs,
			EndMs:        segment.EndMs,
			Speaker:      segment.Speaker,
			Emotion:      segment.Emotion,
			SourceText:   segment.SourceText,
			Text:         segment.Text,
			TakeID:       segment.TakeID,
		}
		if err := r.client.RecordSegmentState(ctx, snapshot); err != nil {
			return fmt.Errorf("record timeline snapshot for line %d: %w", segment.SegmentIndex, err)
		}
	}
	if err := r.client.Flush(ctx); err != nil {
		return fmt.Errorf("flush the ledger after the run: %w", err)
	}
	return nil
}

// attributeWholePass rewrites each whole-pass charge onto the first rendered
// take. charges_raw keys every charge by take_id and RecordTake rejects a
// charge whose segment does not match its take, so the segmentation pass needs
// an owner. The first take carries the run's commit id, which keeps the
// whole-pass cost inside the commit ancestry.
func attributeWholePass(charges []cost.Charge, segmentID int) []cost.Charge {
	out := make([]cost.Charge, len(charges))
	for i, charge := range charges {
		charge.TakeID = segmentID
		out[i] = charge
	}
	return out
}
