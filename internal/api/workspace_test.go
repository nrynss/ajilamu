package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nrynss/ajilamu/internal/api"
)

// standInWorkspaceReader answers the workspace reads without a ledger.
// It counts calls, so a rejected request can prove it never read.
type standInWorkspaceReader struct {
	tracks    []api.LanguageTrack
	charges   []api.Charge
	total     api.Total
	languages []string
	metadata  api.DubSummary
	err       error
	calls     int
}

func (s *standInWorkspaceReader) WorkspaceTakes(context.Context, string) ([]api.LanguageTrack, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return s.tracks, nil
}

func (s *standInWorkspaceReader) WholePassCharges(context.Context, string) ([]api.Charge, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return s.charges, nil
}

func (s *standInWorkspaceReader) RunningTotal(context.Context, string) (api.Total, error) {
	s.calls++
	if s.err != nil {
		return api.Total{}, s.err
	}
	return s.total, nil
}

func (s *standInWorkspaceReader) Languages(context.Context, string) ([]string, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return s.languages, nil
}

func (s *standInWorkspaceReader) ProjectMetadata(context.Context, string) (api.DubSummary, error) {
	s.calls++
	if s.err != nil {
		return api.DubSummary{}, s.err
	}
	return s.metadata, nil
}

// standInWorkspaceHistory records the timeline read, so a test can pin that the
// segments come from the newest commit of the first language.
type standInWorkspaceHistory struct {
	commits       []api.Commit
	segments      []api.TimelineEntry
	err           error
	timelineCalls int
	timelineDub   string
	timelineLang  string
	timelineHead  string
}

func (s *standInWorkspaceHistory) ListCommits(context.Context, string) ([]api.Commit, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.commits, nil
}

func (s *standInWorkspaceHistory) TimelineAt(_ context.Context, dubID, language, commitID string) ([]api.TimelineEntry, error) {
	s.timelineCalls++
	s.timelineDub, s.timelineLang, s.timelineHead = dubID, language, commitID
	if s.err != nil {
		return nil, s.err
	}
	return s.segments, nil
}

func (s *standInWorkspaceHistory) CompareBranches(context.Context, string, string, string, string) (api.BranchComparison, error) {
	if s.err != nil {
		return api.BranchComparison{}, s.err
	}
	return api.BranchComparison{}, nil
}

// serveWorkspace runs one handler and asserts the headers every route shares.
func serveWorkspace(t *testing.T, handler http.Handler, id string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/api/dubs/"+id, nil)
	request.SetPathValue("id", id)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	return response
}

// readyWorkspaceReader is the stand-in the assembled payload test drives.
func readyWorkspaceReader() *standInWorkspaceReader {
	segmentID := 1
	return &standInWorkspaceReader{
		tracks: []api.LanguageTrack{{
			Language: "ml",
			Lines: []api.Line{
				{
					SegmentID: 1,
					Text:      "നമസ്കാരം",
					Takes: []api.Take{{
						File:    "seg_1_try1.wav",
						Voice:   "ml-IN-Chirp3-HD-Achernar",
						Attempt: 1,
						Repair:  api.RepairNone,
						Fit:     api.Fit{SlotMs: 1820, MeasuredMs: 1680, DeltaMs: -140, State: api.StateFits},
						Charges: []api.Charge{{
							Kind:                 api.ChargeTranslate,
							SegmentID:            &segmentID,
							TakeFile:             "seg_1_try1.wav",
							Units:                24,
							UnitPriceNanodollars: 1000,
							TotalNanodollars:     24000,
						}},
					}},
				},
				{
					SegmentID: 2,
					Text:      "ഞാൻ ഒരു ബഹിരാകാശ സഞ്ചാരി",
					Takes: []api.Take{{
						File:    "seg_2_try1.wav",
						Voice:   "ml-IN-Chirp3-HD-Achernar",
						Attempt: 1,
						Repair:  api.RepairNone,
						Fit:     api.Fit{SlotMs: 5480, MeasuredMs: 5480, DeltaMs: 0, State: api.StateFits},
						Charges: []api.Charge{},
					}},
				},
			},
		}},
		charges: []api.Charge{{
			Kind:                 api.ChargeSegment,
			Units:                8,
			UnitPriceNanodollars: 500,
			TotalNanodollars:     4000,
		}},
		total:     api.Total{TotalNanodollars: 28000, Covers: "1 segment call, 1 translation call, and 0 render calls."},
		languages: []string{"ml"},
		metadata: api.DubSummary{
			ID:        "dub-1",
			CreatedAt: "2026-09-01T00:00:00Z",
			UpdatedAt: "2026-09-02T00:00:00Z",
		},
	}
}

func TestWorkspaceRouteServesAssembledDub(t *testing.T) {
	history := &standInWorkspaceHistory{
		commits: []api.Commit{
			{CommitID: "commit-1", VersionNumber: 1, Action: api.ActionSegmentCreated, Author: api.AuthorAgent},
			{CommitID: "commit-2", ParentCommitID: "commit-1", VersionNumber: 2, Action: api.ActionTakeRendered, Author: api.AuthorAgent},
		},
		segments: []api.TimelineEntry{
			{SegmentIndex: 1, StartMs: 5908, EndMs: 7728, Speaker: "Suni Williams", Emotion: "Warm", SourceText: "Hi, I'm Suni Williams"},
			{SegmentIndex: 2, StartMs: 7728, EndMs: 13208, Speaker: "Suni Williams", Emotion: "Explanatory", SourceText: "and I'm an astronaut"},
		},
	}
	handler := api.WorkspaceHandlerFrom(
		readyWorkspaceReader(),
		history,
		func(string) (string, string, bool) { return "Creator video.mp4", "", true },
		nil,
		nil,
	)
	response := serveWorkspace(t, handler, "dub-1")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body.String())
	}
	var dub api.Dub
	raw := decodeHistoryBody(t, response, &dub)
	t.Logf("assembled dub: %s", raw)

	if dub.ID != "dub-1" {
		t.Errorf("id = %q, want dub-1", dub.ID)
	}
	if dub.Title != "Creator video.mp4" {
		t.Errorf("title = %q, want Creator video.mp4", dub.Title)
	}
	if dub.Readiness != api.ReadinessReady {
		t.Errorf("readiness = %q, want %s", dub.Readiness, api.ReadinessReady)
	}
	if len(dub.Segments) != 2 {
		t.Fatalf("segments = %d, want 2", len(dub.Segments))
	}
	if dub.Segments[0].DurationMs != 1820 || dub.Segments[0].Text != "Hi, I'm Suni Williams" || dub.Segments[0].Speaker != "Suni Williams" {
		t.Errorf("segment 1 = %+v, want the head timeline source row", dub.Segments[0])
	}
	if len(dub.Languages) != 1 || dub.Languages[0].Language != "ml" {
		t.Fatalf("languages = %+v, want one ml track", dub.Languages)
	}
	lines := dub.Languages[0].Lines
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2", len(lines))
	}
	if len(lines[0].Takes) != 1 || len(lines[0].Takes[0].Charges) != 1 {
		t.Fatalf("line 1 take charges = %+v, want one itemized charge", lines[0].Takes)
	}
	charge := lines[0].Takes[0].Charges[0]
	if charge.Kind != api.ChargeTranslate || charge.TakeFile != "seg_1_try1.wav" || charge.SegmentID == nil || *charge.SegmentID != 1 {
		t.Errorf("take charge = %+v, want a translate charge bound to segment 1", charge)
	}
	if lines[0].Takes[0].Fit != (api.Fit{SlotMs: 1820, MeasuredMs: 1680, DeltaMs: -140, State: api.StateFits}) {
		t.Errorf("take fit = %+v, want the stand-in fit", lines[0].Takes[0].Fit)
	}
	if lines[1].Takes[0].Charges == nil || len(lines[1].Takes[0].Charges) != 0 {
		t.Errorf("charge-less take charges = %#v, want an empty array", lines[1].Takes[0].Charges)
	}
	if len(dub.Charges) != 1 || dub.Charges[0].SegmentID != nil || dub.Charges[0].Kind != api.ChargeSegment {
		t.Errorf("whole-pass charges = %+v, want one unbound segment charge", dub.Charges)
	}
	if dub.Total.TotalNanodollars != 28000 || dub.Total.Covers == "" {
		t.Errorf("total = %+v, want 28000 nanodollars with a covers sentence", dub.Total)
	}
	if len(dub.Commits) != 2 || dub.Commits[1].ParentCommitID != "commit-1" {
		t.Errorf("commits = %+v, want the two-commit chain", dub.Commits)
	}
	if dub.CreatedAt != "2026-09-01T00:00:00Z" || dub.UpdatedAt != "2026-09-02T00:00:00Z" {
		t.Errorf("timestamps = %q / %q, want the ledger metadata", dub.CreatedAt, dub.UpdatedAt)
	}
	if history.timelineCalls != 1 || history.timelineHead != "commit-2" || history.timelineLang != "ml" || history.timelineDub != "dub-1" {
		t.Errorf("timeline read = %d calls at %s/%s/%s, want one read of the head commit for the first language",
			history.timelineCalls, history.timelineDub, history.timelineLang, history.timelineHead)
	}
	if !strings.Contains(raw, `"charges":[]`) {
		t.Errorf("body = %s, want a charge-less take to carry an empty charges array", raw)
	}
	if strings.Contains(raw, `"charges":null`) {
		t.Errorf("body = %s, want no null charges array", raw)
	}
}

func TestWorkspaceRouteServesEmptyDubForStoredProject(t *testing.T) {
	reader := &standInWorkspaceReader{total: api.Total{Covers: "0 segment calls, 0 translation calls, and 0 render calls."}}
	handler := api.WorkspaceHandlerFrom(
		reader,
		nil,
		func(string) (string, string, bool) { return "Fresh upload.mp4", "2026-09-03T00:00:00Z", true },
		nil,
		nil,
	)
	response := serveWorkspace(t, handler, "dub-2")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body.String())
	}
	var dub api.Dub
	raw := decodeHistoryBody(t, response, &dub)
	if dub.Title != "Fresh upload.mp4" {
		t.Errorf("title = %q, want the upload record title", dub.Title)
	}
	if dub.Readiness != api.ReadinessPending {
		t.Errorf("readiness = %q, want %s", dub.Readiness, api.ReadinessPending)
	}
	if dub.CreatedAt != "2026-09-03T00:00:00Z" || dub.UpdatedAt != "2026-09-03T00:00:00Z" {
		t.Errorf("timestamps = %q / %q, want the upload record timestamp", dub.CreatedAt, dub.UpdatedAt)
	}
	for _, marker := range []string{`"segments":[]`, `"languages":[]`, `"charges":[]`, `"commits":[]`} {
		if !strings.Contains(raw, marker) {
			t.Errorf("body = %s, want %s", raw, marker)
		}
	}
}

func TestWorkspaceRouteServesLedgerProjectWithoutUploadRecord(t *testing.T) {
	handler := api.WorkspaceHandlerFrom(readyWorkspaceReader(), nil, nil, nil, nil)
	response := serveWorkspace(t, handler, "dub-3")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body.String())
	}
	var dub api.Dub
	decodeHistoryBody(t, response, &dub)
	if dub.ID != "dub-3" || len(dub.Languages) != 1 {
		t.Errorf("dub = %+v, want the ledger project with an empty title", dub)
	}
}

func TestWorkspaceRouteAnswersNotFoundForUnknownProject(t *testing.T) {
	handler := api.WorkspaceHandlerFrom(
		&standInWorkspaceReader{},
		nil,
		func(string) (string, string, bool) { return "", "", false },
		nil,
		nil,
	)
	response := serveWorkspace(t, handler, "missing")
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "This project does not exist.") {
		t.Errorf("body = %q, want the not-found sentence", response.Body.String())
	}
}

func TestWorkspaceRouteUnavailableWithoutReader(t *testing.T) {
	handler := api.WorkspaceHandlerFrom(nil, nil, nil, nil, nil)
	response := serveWorkspace(t, handler, "dub-1")
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", response.Code)
	}
	if !strings.Contains(response.Body.String(), "The project ledger is unavailable") {
		t.Errorf("body = %q, want the unavailable sentence", response.Body.String())
	}
}

func TestWorkspaceRouteHidesReadFailure(t *testing.T) {
	reader := &standInWorkspaceReader{err: errors.New("clickhouse refused the query")}
	handler := api.WorkspaceHandlerFrom(reader, nil, nil, nil, nil)
	response := serveWorkspace(t, handler, "dub-1")
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", response.Code)
	}
	if strings.Contains(response.Body.String(), "clickhouse") {
		t.Errorf("body = %q, want no internal detail", response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "Could not read the project ledger.") {
		t.Errorf("body = %q, want the read-failed sentence", response.Body.String())
	}
}

func TestWorkspaceRouteRejectsUnsafeProjectID(t *testing.T) {
	reader := readyWorkspaceReader()
	handler := api.WorkspaceHandlerFrom(reader, nil, nil, nil, nil)
	for _, id := range []string{"..", "../settings"} {
		response := serveWorkspace(t, handler, id)
		if response.Code != http.StatusNotFound {
			t.Errorf("id %q status = %d, want 404", id, response.Code)
		}
	}
	if reader.calls != 0 {
		t.Errorf("reader calls = %d, want no read for an unsafe id", reader.calls)
	}
}

func TestWorkspaceRouteDerivesReadiness(t *testing.T) {
	flagged := api.LanguageTrack{Language: "ml", Lines: []api.Line{
		{SegmentID: 1, Flagged: true, Takes: []api.Take{{File: "a.wav", Charges: []api.Charge{}}}},
	}}
	fitting := api.LanguageTrack{Language: "ml", Lines: []api.Line{
		{SegmentID: 1, Takes: []api.Take{{File: "a.wav", Fit: api.Fit{State: api.StateFits}, Charges: []api.Charge{}}}},
	}}
	waiting := api.LanguageTrack{Language: "ml", Lines: []api.Line{{SegmentID: 1}}}
	for _, test := range []struct {
		name   string
		tracks []api.LanguageTrack
		want   string
	}{
		{"no take", []api.LanguageTrack{waiting}, api.ReadinessPending},
		{"flagged line", []api.LanguageTrack{flagged}, api.ReadinessReview},
		{"every take fits", []api.LanguageTrack{fitting}, api.ReadinessReady},
	} {
		reader := &standInWorkspaceReader{tracks: test.tracks, languages: []string{"ml"}}
		handler := api.WorkspaceHandlerFrom(reader, nil, func(string) (string, string, bool) { return "Title", "", true }, nil, nil)
		response := serveWorkspace(t, handler, "dub-1")
		var dub api.Dub
		decodeHistoryBody(t, response, &dub)
		if dub.Readiness != test.want {
			t.Errorf("%s: readiness = %q, want %q", test.name, dub.Readiness, test.want)
		}
	}
}

// TestWorkspaceReadinessNeedsATakeForEverySegment pins M1. A segment with no
// rendered line is not finished, so the route must answer review rather than
// ready. A complete project still answers ready.
func TestWorkspaceReadinessNeedsATakeForEverySegment(t *testing.T) {
	history := func() *standInWorkspaceHistory {
		return &standInWorkspaceHistory{
			commits: []api.Commit{{CommitID: "commit-1", VersionNumber: 1, Action: api.ActionSegmentCreated, Author: api.AuthorAgent}},
			segments: []api.TimelineEntry{
				{SegmentIndex: 1, StartMs: 0, EndMs: 1000, SourceText: "one"},
				{SegmentIndex: 2, StartMs: 1000, EndMs: 2000, SourceText: "two"},
			},
		}
	}
	partial := readyWorkspaceReader()
	partial.tracks[0].Lines = partial.tracks[0].Lines[:1]
	complete := readyWorkspaceReader()
	for _, test := range []struct {
		name   string
		reader *standInWorkspaceReader
		want   string
	}{
		{"one rendered line for two segments", partial, api.ReadinessReview},
		{"both segments rendered", complete, api.ReadinessReady},
	} {
		handler := api.WorkspaceHandlerFrom(test.reader, history(), func(string) (string, string, bool) { return "Title", "", true }, nil, nil)
		response := serveWorkspace(t, handler, "dub-1")
		var dub api.Dub
		raw := decodeHistoryBody(t, response, &dub)
		t.Logf("%s: %s", test.name, raw)
		if dub.Readiness != test.want {
			t.Errorf("%s: readiness = %q, want %q", test.name, dub.Readiness, test.want)
		}
	}
}

// TestWorkspaceReadinessReportsRunningWhileARunHoldsTheDub pins M2. The wire
// declares running, so the route must answer it while the run registry holds an
// active run for the dub. Without an active run the route keeps its
// ledger-derived state.
func TestWorkspaceReadinessReportsRunningWhileARunHoldsTheDub(t *testing.T) {
	waiting := api.LanguageTrack{Language: "ml", Lines: []api.Line{{SegmentID: 1}}}
	flagged := api.LanguageTrack{Language: "ml", Lines: []api.Line{
		{SegmentID: 1, Flagged: true, Takes: []api.Take{{File: "a.wav", Charges: []api.Charge{}}}},
	}}
	fitting := api.LanguageTrack{Language: "ml", Lines: []api.Line{
		{SegmentID: 1, Takes: []api.Take{{File: "a.wav", Fit: api.Fit{State: api.StateFits}, Charges: []api.Charge{}}}},
	}}
	for _, test := range []struct {
		name   string
		tracks []api.LanguageTrack
		active bool
		want   string
	}{
		{"no take with a run in flight", []api.LanguageTrack{waiting}, true, api.ReadinessRunning},
		{"no take with no run", []api.LanguageTrack{waiting}, false, api.ReadinessPending},
		{"flagged line with a run in flight", []api.LanguageTrack{flagged}, true, api.ReadinessRunning},
		{"flagged line with no run", []api.LanguageTrack{flagged}, false, api.ReadinessReview},
		{"every take fits with a run in flight", []api.LanguageTrack{fitting}, true, api.ReadinessRunning},
		{"every take fits with no run", []api.LanguageTrack{fitting}, false, api.ReadinessReady},
	} {
		reader := &standInWorkspaceReader{tracks: test.tracks, languages: []string{"ml"}}
		active := func(string) bool { return test.active }
		handler := api.WorkspaceHandlerFrom(reader, nil, func(string) (string, string, bool) { return "Title", "", true }, active, nil)
		response := serveWorkspace(t, handler, "dub-1")
		var dub api.Dub
		raw := decodeHistoryBody(t, response, &dub)
		t.Logf("%s: %s", test.name, raw)
		if dub.Readiness != test.want {
			t.Errorf("%s: readiness = %q, want %q", test.name, dub.Readiness, test.want)
		}
	}
}

// TestWorkspaceRouteReportsRunningThroughTheServer pins M2 end to end. The
// server owns the run registry, so a start must flip the workspace route to
// running while the run is in flight and back to ready once it ends.
func TestWorkspaceRouteReportsRunningThroughTheServer(t *testing.T) {
	storage := t.TempDir()
	writeProjectSource(t, storage, "dub-live")
	started := make(chan struct{})
	release := make(chan struct{})
	runner := runFunc(func(ctx context.Context, _ api.RunRequest, _ func(api.ProgressEvent)) (api.RunResult, error) {
		close(started)
		select {
		case <-release:
		case <-ctx.Done():
		}
		return api.RunResult{}, nil
	})
	base := newRunTestServer(t, api.ServerOptions{
		Workspace:  readyWorkspaceReader(),
		Project:    func(string) (string, string, bool) { return "Live project.mp4", "", true },
		Runner:     runner,
		Recorder:   recordFunc(func(context.Context, api.RunRequest, api.RunResult) error { return nil }),
		StorageDir: storage,
	})

	readiness := func() string {
		t.Helper()
		response, err := http.Get(base + "/api/dubs/dub-live")
		if err != nil {
			t.Fatalf("get workspace: %v", err)
		}
		defer response.Body.Close()
		var dub api.Dub
		if err := json.NewDecoder(response.Body).Decode(&dub); err != nil {
			t.Fatalf("decode workspace: %v", err)
		}
		t.Logf("readiness = %q", dub.Readiness)
		return dub.Readiness
	}

	if got := readiness(); got != api.ReadinessReady {
		t.Fatalf("readiness before the run = %q, want %q", got, api.ReadinessReady)
	}
	start := postRun(t, base, "dub-live", "language=ml")
	defer start.Body.Close()
	if start.StatusCode != http.StatusAccepted {
		t.Fatalf("start status = %d, want 202", start.StatusCode)
	}
	<-started
	if got := readiness(); got != api.ReadinessRunning {
		t.Fatalf("readiness during the run = %q, want %q", got, api.ReadinessRunning)
	}
	close(release)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if readiness() == api.ReadinessReady {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("readiness after the run never returned to %q", api.ReadinessReady)
}

// TestWorkspaceRouteFillsTimestampsFromUploadRecord pins L1. A stored project
// with no commits carries empty ledger metadata, so the route must fall back
// to the upload record timestamp.
func TestWorkspaceRouteFillsTimestampsFromUploadRecord(t *testing.T) {
	storage := t.TempDir()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	video, err := form.CreateFormFile("video", "stored.mp4")
	if err != nil {
		t.Fatalf("create video part: %v", err)
	}
	if _, err := video.Write([]byte("video bytes")); err != nil {
		t.Fatalf("write video part: %v", err)
	}
	if err := form.WriteField("language", "ml"); err != nil {
		t.Fatalf("write language field: %v", err)
	}
	if err := form.Close(); err != nil {
		t.Fatalf("close form: %v", err)
	}
	uploadRequest := httptest.NewRequest(http.MethodPost, "/api/dubs/new", &body)
	uploadRequest.Header.Set("Content-Type", form.FormDataContentType())
	uploadResponse := httptest.NewRecorder()
	api.NewUploadHandler(storage).ServeHTTP(uploadResponse, uploadRequest)
	if uploadResponse.Code != http.StatusCreated {
		t.Fatalf("upload status = %d, want 201: %s", uploadResponse.Code, uploadResponse.Body.String())
	}
	var upload struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(uploadResponse.Body).Decode(&upload); err != nil {
		t.Fatalf("decode upload: %v", err)
	}
	recordPath := filepath.Join(storage, upload.ID, "project.json")
	record, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatalf("read record: %v", err)
	}
	var stored struct {
		CreatedAt string `json:"created_at"`
	}
	if err := json.Unmarshal(record, &stored); err != nil {
		t.Fatalf("decode record: %v", err)
	}
	reader := &standInWorkspaceReader{total: api.Total{Covers: "0 segment calls, 0 translation calls, and 0 render calls."}}
	handler := api.WorkspaceHandlerFrom(reader, nil, api.UploadProjectLookup(storage), nil, nil)
	response := serveWorkspace(t, handler, upload.ID)
	var dub api.Dub
	raw := decodeHistoryBody(t, response, &dub)
	t.Logf("record file %s: %s", recordPath, record)
	t.Logf("response: %s", raw)
	if dub.CreatedAt != stored.CreatedAt || dub.UpdatedAt != stored.CreatedAt {
		t.Errorf("timestamps = %q / %q, want the record timestamp %q", dub.CreatedAt, dub.UpdatedAt, stored.CreatedAt)
	}
}
