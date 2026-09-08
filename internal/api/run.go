package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/nrynss/ajilamu/internal/cost"
	"github.com/nrynss/ajilamu/internal/tts"
	"github.com/nrynss/ajilamu/internal/types"
)

// Run status values the registry and the routes report.
const (
	runStatusRunning    = "running"
	runStatusCancelling = "cancelling"
	runStatusFinished   = "finished"
)

// runBranch names the single history branch this task writes.
const runBranch = "main"

// runWorkDir names the per-project directory that holds takes and mixes.
const runWorkDir = "work"

// localOwnerID names the only creator this single-user server serves.
// The upload record carries no owner, so every run writes this identity.
const localOwnerID = "local"

// Run sentences the routes return. A failure never carries internal detail.
const (
	runUnavailable = "Runs are unavailable."
	runActive      = "A run is already active for this project."
	runNoActive    = "No run is active for this project."
	runNoSource    = "This project has no source video."
	runMissing     = "No run exists for this project."
	runBadLanguage = "That language code is not valid."
)

var (
	// errRunActive reports a second start for a dub that already runs.
	errRunActive = errors.New("a run is already active for this project")
	// errNoSourceVideo reports a project whose source video is missing.
	errNoSourceVideo = errors.New("project has no source video")
)

// RunRequest carries the inputs one dubbing run needs.
// The api package resolves every path before it starts the run.
type RunRequest struct {
	// DubID identifies the project the run belongs to.
	DubID string
	// Language is the target language code.
	Language string
	// SourceLanguage is the film language code. Empty means unknown, which a
	// record written before T7.5b carries.
	SourceLanguage string
	// Source is the absolute path of the source video.
	Source string
	// Music is the absolute path of the creator's music track. Empty means none.
	Music string
	// WorkDir receives every take and mix the run writes.
	WorkDir string
}

// RunTake is one rendered take with the provenance the ledger records.
type RunTake struct {
	// TakeID names the take. The api run mints it.
	TakeID string
	// Segment is the source line the take speaks.
	Segment types.Segment
	// Take is the rendered attempt.
	Take types.Take
	// Voice names the voice profile used.
	Voice string
	// Repair names the strategy that produced the file.
	Repair types.Repair
	// RepairDetail explains an atempo ratio or a rewrite.
	RepairDetail string
	// Charges itemizes the API calls this take paid for.
	Charges []cost.Charge
	// Peaks is the waveform sketch the timeline draws.
	Peaks []uint8
}

// RunSegmentState is one timeline snapshot the run produced.
type RunSegmentState struct {
	// SegmentIndex numbers the line inside the dub.
	SegmentIndex int
	// StartMs locates the slot start in the film.
	StartMs int64
	// EndMs locates the slot end in the film.
	EndMs int64
	// Speaker names the person talking.
	Speaker string
	// Emotion describes how the line is spoken.
	Emotion string
	// SourceText is the transcribed source line.
	SourceText string
	// Text is the target-language line.
	Text string
	// TakeID names the active take.
	TakeID string
}

// RunResult is what a pipeline runner hands back to the api package.
// The api package mints the identity fields before it calls Persist.
type RunResult struct {
	// CommitID names the commit this run appends.
	CommitID string
	// ProjectID groups the run's rows. The dub id is the project id here.
	ProjectID string
	// OwnerID names the creator who owns the dub.
	OwnerID string
	// Takes lists one rendered take per finished line.
	Takes []RunTake
	// Timeline lists one snapshot per rendered segment.
	Timeline []RunSegmentState
	// FlaggedSegments lists lines that no attempt could fit.
	FlaggedSegments []int
	// TotalCost is the cumulative price of every API call.
	TotalCost cost.Price
	// WholePassCharges itemizes work that no single take owns, such as the
	// segmentation pass. Persist attributes each one to the first rendered take,
	// because charges_raw keys every charge by take_id and RecordTake rejects a
	// charge whose segment does not match its take. That take carries the run's
	// commit id, so the commit ancestry cost sum includes the whole-pass work.
	WholePassCharges []cost.Charge
}

// PipelineRunner runs the dubbing pipeline behind the run routes.
// internal/api cannot import internal/fit, which imports this package for the
// wire types, so cmd/ajilamu/main.go adapts the pipeline onto this seam.
type PipelineRunner interface {
	// Run executes one run and reports progress through emit.
	Run(ctx context.Context, req RunRequest, emit func(ProgressEvent)) (RunResult, error)
}

// RunRecorder persists what a run produced.
// The adapter lives in cmd/ajilamu/main.go so this package never imports the ledger.
type RunRecorder interface {
	// Persist writes the commit, action, takes, charges, and timeline snapshots.
	Persist(ctx context.Context, req RunRequest, result RunResult) error
}

// subscriberBuffer bounds one subscriber queue. A subscriber that fills it
// drops events rather than stalling the pipeline.
const subscriberBuffer = 64

// runFrame is one numbered event in a run stream.
type runFrame struct {
	id    uint64
	event ProgressEvent
}

// runSubscriber receives frames until the run closes its channel.
type runSubscriber struct {
	frames  chan runFrame
	dropped uint64
}

// dubRun is one active or finished run of one dub.
type dubRun struct {
	id       string
	dubID    string
	language string
	cancel   context.CancelFunc
	logger   *slog.Logger

	mu       sync.Mutex
	status   string
	cost     cost.Price
	nextID   uint64
	last     ProgressEvent
	pending  *ProgressEvent
	subs     map[*runSubscriber]struct{}
	terminal *runFrame
}

// isActive reports whether the run has not finished.
func (run *dubRun) isActive() bool {
	run.mu.Lock()
	defer run.mu.Unlock()
	return run.status != runStatusFinished
}

// emit forwards one pipeline event. A terminal event waits for finish, so the
// run reports it only after persistence has run.
func (run *dubRun) emit(event ProgressEvent) {
	run.mu.Lock()
	defer run.mu.Unlock()
	if run.status == runStatusFinished {
		return
	}
	if event.Type == EventDone || event.Type == EventError {
		run.pending = &event
		return
	}
	run.broadcastLocked(event)
}

// broadcastLocked numbers one event and offers it to every subscriber.
func (run *dubRun) broadcastLocked(event ProgressEvent) {
	run.cost = event.TotalNanodollars
	run.last = event
	frame := run.nextFrameLocked(event)
	for sub := range run.subs {
		select {
		case sub.frames <- frame:
		default:
			sub.dropped++
			run.logger.Warn("dropping a run event for a slow subscriber",
				"run_id", run.id, "dub_id", run.dubID, "dropped", sub.dropped)
		}
	}
}

// nextFrameLocked numbers the next frame. The caller holds run.mu.
func (run *dubRun) nextFrameLocked(event ProgressEvent) runFrame {
	run.nextID++
	return runFrame{id: run.nextID, event: event}
}

// finish reports the terminal event and closes every subscriber channel.
func (run *dubRun) finish(event ProgressEvent) {
	run.mu.Lock()
	if run.status == runStatusFinished {
		run.mu.Unlock()
		return
	}
	run.status = runStatusFinished
	run.cost = event.TotalNanodollars
	run.last = event
	run.pending = nil
	frame := run.nextFrameLocked(event)
	run.terminal = &frame
	for sub := range run.subs {
		select {
		case sub.frames <- frame:
		default:
			sub.dropped++
			run.logger.Warn("dropping a run event for a slow subscriber",
				"run_id", run.id, "dub_id", run.dubID, "dropped", sub.dropped)
		}
		close(sub.frames)
	}
	run.subs = nil
	run.mu.Unlock()
	run.cancel()
}

// subscribe registers a reader. It returns the terminal frame when the run
// already finished, and otherwise queues one resume frame carrying the current cost.
func (run *dubRun) subscribe() (*runSubscriber, *runFrame, bool) {
	run.mu.Lock()
	defer run.mu.Unlock()
	if run.status == runStatusFinished {
		return nil, run.terminal, true
	}
	sub := &runSubscriber{frames: make(chan runFrame, subscriberBuffer)}
	if run.subs == nil {
		run.subs = make(map[*runSubscriber]struct{})
	}
	run.subs[sub] = struct{}{}
	event := run.last
	if event.Type == "" {
		event = ProgressEvent{
			Type:     EventProgress,
			Stage:    StageSegmenting,
			Sentence: "The run is starting.",
			Language: run.language,
		}
	}
	event.TotalNanodollars = run.cost
	sub.frames <- run.nextFrameLocked(event)
	return sub, nil, false
}

// unsubscribe stops offering events to a reader that disconnected.
func (run *dubRun) unsubscribe(sub *runSubscriber) {
	run.mu.Lock()
	if run.subs != nil {
		delete(run.subs, sub)
	}
	run.mu.Unlock()
}

// markCancelling records a cancel request. It reports false once the run ended.
func (run *dubRun) markCancelling() bool {
	run.mu.Lock()
	defer run.mu.Unlock()
	if run.status == runStatusFinished {
		return false
	}
	run.status = runStatusCancelling
	return true
}

// takePending returns and clears the terminal event the pipeline emitted.
func (run *dubRun) takePending() *ProgressEvent {
	run.mu.Lock()
	defer run.mu.Unlock()
	pending := run.pending
	run.pending = nil
	return pending
}

// costSnapshot returns the last cumulative cost the run broadcast.
func (run *dubRun) costSnapshot() cost.Price {
	run.mu.Lock()
	defer run.mu.Unlock()
	return run.cost
}

// terminalFrame returns the stored terminal frame after the run finished.
func (run *dubRun) terminalFrame() *runFrame {
	run.mu.Lock()
	defer run.mu.Unlock()
	return run.terminal
}

// runRegistry holds one run per dub, guarded by a mutex.
type runRegistry struct {
	mu       sync.Mutex
	runs     map[string]*dubRun
	runner   PipelineRunner
	recorder RunRecorder
	logger   *slog.Logger
	wg       sync.WaitGroup
}

// newRunRegistry builds the registry NewServer owns.
func newRunRegistry(runner PipelineRunner, recorder RunRecorder, logger *slog.Logger) *runRegistry {
	if logger == nil {
		logger = slog.Default()
	}
	return &runRegistry{
		runs:     make(map[string]*dubRun),
		runner:   runner,
		recorder: recorder,
		logger:   logger,
	}
}

// available reports whether both seams are present. A nil seam answers 503.
func (r *runRegistry) available() bool {
	return r != nil && r.runner != nil && r.recorder != nil
}

// active reports whether the dub already has a run in flight.
func (r *runRegistry) active(dubID string) bool {
	run, ok := r.lookup(dubID)
	return ok && run.isActive()
}

// lookup returns the run for one dub, active or finished.
func (r *runRegistry) lookup(dubID string) (*dubRun, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	run, ok := r.runs[dubID]
	return run, ok
}

// start registers a run and launches it in its own goroutine.
// A second run for a dub already running returns errRunActive.
func (r *runRegistry) start(req RunRequest) (*dubRun, error) {
	r.mu.Lock()
	if existing, ok := r.runs[req.DubID]; ok && existing.isActive() {
		r.mu.Unlock()
		return nil, errRunActive
	}
	ctx, cancel := context.WithCancel(context.Background())
	run := &dubRun{
		id:       newRunID(),
		dubID:    req.DubID,
		language: req.Language,
		status:   runStatusRunning,
		cancel:   cancel,
		logger:   r.logger,
	}
	r.runs[req.DubID] = run
	r.mu.Unlock()

	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		r.execute(ctx, run, req)
	}()
	return run, nil
}

// execute runs the pipeline, persists the result, and reports the terminal event.
func (r *runRegistry) execute(ctx context.Context, run *dubRun, req RunRequest) {
	result, err := r.runner.Run(ctx, req, run.emit)
	if err != nil {
		run.finish(terminalEvent(req, run.costSnapshot(), err, run.takePending()))
		return
	}
	mintRunIdentity(req, &result)
	if err := r.recorder.Persist(ctx, req, result); err != nil {
		run.finish(terminalEvent(req, result.TotalCost, err, run.takePending()))
		return
	}
	run.finish(terminalEvent(req, result.TotalCost, nil, run.takePending()))
}

// mintRunIdentity fills the identity fields the ledger requires. The api
// package mints them because it owns the run seam types. It links each
// timeline snapshot to the take that speaks its segment.
func mintRunIdentity(req RunRequest, result *RunResult) {
	result.ProjectID = req.DubID
	result.OwnerID = localOwnerID
	result.CommitID = newRunID()
	takeBySegment := make(map[int]string, len(result.Takes))
	for i := range result.Takes {
		id := newRunID()
		result.Takes[i].TakeID = id
		takeBySegment[result.Takes[i].Segment.ID] = id
	}
	for i := range result.Timeline {
		result.Timeline[i].TakeID = takeBySegment[result.Timeline[i].SegmentIndex]
	}
}

// cancelRun cancels the active run of one dub. It reports false when none runs.
func (r *runRegistry) cancelRun(dubID string) bool {
	run, ok := r.lookup(dubID)
	if !ok || !run.markCancelling() {
		return false
	}
	run.cancel()
	return true
}

// cancelAll cancels every run still in flight. Shutdown calls it before the drain.
func (r *runRegistry) cancelAll() {
	r.mu.Lock()
	runs := make([]*dubRun, 0, len(r.runs))
	for _, run := range r.runs {
		runs = append(runs, run)
	}
	r.mu.Unlock()
	for _, run := range runs {
		if run.markCancelling() {
			run.cancel()
		}
	}
}

// wait blocks until every run goroutine stops or ctx expires.
func (r *runRegistry) wait(ctx context.Context) {
	done := make(chan struct{})
	go func() {
		r.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
	}
}

// terminalEvent builds the one event that ends the stream.
func terminalEvent(req RunRequest, total cost.Price, runErr error, pending *ProgressEvent) ProgressEvent {
	if runErr == nil {
		if pending != nil && pending.Type == EventDone {
			event := *pending
			event.TotalNanodollars = total
			if event.Language == "" {
				event.Language = req.Language
			}
			return event
		}
		return ProgressEvent{
			Type:             EventDone,
			Stage:            StageMeasuring,
			Sentence:         "The run finished.",
			TotalNanodollars: total,
			Language:         req.Language,
		}
	}
	sentence := "The run could not finish."
	switch {
	case errors.Is(runErr, context.Canceled):
		sentence = "The run was cancelled."
	case pending != nil && pending.Type == EventError && pending.Sentence != "":
		sentence = pending.Sentence
	}
	return ProgressEvent{
		Type:             EventError,
		Stage:            StageMeasuring,
		Sentence:         sentence,
		TotalNanodollars: total,
		Language:         req.Language,
	}
}

// runStarted is the 202 body the start route returns.
type runStarted struct {
	// RunID identifies the run.
	RunID string `json:"run_id"`
	// Status reports that the run started.
	Status string `json:"status"`
}

// runCancelled is the 202 body the cancel route returns.
type runCancelled struct {
	// Status reports that cancellation started.
	Status string `json:"status"`
}

// RunStartHandler serves POST /api/dubs/{id}/run.
// It answers before the run finishes, because the run owns its own goroutine.
func RunStartHandler(runs *runRegistry, storageDir string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !runs.available() {
			writeRunFailure(w, http.StatusServiceUnavailable, runUnavailable)
			return
		}
		language, problem := requiredHistoryQuery(r, "language")
		if problem != "" {
			writeRunFailure(w, http.StatusBadRequest, problem)
			return
		}
		if !safeDubID(language) {
			writeRunFailure(w, http.StatusBadRequest, runBadLanguage)
			return
		}
		dubID := r.PathValue("id")
		if runs.active(dubID) {
			writeRunFailure(w, http.StatusConflict, runActive)
			return
		}
		source, music, err := resolveProjectMedia(storageDir, dubID)
		if err != nil {
			writeRunFailure(w, http.StatusNotFound, runNoSource)
			return
		}
		run, err := runs.start(RunRequest{
			DubID:          dubID,
			Language:       language,
			SourceLanguage: storedSourceLanguage(storageDir, dubID),
			Source:         source,
			Music:          music,
			WorkDir:        runWorkDirFor(storageDir, dubID, language),
		})
		if err != nil {
			writeRunFailure(w, http.StatusConflict, runActive)
			return
		}
		writeHistoryJSON(w, http.StatusAccepted, runStarted{RunID: run.id, Status: runStatusRunning})
	})
}

// RunCancelHandler serves POST /api/dubs/{id}/run/cancel.
func RunCancelHandler(runs *runRegistry) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !runs.available() {
			writeRunFailure(w, http.StatusServiceUnavailable, runUnavailable)
			return
		}
		if !runs.cancelRun(r.PathValue("id")) {
			writeRunFailure(w, http.StatusConflict, runNoActive)
			return
		}
		writeHistoryJSON(w, http.StatusAccepted, runCancelled{Status: runStatusCancelling})
	})
}

// storedSourceLanguage returns the film language the upload record stores.
// An older record carries none, so empty is a normal answer and the pipeline
// then names no source language.
func storedSourceLanguage(storageDir, dubID string) string {
	_, _, sourceLanguage, ok := UploadProjectLookup(storageDir)(dubID)
	if !ok {
		return ""
	}
	return sourceLanguage
}

// runWorkDirFor names the work directory of one project.
// The name is the resolved language tag, so the sample code `ml` and the
// catalog tag `ml-IN` share one directory. A resume that switches spelling
// then reuses every finished take. A run under the raw code left its records
// in a directory named by that code. This reuses such a directory when it
// holds a record for the same language. A run interrupted before the
// per-language layout left its records in the flat work directory, which is
// the last fallback. The language directory wins when more than one holds a
// record. Take files carry no language, so a second language would overwrite
// the first language's takes in a shared directory. The caller has already
// checked the language is a safe path segment.
func runWorkDirFor(storageDir, dubID, language string) string {
	flat := filepath.Join(storageDir, dubID, runWorkDir)
	perLanguage := filepath.Join(flat, resolveRunTag(language))
	if holdsRecord(perLanguage) {
		return perLanguage
	}
	if holdsRecordFor(flat, language) {
		return flat
	}
	if raw := rawCodeWorkDir(flat, perLanguage, language); raw != "" {
		return raw
	}
	return perLanguage
}

// resolveRunTag returns the catalog tag that names the same language as code.
// The sample code `ml` names the catalog tag `ml-IN`, so both spellings share
// one work directory. A code the catalog does not extend stays as it is. So
// does an ambiguous short code such as `en`, which names four catalog tags.
func resolveRunTag(code string) string {
	match := ""
	for _, tag := range tts.CommittedLanguages() {
		if tag == code {
			return code
		}
		if !sameRunLanguage(tag, code) {
			continue
		}
		if match != "" {
			return code
		}
		match = tag
	}
	if match != "" {
		return match
	}
	return code
}

// rawCodeWorkDir returns an existing language directory that holds a record
// for the same language under another spelling. The sample code `ml` and the
// catalog tag `ml-IN` wrote separate directories before the tag naming, so a
// resume must read the one that holds the records.
func rawCodeWorkDir(flat, perLanguage, language string) string {
	dirs, err := filepath.Glob(filepath.Join(flat, "*"))
	if err != nil {
		return ""
	}
	for _, dir := range dirs {
		if dir == perLanguage || !holdsRecord(dir) {
			continue
		}
		if holdsRecordFor(dir, language) {
			return dir
		}
	}
	return ""
}

// holdsRecord reports whether dir holds any resume record.
func holdsRecord(dir string) bool {
	records, err := filepath.Glob(filepath.Join(dir, "seg_*_result.json"))
	return err == nil && len(records) > 0
}

// holdsRecordFor reports whether dir holds a resume record written for
// language. A record is what a resumed run reads, so a take alone does not
// count. The flat directory can hold records for several languages, because it
// preceded the per-language layout.
func holdsRecordFor(dir, language string) bool {
	records, err := filepath.Glob(filepath.Join(dir, "seg_*_result.json"))
	if err != nil {
		return false
	}
	for _, record := range records {
		if sameRunLanguage(recordLanguage(record), language) {
			return true
		}
	}
	return false
}

// recordLanguage reads the language one resume record names.
// An unreadable record names no language.
func recordLanguage(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var record struct {
		Language string `json:"language"`
	}
	if json.Unmarshal(data, &record) != nil {
		return ""
	}
	return record.Language
}

// sameRunLanguage reports whether two codes name the same language.
// The catalog code `ml` and the tag `ml-IN` name one language, so a subtag
// prefix counts. Two sibling locales such as `pt-BR` and `pt-PT` do not.
func sameRunLanguage(first, second string) bool {
	if first == second {
		return true
	}
	return strings.HasPrefix(first, second+"-") || strings.HasPrefix(second, first+"-")
}

// resolveProjectMedia finds the source video and the optional music track.
// The upload record does not persist those paths, so the run globs them.
func resolveProjectMedia(storageDir, dubID string) (string, string, error) {
	if storageDir == "" || !safeDubID(dubID) {
		return "", "", errNoSourceVideo
	}
	dir := filepath.Join(storageDir, dubID)
	sources, err := filepath.Glob(filepath.Join(dir, "source.*"))
	if err != nil || len(sources) == 0 {
		return "", "", errNoSourceVideo
	}
	music := ""
	if tracks, err := filepath.Glob(filepath.Join(dir, "music.*")); err == nil && len(tracks) > 0 {
		music = tracks[0]
	}
	return sources[0], music, nil
}

// safeDubID reports whether an id names exactly one upload directory.
// A glob metacharacter or a path separator would escape that directory.
func safeDubID(dubID string) bool {
	if dubID == "" || dubID == "." || dubID == ".." {
		return false
	}
	for _, r := range dubID {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-', r == '_', r == '.':
		default:
			return false
		}
	}
	return true
}

// writeRunFailure answers one run route with a sentence and no internal detail.
func writeRunFailure(w http.ResponseWriter, status int, sentence string) {
	writeHistoryJSON(w, status, historyError{Error: sentence})
}

// runIDCounter backs the identifier fallback when the random source fails.
var runIDCounter atomic.Uint64

// newRunID mints a run identifier.
func newRunID() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err == nil {
		return hex.EncodeToString(buf[:])
	}
	return fmt.Sprintf("run-%d", runIDCounter.Add(1))
}
