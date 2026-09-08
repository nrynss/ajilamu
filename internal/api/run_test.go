package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nrynss/ajilamu/internal/api"
	"github.com/nrynss/ajilamu/internal/config"
	"github.com/nrynss/ajilamu/internal/types"
)

// runFunc adapts a function to api.PipelineRunner.
type runFunc func(ctx context.Context, req api.RunRequest, emit func(api.ProgressEvent)) (api.RunResult, error)

// Run invokes the wrapped function.
func (f runFunc) Run(ctx context.Context, req api.RunRequest, emit func(api.ProgressEvent)) (api.RunResult, error) {
	return f(ctx, req, emit)
}

// recordFunc adapts a function to api.RunRecorder.
type recordFunc func(ctx context.Context, req api.RunRequest, result api.RunResult) error

// Persist invokes the wrapped function.
func (f recordFunc) Persist(ctx context.Context, req api.RunRequest, result api.RunResult) error {
	return f(ctx, req, result)
}

// TestRunStartAnswersAccepted proves the start route answers 202 before the run
// finishes and names the new run.
func TestRunStartAnswersAccepted(t *testing.T) {
	storage := t.TempDir()
	writeProjectSource(t, storage, "dub-accept")
	recorded := make(chan api.RunRequest, 1)
	runner := runFunc(func(context.Context, api.RunRequest, func(api.ProgressEvent)) (api.RunResult, error) {
		return api.RunResult{TotalCost: 42}, nil
	})
	recorder := recordFunc(func(_ context.Context, req api.RunRequest, _ api.RunResult) error {
		recorded <- req
		return nil
	})
	base := newRunTestServer(t, api.ServerOptions{Runner: runner, Recorder: recorder, StorageDir: storage})

	response := postRun(t, base, "dub-accept", "language=ml")
	defer response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("start status = %d, want 202", response.StatusCode)
	}
	var payload struct {
		RunID  string `json:"run_id"`
		Status string `json:"status"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode start body: %v", err)
	}
	if payload.RunID == "" {
		t.Fatal("start body carried no run_id")
	}
	if payload.Status != "running" {
		t.Fatalf("start status = %q, want running", payload.Status)
	}

	select {
	case req := <-recorded:
		if req.Language != "ml" {
			t.Errorf("recorded language = %q, want ml", req.Language)
		}
		if req.DubID != "dub-accept" {
			t.Errorf("recorded dub = %q, want dub-accept", req.DubID)
		}
		if want := filepath.Join(storage, "dub-accept", "source.mp4"); req.Source != want {
			t.Errorf("recorded source = %q, want %q", req.Source, want)
		}
		if want := filepath.Join(storage, "dub-accept", "work", "ml-IN"); req.WorkDir != want {
			t.Errorf("recorded work dir = %q, want %q", req.WorkDir, want)
		}
		if req.SourceLanguage != "" {
			t.Errorf("recorded source language = %q, want empty for a project with no record", req.SourceLanguage)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the recorder never saw the finished run")
	}
}

// TestRunStartCarriesStoredLanguages proves the run request reads the source
// language from the upload record and gives each language its own work
// directory, so a second language cannot overwrite the first language's takes.
func TestRunStartCarriesStoredLanguages(t *testing.T) {
	storage := t.TempDir()
	writeProjectSource(t, storage, "dub-languages")
	writeProjectRecord(t, storage, "dub-languages", "en-US", "ml")
	recorded := make(chan api.RunRequest, 1)
	runner := runFunc(func(context.Context, api.RunRequest, func(api.ProgressEvent)) (api.RunResult, error) {
		return api.RunResult{}, nil
	})
	recorder := recordFunc(func(_ context.Context, req api.RunRequest, _ api.RunResult) error {
		recorded <- req
		return nil
	})
	base := newRunTestServer(t, api.ServerOptions{Runner: runner, Recorder: recorder, StorageDir: storage})

	response := postRun(t, base, "dub-languages", "language=ml-IN")
	defer response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("start status = %d, want 202", response.StatusCode)
	}

	select {
	case req := <-recorded:
		if req.SourceLanguage != "en-US" {
			t.Errorf("recorded source language = %q, want en-US", req.SourceLanguage)
		}
		if req.Language != "ml-IN" {
			t.Errorf("recorded language = %q, want ml-IN", req.Language)
		}
		if want := filepath.Join(storage, "dub-languages", "work", "ml-IN"); req.WorkDir != want {
			t.Errorf("recorded work dir = %q, want %q", req.WorkDir, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the recorder never saw the finished run")
	}
}

// TestRunStartResumesFlatWorkDir proves a run falls back to the flat work
// directory when the language directory holds no record. A run interrupted
// before the per-language layout left its records in the flat directory, so a
// resume must read them there.
func TestRunStartResumesFlatWorkDir(t *testing.T) {
	cases := []struct {
		name     string
		language string
		record   string
		want     string
	}{
		{name: "catalog code", language: "ml-IN", record: "ml-IN", want: "work"},
		{name: "sample code", language: "ml", record: "ml-IN", want: "work"},
		{name: "other language", language: "ml-IN", record: "ta-IN", want: filepath.Join("work", "ml-IN")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			storage := t.TempDir()
			writeProjectSource(t, storage, "dub-resume")
			writeProjectRecord(t, storage, "dub-resume", "en-US", "ml-IN")
			writeSegmentRecordFile(t, storage, "dub-resume", "work", tc.record)

			got := startedWorkDir(t, storage, "dub-resume", tc.language)
			t.Logf("request language=%s flat record=%s work dir=%s", tc.language, tc.record, got)
			if want := filepath.Join(storage, "dub-resume", tc.want); got != want {
				t.Errorf("recorded work dir = %q, want %q", got, want)
			}
		})
	}
}

// TestRunStartPrefersLanguageWorkDir proves the language directory wins when
// both directories hold a record. The flat directory is only a fallback.
func TestRunStartPrefersLanguageWorkDir(t *testing.T) {
	storage := t.TempDir()
	writeProjectSource(t, storage, "dub-both")
	writeProjectRecord(t, storage, "dub-both", "en-US", "ml-IN")
	writeSegmentRecordFile(t, storage, "dub-both", "work", "ml-IN")
	writeSegmentRecordFile(t, storage, "dub-both", filepath.Join("work", "ml-IN"), "ml-IN")

	got := startedWorkDir(t, storage, "dub-both", "ml-IN")
	t.Logf("both directories hold a record, work dir=%s", got)
	if want := filepath.Join(storage, "dub-both", "work", "ml-IN"); got != want {
		t.Errorf("recorded work dir = %q, want %q", got, want)
	}
}

// TestRunStartReusesRawCodeWorkDir proves a resume reads the directory an
// earlier run named from the raw code. The sample code `ml` and the catalog
// tag `ml-IN` wrote separate directories before the tag naming, so a resume
// that switches spelling must read the records the earlier run left.
func TestRunStartReusesRawCodeWorkDir(t *testing.T) {
	storage := t.TempDir()
	writeProjectSource(t, storage, "dub-raw-code")
	writeProjectRecord(t, storage, "dub-raw-code", "en-US", "ml")
	writeSegmentRecordFile(t, storage, "dub-raw-code", filepath.Join("work", "ml"), "ml")

	got := startedWorkDir(t, storage, "dub-raw-code", "ml-IN")
	t.Logf("record in work/ml, resume language=ml-IN, work dir=%s", got)
	if want := filepath.Join(storage, "dub-raw-code", "work", "ml"); got != want {
		t.Errorf("recorded work dir = %q, want %q", got, want)
	}
}

// startedWorkDir starts one run and returns the work directory its request
// carried.
func startedWorkDir(t *testing.T, storage, dubID, language string) string {
	t.Helper()
	recorded := make(chan api.RunRequest, 1)
	runner := runFunc(func(context.Context, api.RunRequest, func(api.ProgressEvent)) (api.RunResult, error) {
		return api.RunResult{}, nil
	})
	recorder := recordFunc(func(_ context.Context, req api.RunRequest, _ api.RunResult) error {
		recorded <- req
		return nil
	})
	base := newRunTestServer(t, api.ServerOptions{Runner: runner, Recorder: recorder, StorageDir: storage})

	response := postRun(t, base, dubID, "language="+language)
	defer response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("start status = %d, want 202", response.StatusCode)
	}
	select {
	case req := <-recorded:
		return req.WorkDir
	case <-time.After(5 * time.Second):
		t.Fatal("the recorder never saw the finished run")
		return ""
	}
}

// TestRunStartRejectsUnsafeLanguage proves a language that is not a safe path
// segment answers 400 and starts no run. The language names a work directory,
// so a separator must not escape the project directory.
func TestRunStartRejectsUnsafeLanguage(t *testing.T) {
	storage := t.TempDir()
	writeProjectSource(t, storage, "dub-unsafe")
	var calls atomic.Int64
	runner := runFunc(func(context.Context, api.RunRequest, func(api.ProgressEvent)) (api.RunResult, error) {
		calls.Add(1)
		return api.RunResult{}, nil
	})
	base := newRunTestServer(t, api.ServerOptions{
		Runner:     runner,
		Recorder:   recordFunc(func(context.Context, api.RunRequest, api.RunResult) error { return nil }),
		StorageDir: storage,
	})

	response := postRun(t, base, "dub-unsafe", "language=../escape")
	defer response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", response.StatusCode)
	}
	if calls.Load() != 0 {
		t.Fatalf("the runner started %d times for an unsafe language, want 0", calls.Load())
	}
}

// TestRunMintsLedgerIdentity proves the run mints the commit and take ids the
// recorder needs, and links every snapshot to its take.
func TestRunMintsLedgerIdentity(t *testing.T) {
	storage := t.TempDir()
	writeProjectSource(t, storage, "dub-identity")
	recorded := make(chan api.RunResult, 1)
	runner := runFunc(func(context.Context, api.RunRequest, func(api.ProgressEvent)) (api.RunResult, error) {
		return api.RunResult{
			Takes: []api.RunTake{{Segment: types.Segment{ID: 1}}},
			Timeline: []api.RunSegmentState{
				{SegmentIndex: 1},
			},
		}, nil
	})
	recorder := recordFunc(func(_ context.Context, _ api.RunRequest, result api.RunResult) error {
		recorded <- result
		return nil
	})
	base := newRunTestServer(t, api.ServerOptions{Runner: runner, Recorder: recorder, StorageDir: storage})

	response := postRun(t, base, "dub-identity", "language=ml")
	response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("start status = %d, want 202", response.StatusCode)
	}
	select {
	case result := <-recorded:
		if result.CommitID == "" {
			t.Error("the run minted no commit id")
		}
		if result.ProjectID != "dub-identity" {
			t.Errorf("project id = %q, want dub-identity", result.ProjectID)
		}
		if result.OwnerID == "" {
			t.Error("the run minted no owner id")
		}
		if len(result.Takes) != 1 || result.Takes[0].TakeID == "" {
			t.Fatalf("minted takes = %+v, want one take with an id", result.Takes)
		}
		if len(result.Timeline) != 1 || result.Timeline[0].TakeID != result.Takes[0].TakeID {
			t.Fatalf("minted timeline = %+v, want the take id %q", result.Timeline, result.Takes[0].TakeID)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the recorder never saw the run result")
	}
}

// TestRunStartRejectsDuplicate proves a second start for a running dub answers 409.
func TestRunStartRejectsDuplicate(t *testing.T) {
	storage := t.TempDir()
	writeProjectSource(t, storage, "dub-busy")
	started := make(chan struct{})
	gate := make(chan struct{})
	runner := runFunc(func(ctx context.Context, _ api.RunRequest, _ func(api.ProgressEvent)) (api.RunResult, error) {
		close(started)
		select {
		case <-gate:
		case <-ctx.Done():
		}
		return api.RunResult{}, ctx.Err()
	})
	base := newRunTestServer(t, api.ServerOptions{
		Runner:     runner,
		Recorder:   recordFunc(func(context.Context, api.RunRequest, api.RunResult) error { return nil }),
		StorageDir: storage,
	})

	first := postRun(t, base, "dub-busy", "language=ml")
	first.Body.Close()
	if first.StatusCode != http.StatusAccepted {
		t.Fatalf("first start status = %d, want 202", first.StatusCode)
	}
	<-started

	second := postRun(t, base, "dub-busy", "language=ml")
	defer second.Body.Close()
	if second.StatusCode != http.StatusConflict {
		t.Fatalf("second start status = %d, want 409", second.StatusCode)
	}
	if got := decodeRunError(t, second); got != "A run is already active for this project." {
		t.Fatalf("duplicate error = %q", got)
	}
	close(gate)
	drainEvents(t, base, "dub-busy")
}

// TestRunStartRejectsBlankLanguage proves a blank language answers 400 before any work.
func TestRunStartRejectsBlankLanguage(t *testing.T) {
	storage := t.TempDir()
	writeProjectSource(t, storage, "dub-lang")
	var ran atomic.Bool
	runner := runFunc(func(context.Context, api.RunRequest, func(api.ProgressEvent)) (api.RunResult, error) {
		ran.Store(true)
		return api.RunResult{}, nil
	})
	base := newRunTestServer(t, api.ServerOptions{
		Runner:     runner,
		Recorder:   recordFunc(func(context.Context, api.RunRequest, api.RunResult) error { return nil }),
		StorageDir: storage,
	})

	response := postRun(t, base, "dub-lang", "")
	defer response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("blank language status = %d, want 400", response.StatusCode)
	}
	if got := decodeRunError(t, response); !strings.Contains(got, "language") {
		t.Fatalf("blank language error = %q, want it to name the parameter", got)
	}
	if ran.Load() {
		t.Fatal("the runner started despite a blank language")
	}
}

// TestRunStartRejectsMissingVideo proves a project with no source answers 404.
func TestRunStartRejectsMissingVideo(t *testing.T) {
	storage := t.TempDir()
	base := newRunTestServer(t, api.ServerOptions{
		Runner: runFunc(func(context.Context, api.RunRequest, func(api.ProgressEvent)) (api.RunResult, error) {
			return api.RunResult{}, nil
		}),
		Recorder:   recordFunc(func(context.Context, api.RunRequest, api.RunResult) error { return nil }),
		StorageDir: storage,
	})

	response := postRun(t, base, "dub-missing", "language=ml")
	defer response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("missing video status = %d, want 404", response.StatusCode)
	}
	if got := decodeRunError(t, response); got != "This project has no source video." {
		t.Fatalf("missing video error = %q", got)
	}
}

// TestRunRoutesUnavailableWithoutDependencies proves a nil runner or recorder
// answers 503 on every run route.
func TestRunRoutesUnavailableWithoutDependencies(t *testing.T) {
	base := newRunTestServer(t, api.ServerOptions{})
	for _, request := range []struct {
		method string
		path   string
	}{
		{method: http.MethodPost, path: "/api/dubs/dub-none/run?language=ml"},
		{method: http.MethodPost, path: "/api/dubs/dub-none/run/cancel"},
		{method: http.MethodGet, path: "/api/dubs/dub-none/events"},
	} {
		req, err := http.NewRequest(request.method, base+request.path, nil)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", request.method, request.path, err)
		}
		if response.StatusCode != http.StatusServiceUnavailable {
			response.Body.Close()
			t.Fatalf("%s %s status = %d, want 503", request.method, request.path, response.StatusCode)
		}
		if got := decodeRunError(t, response); got != "Runs are unavailable." {
			t.Fatalf("%s %s error = %q", request.method, request.path, got)
		}
	}
}

// TestRunCancelStopsTheRun proves cancel answers 202, cancels the pipeline
// context, and leaves no active run behind.
func TestRunCancelStopsTheRun(t *testing.T) {
	storage := t.TempDir()
	writeProjectSource(t, storage, "dub-cancel")
	started := make(chan struct{})
	stopped := make(chan error, 1)
	runner := runFunc(func(ctx context.Context, _ api.RunRequest, _ func(api.ProgressEvent)) (api.RunResult, error) {
		close(started)
		<-ctx.Done()
		stopped <- ctx.Err()
		return api.RunResult{}, ctx.Err()
	})
	base := newRunTestServer(t, api.ServerOptions{
		Runner:     runner,
		Recorder:   recordFunc(func(context.Context, api.RunRequest, api.RunResult) error { return nil }),
		StorageDir: storage,
	})

	start := postRun(t, base, "dub-cancel", "language=ml")
	start.Body.Close()
	if start.StatusCode != http.StatusAccepted {
		t.Fatalf("start status = %d, want 202", start.StatusCode)
	}
	<-started

	response := postCancel(t, base, "dub-cancel")
	defer response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("cancel status = %d, want 202", response.StatusCode)
	}
	var payload struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode cancel body: %v", err)
	}
	if payload.Status != "cancelling" {
		t.Fatalf("cancel status = %q, want cancelling", payload.Status)
	}

	select {
	case err := <-stopped:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("pipeline context error = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the pipeline never observed cancellation")
	}
	drainEvents(t, base, "dub-cancel")

	again := postCancel(t, base, "dub-cancel")
	defer again.Body.Close()
	if again.StatusCode != http.StatusConflict {
		t.Fatalf("second cancel status = %d, want 409", again.StatusCode)
	}
	if got := decodeRunError(t, again); got != "No run is active for this project." {
		t.Fatalf("second cancel error = %q", got)
	}
}

// TestRunCancelRejectsWhenIdle proves a cancel with no run answers 409.
func TestRunCancelRejectsWhenIdle(t *testing.T) {
	base := newRunTestServer(t, api.ServerOptions{
		Runner: runFunc(func(context.Context, api.RunRequest, func(api.ProgressEvent)) (api.RunResult, error) {
			return api.RunResult{}, nil
		}),
		Recorder: recordFunc(func(context.Context, api.RunRequest, api.RunResult) error { return nil }),
	})

	response := postCancel(t, base, "dub-idle")
	defer response.Body.Close()
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("idle cancel status = %d, want 409", response.StatusCode)
	}
	if got := decodeRunError(t, response); got != "No run is active for this project." {
		t.Fatalf("idle cancel error = %q", got)
	}
}

// TestRunFailureLeavesNoActiveRun proves a failed run reports an error event
// and frees the dub for the next run.
func TestRunFailureLeavesNoActiveRun(t *testing.T) {
	storage := t.TempDir()
	writeProjectSource(t, storage, "dub-fail")
	var recorded atomic.Bool
	runner := runFunc(func(context.Context, api.RunRequest, func(api.ProgressEvent)) (api.RunResult, error) {
		return api.RunResult{}, errors.New("pipeline exploded")
	})
	recorder := recordFunc(func(context.Context, api.RunRequest, api.RunResult) error {
		recorded.Store(true)
		return nil
	})
	base := newRunTestServer(t, api.ServerOptions{Runner: runner, Recorder: recorder, StorageDir: storage})

	first := postRun(t, base, "dub-fail", "language=ml")
	first.Body.Close()
	if first.StatusCode != http.StatusAccepted {
		t.Fatalf("start status = %d, want 202", first.StatusCode)
	}
	body := drainEvents(t, base, "dub-fail")
	if !strings.Contains(body, `"type":"error"`) {
		t.Fatalf("event stream = %q, want an error event", body)
	}
	if recorded.Load() {
		t.Fatal("the recorder persisted a failed run")
	}

	second := postRun(t, base, "dub-fail", "language=ml")
	defer second.Body.Close()
	if second.StatusCode != http.StatusAccepted {
		t.Fatalf("second start status = %d, want 202 after the failure", second.StatusCode)
	}
}

// newRunTestServer mounts the API over the given options and returns its URL.
func newRunTestServer(t *testing.T, options api.ServerOptions) string {
	t.Helper()
	cfg, err := config.LoadFromMap(map[string]string{})
	if err != nil {
		t.Fatalf("load fixture config: %v", err)
	}
	options.FrontendRoot = testFrontend(t)
	server, err := api.NewServer(cfg, options)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)
	return httpServer.URL
}

// writeProjectSource writes one upload project with a source video.
func writeProjectSource(t *testing.T, storage, dubID string) {
	t.Helper()
	dir := filepath.Join(storage, dubID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create project directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "source.mp4"), []byte("source"), 0o644); err != nil {
		t.Fatalf("write source video: %v", err)
	}
}

// writeProjectRecord writes the upload record of one project. An empty source
// language stands for a record written before T7.5b.
func writeProjectRecord(t *testing.T, storage, dubID, sourceLanguage, language string) {
	t.Helper()
	record := map[string]string{
		"id":              dubID,
		"title":           "clip.mp4",
		"source_language": sourceLanguage,
		"language":        language,
		"created_at":      "2026-09-09T00:00:00Z",
	}
	payload, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("marshal project record: %v", err)
	}
	if err := os.WriteFile(filepath.Join(storage, dubID, "project.json"), payload, 0o644); err != nil {
		t.Fatalf("write project record: %v", err)
	}
}

// writeSegmentRecordFile writes one resume record in a work directory.
func writeSegmentRecordFile(t *testing.T, storage, dubID, workDir, language string) {
	t.Helper()
	dir := filepath.Join(storage, dubID, workDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create work directory: %v", err)
	}
	payload, err := json.Marshal(map[string]string{"language": language})
	if err != nil {
		t.Fatalf("marshal resume record: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "seg_1_result.json"), payload, 0o644); err != nil {
		t.Fatalf("write resume record: %v", err)
	}
}

// postRun starts one run. rawQuery is appended verbatim, so an empty value
// leaves the language parameter out.
func postRun(t *testing.T, base, dubID, rawQuery string) *http.Response {
	t.Helper()
	target := base + "/api/dubs/" + dubID + "/run"
	if rawQuery != "" {
		target += "?" + rawQuery
	}
	request, err := http.NewRequest(http.MethodPost, target, nil)
	if err != nil {
		t.Fatalf("new start request: %v", err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("start run: %v", err)
	}
	return response
}

// postCancel cancels the run of one dub.
func postCancel(t *testing.T, base, dubID string) *http.Response {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, base+"/api/dubs/"+dubID+"/run/cancel", nil)
	if err != nil {
		t.Fatalf("new cancel request: %v", err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("cancel run: %v", err)
	}
	return response
}

// drainEvents reads the event stream to its end and returns the raw body.
func drainEvents(t *testing.T, base, dubID string) string {
	t.Helper()
	response, err := http.Get(base + "/api/dubs/" + dubID + "/events")
	if err != nil {
		t.Fatalf("read event stream: %v", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read event body: %v", err)
	}
	return string(body)
}

// decodeRunError reads one JSON failure body and closes the response.
func decodeRunError(t *testing.T, response *http.Response) string {
	t.Helper()
	defer response.Body.Close()
	var payload struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode failure body: %v", err)
	}
	return payload.Error
}

// TestRunStartRejectsGlobDubID proves a glob metacharacter in the path value
// answers 404 and starts no run, while a hex id still starts.
func TestRunStartRejectsGlobDubID(t *testing.T) {
	storage := t.TempDir()
	writeProjectSource(t, storage, "victim")
	var calls atomic.Int64
	runner := runFunc(func(context.Context, api.RunRequest, func(api.ProgressEvent)) (api.RunResult, error) {
		calls.Add(1)
		return api.RunResult{}, nil
	})
	recorder := recordFunc(func(context.Context, api.RunRequest, api.RunResult) error {
		return nil
	})
	base := newRunTestServer(t, api.ServerOptions{Runner: runner, Recorder: recorder, StorageDir: storage})

	response := postRun(t, base, "%2A", "language=ml")
	defer response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("glob dub id status = %d, want 404", response.StatusCode)
	}
	if calls.Load() != 0 {
		t.Fatalf("the runner started %d times for a glob dub id, want 0", calls.Load())
	}

	const hexID = "0123456789abcdef0123456789abcdef"
	writeProjectSource(t, storage, hexID)
	start := postRun(t, base, hexID, "language=ml")
	start.Body.Close()
	if start.StatusCode != http.StatusAccepted {
		t.Fatalf("hex dub id status = %d, want 202", start.StatusCode)
	}
}
