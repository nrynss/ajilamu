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
