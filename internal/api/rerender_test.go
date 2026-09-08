package api_test

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nrynss/ajilamu/internal/api"
	"github.com/nrynss/ajilamu/internal/cost"
	"github.com/nrynss/ajilamu/internal/fit"
	"github.com/nrynss/ajilamu/internal/gemini"
	"github.com/nrynss/ajilamu/internal/media"
	"github.com/nrynss/ajilamu/internal/tts"
	"github.com/nrynss/ajilamu/internal/types"
)

// lineRenderFunc adapts a function to api.LineRenderer.
type lineRenderFunc func(ctx context.Context, req api.LineRenderRequest) (api.LineRenderResult, error)

// RenderLine invokes the wrapped function.
func (f lineRenderFunc) RenderLine(ctx context.Context, req api.LineRenderRequest) (api.LineRenderResult, error) {
	return f(ctx, req)
}

// standInTake writes a real WAV at the requested path and reports one charge.
// It mirrors what the fit loop hands back, so the route records a real take.
// The take measures 5.320000 seconds, the slot for segment 3.
func standInTake(req api.LineRenderRequest) (api.LineRenderResult, error) {
	if err := os.WriteFile(req.TakeFile, slotWAV(), 0o644); err != nil {
		return api.LineRenderResult{}, err
	}
	slot := time.Duration(req.Segment.EndMs-req.Segment.StartMs) * time.Millisecond
	voice, err := tts.Assign(req.Segment.Speaker, req.Language)
	if err != nil {
		return api.LineRenderResult{}, err
	}
	return api.LineRenderResult{
		Take: types.Take{
			SegmentID: req.Segment.ID,
			Attempt:   1,
			File:      req.TakeFile,
			Duration:  slot,
			Fit:       types.NewFit(slot, slot),
		},
		Text:         req.Text,
		Voice:        voice.Name,
		Repair:       types.RepairNone,
		RepairDetail: "fits slot within dead band",
		Charges:      []cost.Charge{{Kind: cost.ChargeSynthesize, TakeID: req.Segment.ID, Units: 10, UnitPrice: 30000}},
		Total:        300000,
		Peaks:        make([]uint8, 64),
	}, nil
}

// slotWAV returns a silent mono 16 kHz 16-bit WAV of exactly 5320 ms.
// Segment 3's slot is 5320 ms, so the stand-in take fits its slot and
// ffprobe reads 5.320000 seconds.
func slotWAV() []byte {
	return wavOfMs(5320)
}

// wavOfMs returns a silent mono 16 kHz 16-bit WAV of the requested length.
func wavOfMs(ms int) []byte {
	const (
		sampleRate = 16000
		channels   = 1
		bitsPer    = 16
	)
	audio := make([]byte, sampleRate*ms/1000*channels*bitsPer/8)
	wav := make([]byte, 44+len(audio))
	copy(wav, "RIFF")
	binary.LittleEndian.PutUint32(wav[4:], uint32(36+len(audio)))
	copy(wav[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(wav[16:], 16)
	binary.LittleEndian.PutUint16(wav[20:], 1)
	binary.LittleEndian.PutUint16(wav[22:], channels)
	binary.LittleEndian.PutUint32(wav[24:], sampleRate)
	binary.LittleEndian.PutUint32(wav[28:], sampleRate*channels*bitsPer/8)
	binary.LittleEndian.PutUint16(wav[32:], channels*bitsPer/8)
	binary.LittleEndian.PutUint16(wav[34:], bitsPer)
	copy(wav[36:], "data")
	binary.LittleEndian.PutUint32(wav[40:], uint32(len(audio)))
	return wav
}

// rerenderProject writes one project with a stored target language and a first
// take for line 3. It returns the storage root and the take file.
func rerenderProject(t *testing.T, dubID string) (string, string) {
	t.Helper()
	storage := t.TempDir()
	writeProjectSource(t, storage, dubID)
	writeProjectRecord(t, storage, dubID, "en-US", "ml")
	workDir := filepath.Join(storage, dubID, "work", "ml-IN")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("create work directory: %v", err)
	}
	first := filepath.Join(workDir, "seg_3_try1.wav")
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "takes", "seg_3_try1.wav"))
	if err != nil {
		t.Fatalf("read fixture take: %v", err)
	}
	if err := os.WriteFile(first, data, 0o644); err != nil {
		t.Fatalf("write first take: %v", err)
	}
	return storage, first
}

// rerenderHistory is the stand-in ledger the route reads the line from.
func rerenderHistory() *standInWorkspaceHistory {
	return &standInWorkspaceHistory{
		commits: []api.Commit{{CommitID: "c1", VersionNumber: 1}},
		segments: []api.TimelineEntry{{
			SegmentIndex: 3,
			StartMs:      13208,
			EndMs:        18528,
			Speaker:      "Suni Williams",
			Emotion:      "Warm",
			SourceText:   "source line",
			Text:         "stored target line",
		}},
	}
}

// languageTrackHistory stands in for the ledger view. The real view filters a
// track on exact language equality, so this serves a track only under its exact
// key. It records every language the route asks for.
type languageTrackHistory struct {
	commits []api.Commit
	tracks  map[string][]api.TimelineEntry
	asked   []string
}

// ListCommits returns the commits every track shares.
func (h *languageTrackHistory) ListCommits(context.Context, string) ([]api.Commit, error) {
	return h.commits, nil
}

// TimelineAt serves the track keyed by language and records the ask.
func (h *languageTrackHistory) TimelineAt(_ context.Context, _, language, _ string) ([]api.TimelineEntry, error) {
	h.asked = append(h.asked, language)
	return h.tracks[language], nil
}

// CompareBranches is unused by the re-render route.
func (h *languageTrackHistory) CompareBranches(context.Context, string, string, string, string) (api.BranchComparison, error) {
	return api.BranchComparison{}, nil
}

// postRerender posts one re-render request.
func postRerender(t *testing.T, base, dubID string, segment int, body string) *http.Response {
	t.Helper()
	target := fmt.Sprintf("%s/api/dubs/%s/lines/%d/rerender", base, dubID, segment)
	response, err := http.Post(target, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("post re-render: %v", err)
	}
	return response
}

// decodeRerender reads one 201 body and closes the response.
func decodeRerender(t *testing.T, response *http.Response) api.RerenderResponse {
	t.Helper()
	defer response.Body.Close()
	var body api.RerenderResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode re-render body: %v", err)
	}
	return body
}

// TestRerenderWritesNewTakeBesidePrevious proves a re-render writes seg_3_try2
// beside seg_3_try1, leaves try1 untouched, records one take, one charge, and
// one commit, and names the new take in the response.
func TestRerenderWritesNewTakeBesidePrevious(t *testing.T) {
	storage, first := rerenderProject(t, "dub-rerender")
	before, err := os.ReadFile(first)
	if err != nil {
		t.Fatalf("read first take: %v", err)
	}

	var got api.LineRenderRequest
	renderer := lineRenderFunc(func(_ context.Context, req api.LineRenderRequest) (api.LineRenderResult, error) {
		got = req
		return standInTake(req)
	})
	var persists int
	var persisted api.RunResult
	recorder := recordFunc(func(_ context.Context, _ api.RunRequest, result api.RunResult) error {
		persists++
		persisted = result
		return nil
	})
	base := newRunTestServer(t, api.ServerOptions{
		Rerender:   renderer,
		Recorder:   recorder,
		History:    rerenderHistory(),
		StorageDir: storage,
	})

	response := postRerender(t, base, "dub-rerender", 3, `{}`)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("re-render status = %d, want 201", response.StatusCode)
	}
	body := decodeRerender(t, response)

	wantFile := filepath.Join(storage, "dub-rerender", "work", "ml-IN", "seg_3_try2.wav")
	if body.Take.Name != "seg_3_try2.wav" {
		t.Errorf("take name = %q, want seg_3_try2.wav", body.Take.Name)
	}
	if body.Take.File != wantFile {
		t.Errorf("take file = %q, want %q", body.Take.File, wantFile)
	}
	if body.Take.Attempt != 2 {
		t.Errorf("take attempt = %d, want 2", body.Take.Attempt)
	}
	if body.CommitID == "" {
		t.Error("response carried no commit id")
	}
	if body.Language != "ml-IN" {
		t.Errorf("language = %q, want ml-IN resolved from the record", body.Language)
	}
	if got.TakeFile != wantFile {
		t.Errorf("renderer take file = %q, want %q", got.TakeFile, wantFile)
	}
	if got.Language != "ml-IN" {
		t.Errorf("renderer language = %q, want ml-IN", got.Language)
	}
	if got.Text != "stored target line" {
		t.Errorf("renderer text = %q, want the stored target line", got.Text)
	}
	if got.Segment.Speaker.Name != "Suni Williams" {
		t.Errorf("renderer speaker = %q, want Suni Williams", got.Segment.Speaker.Name)
	}

	after, err := os.ReadFile(first)
	if err != nil {
		t.Fatalf("read first take after the re-render: %v", err)
	}
	if string(after) != string(before) {
		t.Error("the re-render changed the first take")
	}
	if _, err := os.Stat(wantFile); err != nil {
		t.Errorf("new take missing: %v", err)
	}

	if persists != 1 {
		t.Fatalf("recorder persisted %d times, want 1", persists)
	}
	if len(persisted.Takes) != 1 {
		t.Fatalf("recorded %d takes, want 1", len(persisted.Takes))
	}
	if len(persisted.Takes[0].Charges) != 1 {
		t.Errorf("recorded %d charges, want 1", len(persisted.Takes[0].Charges))
	}
	if persisted.Takes[0].Take.Attempt != 2 {
		t.Errorf("recorded attempt = %d, want 2 to match the file", persisted.Takes[0].Take.Attempt)
	}
	if len(persisted.Timeline) != 1 || persisted.Timeline[0].Text != "stored target line" {
		t.Errorf("recorded timeline = %+v, want one entry with the spoken text", persisted.Timeline)
	}
	if persisted.CommitID == "" {
		t.Error("recorded result carried no commit id")
	}
}

// TestRerenderReadsTheLedgerTrackUnderTheCallerCode proves the route reads the
// head line under the project's raw language code, then under the catalog tag.
// The bundled fixture records `ml`, while the work directory and the fit loop
// use the tag `ml-IN`.
func TestRerenderReadsTheLedgerTrackUnderTheCallerCode(t *testing.T) {
	cases := []struct {
		name    string
		track   string
		wantAsk []string
	}{
		{name: "raw code track", track: "ml", wantAsk: []string{"ml"}},
		{name: "tag track", track: "ml-IN", wantAsk: []string{"ml", "ml-IN"}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			storage, _ := rerenderProject(t, "dub-rawcode")
			history := &languageTrackHistory{
				commits: []api.Commit{{CommitID: "c1", VersionNumber: 1}},
				tracks: map[string][]api.TimelineEntry{
					test.track: {{
						SegmentIndex: 3,
						StartMs:      13208,
						EndMs:        18528,
						Speaker:      "Suni Williams",
						Emotion:      "Warm",
						SourceText:   "source line",
						Text:         "stored target line",
					}},
				},
			}
			var got api.LineRenderRequest
			renderer := lineRenderFunc(func(_ context.Context, req api.LineRenderRequest) (api.LineRenderResult, error) {
				got = req
				return standInTake(req)
			})
			base := newRunTestServer(t, api.ServerOptions{
				Rerender:   renderer,
				Recorder:   recordFunc(func(context.Context, api.RunRequest, api.RunResult) error { return nil }),
				History:    history,
				StorageDir: storage,
			})

			response := postRerender(t, base, "dub-rawcode", 3, `{}`)
			if response.StatusCode != http.StatusCreated {
				t.Fatalf("re-render status = %d, want 201", response.StatusCode)
			}
			body := decodeRerender(t, response)
			if body.Take.Name != "seg_3_try2.wav" {
				t.Errorf("take name = %q, want seg_3_try2.wav", body.Take.Name)
			}
			if got.Language != "ml-IN" {
				t.Errorf("renderer language = %q, want the resolved tag ml-IN", got.Language)
			}
			if !slices.Equal(history.asked, test.wantAsk) {
				t.Errorf("ledger languages asked = %v, want %v", history.asked, test.wantAsk)
			}

			encoded, err := json.Marshal(body)
			if err != nil {
				t.Fatalf("marshal response body: %v", err)
			}
			t.Logf("201 body = %s", encoded)
			t.Logf("ledger languages asked = %v", history.asked)

			duration, err := media.Duration(t.Context(), body.Take.File)
			if err != nil {
				t.Fatalf("measure the new take: %v", err)
			}
			if duration != 5320*time.Millisecond {
				t.Errorf("new take duration = %v, want 5.320000s", duration)
			}
			t.Logf("ffprobe new take = %v", duration)
		})
	}
}

// TestRerenderCorrectedTextAndSpeaker proves the optional body replaces the
// stored text and the stored speaker before the fit loop runs.
func TestRerenderCorrectedTextAndSpeaker(t *testing.T) {
	storage, _ := rerenderProject(t, "dub-edit")
	var got api.LineRenderRequest
	renderer := lineRenderFunc(func(_ context.Context, req api.LineRenderRequest) (api.LineRenderResult, error) {
		got = req
		return standInTake(req)
	})
	base := newRunTestServer(t, api.ServerOptions{
		Rerender:   renderer,
		Recorder:   recordFunc(func(context.Context, api.RunRequest, api.RunResult) error { return nil }),
		History:    rerenderHistory(),
		StorageDir: storage,
	})

	response := postRerender(t, base, "dub-edit", 3, `{"text":"corrected line","speaker":"Mark Vande Hei"}`)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("re-render status = %d, want 201", response.StatusCode)
	}
	body := decodeRerender(t, response)
	if got.Text != "corrected line" {
		t.Errorf("renderer text = %q, want the corrected line", got.Text)
	}
	if got.Segment.Speaker.Name != "Mark Vande Hei" {
		t.Errorf("renderer speaker = %q, want Mark Vande Hei", got.Segment.Speaker.Name)
	}
	if body.Take.Voice != "ml-IN-Chirp3-HD-Achird" {
		t.Errorf("take voice = %q, want the new speaker's voice", body.Take.Voice)
	}
}

// TestRerenderNumbersTheNextFreeTake proves an existing try2 pushes the new
// take to try3, so a re-render never overwrites a previous one.
func TestRerenderNumbersTheNextFreeTake(t *testing.T) {
	storage, first := rerenderProject(t, "dub-gap")
	workDir := filepath.Dir(first)
	data, err := os.ReadFile(first)
	if err != nil {
		t.Fatalf("read first take: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workDir, "seg_3_try2.wav"), data, 0o644); err != nil {
		t.Fatalf("write second take: %v", err)
	}
	var got api.LineRenderRequest
	renderer := lineRenderFunc(func(_ context.Context, req api.LineRenderRequest) (api.LineRenderResult, error) {
		got = req
		return standInTake(req)
	})
	base := newRunTestServer(t, api.ServerOptions{
		Rerender:   renderer,
		Recorder:   recordFunc(func(context.Context, api.RunRequest, api.RunResult) error { return nil }),
		History:    rerenderHistory(),
		StorageDir: storage,
	})

	response := postRerender(t, base, "dub-gap", 3, `{}`)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("re-render status = %d, want 201", response.StatusCode)
	}
	if filepath.Base(got.TakeFile) != "seg_3_try3.wav" {
		t.Errorf("renderer take file = %q, want seg_3_try3.wav", got.TakeFile)
	}
}

// TestRerenderRejectsBadRequests proves the route answers each failure with a
// status and never reaches the renderer.
func TestRerenderRejectsBadRequests(t *testing.T) {
	storage, _ := rerenderProject(t, "dub-reject")
	missing := t.TempDir()
	writeProjectSource(t, missing, "dub-nolang")
	var calls int
	renderer := lineRenderFunc(func(_ context.Context, req api.LineRenderRequest) (api.LineRenderResult, error) {
		calls++
		return standInTake(req)
	})
	recorder := recordFunc(func(context.Context, api.RunRequest, api.RunResult) error { return nil })

	cases := []struct {
		name     string
		storage  string
		dubID    string
		segment  int
		body     string
		wantCode int
	}{
		{name: "bad segment", storage: storage, dubID: "dub-reject", segment: -1, body: `{}`, wantCode: http.StatusBadRequest},
		{name: "unknown line", storage: storage, dubID: "dub-reject", segment: 9, body: `{}`, wantCode: http.StatusNotFound},
		{name: "no target language", storage: missing, dubID: "dub-nolang", segment: 3, body: `{}`, wantCode: http.StatusBadRequest},
		{name: "empty corrected text", storage: storage, dubID: "dub-reject", segment: 3, body: `{"text":"   "}`, wantCode: http.StatusBadRequest},
		{name: "empty corrected source", storage: storage, dubID: "dub-reject", segment: 3, body: `{"source_text":"   "}`, wantCode: http.StatusBadRequest},
		{name: "both corrected texts", storage: storage, dubID: "dub-reject", segment: 3, body: `{"text":"a","source_text":"b"}`, wantCode: http.StatusBadRequest},
		{name: "unknown speaker", storage: storage, dubID: "dub-reject", segment: 3, body: `{"speaker":"Nobody"}`, wantCode: http.StatusBadRequest},
		{name: "bad json", storage: storage, dubID: "dub-reject", segment: 3, body: `{`, wantCode: http.StatusBadRequest},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			calls = 0
			base := newRunTestServer(t, api.ServerOptions{
				Rerender:   renderer,
				Recorder:   recorder,
				History:    rerenderHistory(),
				StorageDir: test.storage,
			})
			response := postRerender(t, base, test.dubID, test.segment, test.body)
			defer response.Body.Close()
			if response.StatusCode != test.wantCode {
				t.Fatalf("status = %d, want %d", response.StatusCode, test.wantCode)
			}
			if calls != 0 {
				t.Fatalf("the renderer ran %d times for a rejected request", calls)
			}
		})
	}
}

// TestRerenderRejectsAnActiveRun proves the route refuses while another run
// holds the project, so a re-render never races a run over the same takes.
func TestRerenderRejectsAnActiveRun(t *testing.T) {
	storage, _ := rerenderProject(t, "dub-busy")
	var calls int
	renderer := lineRenderFunc(func(_ context.Context, req api.LineRenderRequest) (api.LineRenderResult, error) {
		calls++
		return standInTake(req)
	})
	handler := api.RerenderHandler(
		renderer,
		recordFunc(func(context.Context, api.RunRequest, api.RunResult) error { return nil }),
		rerenderHistory(),
		storage,
		func(string) bool { return true },
		nil,
	)
	request := httptest.NewRequest(http.MethodPost, "/api/dubs/dub-busy/lines/3/rerender", strings.NewReader(`{}`))
	request.SetPathValue("id", "dub-busy")
	request.SetPathValue("segment", "3")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", response.Code)
	}
	if calls != 0 {
		t.Fatalf("the renderer ran %d times while a run was active", calls)
	}
}

// TestRerenderUnavailableWithoutSeams proves a nil seam answers 503 rather
// than a panic or a false success.
func TestRerenderUnavailableWithoutSeams(t *testing.T) {
	storage, _ := rerenderProject(t, "dub-noseam")
	base := newRunTestServer(t, api.ServerOptions{StorageDir: storage})
	response := postRerender(t, base, "dub-noseam", 3, `{}`)
	defer response.Body.Close()
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", response.StatusCode)
	}
}

// provenanceRecorder captures what the route hands the ledger, including the
// corrected-source provenance a plain RunRecorder cannot carry.
type provenanceRecorder struct {
	persists  int
	corrected int
	record    api.CorrectedRerender
	result    api.RunResult
}

// Persist counts a plain re-render.
func (r *provenanceRecorder) Persist(context.Context, api.RunRequest, api.RunResult) error {
	r.persists++
	return nil
}

// PersistCorrected captures the corrected-source provenance.
func (r *provenanceRecorder) PersistCorrected(_ context.Context, _ api.RunRequest, result api.RunResult, corrected api.CorrectedRerender) error {
	r.corrected++
	r.record = corrected
	r.result = result
	return nil
}

// countingTranslator counts the translation calls the real fit loop makes.
type countingTranslator struct {
	calls int
	texts []string
}

// Translate answers the source line and counts the call.
func (t *countingTranslator) Translate(_ context.Context, req gemini.TranslateRequest) (string, error) {
	t.calls++
	t.texts = append(t.texts, req.Text)
	return "translated " + req.Text, nil
}

// wavSynthesizer writes a real WAV that exactly fills the line's slot.
type wavSynthesizer struct {
	calls int
}

// Synthesize writes the slot-length WAV at the requested path.
func (s *wavSynthesizer) Synthesize(_ context.Context, req tts.SynthesizeRequest) error {
	s.calls++
	return os.WriteFile(req.OutPath, slotWAV(), 0o644)
}

// missSynthesizer writes a WAV far longer than the slot, so every attempt
// misses and the fit loop flags the line.
type missSynthesizer struct {
	calls int
}

// Synthesize writes a 10640 ms WAV, twice segment 3's 5320 ms slot.
func (s *missSynthesizer) Synthesize(_ context.Context, req tts.SynthesizeRequest) error {
	s.calls++
	return os.WriteFile(req.OutPath, wavOfMs(10640), 0o644)
}

// fitRenderer runs the real fit loop through the route seam, so a test counts
// real translation calls rather than trusting a stand-in's guess.
func fitRenderer(translator gemini.Translator, synthesizer tts.Synthesizer) lineRenderFunc {
	return func(ctx context.Context, req api.LineRenderRequest) (api.LineRenderResult, error) {
		cfg := fit.RewriteConfig{
			Translator:  translator,
			Synthesizer: synthesizer,
			WorkDir:     req.WorkDir,
			PathBuilder: func(int, int, bool) string { return req.TakeFile },
			InitialText: req.Text,
		}
		if req.Text != "" {
			// A non-empty text is the creator's authoritative target line.
			cfg.AuthoritativeText = true
		}
		line, err := fit.RepairLine(ctx, req.Segment, cfg)
		if err != nil {
			return api.LineRenderResult{}, err
		}
		attempt := line.Attempts[len(line.Attempts)-1]
		charges := []cost.Charge{{Kind: cost.ChargeSynthesize, TakeID: req.Segment.ID, Units: 1, UnitPrice: 30000}}
		if req.Text == "" {
			charges = append([]cost.Charge{{Kind: cost.ChargeTranslate, TakeID: req.Segment.ID, Units: 1, UnitPrice: 1000}}, charges...)
		}
		total := cost.Price(0)
		for _, charge := range charges {
			total += charge.Total()
		}
		return api.LineRenderResult{
			Take:         line.ChosenTake,
			Text:         attempt.Text,
			Voice:        "test-voice",
			Repair:       attempt.Repair,
			RepairDetail: attempt.RepairDetail,
			Flagged:      line.Flagged,
			Charges:      charges,
			Total:        total,
			Peaks:        make([]uint8, 4),
		}, nil
	}
}

// TestRerenderCorrectedSourceRetranslates proves a corrected source line
// replaces the stored transcript, runs the translation model once, writes a
// new take beside the old one, and records text_corrected by manual_ui.
func TestRerenderCorrectedSourceRetranslates(t *testing.T) {
	storage, first := rerenderProject(t, "dub-source")
	before, err := os.ReadFile(first)
	if err != nil {
		t.Fatalf("read first take: %v", err)
	}

	translator := &countingTranslator{}
	synthesizer := &wavSynthesizer{}
	recorder := &provenanceRecorder{}
	base := newRunTestServer(t, api.ServerOptions{
		Rerender:   fitRenderer(translator, synthesizer),
		Recorder:   recorder,
		History:    rerenderHistory(),
		StorageDir: storage,
	})

	response := postRerender(t, base, "dub-source", 3, `{"language":"ml","source_text":"corrected source line"}`)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("re-render status = %d, want 201", response.StatusCode)
	}
	body := decodeRerender(t, response)
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal response body: %v", err)
	}
	t.Logf("201 body = %s", encoded)
	t.Logf("translation calls = %d, synthesis calls = %d", translator.calls, synthesizer.calls)

	if translator.calls != 1 {
		t.Errorf("translation calls = %d, want 1", translator.calls)
	}
	if len(translator.texts) != 1 || translator.texts[0] != "corrected source line" {
		t.Errorf("translated source = %v, want the corrected source line", translator.texts)
	}
	if body.Take.Name != "seg_3_try2.wav" {
		t.Errorf("take name = %q, want seg_3_try2.wav", body.Take.Name)
	}
	if body.Take.Attempt != 2 {
		t.Errorf("take attempt = %d, want 2", body.Take.Attempt)
	}
	if body.Text != "translated corrected source line" {
		t.Errorf("response text = %q, want the translated line", body.Text)
	}
	if body.SourceText != "corrected source line" {
		t.Errorf("response source_text = %q, want the corrected source", body.SourceText)
	}
	if recorder.corrected != 1 || recorder.persists != 0 {
		t.Errorf("recorder corrected = %d persists = %d, want 1 and 0", recorder.corrected, recorder.persists)
	}
	if recorder.record.Action != api.ActionTextCorrected {
		t.Errorf("recorded action = %q, want %q", recorder.record.Action, api.ActionTextCorrected)
	}
	if recorder.record.Author != api.AuthorManualUI {
		t.Errorf("recorded author = %q, want %q", recorder.record.Author, api.AuthorManualUI)
	}
	if recorder.record.Segment != 3 {
		t.Errorf("recorded segment = %d, want 3", recorder.record.Segment)
	}
	if len(recorder.result.Timeline) != 1 ||
		recorder.result.Timeline[0].SourceText != "corrected source line" ||
		recorder.result.Timeline[0].Text != "translated corrected source line" {
		t.Errorf("recorded timeline = %+v, want the corrected source and translated text", recorder.result.Timeline)
	}

	after, err := os.ReadFile(first)
	if err != nil {
		t.Fatalf("read first take after the re-render: %v", err)
	}
	if string(after) != string(before) {
		t.Error("the corrected-source re-render changed the first take")
	}
	if _, err := os.Stat(body.Take.File); err != nil {
		t.Errorf("new take missing: %v", err)
	}
}

// TestRerenderCorrectedTargetSkipsTranslation proves the target-text path
// still calls the translation model zero times while the source path calls it
// once. It runs the real fit loop through the route seam.
func TestRerenderCorrectedTargetSkipsTranslation(t *testing.T) {
	storage, _ := rerenderProject(t, "dub-target")
	translator := &countingTranslator{}
	synthesizer := &wavSynthesizer{}
	recorder := &provenanceRecorder{}
	base := newRunTestServer(t, api.ServerOptions{
		Rerender:   fitRenderer(translator, synthesizer),
		Recorder:   recorder,
		History:    rerenderHistory(),
		StorageDir: storage,
	})

	response := postRerender(t, base, "dub-target", 3, `{"language":"ml","text":"corrected target line"}`)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("re-render status = %d, want 201", response.StatusCode)
	}
	body := decodeRerender(t, response)
	t.Logf("translation calls = %d, synthesis calls = %d", translator.calls, synthesizer.calls)

	if translator.calls != 0 {
		t.Errorf("translation calls = %d, want 0 on the target-text path", translator.calls)
	}
	if synthesizer.calls != 1 {
		t.Errorf("synthesis calls = %d, want 1", synthesizer.calls)
	}
	if recorder.persists != 1 || recorder.corrected != 0 {
		t.Errorf("recorder persists = %d corrected = %d, want 1 and 0", recorder.persists, recorder.corrected)
	}
	if body.Text != "corrected target line" {
		t.Errorf("response text = %q, want the corrected target line", body.Text)
	}
	if body.SourceText != "" {
		t.Errorf("response source_text = %q, want empty on the target path", body.SourceText)
	}
}

// TestRerenderCorrectedTargetMissNeverTranslates proves a corrected target
// line that misses its slot keeps the creator's text on every attempt. The
// loop calls no translation model and flags the line instead of rewriting it.
func TestRerenderCorrectedTargetMissNeverTranslates(t *testing.T) {
	storage, _ := rerenderProject(t, "dub-target-miss")
	translator := &countingTranslator{}
	synthesizer := &missSynthesizer{}
	recorder := &provenanceRecorder{}
	base := newRunTestServer(t, api.ServerOptions{
		Rerender:   fitRenderer(translator, synthesizer),
		Recorder:   recorder,
		History:    rerenderHistory(),
		StorageDir: storage,
	})

	response := postRerender(t, base, "dub-target-miss", 3, `{"language":"ml","text":"corrected target line"}`)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("re-render status = %d, want 201", response.StatusCode)
	}
	body := decodeRerender(t, response)
	t.Logf("translation calls = %d, synthesis calls = %d, attempt = %d, text = %q, flagged = %v",
		translator.calls, synthesizer.calls, body.Take.Attempt, body.Text, body.Take.Flagged)

	if translator.calls != 0 {
		t.Errorf("translation calls = %d, want 0 on the corrected target path", translator.calls)
	}
	if synthesizer.calls != fit.DefaultMaxAttempts {
		t.Errorf("synthesis calls = %d, want %d", synthesizer.calls, fit.DefaultMaxAttempts)
	}
	if body.Text != "corrected target line" {
		t.Errorf("spoken text = %q, want the creator's corrected target line", body.Text)
	}
	if !body.Take.Flagged {
		t.Error("flagged = false, want a flagged line")
	}
	if recorder.persists != 1 || recorder.corrected != 0 {
		t.Errorf("recorder persists = %d corrected = %d, want 1 and 0", recorder.persists, recorder.corrected)
	}
}

// TestRerenderSourcePathNeedsCorrectedRecorder proves a recorder that cannot
// name the text_corrected action answers 503 before any model runs.
func TestRerenderSourcePathNeedsCorrectedRecorder(t *testing.T) {
	storage, _ := rerenderProject(t, "dub-nocorrect")
	var calls int
	renderer := lineRenderFunc(func(_ context.Context, req api.LineRenderRequest) (api.LineRenderResult, error) {
		calls++
		return standInTake(req)
	})
	base := newRunTestServer(t, api.ServerOptions{
		Rerender:   renderer,
		Recorder:   recordFunc(func(context.Context, api.RunRequest, api.RunResult) error { return nil }),
		History:    rerenderHistory(),
		StorageDir: storage,
	})

	response := postRerender(t, base, "dub-nocorrect", 3, `{"language":"ml","source_text":"corrected"}`)
	defer response.Body.Close()
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", response.StatusCode)
	}
	if calls != 0 {
		t.Fatalf("the renderer ran %d times without a corrected recorder", calls)
	}
}

// TestRerenderKeepsEveryTakeAcrossCorrections proves a source correction and a
// target correction in either order leave the previous takes byte for byte.
func TestRerenderKeepsEveryTakeAcrossCorrections(t *testing.T) {
	orders := []struct {
		name   string
		first  string
		second string
	}{
		{name: "source then target", first: `{"language":"ml","source_text":"first source"}`, second: `{"language":"ml","text":"second target"}`},
		{name: "target then source", first: `{"language":"ml","text":"first target"}`, second: `{"language":"ml","source_text":"second source"}`},
	}
	for _, order := range orders {
		t.Run(order.name, func(t *testing.T) {
			storage, first := rerenderProject(t, "dub-history")
			original, err := os.ReadFile(first)
			if err != nil {
				t.Fatalf("read first take: %v", err)
			}
			base := newRunTestServer(t, api.ServerOptions{
				Rerender:   fitRenderer(&countingTranslator{}, &wavSynthesizer{}),
				Recorder:   &provenanceRecorder{},
				History:    rerenderHistory(),
				StorageDir: storage,
			})

			firstResponse := postRerender(t, base, "dub-history", 3, order.first)
			if firstResponse.StatusCode != http.StatusCreated {
				t.Fatalf("first re-render status = %d, want 201", firstResponse.StatusCode)
			}
			firstBody := decodeRerender(t, firstResponse)
			secondTake, err := os.ReadFile(firstBody.Take.File)
			if err != nil {
				t.Fatalf("read the second take: %v", err)
			}

			secondResponse := postRerender(t, base, "dub-history", 3, order.second)
			if secondResponse.StatusCode != http.StatusCreated {
				t.Fatalf("second re-render status = %d, want 201", secondResponse.StatusCode)
			}
			secondBody := decodeRerender(t, secondResponse)

			if firstBody.Take.Name != "seg_3_try2.wav" {
				t.Errorf("first take name = %q, want seg_3_try2.wav", firstBody.Take.Name)
			}
			if secondBody.Take.Name != "seg_3_try3.wav" {
				t.Errorf("second take name = %q, want seg_3_try3.wav", secondBody.Take.Name)
			}
			afterOriginal, err := os.ReadFile(first)
			if err != nil {
				t.Fatalf("read the first take again: %v", err)
			}
			if string(afterOriginal) != string(original) {
				t.Error("a correction changed the first take")
			}
			afterSecond, err := os.ReadFile(firstBody.Take.File)
			if err != nil {
				t.Fatalf("read the second take again: %v", err)
			}
			if string(afterSecond) != string(secondTake) {
				t.Error("the second correction changed the second take")
			}
			names, err := filepath.Glob(filepath.Join(filepath.Dir(first), "seg_3_try*.wav"))
			if err != nil {
				t.Fatalf("list take files: %v", err)
			}
			slices.Sort(names)
			if len(names) != 3 {
				t.Errorf("take files = %v, want three", names)
			}
			t.Logf("take list = %v", names)
		})
	}
}
