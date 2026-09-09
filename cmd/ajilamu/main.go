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
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/nrynss/ajilamu/internal/agent"
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
	var editRecorder api.EditRecorder
	var agentChargeRecorder api.AgentChargeRecorder
	var pipelineRunner api.PipelineRunner
	var lineRenderer api.LineRenderer
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
		editRecorder = newEditRecorder(eventLedger)
		agentChargeRecorder = newAgentChargeRecorder(eventLedger, cfg.GeminiModel)
	}
	if cfg.RequireGoogleCloud() == nil {
		runner, err := newPipelineRunner(cfg, cost.DefaultRateCard())
		if err != nil {
			slog.Warn("dubbing runs are unavailable", "error", err)
		} else {
			pipelineRunner = runner
			lineRenderer, _ = runner.(api.LineRenderer)
		}
	}

	editor := newEditorAgent(context.Background(), cfg, agent.NewFromConfig, slog.Default())

	// The catalog serves the committed list with no credentials, and a
	// refresh fetches the live list through ADC.
	languageCatalog := tts.NewCatalog(nil)

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
		Agent:        editor,
		AgentCharges: agentChargeRecorder,
		FrontendRoot: frontendRoot(cfg),
		Ledger:       ledgerFlusher,
		History:      history,
		Workspace:    workspace,
		Project:      api.UploadProjectLookup(uploadDir),
		Runner:       pipelineRunner,
		Rerender:     lineRenderer,
		Recorder:     runRecorder,
		Edits:        editRecorder,
		StorageDir:   uploadDir,
		Upload:       api.NewUploadHandler(uploadDir),
		Sample:       api.NewSampleHandler(uploadDir),
		Index: api.IndexHandlerWithLedger(func() []api.DubSummary {
			return append([]api.DubSummary{fixtureSummary}, api.ListUploadSummaries(uploadDir)...)
		}, newIndexLedger(eventLedger), slog.Default()),
		// T7.4a persists submitted credentials under AJILAMU_DATA_DIR. The
		// store never returns a value, so the read route reports presence only.
		Config:           api.ConfigHandler(newConfigSaver(settings)),
		ConfigPresence:   api.ConfigPresenceHandler(newConfigPresence(settings)),
		Languages:        api.LanguagesHandler(languageSource{catalog: languageCatalog}),
		LanguagesRefresh: api.LanguagesRefreshHandler(languageSource{catalog: languageCatalog}),
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

// languageSource adapts the TTS catalog onto the api language routes.
type languageSource struct {
	catalog *tts.Catalog
}

var _ api.LanguageCatalogSource = languageSource{}

// Catalog returns the catalog as it stands.
func (l languageSource) Catalog() api.LanguageCatalog {
	return wireLanguageCatalog(l.catalog.Current())
}

// Refresh fetches the provider list and returns the refreshed catalog.
func (l languageSource) Refresh(ctx context.Context) (api.LanguageCatalog, error) {
	if err := l.catalog.Refresh(ctx); err != nil {
		return api.LanguageCatalog{}, err
	}
	return wireLanguageCatalog(l.catalog.Current()), nil
}

// wireLanguageCatalog maps one catalog reading onto the wire payload.
func wireLanguageCatalog(state tts.CatalogState) api.LanguageCatalog {
	source := api.CatalogSourceCommitted
	if state.FromProvider {
		source = api.CatalogSourceProvider
	}
	fetchedAt := ""
	if !state.FetchedAt.IsZero() {
		fetchedAt = state.FetchedAt.UTC().Format(time.RFC3339)
	}
	return api.LanguageCatalog{
		Languages: state.Languages,
		Source:    source,
		FetchedAt: fetchedAt,
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

// indexLedger adapts the ledger client to the api index interface.
// The grouped read already returns the wire fact, so the method forwards.
type indexLedger struct {
	client *ledger.Client
}

// The adapter satisfies the index route without internal/api importing internal/ledger.
var _ api.IndexLedger = (*indexLedger)(nil)

// newIndexLedger returns an adapter for client.
// A nil client returns a nil interface, so a typed nil never reaches the index route.
func newIndexLedger(client *ledger.Client) api.IndexLedger {
	if client == nil {
		return nil
	}
	return &indexLedger{client: client}
}

// IndexFacts returns the ledger facts for the requested projects.
func (l *indexLedger) IndexFacts(ctx context.Context, dubIDs []string) (map[string]api.IndexFact, error) {
	return l.client.IndexFacts(ctx, dubIDs)
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

// runLanguage names one resolved target language.
type runLanguage struct {
	// tag is the BCP-47 code Cloud TTS receives as the LanguageCode.
	tag string
	// name is the language in words, such as Malayalam.
	name string
}

// createScreenLanguages maps the language codes the catalog serves onto their
// BCP-47 tag and display name. The create screen sends catalog codes, so the
// runner names the target and the source in words rather than in code. The
// sample keeps the shorter spellings `ml` and `en`, which the spec mandates.
// Any other well formed code falls back to itself, so the runner never
// invents a name.
var createScreenLanguages = map[string]runLanguage{
	"ar-XA":  {tag: "ar-XA", name: "Arabic"},
	"bg-BG":  {tag: "bg-BG", name: "Bulgarian"},
	"bn-IN":  {tag: "bn-IN", name: "Bengali"},
	"cmn-CN": {tag: "cmn-CN", name: "Mandarin Chinese"},
	"cs-CZ":  {tag: "cs-CZ", name: "Czech"},
	"da-DK":  {tag: "da-DK", name: "Danish"},
	"de-DE":  {tag: "de-DE", name: "German"},
	"el-GR":  {tag: "el-GR", name: "Greek"},
	"en":     {tag: "en-US", name: "English"},
	"en-AU":  {tag: "en-AU", name: "English"},
	"en-GB":  {tag: "en-GB", name: "English"},
	"en-IN":  {tag: "en-IN", name: "English"},
	"en-US":  {tag: "en-US", name: "English"},
	"es-ES":  {tag: "es-ES", name: "Spanish"},
	"es-US":  {tag: "es-US", name: "Spanish"},
	"et-EE":  {tag: "et-EE", name: "Estonian"},
	"fi-FI":  {tag: "fi-FI", name: "Finnish"},
	"fr-CA":  {tag: "fr-CA", name: "French"},
	"fr-FR":  {tag: "fr-FR", name: "French"},
	"gu-IN":  {tag: "gu-IN", name: "Gujarati"},
	"he-IL":  {tag: "he-IL", name: "Hebrew"},
	"hi-IN":  {tag: "hi-IN", name: "Hindi"},
	"hr-HR":  {tag: "hr-HR", name: "Croatian"},
	"hu-HU":  {tag: "hu-HU", name: "Hungarian"},
	"id-ID":  {tag: "id-ID", name: "Indonesian"},
	"it-IT":  {tag: "it-IT", name: "Italian"},
	"ja-JP":  {tag: "ja-JP", name: "Japanese"},
	"kn-IN":  {tag: "kn-IN", name: "Kannada"},
	"ko-KR":  {tag: "ko-KR", name: "Korean"},
	"lt-LT":  {tag: "lt-LT", name: "Lithuanian"},
	"lv-LV":  {tag: "lv-LV", name: "Latvian"},
	"ml":     {tag: "ml-IN", name: "Malayalam"},
	"ml-IN":  {tag: "ml-IN", name: "Malayalam"},
	"mr-IN":  {tag: "mr-IN", name: "Marathi"},
	"nb-NO":  {tag: "nb-NO", name: "Norwegian"},
	"nl-BE":  {tag: "nl-BE", name: "Dutch"},
	"nl-NL":  {tag: "nl-NL", name: "Dutch"},
	"pa-IN":  {tag: "pa-IN", name: "Punjabi"},
	"pl-PL":  {tag: "pl-PL", name: "Polish"},
	"pt-BR":  {tag: "pt-BR", name: "Portuguese"},
	"ro-RO":  {tag: "ro-RO", name: "Romanian"},
	"ru-RU":  {tag: "ru-RU", name: "Russian"},
	"sk-SK":  {tag: "sk-SK", name: "Slovak"},
	"sl-SI":  {tag: "sl-SI", name: "Slovenian"},
	"sr-RS":  {tag: "sr-RS", name: "Serbian"},
	"sv-SE":  {tag: "sv-SE", name: "Swedish"},
	"sw-KE":  {tag: "sw-KE", name: "Swahili"},
	"ta-IN":  {tag: "ta-IN", name: "Tamil"},
	"te-IN":  {tag: "te-IN", name: "Telugu"},
	"th-TH":  {tag: "th-TH", name: "Thai"},
	"tr-TR":  {tag: "tr-TR", name: "Turkish"},
	"uk-UA":  {tag: "uk-UA", name: "Ukrainian"},
	"ur-IN":  {tag: "ur-IN", name: "Urdu"},
	"vi-VN":  {tag: "vi-VN", name: "Vietnamese"},
	"yue-HK": {tag: "yue-HK", name: "Cantonese"},
}

// resolveRunLanguage resolves the target language of one run. A code the
// create screen offers resolves to its tag and display name. Any other well
// formed code falls back to itself as the display name. An empty or malformed
// code fails, so the run stops before any billable call.
func resolveRunLanguage(code string) (runLanguage, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return runLanguage{}, errors.New("the run needs a target language")
	}
	if known, ok := createScreenLanguages[code]; ok {
		return known, nil
	}
	if err := tts.ValidateLanguage(code); err != nil {
		return runLanguage{}, fmt.Errorf("the run cannot use %q as a target language: %w", code, err)
	}
	return runLanguage{tag: code, name: code}, nil
}

// resolveLanguageName names a language in words when the runner knows it.
// Any other code falls back to itself, so the pipeline never receives an
// invented name. An empty code stays empty, which the pipeline reads as an
// unknown source language.
func resolveLanguageName(code string) string {
	code = strings.TrimSpace(code)
	if code == "" {
		return ""
	}
	if known, ok := createScreenLanguages[code]; ok {
		return known.name
	}
	return code
}

// pipelineRunner adapts the fit pipeline and the assembler onto api.PipelineRunner.
// internal/api cannot import internal/fit, so this adapter is the only seam.
// The segmenter and the translator are built once, so no run leaks a gRPC
// connection. Runs are serialized, so the charge router always has exactly one
// active target. The synthesizer is built per run, because its target language
// binds at construction and one project may carry more than one language.
type pipelineRunner struct {
	segmenter  gemini.Segmenter
	translator gemini.Translator
	router     *chargeRouter
	// newSynthesizer builds the synthesizer for one run from the run's
	// BCP-47 language tag and the run's charge ledger.
	newSynthesizer func(language string, rec tts.ChargeRecorder) (tts.Synthesizer, error)
	mu             sync.Mutex
}

var _ api.PipelineRunner = (*pipelineRunner)(nil)

// The runner also re-renders one line, so the server passes it for both seams.
var _ api.LineRenderer = (*pipelineRunner)(nil)

// runSynthesizerFactory builds the per-run synthesizer factory. Production
// passes a nil client, so Cloud TTS opens on ADC. A test passes a fake client
// and reads the language the factory forwards.
func runSynthesizerFactory(cfg *config.Config, card cost.RateCard, client tts.TTSClient) func(language string, rec tts.ChargeRecorder) (tts.Synthesizer, error) {
	return func(language string, rec tts.ChargeRecorder) (tts.Synthesizer, error) {
		return tts.NewSynthesizer(cfg, language, rec, card, client)
	}
}

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
	return &pipelineRunner{
		segmenter:      segmenter,
		translator:     translator,
		router:         router,
		newSynthesizer: runSynthesizerFactory(cfg, card, nil),
	}, nil
}

// Run segments, translates, synthesizes, fits, and assembles one dub.
func (p *pipelineRunner) Run(ctx context.Context, req api.RunRequest, emit func(api.ProgressEvent)) (api.RunResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	language, err := resolveRunLanguage(req.Language)
	if err != nil {
		return api.RunResult{}, err
	}
	// The resolved tag is the run's language from here on, so every event,
	// the pipeline, and the synthesizer name the same code.
	req.Language = language.tag

	charges := cost.NewLedger()
	p.router.set(charges)
	defer p.router.set(nil)

	synthesizer, err := p.newSynthesizer(language.tag, charges)
	if err != nil {
		return api.RunResult{}, fmt.Errorf("open the synthesizer: %w", err)
	}

	source, err := os.ReadFile(req.Source)
	if err != nil {
		return api.RunResult{}, fmt.Errorf("read the source video: %w", err)
	}
	emit(api.ProgressEvent{
		Type:     api.EventProgress,
		Stage:    api.StageSegmenting,
		Sentence: "Reading the source video.",
		Language: language.tag,
	})
	result, err := fit.RunPipeline(ctx, fit.PipelineConfig{
		Segmenter:          p.segmenter,
		InputMedia:         &gemini.Input{Data: source, MIMEType: mediaType(req.Source)},
		Translator:         p.translator,
		Synthesizer:        synthesizer,
		Language:           language.tag,
		TargetLanguageName: language.name,
		SourceLanguageName: resolveLanguageName(req.SourceLanguage),
		WorkDir:            req.WorkDir,
		Recorder:           charges,
		ProgressFn:         fit.ProgressFunc(emit),
	})
	if err != nil {
		return api.RunResult{}, err
	}
	if err := assembleRun(ctx, req, result, emit); err != nil {
		return api.RunResult{}, err
	}
	return pipelineRunResult(ctx, result, assemble.Peaks)
}

// RenderLine re-runs one dialogue line through the fit loop and writes its new
// take. It holds the runner mutex, so a re-render never overlaps a run.
func (p *pipelineRunner) RenderLine(ctx context.Context, req api.LineRenderRequest) (api.LineRenderResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	language, err := resolveRunLanguage(req.Language)
	if err != nil {
		return api.LineRenderResult{}, err
	}

	charges := cost.NewLedger()
	p.router.set(charges)
	defer p.router.set(nil)

	synthesizer, err := p.newSynthesizer(language.tag, charges)
	if err != nil {
		return api.LineRenderResult{}, fmt.Errorf("open the synthesizer: %w", err)
	}

	cfg := fit.RewriteConfig{
		Translator: &namedTranslator{
			inner:  p.translator,
			target: language.name,
			source: resolveLanguageName(req.SourceLanguage),
		},
		Synthesizer: synthesizer,
		WorkDir:     req.WorkDir,
		PathBuilder: rerenderTakePath(req.TakeFile),
		MaxAttempts: fit.DefaultMaxAttempts,
		InitialText: req.Text,
	}
	if req.Text != "" {
		// A corrected target line is authoritative. Every attempt speaks it,
		// so the loop never translates the source to second-guess the creator.
		cfg.AuthoritativeText = true
	}
	line, err := fit.RepairLine(ctx, req.Segment, cfg)
	if err != nil {
		return api.LineRenderResult{}, err
	}

	attempt := chosenAttempt(line)
	peaks, err := assemble.Peaks(ctx, line.ChosenTake.File)
	if err != nil {
		return api.LineRenderResult{}, fmt.Errorf("sketch take peaks: %w", err)
	}
	voice, err := tts.Assign(line.Segment.Speaker, language.tag)
	if err != nil {
		return api.LineRenderResult{}, err
	}
	return api.LineRenderResult{
		Take:         line.ChosenTake,
		Text:         attempt.Text,
		Voice:        voice.Name,
		Repair:       attempt.Repair,
		RepairDetail: attempt.RepairDetail,
		Flagged:      line.Flagged,
		Charges:      charges.Charges(),
		Total:        charges.Total(),
		Peaks:        peaks,
	}, nil
}

// namedTranslator names the target and source language on every translate
// request. fit.RunPipeline names them through its own wrapper. A re-render
// calls the translator directly, so this adapter names them instead.
type namedTranslator struct {
	inner  gemini.Translator
	target string
	source string
}

// Translate names the languages and forwards the request.
func (t *namedTranslator) Translate(ctx context.Context, req gemini.TranslateRequest) (string, error) {
	req.TargetLanguageName = t.target
	req.SourceLanguageName = t.source
	return t.inner.Translate(ctx, req)
}

// rerenderTakePath names each attempt of one re-render. Attempt one keeps the
// path the route chose. Later attempts extend its try number, so no attempt
// reuses a take file that already exists.
func rerenderTakePath(first string) fit.PathBuilder {
	stem := strings.TrimSuffix(first, filepath.Ext(first))
	prefix, number := stem, 0
	if at := strings.LastIndex(stem, "_try"); at >= 0 {
		prefix = stem[:at]
		if parsed, err := strconv.Atoi(stem[at+len("_try"):]); err == nil {
			number = parsed
		}
	}
	return func(_ int, attempt int, stretched bool) string {
		if number < 1 {
			return first
		}
		suffix := ".wav"
		if stretched {
			suffix = "_stretched.wav"
		}
		return fmt.Sprintf("%s_try%d%s", prefix, number+attempt-1, suffix)
	}
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
	bySegment := make(map[int][]cost.AttemptCharge, len(rendered))
	var wholePass []cost.Charge
	for _, item := range result.AttemptCharges {
		if _, ok := rendered[item.Charge.TakeID]; ok {
			bySegment[item.Charge.TakeID] = append(bySegment[item.Charge.TakeID], item)
			continue
		}
		wholePass = append(wholePass, item.Charge)
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
			Text:           attempt.Text,
			Segment:        line.Segment,
			Take:           take,
			Voice:          voiceName(result.Voices, line.Segment.Speaker.Name),
			Repair:         attempt.Repair,
			RepairDetail:   attempt.RepairDetail,
			Charges:        chargesOf(bySegment[line.Segment.ID]),
			AttemptCharges: bySegment[line.Segment.ID],
			Peaks:          waveform,
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
var _ api.CorrectedRecorder = (*runRecorder)(nil)

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

// Persist writes a full run in ledger order. It flushes the durable queue
// first, so a parent commit still queued is delivered before the head read,
// and again at the end, so every row reaches ClickHouse before the run
// reports done.
func (r *runRecorder) Persist(ctx context.Context, req api.RunRequest, result api.RunResult) error {
	return r.persist(ctx, req, result, api.ActionTakeRendered, api.AuthorAgent, -1,
		fmt.Sprintf("Rendered %d lines into %s.", len(result.Takes), req.Language))
}

// PersistCorrected writes one corrected-source re-render. It is the only
// path that names the text_corrected action and the manual_ui author.
func (r *runRecorder) PersistCorrected(ctx context.Context, req api.RunRequest, result api.RunResult, corrected api.CorrectedRerender) error {
	return r.persist(ctx, req, result, corrected.Action, corrected.Author, int32(corrected.Segment),
		fmt.Sprintf("Corrected the source of line %d and re-rendered it.", corrected.Segment))
}

// persist writes one commit with its action, takes, charges and timeline
// snapshots. The caller names the action, the author and the commit message.
// It holds the dub's shared commit lock across the head read, the append and
// the final flush, so a run or re-render commit never shares a parent and a
// version_seq with a concurrent edit. PersistCorrected shares this path.
func (r *runRecorder) persist(ctx context.Context, req api.RunRequest, result api.RunResult, actionType, author string, segmentIndex int32, message string) error {
	if r.client == nil {
		return errors.New("run recorder has no ledger client")
	}
	unlock := api.LockDubCommit(req.DubID)
	defer unlock()
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
		Message:        message,
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
		SegmentIndex: segmentIndex,
		ActionType:   actionType,
		Author:       author,
	}
	if err := r.client.RecordAction(ctx, action); err != nil {
		return fmt.Errorf("record the run action: %w", err)
	}
	if len(result.Takes) == 0 && len(result.WholePassCharges) > 0 {
		r.logger.Warn("run rendered no takes, so whole-pass charges have no owner",
			"dub_id", req.DubID, "charges", len(result.WholePassCharges))
	}
	for i, take := range result.Takes {
		charges := takeAttemptCharges(take)
		if i == 0 && len(result.WholePassCharges) > 0 {
			charges = append(append([]cost.AttemptCharge(nil), charges...),
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
			Text:           take.Text,
			Take:           take.Take,
			AttemptCharges: charges,
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

// takeAttemptCharges pairs each billed call with the attempt that produced it.
// A run names the attempt of every call. A re-render reaches the ledger through
// the route, which builds its run take without per-call attempts, so its charges
// take the attempt of the recorded take.
func takeAttemptCharges(take api.RunTake) []cost.AttemptCharge {
	if len(take.AttemptCharges) > 0 {
		return take.AttemptCharges
	}
	out := make([]cost.AttemptCharge, len(take.Charges))
	for i, charge := range take.Charges {
		out[i] = cost.AttemptCharge{Charge: charge, Attempt: take.Take.Attempt}
	}
	return out
}

// chargesOf strips the attempt pairing from a take's itemized calls.
func chargesOf(items []cost.AttemptCharge) []cost.Charge {
	out := make([]cost.Charge, len(items))
	for i, item := range items {
		out[i] = item.Charge
	}
	return out
}

// attributeWholePass rewrites each whole-pass charge onto the first rendered
// take. charges_raw keys every charge by take_id and RecordTake rejects a
// charge whose segment does not match its take, so the segmentation pass needs
// an owner. The first take carries the run's commit id, which keeps the
// whole-pass cost inside the commit ancestry. Segmentation runs before any
// attempt, so the attributed calls carry attempt 0.
func attributeWholePass(charges []cost.Charge, segmentID int) []cost.AttemptCharge {
	out := make([]cost.AttemptCharge, len(charges))
	for i, charge := range charges {
		charge.TakeID = segmentID
		out[i] = cost.AttemptCharge{Charge: charge}
	}
	return out
}

// editRecorder adapts the ledger client onto api.EditRecorder.
// The adapter is the only seam, so internal/api never imports internal/ledger.
type editRecorder struct {
	client *ledger.Client
}

var _ api.EditRecorder = (*editRecorder)(nil)

// newEditRecorder returns nil for a nil client, so a typed nil never reaches a route.
func newEditRecorder(client *ledger.Client) api.EditRecorder {
	if client == nil {
		return nil
	}
	return &editRecorder{client: client}
}

// RecordEdit writes one edit in ledger order. It flushes the durable queue
// first, so a parent commit still queued is delivered before the head read,
// and again at the end, so every row reaches ClickHouse before the route
// reports success. The route holds the dub's edit lock across this call, so
// the head read and the append never interleave with another edit of the
// same dub.
func (r *editRecorder) RecordEdit(ctx context.Context, edit api.EditRecord) error {
	if r.client == nil {
		return errors.New("edit recorder has no ledger client")
	}
	if err := r.client.Flush(ctx); err != nil {
		return fmt.Errorf("flush the ledger before the edit commit: %w", err)
	}
	commits, err := r.client.ListCommits(ctx, edit.DubID)
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
	if err := r.client.AppendCommit(ctx, ledger.Commit{
		CommitID:       edit.CommitID,
		ParentCommitID: parent,
		ProjectID:      edit.ProjectID,
		DubID:          edit.DubID,
		OwnerID:        edit.OwnerID,
		Branch:         runBranch,
		Language:       edit.Language,
		VersionSeq:     version,
		Message:        edit.Message,
	}); err != nil {
		return fmt.Errorf("append the edit commit: %w", err)
	}
	if err := r.client.RecordAction(ctx, ledger.Action{
		CommitID:     edit.CommitID,
		ProjectID:    edit.ProjectID,
		DubID:        edit.DubID,
		OwnerID:      edit.OwnerID,
		Language:     edit.Language,
		SegmentIndex: int32(edit.Segment.SegmentIndex),
		ActionType:   edit.Action,
		Author:       edit.Author,
		Prompt:       edit.Prompt,
		BeforeValue:  edit.BeforeValue,
		AfterValue:   edit.AfterValue,
	}); err != nil {
		return fmt.Errorf("record the edit action: %w", err)
	}
	if err := r.client.RecordSegmentState(ctx, ledger.TimelineSegment{
		CommitID:     edit.CommitID,
		ProjectID:    edit.ProjectID,
		DubID:        edit.DubID,
		OwnerID:      edit.OwnerID,
		Language:     edit.Language,
		VersionSeq:   version,
		SegmentIndex: int32(edit.Segment.SegmentIndex),
		StartMs:      edit.Segment.StartMs,
		EndMs:        edit.Segment.EndMs,
		Speaker:      edit.Segment.Speaker,
		Emotion:      edit.Segment.Emotion,
		SourceText:   edit.Segment.SourceText,
		Text:         edit.Segment.Text,
		TakeID:       edit.Segment.TakeID,
	}); err != nil {
		return fmt.Errorf("record the edit timeline snapshot: %w", err)
	}
	if err := r.client.Flush(ctx); err != nil {
		return fmt.Errorf("flush the ledger after the edit: %w", err)
	}
	return nil
}

// agentChargeRecorder adapts completed agent turns onto the durable ledger.
type agentChargeRecorder struct {
	client   *ledger.Client
	provider string
}

var _ api.AgentChargeRecorder = (*agentChargeRecorder)(nil)

// newAgentChargeRecorder returns nil when the server has no ledger.
func newAgentChargeRecorder(client *ledger.Client, provider string) api.AgentChargeRecorder {
	if client == nil {
		return nil
	}
	return &agentChargeRecorder{client: client, provider: provider}
}

// RecordAgentTurn journals one turn against the empty commit sentinel.
// Agent charges can exist before the first run creates a commit.
func (r *agentChargeRecorder) RecordAgentTurn(ctx context.Context, record api.AgentChargeRecord) error {
	if r.client == nil {
		return errors.New("agent charge recorder has no ledger client")
	}
	return r.client.RecordAgentTurn(ctx, ledger.AgentTurn{
		TurnID:    record.TurnID,
		CommitID:  "",
		ProjectID: record.DubID,
		DubID:     record.DubID,
		OwnerID:   "local",
		Provider:  r.provider,
		Charges:   record.Charges,
	})
}

// newEditorAgent keeps missing or unusable MCP settings from stopping the workspace.
// A nil result stays a nil interface so the route answers 503.
func newEditorAgent(ctx context.Context, cfg *config.Config, build func(context.Context, *config.Config) (*agent.Agent, error), logger *slog.Logger) api.EditorAgent {
	if !cfg.MCPConfigured() {
		return nil
	}
	editor, err := build(ctx, cfg)
	if err != nil {
		logger.Warn("editor agent is unavailable", "error", err)
		return nil
	}
	if editor == nil {
		return nil
	}
	return editor
}
