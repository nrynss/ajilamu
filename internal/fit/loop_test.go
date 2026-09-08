package fit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nrynss/ajilamu/internal/api"
	"github.com/nrynss/ajilamu/internal/cost"
	"github.com/nrynss/ajilamu/internal/gemini"
	"github.com/nrynss/ajilamu/internal/media"
	"github.com/nrynss/ajilamu/internal/tts"
	"github.com/nrynss/ajilamu/internal/types"
)

// resolveFixturePath locates testdata files from any working directory.
func resolveFixturePath(t *testing.T, rel string) string {
	t.Helper()
	candidates := []string{
		rel,
		filepath.Join("testdata", rel),
		filepath.Join("..", "testdata", rel),
		filepath.Join("..", "..", "testdata", rel),
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	t.Fatalf("fixture file not found: %s", rel)
	return ""
}

// loadFixtureSegments reads dialogue segments from testdata/segments.json.
func loadFixtureSegments(t *testing.T) []types.Segment {
	t.Helper()
	path := resolveFixturePath(t, "segments.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read segments.json: %v", err)
	}

	type wireSegment struct {
		ID      int    `json:"id"`
		StartMs int64  `json:"start_ms"`
		EndMs   int64  `json:"end_ms"`
		Text    string `json:"text"`
		Speaker string `json:"speaker"`
		Emotion string `json:"emotion"`
	}

	var wire []wireSegment
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatalf("unmarshal segments: %v", err)
	}

	segs := make([]types.Segment, len(wire))
	for i, w := range wire {
		segs[i] = types.Segment{
			ID:      w.ID,
			StartMs: w.StartMs,
			EndMs:   w.EndMs,
			Text:    w.Text,
			Speaker: types.Speaker{Name: w.Speaker},
			Emotion: w.Emotion,
		}
	}
	return segs
}

// mockPipelineTranslator implements gemini.Translator with billing charges.
type mockPipelineTranslator struct {
	mu       sync.Mutex
	rec      ChargeRecorder
	requests []gemini.TranslateRequest
	replies  map[int][]string
	errs     map[int]error
}

func newMockPipelineTranslator(rec ChargeRecorder) *mockPipelineTranslator {
	return &mockPipelineTranslator{
		rec:     rec,
		replies: make(map[int][]string),
		errs:    make(map[int]error),
	}
}

func (m *mockPipelineTranslator) Translate(ctx context.Context, req gemini.TranslateRequest) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.requests = append(m.requests, req)
	if err, ok := m.errs[req.SegmentID]; ok && err != nil {
		return "", err
	}

	reply := fmt.Sprintf("Malayalam translation for line %d", req.SegmentID)
	if list, ok := m.replies[req.SegmentID]; ok && len(list) > 0 {
		reply = list[0]
		m.replies[req.SegmentID] = list[1:]
	}

	if m.rec != nil {
		m.rec.Add(cost.Charge{
			Kind:               cost.ChargeTranslate,
			TakeID:             req.SegmentID,
			PromptTokens:       25,
			CandidateTokens:    15,
			PromptUnitPrice:    150,
			CandidateUnitPrice: 600,
		})
	}
	return reply, nil
}

// SetRecorder binds the charge recorder to the mock translator.
func (m *mockPipelineTranslator) SetRecorder(rec ChargeRecorder) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rec = rec
}

// mockPipelineSynthesizer implements tts.Synthesizer with fixture audio copying.
type mockPipelineSynthesizer struct {
	mu        sync.Mutex
	rec       ChargeRecorder
	takesDir  string
	durations map[int][]time.Duration
	requests  []tts.SynthesizeRequest
	errs      map[int]error
}

func newMockPipelineSynthesizer(takesDir string, rec ChargeRecorder) *mockPipelineSynthesizer {
	return &mockPipelineSynthesizer{
		takesDir:  takesDir,
		rec:       rec,
		durations: make(map[int][]time.Duration),
		errs:      make(map[int]error),
	}
}

func (s *mockPipelineSynthesizer) Synthesize(ctx context.Context, req tts.SynthesizeRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.requests = append(s.requests, req)
	if err, ok := s.errs[req.SegmentID]; ok && err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(req.OutPath), 0o755); err != nil {
		return err
	}

	// Check if custom durations exist for this segment attempt.
	if list, ok := s.durations[req.SegmentID]; ok && len(list) > 0 {
		dur := list[0]
		s.durations[req.SegmentID] = list[1:]
		if err := writeTestWAV(req.OutPath, dur); err != nil {
			return err
		}
	} else if s.takesDir != "" {
		// Copy golden take from testdata/takes/seg_{id}_try1.wav.
		src := filepath.Join(s.takesDir, fmt.Sprintf("seg_%d_try1.wav", req.SegmentID))
		data, err := os.ReadFile(src)
		if err != nil {
			return fmt.Errorf("read fixture take: %w", err)
		}
		if err := os.WriteFile(req.OutPath, data, 0o644); err != nil {
			return fmt.Errorf("write take file: %w", err)
		}
	} else {
		// Fall back to writing 1-second WAV.
		if err := writeTestWAV(req.OutPath, 1000*time.Millisecond); err != nil {
			return err
		}
	}

	if s.rec != nil {
		s.rec.Add(cost.Charge{
			Kind:      cost.ChargeSynthesize,
			TakeID:    req.SegmentID,
			Units:     len([]rune(req.Text)),
			UnitPrice: 30000,
		})
	}
	return nil
}

// SetRecorder binds the charge recorder to the mock synthesizer.
func (s *mockPipelineSynthesizer) SetRecorder(rec ChargeRecorder) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rec = rec
}

// mockSegmenter implements gemini.Segmenter with billing charge support.
type mockSegmenter struct {
	rec      ChargeRecorder
	segments []types.Segment
	err      error
}

// SetRecorder binds the charge recorder to the mock segmenter.
func (s *mockSegmenter) SetRecorder(rec ChargeRecorder) {
	s.rec = rec
}

func (s *mockSegmenter) Segment(ctx context.Context, in gemini.Input) ([]types.Segment, error) {
	if s.err != nil {
		return nil, s.err
	}
	if s.rec != nil {
		s.rec.Add(cost.Charge{
			Kind:               cost.ChargeSegment,
			TakeID:             0,
			PromptTokens:       658,
			CandidateTokens:    120,
			PromptUnitPrice:    150,
			CandidateUnitPrice: 600,
		})
	}
	return s.segments, nil
}

// eventCollector collects progress events in memory.
type eventCollector struct {
	mu     sync.Mutex
	events []api.ProgressEvent
}

func (c *eventCollector) OnProgress(event api.ProgressEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, event)
}

func (c *eventCollector) Events() []api.ProgressEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]api.ProgressEvent, len(c.events))
	copy(out, c.events)
	return out
}

// TestPipelineEndToEndWithFixtures tests the complete dubbing pipeline run.
// It reproduces test run outputs against fixtures and flags segment 8.
func TestPipelineEndToEndWithFixtures(t *testing.T) {
	takesDir := resolveFixturePath(t, "takes")
	segments := loadFixtureSegments(t)
	workDir := t.TempDir()

	ledger := cost.NewLedger()
	collector := &eventCollector{}

	trans := newMockPipelineTranslator(ledger)
	synth := newMockPipelineSynthesizer(takesDir, ledger)

	// Segment 1 fixture take is 1680ms, missing slot 1820ms by 7.7 percent.
	// We provide 1820ms on retry attempt 2 so segment 1 fits.
	synth.durations[1] = []time.Duration{
		1680 * time.Millisecond,
		1820 * time.Millisecond,
	}

	// Segment 8 fixture take is 4200ms, missing slot 7110ms by 40.9 percent.
	// We supply 3 attempts that all fail to fit the 7110ms slot.
	synth.durations[8] = []time.Duration{
		4200 * time.Millisecond,
		4600 * time.Millisecond,
		4400 * time.Millisecond,
	}

	eventChan := make(chan api.ProgressEvent, 200)

	cfg := PipelineConfig{
		Segments:    segments,
		Translator:  trans,
		Synthesizer: synth,
		WorkDir:     workDir,
		Recorder:    ledger,
		Listener:    collector,
		Events:      eventChan,
	}

	res, err := RunPipeline(context.Background(), cfg)
	if err != nil {
		t.Fatalf("RunPipeline failed: %v", err)
	}

	// Verify segment 8 is the only flagged line.
	if res.IsClean() {
		t.Fatalf("expected pipeline result with flagged lines, got clean")
	}
	if res.FlaggedCount() != 1 {
		t.Errorf("got %d flagged lines, want 1", res.FlaggedCount())
	}
	if len(res.FlaggedSegments) != 1 || res.FlaggedSegments[0] != 8 {
		t.Errorf("flagged segments = %v, want [8]", res.FlaggedSegments)
	}

	// Verify lines 1 through 7 resolved successfully.
	for id := 1; id <= 7; id++ {
		line, ok := res.Line(id)
		if !ok {
			t.Fatalf("missing line %d in results", id)
		}
		if line.Flagged {
			t.Errorf("line %d unexpectedly flagged", id)
		}
	}

	// Verify segment 3 and segment 4 time stretch repair.
	line3, _ := res.Line(3)
	if line3.ChosenTake.Attempt != 1 {
		t.Errorf("line 3 attempt = %d, want 1", line3.ChosenTake.Attempt)
	}
	if filepath.Base(line3.ChosenTake.File) != "seg_3_stretched.wav" {
		t.Errorf("line 3 file = %s, want seg_3_stretched.wav", filepath.Base(line3.ChosenTake.File))
	}

	line4, _ := res.Line(4)
	if line4.ChosenTake.Attempt != 1 {
		t.Errorf("line 4 attempt = %d, want 1", line4.ChosenTake.Attempt)
	}
	if filepath.Base(line4.ChosenTake.File) != "seg_4_stretched.wav" {
		t.Errorf("line 4 file = %s, want seg_4_stretched.wav", filepath.Base(line4.ChosenTake.File))
	}

	// Verify immutable take files exist independently on disk.
	expectedFiles := []string{
		"seg_3_try1.wav",
		"seg_3_stretched.wav",
		"seg_4_try1.wav",
		"seg_4_stretched.wav",
		"seg_8_try1.wav",
		"seg_8_try2.wav",
		"seg_8_try3.wav",
	}
	for _, fname := range expectedFiles {
		p := filepath.Join(workDir, fname)
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected immutable file missing: %s", fname)
		}
	}

	// Verify segment 8 closest take selection and notifications.
	line8, _ := res.Line(8)
	if !line8.Flagged {
		t.Errorf("line 8 expected flagged, got unflagged")
	}
	if len(line8.Attempts) != 3 {
		t.Fatalf("line 8 attempts = %d, want 3", len(line8.Attempts))
	}
	// Closest attempt has minimum absolute delta.
	// Try 1: 4200 - 7110 = -2910ms (abs 2910ms).
	// Try 2: 4600 - 7110 = -2510ms (abs 2510ms).
	// Try 3: 4400 - 7110 = -2710ms (abs 2710ms).
	// Closest attempt is try 2.
	if line8.ChosenTake.Attempt != 2 {
		t.Errorf("line 8 chosen attempt = %d, want 2", line8.ChosenTake.Attempt)
	}
	assertProseStyle(t, line8.NotificationCopy)

	// Verify distinct speaker voice assignments.
	if len(res.Voices) != 2 {
		t.Errorf("got %d speaker voices, want 2", len(res.Voices))
	}
	suniVoice, ok := res.Voices["Suni Williams"]
	if !ok || !strings.Contains(suniVoice.Name, "Achernar") {
		t.Errorf("Suni Williams voice = %v, want Achernar", suniVoice)
	}
	markVoice, ok := res.Voices["Mark Vande Hei"]
	if !ok || !strings.Contains(markVoice.Name, "Achird") {
		t.Errorf("Mark Vande Hei voice = %v, want Achird", markVoice)
	}
	if suniVoice.Name == markVoice.Name {
		t.Errorf("speaker voice collision: both got %s", suniVoice.Name)
	}

	// Verify progress events across stages.
	events := collector.Events()
	if len(events) == 0 {
		t.Fatalf("no progress events recorded")
	}

	stagesSeen := make(map[string]bool)
	var prevCost cost.Price
	for _, ev := range events {
		stagesSeen[ev.Stage] = true
		assertProseStyle(t, ev.Sentence)
		if ev.TotalNanodollars < prevCost {
			t.Errorf("event total cost decreased: prev %v, now %v", prevCost, ev.TotalNanodollars)
		}
		prevCost = ev.TotalNanodollars
	}

	requiredStages := []string{
		api.StageSegmenting,
		api.StageTranslating,
		api.StageSynthesizing,
		api.StageMeasuring,
		api.StageRepairing,
	}
	for _, st := range requiredStages {
		if !stagesSeen[st] {
			t.Errorf("missing progress event stage: %s", st)
		}
	}

	// Verify final event is EventDone with flagged note.
	lastEvent := events[len(events)-1]
	if lastEvent.Type != api.EventDone {
		t.Errorf("last event type = %s, want done", lastEvent.Type)
	}

	// Verify ledger charges recorded for all API steps.
	charges := ledger.Charges()
	if len(charges) == 0 {
		t.Fatalf("no ledger charges recorded")
	}
	if res.TotalCost <= 0 {
		t.Errorf("pipeline total cost = %v, want positive", res.TotalCost)
	}
	if res.TotalCost != ledger.Total() {
		t.Errorf("result total cost %v != ledger total %v", res.TotalCost, ledger.Total())
	}

	// Verify exact count and positive total cost for each charge kind.
	var translateCount, synthesizeCount int
	var translateCost, synthesizeCost cost.Price
	for _, c := range charges {
		switch c.Kind {
		case cost.ChargeTranslate:
			translateCount++
			translateCost += c.Total()
		case cost.ChargeSynthesize:
			synthesizeCount++
			synthesizeCost += c.Total()
		default:
			t.Errorf("unexpected charge kind: %v", c.Kind)
		}
	}

	const wantTranslateCount = 11
	const wantSynthesizeCount = 11
	if translateCount != wantTranslateCount {
		t.Errorf("translate charge count = %d, want %d", translateCount, wantTranslateCount)
	}
	if synthesizeCount != wantSynthesizeCount {
		t.Errorf("synthesize charge count = %d, want %d", synthesizeCount, wantSynthesizeCount)
	}
	if translateCost <= 0 {
		t.Errorf("translate total cost = %v, want positive", translateCost)
	}
	if synthesizeCost <= 0 {
		t.Errorf("synthesize total cost = %v, want positive", synthesizeCost)
	}
	if len(charges) != wantTranslateCount+wantSynthesizeCount {
		t.Errorf("total charges count = %d, want %d", len(charges), wantTranslateCount+wantSynthesizeCount)
	}
}

// TestPipelineWithMediaInputSegmenter verifies segmenting from input media.
func TestPipelineWithMediaInputSegmenter(t *testing.T) {
	fixtureSegs := loadFixtureSegments(t)
	segSubset := fixtureSegs[:2]

	ledger := cost.NewLedger()
	segClient := &mockSegmenter{
		rec:      ledger,
		segments: segSubset,
	}
	trans := newMockPipelineTranslator(ledger)
	synth := newMockPipelineSynthesizer("", ledger)

	synth.durations[1] = []time.Duration{segSubset[0].SlotDuration()}
	synth.durations[2] = []time.Duration{segSubset[1].SlotDuration()}

	inputMedia := &gemini.Input{
		Data:     []byte("dummy audio bytes"),
		MIMEType: "audio/mp3",
	}

	collector := &eventCollector{}
	cfg := PipelineConfig{
		Segmenter:   segClient,
		InputMedia:  inputMedia,
		Translator:  trans,
		Synthesizer: synth,
		WorkDir:     t.TempDir(),
		Recorder:    ledger,
		Listener:    collector,
	}

	res, err := RunPipeline(context.Background(), cfg)
	if err != nil {
		t.Fatalf("RunPipeline failed: %v", err)
	}

	if !res.IsClean() {
		t.Errorf("expected clean run, got flagged %v", res.FlaggedSegments)
	}
	if len(res.Segments) != 2 {
		t.Errorf("got %d segments, want 2", len(res.Segments))
	}

	// Verify segment charge was recorded in ledger.
	segCharges := ledger.TotalByKind(cost.ChargeSegment)
	if segCharges <= 0 {
		t.Errorf("segment charges = %v, want positive", segCharges)
	}
}

// TestPipelineSegment8FlaggedThreeAttempts verifies retry progression for segment 8.
func TestPipelineSegment8FlaggedThreeAttempts(t *testing.T) {
	workDir := t.TempDir()
	slot := 7110 * time.Millisecond
	seg := types.Segment{
		ID:      8,
		StartMs: 0,
		EndMs:   int64(slot.Milliseconds()),
		Text:    "NASA speech line 8",
		Speaker: types.Speaker{Name: "Mark Vande Hei"},
	}

	trans := newMockPipelineTranslator(nil)
	synth := newMockPipelineSynthesizer("", nil)

	dur1 := 4200 * time.Millisecond
	dur2 := 4500 * time.Millisecond
	dur3 := 4300 * time.Millisecond
	synth.durations[8] = []time.Duration{dur1, dur2, dur3}

	cfg := PipelineConfig{
		Segments:    []types.Segment{seg},
		Translator:  trans,
		Synthesizer: synth,
		WorkDir:     workDir,
	}

	res, err := RunPipeline(context.Background(), cfg)
	if err != nil {
		t.Fatalf("RunPipeline failed: %v", err)
	}

	if res.FlaggedCount() != 1 {
		t.Fatalf("flagged count = %d, want 1", res.FlaggedCount())
	}

	line, ok := res.Line(8)
	if !ok || !line.Flagged {
		t.Fatalf("line 8 not flagged")
	}

	// Check translation modes: attempt 1 normal, 2 fuller, 3 fuller.
	if len(trans.requests) != 3 {
		t.Fatalf("translate requests = %d, want 3", len(trans.requests))
	}
	if trans.requests[0].Mode != gemini.ModeNormal {
		t.Errorf("attempt 1 mode = %v, want ModeNormal", trans.requests[0].Mode)
	}
	if trans.requests[1].Mode != gemini.ModeFuller {
		t.Errorf("attempt 2 mode = %v, want ModeFuller", trans.requests[1].Mode)
	}
	if trans.requests[2].Mode != gemini.ModeFuller {
		t.Errorf("attempt 3 mode = %v, want ModeFuller", trans.requests[2].Mode)
	}

	// Closest take is attempt 2: |4500-7110| = 2610ms.
	if line.ChosenTake.Attempt != 2 {
		t.Errorf("chosen take attempt = %d, want 2", line.ChosenTake.Attempt)
	}

	deltas := line.SignedDeltaMs()
	if len(deltas) != 3 {
		t.Fatalf("signed deltas count = %d, want 3", len(deltas))
	}
	if deltas[0] != -2910 || deltas[1] != -2610 || deltas[2] != -2810 {
		t.Errorf("deltas = %v, want [-2910, -2610, -2810]", deltas)
	}

	assertProseStyle(t, line.NotificationCopy)
}

// TestPipelineSpeakerVoiceCollision verifies duplicate voice profiles trigger an error.
func TestPipelineSpeakerVoiceCollision(t *testing.T) {
	seg1 := types.Segment{
		ID:      1,
		StartMs: 0,
		EndMs:   2000,
		Speaker: types.Speaker{Name: "Speaker Alpha"},
	}
	seg2 := types.Segment{
		ID:      2,
		StartMs: 2000,
		EndMs:   4000,
		Speaker: types.Speaker{Name: "Speaker Beta"},
	}

	// Custom assigner that returns the exact same voice for both speakers.
	collidingAssigner := func(speaker types.Speaker, language string) (tts.Voice, error) {
		return tts.Voice{
			Name:         "ml-IN-Chirp3-HD-SharedVoice",
			LanguageCode: language,
			Gender:       "Neutral",
		}, nil
	}

	cfg := PipelineConfig{
		Segments:      []types.Segment{seg1, seg2},
		Translator:    newMockPipelineTranslator(nil),
		Synthesizer:   newMockPipelineSynthesizer("", nil),
		VoiceAssigner: collidingAssigner,
	}

	_, err := RunPipeline(context.Background(), cfg)
	if err == nil {
		t.Fatalf("expected error on speaker voice collision, got nil")
	}
	if !errors.Is(err, ErrSpeakerVoiceCollision) {
		t.Errorf("err = %v, want ErrSpeakerVoiceCollision", err)
	}
}

// TestPipelineSpeakerMissing verifies empty speaker names are rejected.
func TestPipelineSpeakerMissing(t *testing.T) {
	seg := types.Segment{
		ID:      1,
		StartMs: 0,
		EndMs:   2000,
		Speaker: types.Speaker{Name: ""},
	}

	cfg := PipelineConfig{
		Segments:    []types.Segment{seg},
		Translator:  newMockPipelineTranslator(nil),
		Synthesizer: newMockPipelineSynthesizer("", nil),
	}

	_, err := RunPipeline(context.Background(), cfg)
	if err == nil {
		t.Fatalf("expected error on missing speaker name, got nil")
	}
	if !errors.Is(err, ErrMissingSpeaker) {
		t.Errorf("err = %v, want ErrMissingSpeaker", err)
	}
}

// TestPipelineUnknownSpeaker verifies unknown speakers cause a clean error.
func TestPipelineUnknownSpeaker(t *testing.T) {
	seg := types.Segment{
		ID:      1,
		StartMs: 0,
		EndMs:   2000,
		Speaker: types.Speaker{Name: "Unknown Person"},
	}

	cfg := PipelineConfig{
		Segments:    []types.Segment{seg},
		Translator:  newMockPipelineTranslator(nil),
		Synthesizer: newMockPipelineSynthesizer("", nil),
	}

	_, err := RunPipeline(context.Background(), cfg)
	if err == nil {
		t.Fatalf("expected error on unknown speaker, got nil")
	}
}

// TestPipelineContextCancellation verifies context cancel cleanly stops execution.
func TestPipelineContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	seg := types.Segment{
		ID:      1,
		StartMs: 0,
		EndMs:   2000,
		Speaker: types.Speaker{Name: "Suni Williams"},
	}

	cfg := PipelineConfig{
		Segments:    []types.Segment{seg},
		Translator:  newMockPipelineTranslator(nil),
		Synthesizer: newMockPipelineSynthesizer("", nil),
	}

	_, err := RunPipeline(ctx, cfg)
	if err == nil {
		t.Fatalf("expected context cancellation error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

// TestPipelineSegmenterError verifies segmenter errors are cleanly propagated.
func TestPipelineSegmenterError(t *testing.T) {
	wantErr := errors.New("gemini network error")
	segClient := &mockSegmenter{err: wantErr}

	collector := &eventCollector{}
	cfg := PipelineConfig{
		Segmenter:   segClient,
		InputMedia:  &gemini.Input{Data: []byte("media")},
		Translator:  newMockPipelineTranslator(nil),
		Synthesizer: newMockPipelineSynthesizer("", nil),
		Listener:    collector,
	}

	_, err := RunPipeline(context.Background(), cfg)
	if err == nil {
		t.Fatalf("expected segmenter error, got nil")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("err = %v, want %v", err, wantErr)
	}

	// Verify error event was emitted.
	events := collector.Events()
	if len(events) == 0 {
		t.Fatalf("no events recorded")
	}
	lastEvent := events[len(events)-1]
	if lastEvent.Type != api.EventError {
		t.Errorf("last event type = %s, want error", lastEvent.Type)
	}
}

// TestPipelineTranslatorError verifies translator failures abort execution cleanly.
func TestPipelineTranslatorError(t *testing.T) {
	seg := types.Segment{
		ID:      1,
		StartMs: 0,
		EndMs:   2000,
		Speaker: types.Speaker{Name: "Suni Williams"},
	}

	wantErr := errors.New("translation service unavailable")
	trans := newMockPipelineTranslator(nil)
	trans.errs[1] = wantErr

	collector := &eventCollector{}
	cfg := PipelineConfig{
		Segments:    []types.Segment{seg},
		Translator:  trans,
		Synthesizer: newMockPipelineSynthesizer("", nil),
		Listener:    collector,
	}

	_, err := RunPipeline(context.Background(), cfg)
	if err == nil {
		t.Fatalf("expected translation error, got nil")
	}
}

// TestPipelineSynthesizerError verifies synthesizer failures abort execution cleanly.
func TestPipelineSynthesizerError(t *testing.T) {
	seg := types.Segment{
		ID:      1,
		StartMs: 0,
		EndMs:   2000,
		Speaker: types.Speaker{Name: "Suni Williams"},
	}

	wantErr := errors.New("tts quota exceeded")
	synth := newMockPipelineSynthesizer("", nil)
	synth.errs[1] = wantErr

	collector := &eventCollector{}
	cfg := PipelineConfig{
		Segments:    []types.Segment{seg},
		Translator:  newMockPipelineTranslator(nil),
		Synthesizer: synth,
		Listener:    collector,
	}

	_, err := RunPipeline(context.Background(), cfg)
	if err == nil {
		t.Fatalf("expected synthesizer error, got nil")
	}
}

// TestPipelineConfigValidation verifies configuration guards.
func TestPipelineConfigValidation(t *testing.T) {
	t.Run("missing segmenter when input media provided", func(t *testing.T) {
		cfg := PipelineConfig{
			InputMedia:  &gemini.Input{Data: []byte("media")},
			Translator:  newMockPipelineTranslator(nil),
			Synthesizer: newMockPipelineSynthesizer("", nil),
		}
		_, err := NewPipeline(cfg)
		if !errors.Is(err, ErrSegmenterRequired) {
			t.Errorf("err = %v, want ErrSegmenterRequired", err)
		}
	})

	t.Run("missing segments when media is nil", func(t *testing.T) {
		cfg := PipelineConfig{
			Translator:  newMockPipelineTranslator(nil),
			Synthesizer: newMockPipelineSynthesizer("", nil),
		}
		_, err := NewPipeline(cfg)
		if !errors.Is(err, ErrNoSegments) {
			t.Errorf("err = %v, want ErrNoSegments", err)
		}
	})

	t.Run("missing translator", func(t *testing.T) {
		cfg := PipelineConfig{
			Segments:    []types.Segment{{ID: 1}},
			Synthesizer: newMockPipelineSynthesizer("", nil),
		}
		_, err := NewPipeline(cfg)
		if !errors.Is(err, ErrTranslatorRequired) {
			t.Errorf("err = %v, want ErrTranslatorRequired", err)
		}
	})

	t.Run("missing synthesizer", func(t *testing.T) {
		cfg := PipelineConfig{
			Segments:   []types.Segment{{ID: 1}},
			Translator: newMockPipelineTranslator(nil),
		}
		_, err := NewPipeline(cfg)
		if !errors.Is(err, ErrSynthesizerRequired) {
			t.Errorf("err = %v, want ErrSynthesizerRequired", err)
		}
	})

	t.Run("invalid stretch limits", func(t *testing.T) {
		cfg := PipelineConfig{
			Segments:    []types.Segment{{ID: 1}},
			Translator:  newMockPipelineTranslator(nil),
			Synthesizer: newMockPipelineSynthesizer("", nil),
			Limits:      StretchLimits{Short: 0.20, Long: 0.05},
		}
		_, err := NewPipeline(cfg)
		if !errors.Is(err, ErrLimits) {
			t.Errorf("err = %v, want ErrLimits", err)
		}
	})
}

// TestPipelineTakeNaming verifies immutable take naming rules.
func TestPipelineTakeNaming(t *testing.T) {
	tests := []struct {
		segID     int
		attempt   int
		stretched bool
		want      string
	}{
		{1, 1, false, "seg_1_try1.wav"},
		{1, 1, true, "seg_1_stretched.wav"},
		{1, 2, false, "seg_1_try2.wav"},
		{1, 2, true, "seg_1_try2_stretched.wav"},
		{8, 3, false, "seg_8_try3.wav"},
		{8, 3, true, "seg_8_try3_stretched.wav"},
	}

	for _, tt := range tests {
		got := PipelineTakeName(tt.segID, tt.attempt, tt.stretched)
		if got != tt.want {
			t.Errorf("PipelineTakeName(%d, %d, %v) = %s, want %s",
				tt.segID, tt.attempt, tt.stretched, got, tt.want)
		}
	}
}

// TestCleanErrorMessage verifies clean error reporting and sentence style.
func TestCleanErrorMessage(t *testing.T) {
	errs := []error{
		context.Canceled,
		context.DeadlineExceeded,
		ErrNoSegments,
		ErrSegmenterRequired,
		ErrTranslatorRequired,
		ErrSynthesizerRequired,
		ErrSpeakerVoiceCollision,
		ErrMissingSpeaker,
		ErrDeadBand,
		ErrOverrunTooLarge,
		ErrUnderrunTooLarge,
		ErrLandedOutside,
		ErrRenderFailed,
		ErrOutputExists,
		ErrSourceMissing,
		ErrSourceUnreadable,
		ErrSameFile,
		ErrPathRequired,
		ErrNoSlot,
		ErrLimits,
		ErrRatioRange,
		ErrAlreadyStretched,
		ErrTotalStretchExceeded,
		errors.New("custom unknown error"),
	}

	for _, err := range errs {
		msg := CleanErrorMessage(err)
		if msg == "" {
			t.Errorf("empty clean message for error: %v", err)
		}
		assertProseStyle(t, msg)
	}

	if CleanErrorMessage(nil) != "" {
		t.Errorf("expected empty string for nil error")
	}
}

// bareTestRecorder implements only ChargeRecorder.
// It deliberately omits TotalReporter and ChargesProvider methods.
type bareTestRecorder struct {
	mu      sync.Mutex
	charges []cost.Charge
}

func (b *bareTestRecorder) Add(c cost.Charge) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.charges = append(b.charges, c)
}

// TestPipelineWithBareChargeRecorder verifies cost tracking on bare recorders.
func TestPipelineWithBareChargeRecorder(t *testing.T) {
	takesDir := resolveFixturePath(t, "takes")
	segments := loadFixtureSegments(t)
	segSubset := segments[:2]
	workDir := t.TempDir()

	bare := &bareTestRecorder{}

	// Assert bareTestRecorder implements neither TotalReporter nor ChargesProvider.
	if _, ok := any(bare).(TotalReporter); ok {
		t.Fatalf("bareTestRecorder must not implement TotalReporter")
	}
	if _, ok := any(bare).(ChargesProvider); ok {
		t.Fatalf("bareTestRecorder must not implement ChargesProvider")
	}

	trans := newMockPipelineTranslator(nil)
	synth := newMockPipelineSynthesizer(takesDir, nil)

	// Segment 1 retry fits on attempt 2.
	synth.durations[1] = []time.Duration{
		1680 * time.Millisecond,
		1820 * time.Millisecond,
	}

	cfg := PipelineConfig{
		Segments:    segSubset,
		Translator:  trans,
		Synthesizer: synth,
		WorkDir:     workDir,
		Recorder:    bare,
	}

	res, err := RunPipeline(context.Background(), cfg)
	if err != nil {
		t.Fatalf("RunPipeline failed: %v", err)
	}

	if res.TotalCost <= 0 {
		t.Errorf("pipeline TotalCost = %v, want positive", res.TotalCost)
	}
	if len(res.Charges) == 0 {
		t.Fatalf("pipeline Charges is empty, want recorded charges")
	}

	bare.mu.Lock()
	bareCount := len(bare.charges)
	var bareTotal cost.Price
	for _, c := range bare.charges {
		bareTotal += c.Total()
	}
	bare.mu.Unlock()

	if bareCount != len(res.Charges) {
		t.Errorf("bare recorder charges count = %d, want %d", bareCount, len(res.Charges))
	}
	if res.TotalCost != bareTotal {
		t.Errorf("result TotalCost %v != bare recorder total %v", res.TotalCost, bareTotal)
	}
}

// TestPipelineProxyDecorators verifies translator and synthesizer proxies.
func TestPipelineProxyDecorators(t *testing.T) {
	takesDir := resolveFixturePath(t, "takes")
	segments := loadFixtureSegments(t)
	segSubset := segments[:1]
	workDir := t.TempDir()

	ledger := cost.NewLedger()
	trans := newMockPipelineTranslator(nil)
	synth := newMockPipelineSynthesizer(takesDir, nil)

	transProxy := NewTranslatorProxy(trans, nil)
	synthProxy := NewSynthesizerProxy(synth, nil)

	synth.durations[1] = []time.Duration{
		1820 * time.Millisecond,
	}

	cfg := PipelineConfig{
		Segments:    segSubset,
		Translator:  transProxy,
		Synthesizer: synthProxy,
		WorkDir:     workDir,
		Recorder:    ledger,
	}

	res, err := RunPipeline(context.Background(), cfg)
	if err != nil {
		t.Fatalf("RunPipeline failed: %v", err)
	}

	if res.TotalCost <= 0 {
		t.Errorf("pipeline TotalCost = %v, want positive", res.TotalCost)
	}
	if len(res.Charges) == 0 {
		t.Errorf("pipeline Charges is empty, want recorded charges")
	}
}

// TestPipelineResumesAfterCompletedTake proves an interrupted run resumes.
// The second pass renders only the lines the first pass never finished.
func TestPipelineResumesAfterCompletedTake(t *testing.T) {
	workDir := t.TempDir()
	segments := []types.Segment{
		{ID: 1, StartMs: 0, EndMs: 2000, Text: "First line", Speaker: types.Speaker{Name: "Suni Williams"}},
		{ID: 2, StartMs: 2000, EndMs: 4000, Text: "Second line", Speaker: types.Speaker{Name: "Suni Williams"}},
		{ID: 3, StartMs: 4000, EndMs: 6000, Text: "Third line", Speaker: types.Speaker{Name: "Suni Williams"}},
	}

	first := newMockPipelineSynthesizer("", nil)
	first.durations[1] = []time.Duration{2000 * time.Millisecond}
	first.errs[2] = errors.New("interrupted mid run")

	cfg := PipelineConfig{
		Segments:    segments,
		Translator:  newMockPipelineTranslator(nil),
		Synthesizer: first,
		WorkDir:     workDir,
	}
	if _, err := RunPipeline(context.Background(), cfg); err == nil {
		t.Fatal("first pass error = nil, want the interrupted run to fail")
	}
	if len(first.requests) != 2 {
		t.Fatalf("first pass synth requests = %d, want 2", len(first.requests))
	}

	completed := filepath.Join(workDir, "seg_1_try1.wav")
	if _, err := os.Stat(completed); err != nil {
		t.Fatalf("completed take missing after the first pass: %v", err)
	}
	if _, err := os.Stat(filepath.Join(workDir, "seg_2_try1.wav")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("interrupted line 2 left a take, stat err = %v", err)
	}

	second := newMockPipelineSynthesizer("", nil)
	second.durations[2] = []time.Duration{2000 * time.Millisecond}
	second.durations[3] = []time.Duration{2000 * time.Millisecond}
	cfg.Translator = newMockPipelineTranslator(nil)
	cfg.Synthesizer = second

	res, err := RunPipeline(context.Background(), cfg)
	if err != nil {
		t.Fatalf("resumed run failed: %v", err)
	}

	var rendered []int
	for _, req := range second.requests {
		rendered = append(rendered, req.SegmentID)
	}
	if len(rendered) != 2 || rendered[0] != 2 || rendered[1] != 3 {
		t.Fatalf("resumed synth requests = %v, want [2 3]", rendered)
	}

	line1, ok := res.Line(1)
	if !ok {
		t.Fatal("resumed run lost line 1")
	}
	if line1.ChosenTake.File != completed {
		t.Errorf("line 1 take = %s, want the take from the first pass", line1.ChosenTake.File)
	}
	if line1.Flagged {
		t.Errorf("line 1 flagged = true, want false")
	}
	if line1.ChosenTake.Fit.Measured != 2000*time.Millisecond {
		t.Errorf("line 1 measured = %v, want 2000ms", line1.ChosenTake.Fit.Measured)
	}
	if !res.IsClean() {
		t.Errorf("resumed run flagged %v, want clean", res.FlaggedSegments)
	}
}

// TestPipelineReplaysRecordedStretchedTake proves the loop never hands a
// stretched take to RepairLine. It replays the recorded result instead.
func TestPipelineReplaysRecordedStretchedTake(t *testing.T) {
	workDir := t.TempDir()
	slot := 2000 * time.Millisecond
	seg := types.Segment{
		ID:      1,
		StartMs: 0,
		EndMs:   2000,
		Text:    "Stretched resume line",
		Speaker: types.Speaker{Name: "Suni Williams"},
	}

	stretchedPath := filepath.Join(workDir, "seg_1_stretched.wav")
	if err := writeTestWAV(stretchedPath, slot); err != nil {
		t.Fatalf("writeTestWAV failed: %v", err)
	}

	recorded := LineResult{
		Segment:    seg,
		ChosenTake: types.Take{SegmentID: 1, Attempt: 1, File: stretchedPath, Duration: slot, Fit: types.NewFit(slot, slot)},
		Attempts: []LineAttempt{{
			Attempt:      1,
			Mode:         gemini.ModeNormal,
			Text:         "Stretched resume line",
			AudioPath:    stretchedPath,
			Fit:          types.NewFit(slot, slot),
			Repair:       types.RepairAtempo,
			RepairDetail: "atempo stretch applied at ratio 1.0500",
			Stretched:    true,
			Ratio:        1.05,
		}},
		NotificationCopy: "Line 1 fits slot (2000ms) on attempt 1 after time stretch at ratio 1.0500.",
	}
	if err := writeSegmentRecord(workDir, tts.Malayalam, recorded); err != nil {
		t.Fatalf("writeSegmentRecord failed: %v", err)
	}

	synth := newMockPipelineSynthesizer("", nil)
	cfg := PipelineConfig{
		Segments:    []types.Segment{seg},
		Translator:  newMockPipelineTranslator(nil),
		Synthesizer: synth,
		WorkDir:     workDir,
	}
	res, err := RunPipeline(context.Background(), cfg)
	if err != nil {
		t.Fatalf("RunPipeline failed: %v", err)
	}

	if len(synth.requests) != 0 {
		t.Fatalf("synth requests = %d, want 0 for a replayed stretched take", len(synth.requests))
	}
	line, ok := res.Line(1)
	if !ok {
		t.Fatal("missing line 1 in results")
	}
	if line.ChosenTake.File != stretchedPath {
		t.Errorf("line 1 take = %s, want %s", line.ChosenTake.File, stretchedPath)
	}
	if line.Flagged {
		t.Errorf("line 1 flagged = true, want false")
	}
	if len(line.Attempts) != 1 {
		t.Fatalf("line 1 attempts = %d, want 1", len(line.Attempts))
	}
	if line.Attempts[0].Repair != types.RepairAtempo || !line.Attempts[0].Stretched {
		t.Errorf("line 1 attempt = %+v, want the recorded atempo attempt", line.Attempts[0])
	}
	if line.Attempts[0].Text != "Stretched resume line" {
		t.Errorf("line 1 text = %q, want the recorded text", line.Attempts[0].Text)
	}
}

// TestReusableTakeRefusesStretchedAndFlagged pins the resume rule.
// Only a first attempt take that fit without a flag is reusable.
func TestReusableTakeRefusesStretchedAndFlagged(t *testing.T) {
	raw := LineResult{
		ChosenTake: types.Take{SegmentID: 1, Attempt: 1, File: "seg_1_try1.wav"},
	}
	if take := reusableTake(raw); take == nil || take.File != "seg_1_try1.wav" {
		t.Errorf("reusableTake(raw first attempt) = %v, want the recorded take", take)
	}

	stretched := LineResult{
		ChosenTake: types.Take{SegmentID: 1, Attempt: 1, File: "seg_1_stretched.wav"},
	}
	if take := reusableTake(stretched); take != nil {
		t.Errorf("reusableTake(stretched) = %v, want nil", take)
	}

	flagged := LineResult{
		Flagged:    true,
		ChosenTake: types.Take{SegmentID: 1, Attempt: 1, File: "seg_1_try1.wav"},
	}
	if take := reusableTake(flagged); take != nil {
		t.Errorf("reusableTake(flagged) = %v, want nil", take)
	}

	late := LineResult{
		ChosenTake: types.Take{SegmentID: 1, Attempt: 2, File: "seg_1_try2.wav"},
	}
	if take := reusableTake(late); take != nil {
		t.Errorf("reusableTake(second attempt) = %v, want nil", take)
	}
}

// refusingSynthesizer mirrors the production client and refuses to overwrite.
// The loop must clear a broken take before the fresh render claims that path.
type refusingSynthesizer struct {
	inner *mockPipelineSynthesizer
}

func (s *refusingSynthesizer) Synthesize(ctx context.Context, req tts.SynthesizeRequest) error {
	if _, err := os.Stat(req.OutPath); err == nil {
		return fmt.Errorf("tts refuses to overwrite %s", req.OutPath)
	}
	return s.inner.Synthesize(ctx, req)
}

// TestPipelineRerendersEmptyRecordedTake proves a zero-length take is not
// completed work. The loop rejects the record and renders the line again.
func TestPipelineRerendersEmptyRecordedTake(t *testing.T) {
	workDir := t.TempDir()
	slot := 2000 * time.Millisecond
	seg := types.Segment{
		ID:      1,
		StartMs: 0,
		EndMs:   2000,
		Text:    "Truncated take line",
		Speaker: types.Speaker{Name: "Suni Williams"},
	}

	takePath := filepath.Join(workDir, "seg_1_try1.wav")
	if err := os.WriteFile(takePath, nil, 0o644); err != nil {
		t.Fatalf("write empty take: %v", err)
	}

	recorded := LineResult{
		Segment:    seg,
		ChosenTake: types.Take{SegmentID: 1, Attempt: 1, File: takePath, Duration: slot, Fit: types.NewFit(slot, slot)},
		Attempts: []LineAttempt{{
			Attempt:      1,
			Mode:         gemini.ModeNormal,
			Text:         "Recorded text",
			AudioPath:    takePath,
			Fit:          types.NewFit(slot, slot),
			Repair:       types.RepairNone,
			RepairDetail: "fits slot within dead band",
		}},
		NotificationCopy: "Line 1 fits slot on attempt 1.",
	}
	if err := writeSegmentRecord(workDir, tts.Malayalam, recorded); err != nil {
		t.Fatalf("writeSegmentRecord failed: %v", err)
	}

	synth := &refusingSynthesizer{inner: newMockPipelineSynthesizer("", nil)}
	synth.inner.durations[1] = []time.Duration{slot}

	cfg := PipelineConfig{
		Segments:    []types.Segment{seg},
		Translator:  newMockPipelineTranslator(nil),
		Synthesizer: synth,
		WorkDir:     workDir,
	}
	res, err := RunPipeline(context.Background(), cfg)
	if err != nil {
		t.Fatalf("resumed run failed: %v", err)
	}

	if len(synth.inner.requests) != 1 {
		t.Fatalf("synth requests = %d, want 1 to re-render the empty take", len(synth.inner.requests))
	}
	line, ok := res.Line(1)
	if !ok {
		t.Fatal("missing line 1 in results")
	}
	if line.ChosenTake.File != takePath {
		t.Errorf("line 1 take = %s, want %s", line.ChosenTake.File, takePath)
	}
	if line.ChosenTake.Fit.Measured != slot {
		t.Errorf("line 1 measured = %v, want %v", line.ChosenTake.Fit.Measured, slot)
	}
	info, err := os.Stat(takePath)
	if err != nil {
		t.Fatalf("stat re-rendered take: %v", err)
	}
	if info.Size() == 0 {
		t.Error("re-rendered take is still empty")
	}
}

// TestPipelineRerendersEmptyReplayedTake proves the replay path never adopts
// an empty take. A flagged or stretched record with a truncated take renders again.
func TestPipelineRerendersEmptyReplayedTake(t *testing.T) {
	slot := 2000 * time.Millisecond
	seg := types.Segment{
		ID:      1,
		StartMs: 0,
		EndMs:   2000,
		Text:    "Replayed truncated line",
		Speaker: types.Speaker{Name: "Suni Williams"},
	}

	cases := []struct {
		name     string
		takeName string
		record   func(takePath string) LineResult
	}{
		{
			name:     "flagged",
			takeName: "seg_1_try1.wav",
			record: func(takePath string) LineResult {
				return LineResult{
					Segment:    seg,
					Flagged:    true,
					ChosenTake: types.Take{SegmentID: 1, Attempt: 1, File: takePath, Fit: types.NewFit(slot, 0)},
					Attempts: []LineAttempt{{
						Attempt:      1,
						Mode:         gemini.ModeNormal,
						Text:         "Stale flagged text",
						AudioPath:    takePath,
						Fit:          types.NewFit(slot, 0),
						Repair:       types.RepairManual,
						RepairDetail: "flagged for creator review",
					}},
					NotificationCopy: "Line 1 flagged for creator review.",
				}
			},
		},
		{
			name:     "stretched",
			takeName: "seg_1_stretched.wav",
			record: func(takePath string) LineResult {
				return LineResult{
					Segment:    seg,
					ChosenTake: types.Take{SegmentID: 1, Attempt: 1, File: takePath, Duration: slot, Fit: types.NewFit(slot, slot)},
					Attempts: []LineAttempt{{
						Attempt:      1,
						Mode:         gemini.ModeNormal,
						Text:         "Stale stretched text",
						AudioPath:    takePath,
						Fit:          types.NewFit(slot, slot),
						Repair:       types.RepairAtempo,
						RepairDetail: "atempo stretch applied at ratio 1.0500",
						Stretched:    true,
						Ratio:        1.05,
					}},
					NotificationCopy: "Line 1 fits slot after time stretch.",
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			workDir := t.TempDir()
			takePath := filepath.Join(workDir, tc.takeName)
			if err := os.WriteFile(takePath, nil, 0o644); err != nil {
				t.Fatalf("write empty take: %v", err)
			}
			if err := writeSegmentRecord(workDir, tts.Malayalam, tc.record(takePath)); err != nil {
				t.Fatalf("writeSegmentRecord failed: %v", err)
			}

			synth := &refusingSynthesizer{inner: newMockPipelineSynthesizer("", nil)}
			synth.inner.durations[1] = []time.Duration{slot}

			cfg := PipelineConfig{
				Segments:    []types.Segment{seg},
				Translator:  newMockPipelineTranslator(nil),
				Synthesizer: synth,
				WorkDir:     workDir,
			}
			res, err := RunPipeline(context.Background(), cfg)
			if err != nil {
				t.Fatalf("resumed run failed: %v", err)
			}
			if len(synth.inner.requests) != 1 {
				t.Fatalf("synth requests = %d, want 1 because an empty take cannot be replayed", len(synth.inner.requests))
			}
			line, ok := res.Line(1)
			if !ok {
				t.Fatal("missing line 1 in results")
			}
			if line.Flagged {
				t.Error("line 1 flagged = true, want the fresh render")
			}
			if len(line.Attempts) == 0 || strings.HasPrefix(line.Attempts[0].Text, "Stale") {
				t.Errorf("line 1 attempts = %+v, want a fresh render", line.Attempts)
			}

			freshPath := filepath.Join(workDir, "seg_1_try1.wav")
			if takePath != freshPath {
				if _, err := os.Stat(takePath); !errors.Is(err, os.ErrNotExist) {
					t.Errorf("empty recorded take still present, stat err = %v", err)
				}
			}
			info, err := os.Stat(freshPath)
			if err != nil {
				t.Fatalf("stat fresh take: %v", err)
			}
			if info.Size() == 0 {
				t.Error("fresh take is empty")
			}
		})
	}
}

// writePartialHeaderWAV writes a non-empty file cut inside the WAV header.
// ffprobe cannot decode it, so the loop must not treat it as completed work.
func writePartialHeaderWAV(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("RIFF\x00\x00\x00\x00WAVEfmt "), 0o644); err != nil {
		t.Fatalf("write partial header take: %v", err)
	}
}

// TestPipelineRerendersUndecodableRecordedTake proves a non-empty take that
// no decoder can read is not completed work. The loop rejects the record,
// removes the corrupt take, and renders the line again on the InitialTake path.
func TestPipelineRerendersUndecodableRecordedTake(t *testing.T) {
	workDir := t.TempDir()
	slot := 2000 * time.Millisecond
	seg := types.Segment{
		ID:      1,
		StartMs: 0,
		EndMs:   2000,
		Text:    "Undecodable take line",
		Speaker: types.Speaker{Name: "Suni Williams"},
	}

	takePath := filepath.Join(workDir, "seg_1_try1.wav")
	writePartialHeaderWAV(t, takePath)

	recorded := LineResult{
		Segment:    seg,
		ChosenTake: types.Take{SegmentID: 1, Attempt: 1, File: takePath, Duration: slot, Fit: types.NewFit(slot, slot)},
		Attempts: []LineAttempt{{
			Attempt:      1,
			Mode:         gemini.ModeNormal,
			Text:         "Recorded text",
			AudioPath:    takePath,
			Fit:          types.NewFit(slot, slot),
			Repair:       types.RepairNone,
			RepairDetail: "fits slot within dead band",
		}},
		NotificationCopy: "Line 1 fits slot on attempt 1.",
	}
	if err := writeSegmentRecord(workDir, tts.Malayalam, recorded); err != nil {
		t.Fatalf("writeSegmentRecord failed: %v", err)
	}

	synth := &refusingSynthesizer{inner: newMockPipelineSynthesizer("", nil)}
	synth.inner.durations[1] = []time.Duration{slot}

	cfg := PipelineConfig{
		Segments:    []types.Segment{seg},
		Translator:  newMockPipelineTranslator(nil),
		Synthesizer: synth,
		WorkDir:     workDir,
	}
	res, err := RunPipeline(context.Background(), cfg)
	if err != nil {
		t.Fatalf("resumed run failed: %v", err)
	}

	if len(synth.inner.requests) != 1 {
		t.Fatalf("synth requests = %d, want 1 to re-render the undecodable take", len(synth.inner.requests))
	}
	line, ok := res.Line(1)
	if !ok {
		t.Fatal("missing line 1 in results")
	}
	if line.ChosenTake.File != takePath {
		t.Errorf("line 1 take = %s, want %s", line.ChosenTake.File, takePath)
	}
	measured, err := media.Duration(context.Background(), takePath)
	if err != nil {
		t.Fatalf("re-rendered take is not decodable: %v", err)
	}
	if measured != slot {
		t.Errorf("re-rendered take measured = %v, want %v", measured, slot)
	}
}

// TestPipelineRerendersUndecodableReplayedTake proves the replay path never
// adopts a non-empty take that no decoder can read. A flagged or stretched
// record with such a take renders the line again.
func TestPipelineRerendersUndecodableReplayedTake(t *testing.T) {
	slot := 2000 * time.Millisecond
	seg := types.Segment{
		ID:      1,
		StartMs: 0,
		EndMs:   2000,
		Text:    "Replayed undecodable line",
		Speaker: types.Speaker{Name: "Suni Williams"},
	}

	cases := []struct {
		name     string
		takeName string
		record   func(takePath string) LineResult
	}{
		{
			name:     "flagged",
			takeName: "seg_1_try1.wav",
			record: func(takePath string) LineResult {
				return LineResult{
					Segment:    seg,
					Flagged:    true,
					ChosenTake: types.Take{SegmentID: 1, Attempt: 1, File: takePath, Fit: types.NewFit(slot, 0)},
					Attempts: []LineAttempt{{
						Attempt:      1,
						Mode:         gemini.ModeNormal,
						Text:         "Stale flagged text",
						AudioPath:    takePath,
						Fit:          types.NewFit(slot, 0),
						Repair:       types.RepairManual,
						RepairDetail: "flagged for creator review",
					}},
					NotificationCopy: "Line 1 flagged for creator review.",
				}
			},
		},
		{
			name:     "stretched",
			takeName: "seg_1_stretched.wav",
			record: func(takePath string) LineResult {
				return LineResult{
					Segment:    seg,
					ChosenTake: types.Take{SegmentID: 1, Attempt: 1, File: takePath, Duration: slot, Fit: types.NewFit(slot, slot)},
					Attempts: []LineAttempt{{
						Attempt:      1,
						Mode:         gemini.ModeNormal,
						Text:         "Stale stretched text",
						AudioPath:    takePath,
						Fit:          types.NewFit(slot, slot),
						Repair:       types.RepairAtempo,
						RepairDetail: "atempo stretch applied at ratio 1.0500",
						Stretched:    true,
						Ratio:        1.05,
					}},
					NotificationCopy: "Line 1 fits slot after time stretch.",
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			workDir := t.TempDir()
			takePath := filepath.Join(workDir, tc.takeName)
			writePartialHeaderWAV(t, takePath)
			if err := writeSegmentRecord(workDir, tts.Malayalam, tc.record(takePath)); err != nil {
				t.Fatalf("writeSegmentRecord failed: %v", err)
			}

			synth := &refusingSynthesizer{inner: newMockPipelineSynthesizer("", nil)}
			synth.inner.durations[1] = []time.Duration{slot}

			cfg := PipelineConfig{
				Segments:    []types.Segment{seg},
				Translator:  newMockPipelineTranslator(nil),
				Synthesizer: synth,
				WorkDir:     workDir,
			}
			res, err := RunPipeline(context.Background(), cfg)
			if err != nil {
				t.Fatalf("resumed run failed: %v", err)
			}
			if len(synth.inner.requests) != 1 {
				t.Fatalf("synth requests = %d, want 1 because an undecodable take cannot be replayed", len(synth.inner.requests))
			}
			line, ok := res.Line(1)
			if !ok {
				t.Fatal("missing line 1 in results")
			}
			if line.Flagged {
				t.Error("line 1 flagged = true, want the fresh render")
			}
			if len(line.Attempts) == 0 || strings.HasPrefix(line.Attempts[0].Text, "Stale") {
				t.Errorf("line 1 attempts = %+v, want a fresh render", line.Attempts)
			}

			freshPath := filepath.Join(workDir, "seg_1_try1.wav")
			if takePath != freshPath {
				if _, err := os.Stat(takePath); !errors.Is(err, os.ErrNotExist) {
					t.Errorf("undecodable recorded take still present, stat err = %v", err)
				}
			}
			measured, err := media.Duration(context.Background(), freshPath)
			if err != nil {
				t.Fatalf("fresh take is not decodable: %v", err)
			}
			if measured != slot {
				t.Errorf("fresh take measured = %v, want %v", measured, slot)
			}
		})
	}
}

// installFakeFFProbe puts a fake ffprobe ahead of the real one on PATH.
// It returns a function that restores the original PATH.
func installFakeFFProbe(t *testing.T, script string) func() {
	t.Helper()
	dir := t.TempDir()
	exe := filepath.Join(dir, "ffprobe")
	if err := os.WriteFile(exe, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake ffprobe: %v", err)
	}
	orig := os.Getenv("PATH")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+orig)
	return func() {
		if err := os.Setenv("PATH", orig); err != nil {
			t.Fatalf("restore PATH: %v", err)
		}
	}
}

// waitForFile blocks until path exists or ten seconds elapse.
func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", path)
}

// completedTakeSetup writes a decodable take and a valid record for line 1.
// It returns the work directory, the take path, and the segment.
func completedTakeSetup(t *testing.T, slot time.Duration) (string, string, types.Segment) {
	t.Helper()
	workDir := t.TempDir()
	seg := types.Segment{
		ID:      1,
		StartMs: 0,
		EndMs:   2000,
		Text:    "Completed line",
		Speaker: types.Speaker{Name: "Suni Williams"},
	}

	takePath := filepath.Join(workDir, "seg_1_try1.wav")
	if err := writeTestWAV(takePath, slot); err != nil {
		t.Fatalf("writeTestWAV failed: %v", err)
	}

	recorded := LineResult{
		Segment:    seg,
		ChosenTake: types.Take{SegmentID: 1, Attempt: 1, File: takePath, Duration: slot, Fit: types.NewFit(slot, slot)},
		Attempts: []LineAttempt{{
			Attempt:      1,
			Mode:         gemini.ModeNormal,
			Text:         "Completed line",
			AudioPath:    takePath,
			Fit:          types.NewFit(slot, slot),
			Repair:       types.RepairNone,
			RepairDetail: "fits slot within dead band",
		}},
		NotificationCopy: "Line 1 fits slot on attempt 1.",
	}
	if err := writeSegmentRecord(workDir, tts.Malayalam, recorded); err != nil {
		t.Fatalf("writeSegmentRecord failed: %v", err)
	}
	return workDir, takePath, seg
}

// TestPipelineCancelDuringProbeKeepsCompletedTake proves a cancel that lands
// inside the probe never discards a completed take. The next resume reuses it.
func TestPipelineCancelDuringProbeKeepsCompletedTake(t *testing.T) {
	workDir, takePath, seg := completedTakeSetup(t, 2000*time.Millisecond)

	marker := filepath.Join(t.TempDir(), "probe-started")
	restorePath := installFakeFFProbe(t, fmt.Sprintf("#!/bin/sh\n: > '%s'\nexec sleep 30\n", marker))
	defer restorePath()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	synth := newMockPipelineSynthesizer("", nil)
	cfg := PipelineConfig{
		Segments:    []types.Segment{seg},
		Translator:  newMockPipelineTranslator(nil),
		Synthesizer: synth,
		WorkDir:     workDir,
	}

	done := make(chan error, 1)
	go func() {
		_, err := RunPipeline(ctx, cfg)
		done <- err
	}()

	waitForFile(t, marker)
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("interrupted run error = %v, want context.Canceled", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("interrupted run did not return after the cancel")
	}

	if _, err := os.Stat(takePath); err != nil {
		t.Fatalf("cancelled probe removed the completed take: %v", err)
	}

	restorePath()

	second := newMockPipelineSynthesizer("", nil)
	cfg.Synthesizer = second
	cfg.Translator = newMockPipelineTranslator(nil)
	res, err := RunPipeline(context.Background(), cfg)
	if err != nil {
		t.Fatalf("resumed run failed: %v", err)
	}
	if len(second.requests) != 0 {
		t.Fatalf("resumed synth requests = %d, want 0 to reuse the completed take", len(second.requests))
	}
	line, ok := res.Line(1)
	if !ok {
		t.Fatal("missing line 1 in results")
	}
	if line.ChosenTake.File != takePath {
		t.Errorf("line 1 take = %s, want %s", line.ChosenTake.File, takePath)
	}
	if line.ChosenTake.Fit.Measured != 2000*time.Millisecond {
		t.Errorf("line 1 measured = %v, want 2000ms", line.ChosenTake.Fit.Measured)
	}
}

// TestPipelineDeadlineDuringProbeKeepsCompletedTake proves a deadline that
// expires inside the probe leaves the completed take in place.
func TestPipelineDeadlineDuringProbeKeepsCompletedTake(t *testing.T) {
	workDir, takePath, seg := completedTakeSetup(t, 2000*time.Millisecond)

	marker := filepath.Join(t.TempDir(), "probe-started")
	restorePath := installFakeFFProbe(t, fmt.Sprintf("#!/bin/sh\n: > '%s'\nexec sleep 30\n", marker))
	defer restorePath()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	cfg := PipelineConfig{
		Segments:    []types.Segment{seg},
		Translator:  newMockPipelineTranslator(nil),
		Synthesizer: newMockPipelineSynthesizer("", nil),
		WorkDir:     workDir,
	}

	if _, err := RunPipeline(ctx, cfg); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timed out run error = %v, want context.DeadlineExceeded", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("deadline expired before the probe started: %v", err)
	}
	if _, err := os.Stat(takePath); err != nil {
		t.Fatalf("expired probe removed the completed take: %v", err)
	}
}
