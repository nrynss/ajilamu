package fit

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nrynss/ajilamu/internal/gemini"
	"github.com/nrynss/ajilamu/internal/tts"
	"github.com/nrynss/ajilamu/internal/types"
)

type mockTranslator struct {
	mu       sync.Mutex
	requests []gemini.TranslateRequest
	replies  []string
	errs     []error
}

func (m *mockTranslator) Translate(ctx context.Context, req gemini.TranslateRequest) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	idx := len(m.requests)
	m.requests = append(m.requests, req)
	if idx < len(m.errs) && m.errs[idx] != nil {
		return "", m.errs[idx]
	}
	if idx < len(m.replies) && m.replies[idx] != "" {
		return m.replies[idx], nil
	}
	return fmt.Sprintf("translated attempt %d mode %s", idx+1, req.Mode), nil
}

type mockSynthesizer struct {
	mu        sync.Mutex
	requests  []tts.SynthesizeRequest
	durations []time.Duration
	errs      []error
}

func (s *mockSynthesizer) Synthesize(ctx context.Context, req tts.SynthesizeRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := len(s.requests)
	s.requests = append(s.requests, req)
	if idx < len(s.errs) && s.errs[idx] != nil {
		return s.errs[idx]
	}
	dur := 1000 * time.Millisecond
	if idx < len(s.durations) && s.durations[idx] > 0 {
		dur = s.durations[idx]
	}
	return writeTestWAV(req.OutPath, dur)
}

func writeTestWAV(path string, dur time.Duration) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	sampleRate := uint32(16000)
	numSamples := int(int64(sampleRate) * dur.Milliseconds() / 1000)
	dataSize := uint32(numSamples * 2)

	buf := make([]byte, 44+dataSize)
	copy(buf[0:4], "RIFF")
	binary.LittleEndian.PutUint32(buf[4:8], 36+dataSize)
	copy(buf[8:12], "WAVE")
	copy(buf[12:16], "fmt ")
	binary.LittleEndian.PutUint32(buf[16:20], 16)
	binary.LittleEndian.PutUint16(buf[20:22], 1)
	binary.LittleEndian.PutUint16(buf[22:24], 1)
	binary.LittleEndian.PutUint32(buf[24:28], sampleRate)
	binary.LittleEndian.PutUint32(buf[28:32], sampleRate*2)
	binary.LittleEndian.PutUint16(buf[32:34], 2)
	binary.LittleEndian.PutUint16(buf[34:36], 16)
	copy(buf[36:40], "data")
	binary.LittleEndian.PutUint32(buf[40:44], dataSize)

	return os.WriteFile(path, buf, 0o644)
}

func assertProseStyle(t *testing.T, text string) {
	t.Helper()
	if strings.Contains(text, ";") {
		t.Errorf("text contains semicolon: %q", text)
	}
	if strings.Contains(text, "—") || strings.Contains(text, "--") {
		t.Errorf("text contains em dash: %q", text)
	}
	sentences := strings.Split(text, ".")
	for _, raw := range sentences {
		s := strings.TrimSpace(raw)
		if s == "" {
			continue
		}
		words := strings.Fields(s)
		if len(words) > 30 {
			t.Errorf("sentence exceeds 30 words (%d words): %q", len(words), s)
		}
	}
}

func TestCleanFitAttempt1(t *testing.T) {
	workDir := t.TempDir()
	slot := 1820 * time.Millisecond
	seg := types.Segment{
		ID:      1,
		StartMs: 0,
		EndMs:   1820,
		Text:    "Clean line",
		Speaker: types.Speaker{Name: "Speaker1"},
	}

	trans := &mockTranslator{replies: []string{"Clean translated"}}
	synth := &mockSynthesizer{durations: []time.Duration{slot}}

	cfg := RewriteConfig{
		Translator:  trans,
		Synthesizer: synth,
		WorkDir:     workDir,
	}

	res, err := RepairLine(context.Background(), seg, cfg)
	if err != nil {
		t.Fatalf("RepairLine failed: %v", err)
	}

	if res.Flagged {
		t.Errorf("got Flagged true, want false")
	}
	if len(res.Attempts) != 1 {
		t.Fatalf("got %d attempts, want 1", len(res.Attempts))
	}
	att := res.Attempts[0]
	if att.Attempt != 1 {
		t.Errorf("attempt = %d, want 1", att.Attempt)
	}
	if att.Repair != types.RepairNone {
		t.Errorf("repair = %v, want RepairNone", att.Repair)
	}
	if att.Stretched {
		t.Errorf("stretched = true, want false")
	}
	if res.ChosenTake.Attempt != 1 {
		t.Errorf("chosen take attempt = %d, want 1", res.ChosenTake.Attempt)
	}
	if res.ChosenTake.Duration != slot {
		t.Errorf("chosen take duration = %v, want %v", res.ChosenTake.Duration, slot)
	}

	assertProseStyle(t, res.NotificationCopy)
}

func TestAtempoStretchAttempt1Overrun(t *testing.T) {
	workDir := t.TempDir()
	slot := 5320 * time.Millisecond
	takeDur := 5720 * time.Millisecond
	seg := types.Segment{
		ID:      3,
		StartMs: 0,
		EndMs:   int64(slot.Milliseconds()),
		Text:    "Overrun line within budget",
		Speaker: types.Speaker{Name: "Speaker3"},
	}

	trans := &mockTranslator{replies: []string{"Overrun translated"}}
	synth := &mockSynthesizer{durations: []time.Duration{takeDur}}

	cfg := RewriteConfig{
		Translator:  trans,
		Synthesizer: synth,
		WorkDir:     workDir,
	}

	res, err := RepairLine(context.Background(), seg, cfg)
	if err != nil {
		t.Fatalf("RepairLine failed: %v", err)
	}

	if res.Flagged {
		t.Errorf("got Flagged true, want false")
	}
	if len(res.Attempts) != 1 {
		t.Fatalf("got %d attempts, want 1", len(res.Attempts))
	}
	att := res.Attempts[0]
	if att.Repair != types.RepairAtempo {
		t.Errorf("repair = %v, want RepairAtempo", att.Repair)
	}
	if !att.Stretched {
		t.Errorf("stretched = false, want true")
	}
	wantName := "seg_3_try1_stretched.wav"
	if filepath.Base(att.AudioPath) != wantName {
		t.Errorf("audio path = %s, want %s", filepath.Base(att.AudioPath), wantName)
	}
	if filepath.Base(res.ChosenTake.File) != wantName {
		t.Errorf("chosen take file = %s, want %s", filepath.Base(res.ChosenTake.File), wantName)
	}

	if _, err := os.Stat(res.ChosenTake.File); err != nil {
		t.Fatalf("stretched file does not exist: %v", err)
	}

	assertProseStyle(t, res.NotificationCopy)
}

func TestAtempoStretchAttempt1Underrun(t *testing.T) {
	workDir := t.TempDir()
	slot := 5480 * time.Millisecond
	takeDur := 5320 * time.Millisecond
	seg := types.Segment{
		ID:      2,
		StartMs: 0,
		EndMs:   int64(slot.Milliseconds()),
		Text:    "Underrun line within budget",
		Speaker: types.Speaker{Name: "Speaker2"},
	}

	trans := &mockTranslator{replies: []string{"Underrun translated"}}
	synth := &mockSynthesizer{durations: []time.Duration{takeDur}}

	cfg := RewriteConfig{
		Translator:  trans,
		Synthesizer: synth,
		WorkDir:     workDir,
	}

	res, err := RepairLine(context.Background(), seg, cfg)
	if err != nil {
		t.Fatalf("RepairLine failed: %v", err)
	}

	if res.Flagged {
		t.Errorf("got Flagged true, want false")
	}
	if len(res.Attempts) != 1 {
		t.Fatalf("got %d attempts, want 1", len(res.Attempts))
	}
	att := res.Attempts[0]
	if att.Repair != types.RepairAtempo {
		t.Errorf("repair = %v, want RepairAtempo", att.Repair)
	}
	if !att.Stretched {
		t.Errorf("stretched = false, want true")
	}

	assertProseStyle(t, res.NotificationCopy)
}

func TestOverrunRewriteAttempt2Fits(t *testing.T) {
	workDir := t.TempDir()
	slot := 2000 * time.Millisecond
	dur1 := 2400 * time.Millisecond
	dur2 := 2000 * time.Millisecond

	seg := types.Segment{
		ID:      10,
		StartMs: 0,
		EndMs:   int64(slot.Milliseconds()),
		Text:    "Large overrun line",
		Speaker: types.Speaker{Name: "Speaker10"},
	}

	trans := &mockTranslator{replies: []string{"Too long text", "Shorter text"}}
	synth := &mockSynthesizer{durations: []time.Duration{dur1, dur2}}

	cfg := RewriteConfig{
		Translator:  trans,
		Synthesizer: synth,
		WorkDir:     workDir,
	}

	res, err := RepairLine(context.Background(), seg, cfg)
	if err != nil {
		t.Fatalf("RepairLine failed: %v", err)
	}

	if res.Flagged {
		t.Errorf("got Flagged true, want false")
	}
	if len(res.Attempts) != 2 {
		t.Fatalf("got %d attempts, want 2", len(res.Attempts))
	}

	if len(trans.requests) != 2 {
		t.Fatalf("got %d translate calls, want 2", len(trans.requests))
	}
	if trans.requests[0].Mode != gemini.ModeNormal {
		t.Errorf("attempt 1 mode = %v, want ModeNormal", trans.requests[0].Mode)
	}
	if trans.requests[1].Mode != gemini.ModeShorter {
		t.Errorf("attempt 2 mode = %v, want ModeShorter", trans.requests[1].Mode)
	}

	if res.Attempts[0].Repair != types.RepairRewrite {
		t.Errorf("attempt 1 repair = %v, want RepairRewrite", res.Attempts[0].Repair)
	}
	if res.Attempts[1].Repair != types.RepairNone {
		t.Errorf("attempt 2 repair = %v, want RepairNone", res.Attempts[1].Repair)
	}
	if res.ChosenTake.Attempt != 2 {
		t.Errorf("chosen take attempt = %d, want 2", res.ChosenTake.Attempt)
	}

	assertProseStyle(t, res.NotificationCopy)
}

func TestUnderrunRewriteAttempt2Fits(t *testing.T) {
	workDir := t.TempDir()
	slot := 2000 * time.Millisecond
	dur1 := 1600 * time.Millisecond
	dur2 := 2010 * time.Millisecond

	seg := types.Segment{
		ID:      11,
		StartMs: 0,
		EndMs:   int64(slot.Milliseconds()),
		Text:    "Large underrun line",
		Speaker: types.Speaker{Name: "Speaker11"},
	}

	trans := &mockTranslator{replies: []string{"Short text", "Fuller text"}}
	synth := &mockSynthesizer{durations: []time.Duration{dur1, dur2}}

	cfg := RewriteConfig{
		Translator:  trans,
		Synthesizer: synth,
		WorkDir:     workDir,
	}

	res, err := RepairLine(context.Background(), seg, cfg)
	if err != nil {
		t.Fatalf("RepairLine failed: %v", err)
	}

	if res.Flagged {
		t.Errorf("got Flagged true, want false")
	}
	if len(res.Attempts) != 2 {
		t.Fatalf("got %d attempts, want 2", len(res.Attempts))
	}

	if len(trans.requests) != 2 {
		t.Fatalf("got %d translate calls, want 2", len(trans.requests))
	}
	if trans.requests[1].Mode != gemini.ModeFuller {
		t.Errorf("attempt 2 mode = %v, want ModeFuller", trans.requests[1].Mode)
	}

	if res.Attempts[0].Repair != types.RepairRewrite {
		t.Errorf("attempt 1 repair = %v, want RepairRewrite", res.Attempts[0].Repair)
	}
	if res.Attempts[1].Repair != types.RepairNone {
		t.Errorf("attempt 2 repair = %v, want RepairNone", res.Attempts[1].Repair)
	}
	if res.ChosenTake.Attempt != 2 {
		t.Errorf("chosen take attempt = %d, want 2", res.ChosenTake.Attempt)
	}

	assertProseStyle(t, res.NotificationCopy)
}

func TestRewriteThenStretch(t *testing.T) {
	workDir := t.TempDir()
	slot := 5000 * time.Millisecond
	dur1 := 4400 * time.Millisecond
	dur2 := 4850 * time.Millisecond

	seg := types.Segment{
		ID:      12,
		StartMs: 0,
		EndMs:   int64(slot.Milliseconds()),
		Text:    "Needs rewrite then stretch",
		Speaker: types.Speaker{Name: "Speaker12"},
	}

	trans := &mockTranslator{replies: []string{"Attempt 1 text", "Attempt 2 fuller text"}}
	synth := &mockSynthesizer{durations: []time.Duration{dur1, dur2}}

	cfg := RewriteConfig{
		Translator:  trans,
		Synthesizer: synth,
		WorkDir:     workDir,
	}

	res, err := RepairLine(context.Background(), seg, cfg)
	if err != nil {
		t.Fatalf("RepairLine failed: %v", err)
	}

	if res.Flagged {
		t.Errorf("got Flagged true, want false")
	}
	if len(res.Attempts) != 2 {
		t.Fatalf("got %d attempts, want 2", len(res.Attempts))
	}

	if trans.requests[1].Mode != gemini.ModeFuller {
		t.Errorf("attempt 2 mode = %v, want ModeFuller", trans.requests[1].Mode)
	}

	att2 := res.Attempts[1]
	if att2.Repair != types.RepairAtempo {
		t.Errorf("attempt 2 repair = %v, want RepairAtempo", att2.Repair)
	}
	if !att2.Stretched {
		t.Errorf("attempt 2 stretched = false, want true")
	}
	wantName := "seg_12_try2_stretched.wav"
	if filepath.Base(att2.AudioPath) != wantName {
		t.Errorf("audio path = %s, want %s", filepath.Base(att2.AudioPath), wantName)
	}
	if filepath.Base(res.ChosenTake.File) != wantName {
		t.Errorf("chosen take file = %s, want %s", filepath.Base(res.ChosenTake.File), wantName)
	}

	assertProseStyle(t, res.NotificationCopy)
}

func TestAllThreeAttemptsFailing(t *testing.T) {
	workDir := t.TempDir()
	slot := 5000 * time.Millisecond
	dur1 := 6000 * time.Millisecond
	dur2 := 4200 * time.Millisecond
	dur3 := 5600 * time.Millisecond

	seg := types.Segment{
		ID:      13,
		StartMs: 0,
		EndMs:   int64(slot.Milliseconds()),
		Text:    "Stubborn line failing all attempts",
		Speaker: types.Speaker{Name: "Speaker13"},
	}

	trans := &mockTranslator{replies: []string{"Try 1", "Try 2", "Try 3"}}
	synth := &mockSynthesizer{durations: []time.Duration{dur1, dur2, dur3}}

	cfg := RewriteConfig{
		Translator:  trans,
		Synthesizer: synth,
		WorkDir:     workDir,
	}

	res, err := RepairLine(context.Background(), seg, cfg)
	if err != nil {
		t.Fatalf("RepairLine failed: %v", err)
	}

	if !res.Flagged {
		t.Errorf("got Flagged false, want true")
	}
	if len(res.Attempts) != 3 {
		t.Fatalf("got %d attempts, want 3", len(res.Attempts))
	}

	if trans.requests[0].Mode != gemini.ModeNormal {
		t.Errorf("mode 1 = %v, want ModeNormal", trans.requests[0].Mode)
	}
	if trans.requests[1].Mode != gemini.ModeShorter {
		t.Errorf("mode 2 = %v, want ModeShorter", trans.requests[1].Mode)
	}
	if trans.requests[2].Mode != gemini.ModeFuller {
		t.Errorf("mode 3 = %v, want ModeFuller", trans.requests[2].Mode)
	}

	if res.ChosenTake.Attempt != 3 {
		t.Errorf("chosen take attempt = %d, want 3 (closest take)", res.ChosenTake.Attempt)
	}
	if res.ChosenTake.Duration != dur3 {
		t.Errorf("chosen take duration = %v, want %v", res.ChosenTake.Duration, dur3)
	}

	deltas := res.SignedDeltas()
	if len(deltas) != 3 {
		t.Fatalf("got %d signed deltas, want 3", len(deltas))
	}
	if deltas[0] != 1000*time.Millisecond {
		t.Errorf("delta 1 = %v, want 1000ms", deltas[0])
	}
	if deltas[1] != -800*time.Millisecond {
		t.Errorf("delta 2 = %v, want -800ms", deltas[1])
	}
	if deltas[2] != 600*time.Millisecond {
		t.Errorf("delta 3 = %v, want 600ms", deltas[2])
	}

	ms := res.SignedDeltaMs()
	if ms[0] != 1000 || ms[1] != -800 || ms[2] != 600 {
		t.Errorf("SignedDeltaMs = %v, want [1000, -800, 600]", ms)
	}

	if !strings.Contains(res.NotificationCopy, "Line 13 flagged for creator review") {
		t.Errorf("notification misses flag notice: %s", res.NotificationCopy)
	}
	if !strings.Contains(res.NotificationCopy, "Pipeline kept attempt 3") {
		t.Errorf("notification misses kept attempt: %s", res.NotificationCopy)
	}
	if !strings.Contains(res.NotificationCopy, "try 1 (+1000ms)") {
		t.Errorf("notification misses signed delta for try 1: %s", res.NotificationCopy)
	}
	if !strings.Contains(res.NotificationCopy, "try 2 (-800ms)") {
		t.Errorf("notification misses signed delta for try 2: %s", res.NotificationCopy)
	}
	if !strings.Contains(res.NotificationCopy, "try 3 (+600ms)") {
		t.Errorf("notification misses signed delta for try 3: %s", res.NotificationCopy)
	}

	assertProseStyle(t, res.NotificationCopy)
}

func TestSegment8NumbersUnderrunTriggersFuller(t *testing.T) {
	workDir := t.TempDir()
	slot := 7110 * time.Millisecond
	dur1 := 4200 * time.Millisecond
	dur2 := 7110 * time.Millisecond

	seg := types.Segment{
		ID:      8,
		StartMs: 0,
		EndMs:   int64(slot.Milliseconds()),
		Text:    "Segment 8 NASA speech",
		Speaker: types.Speaker{Name: "Mark"},
	}

	trans := &mockTranslator{replies: []string{"Try 1 short", "Try 2 full"}}
	synth := &mockSynthesizer{durations: []time.Duration{dur1, dur2}}

	cfg := RewriteConfig{
		Translator:  trans,
		Synthesizer: synth,
		WorkDir:     workDir,
	}

	res, err := RepairLine(context.Background(), seg, cfg)
	if err != nil {
		t.Fatalf("RepairLine failed: %v", err)
	}

	if res.Flagged {
		t.Errorf("got Flagged true, want false")
	}
	if len(trans.requests) != 2 {
		t.Fatalf("got %d translate requests, want 2", len(trans.requests))
	}
	if trans.requests[1].Mode != gemini.ModeFuller {
		t.Errorf("attempt 2 mode = %v, want ModeFuller", trans.requests[1].Mode)
	}
	if res.Attempts[0].Fit.Delta != -2910*time.Millisecond {
		t.Errorf("attempt 1 delta = %v, want -2910ms", res.Attempts[0].Fit.Delta)
	}
	if res.ChosenTake.Attempt != 2 {
		t.Errorf("chosen take attempt = %d, want 2", res.ChosenTake.Attempt)
	}
}

func TestErrorHandlingTranslator(t *testing.T) {
	workDir := t.TempDir()
	seg := types.Segment{
		ID:      20,
		StartMs: 0,
		EndMs:   2000,
		Text:    "Translate error test",
	}

	t.Run("attempt 1 error", func(t *testing.T) {
		wantErr := errors.New("network failure during translate")
		trans := &mockTranslator{errs: []error{wantErr}}
		synth := &mockSynthesizer{}
		cfg := RewriteConfig{
			Translator:  trans,
			Synthesizer: synth,
			WorkDir:     workDir,
		}

		_, err := RepairLine(context.Background(), seg, cfg)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, wantErr) {
			t.Errorf("err = %v, want wrapped %v", err, wantErr)
		}
	})

	t.Run("attempt 2 error", func(t *testing.T) {
		wantErr := errors.New("rate limit on attempt 2")
		trans := &mockTranslator{
			replies: []string{"Try 1"},
			errs:    []error{nil, wantErr},
		}
		synth := &mockSynthesizer{durations: []time.Duration{3000 * time.Millisecond}}
		cfg := RewriteConfig{
			Translator:  trans,
			Synthesizer: synth,
			WorkDir:     workDir,
		}

		_, err := RepairLine(context.Background(), seg, cfg)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, wantErr) {
			t.Errorf("err = %v, want wrapped %v", err, wantErr)
		}
	})
}

func TestErrorHandlingSynthesizer(t *testing.T) {
	workDir := t.TempDir()
	seg := types.Segment{
		ID:      21,
		StartMs: 0,
		EndMs:   2000,
		Text:    "Synthesize error test",
	}

	t.Run("attempt 1 synth error", func(t *testing.T) {
		wantErr := errors.New("cloud tts unavailable")
		trans := &mockTranslator{replies: []string{"Text"}}
		synth := &mockSynthesizer{errs: []error{wantErr}}
		cfg := RewriteConfig{
			Translator:  trans,
			Synthesizer: synth,
			WorkDir:     workDir,
		}

		_, err := RepairLine(context.Background(), seg, cfg)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, wantErr) {
			t.Errorf("err = %v, want wrapped %v", err, wantErr)
		}
	})

	t.Run("attempt 2 synth error", func(t *testing.T) {
		wantErr := errors.New("cloud tts failed on retry")
		trans := &mockTranslator{replies: []string{"Try 1", "Try 2"}}
		synth := &mockSynthesizer{
			durations: []time.Duration{4000 * time.Millisecond},
			errs:      []error{nil, wantErr},
		}
		cfg := RewriteConfig{
			Translator:  trans,
			Synthesizer: synth,
			WorkDir:     workDir,
		}

		_, err := RepairLine(context.Background(), seg, cfg)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, wantErr) {
			t.Errorf("err = %v, want wrapped %v", err, wantErr)
		}
	})
}

func TestErrorHandlingStretch(t *testing.T) {
	workDir := t.TempDir()
	slot := 5320 * time.Millisecond
	takeDur := 5720 * time.Millisecond
	seg := types.Segment{
		ID:      22,
		StartMs: 0,
		EndMs:   int64(slot.Milliseconds()),
		Text:    "Stretch error test",
	}

	t.Run("render failure returns wrapped error", func(t *testing.T) {
		trans := &mockTranslator{replies: []string{"Take text"}}
		synth := &mockSynthesizer{durations: []time.Duration{takeDur}}

		// Point stretched output to an invalid path that cannot be created.
		blockerFile := filepath.Join(workDir, "blocker.txt")
		if err := os.WriteFile(blockerFile, []byte("blocker"), 0o644); err != nil {
			t.Fatalf("failed to write blocker file: %v", err)
		}

		cfg := RewriteConfig{
			Translator:  trans,
			Synthesizer: synth,
			WorkDir:     workDir,
			PathBuilder: func(segID, attempt int, stretched bool) string {
				if stretched {
					return filepath.Join(blockerFile, "invalid_stretched.wav")
				}
				return filepath.Join(workDir, fmt.Sprintf("seg_%d_try%d.wav", segID, attempt))
			},
		}

		_, err := RepairLine(context.Background(), seg, cfg)
		if err == nil {
			t.Fatal("expected error from blocked stretch render, got nil")
		}
	})
}

func TestClosestTakeSelection(t *testing.T) {
	workDir := t.TempDir()
	slot := 5000 * time.Millisecond

	seg := types.Segment{
		ID:      25,
		StartMs: 0,
		EndMs:   int64(slot.Milliseconds()),
		Text:    "Closest take selection test",
	}

	t.Run("attempt 1 is closest", func(t *testing.T) {
		// Attempt 1: 5500ms (+500ms delta)
		// Attempt 2: 4000ms (-1000ms delta)
		// Attempt 3: 6200ms (+1200ms delta)
		trans := &mockTranslator{replies: []string{"T1", "T2", "T3"}}
		synth := &mockSynthesizer{
			durations: []time.Duration{
				5500 * time.Millisecond,
				4000 * time.Millisecond,
				6200 * time.Millisecond,
			},
		}
		cfg := RewriteConfig{
			Translator:  trans,
			Synthesizer: synth,
			WorkDir:     workDir,
			PathBuilder: func(segID, attempt int, stretched bool) string {
				return filepath.Join(workDir, fmt.Sprintf("closest1_try%d.wav", attempt))
			},
		}

		res, err := RepairLine(context.Background(), seg, cfg)
		if err != nil {
			t.Fatalf("RepairLine failed: %v", err)
		}
		if !res.Flagged {
			t.Errorf("res.Flagged = false, want true")
		}
		if res.ChosenTake.Attempt != 1 {
			t.Errorf("chosen take attempt = %d, want 1", res.ChosenTake.Attempt)
		}
		if res.ChosenTake.Duration != 5500*time.Millisecond {
			t.Errorf("chosen take duration = %v, want 5500ms", res.ChosenTake.Duration)
		}
	})

	t.Run("attempt 2 is closest", func(t *testing.T) {
		// Attempt 1: 6000ms (+1000ms delta)
		// Attempt 2: 4700ms (-300ms delta, but short budget is 5%=250ms, so 300ms is rewrite)
		// Attempt 3: 5600ms (+600ms delta)
		trans := &mockTranslator{replies: []string{"T1", "T2", "T3"}}
		synth := &mockSynthesizer{
			durations: []time.Duration{
				6000 * time.Millisecond,
				4700 * time.Millisecond,
				5600 * time.Millisecond,
			},
		}
		cfg := RewriteConfig{
			Translator:  trans,
			Synthesizer: synth,
			WorkDir:     workDir,
			PathBuilder: func(segID, attempt int, stretched bool) string {
				return filepath.Join(workDir, fmt.Sprintf("closest2_try%d.wav", attempt))
			},
		}

		res, err := RepairLine(context.Background(), seg, cfg)
		if err != nil {
			t.Fatalf("RepairLine failed: %v", err)
		}
		if !res.Flagged {
			t.Errorf("res.Flagged = false, want true")
		}
		if res.ChosenTake.Attempt != 2 {
			t.Errorf("chosen take attempt = %d, want 2", res.ChosenTake.Attempt)
		}
		if res.ChosenTake.Duration != 4700*time.Millisecond {
			t.Errorf("chosen take duration = %v, want 4700ms", res.ChosenTake.Duration)
		}
	})
}

func TestDistinctTakeFilenames(t *testing.T) {
	workDir := t.TempDir()
	slot := 5000 * time.Millisecond

	seg := types.Segment{
		ID:      5,
		StartMs: 0,
		EndMs:   int64(slot.Milliseconds()),
		Text:    "Distinct filename test",
	}

	trans := &mockTranslator{replies: []string{"T1", "T2", "T3"}}
	synth := &mockSynthesizer{
		durations: []time.Duration{
			6000 * time.Millisecond,
			4200 * time.Millisecond,
			5600 * time.Millisecond,
		},
	}

	cfg := RewriteConfig{
		Translator:  trans,
		Synthesizer: synth,
		WorkDir:     workDir,
	}

	res, err := RepairLine(context.Background(), seg, cfg)
	if err != nil {
		t.Fatalf("RepairLine failed: %v", err)
	}

	seen := make(map[string]bool)
	for _, a := range res.Attempts {
		if seen[a.AudioPath] {
			t.Errorf("duplicate take path: %s", a.AudioPath)
		}
		seen[a.AudioPath] = true
	}

	wantNames := []string{"seg_5_try1.wav", "seg_5_try2.wav", "seg_5_try3.wav"}
	for i, want := range wantNames {
		if filepath.Base(res.Attempts[i].AudioPath) != want {
			t.Errorf("attempt %d path = %s, want %s", i+1, filepath.Base(res.Attempts[i].AudioPath), want)
		}
	}
}

func TestRefuseStretchAlreadyStretched(t *testing.T) {
	stretchedPath := "/some/path/seg_1_try1_stretched.wav"
	if !isStretchedTake(stretchedPath) {
		t.Errorf("isStretchedTake(%q) = false, want true", stretchedPath)
	}

	regularPath := "/some/path/seg_1_try1.wav"
	if isStretchedTake(regularPath) {
		t.Errorf("isStretchedTake(%q) = true, want false", regularPath)
	}

	cleanMsg := cleanStretchErrorMessage(ErrAlreadyStretched)
	if cleanMsg != "Source take is already stretched." {
		t.Errorf("clean message = %q", cleanMsg)
	}
	assertProseStyle(t, cleanMsg)
}

func TestRepairLineRefusesStretchedInitialTake(t *testing.T) {
	workDir := t.TempDir()
	slot := 2000 * time.Millisecond
	stretchedInitialPath := filepath.Join(workDir, "seg_1_try1_stretched.wav")
	dur := 2100 * time.Millisecond
	if err := writeTestWAV(stretchedInitialPath, dur); err != nil {
		t.Fatalf("writeTestWAV failed: %v", err)
	}

	seg := types.Segment{
		ID:      1,
		StartMs: 0,
		EndMs:   2000,
		Text:    "Stretched take refusal test",
	}

	take := types.Take{
		SegmentID: 1,
		Attempt:   1,
		File:      stretchedInitialPath,
		Duration:  dur,
		Fit:       types.NewFit(slot, dur),
	}

	cfg := RewriteConfig{
		WorkDir:     workDir,
		InitialTake: &take,
		MaxAttempts: 1,
	}

	res, err := RepairLine(context.Background(), seg, cfg)
	if err != nil {
		t.Fatalf("RepairLine failed: %v", err)
	}

	if len(res.Attempts) != 1 {
		t.Fatalf("got %d attempts, want 1", len(res.Attempts))
	}
	att := res.Attempts[0]
	if att.Repair != types.RepairRewrite {
		t.Errorf("attempt 1 repair = %v, want RepairRewrite", att.Repair)
	}
	if att.RepairDetail != "Source take is already stretched." {
		t.Errorf("attempt 1 repair detail = %q, want 'Source take is already stretched.'", att.RepairDetail)
	}
	if att.Stretched {
		t.Errorf("attempt 1 Stretched = true, want false")
	}
	if !res.Flagged {
		t.Errorf("res.Flagged = false, want true when single attempt refused stretch")
	}
	if res.ChosenTake.File != stretchedInitialPath {
		t.Errorf("chosen take file = %s, want %s", res.ChosenTake.File, stretchedInitialPath)
	}
}

func TestRepairLineRefusesStretchedInitialTakeThenSynthesizes(t *testing.T) {
	workDir := t.TempDir()
	slot := 2000 * time.Millisecond
	stretchedInitialPath := filepath.Join(workDir, "source_stretched.wav")
	dur := 2100 * time.Millisecond
	if err := writeTestWAV(stretchedInitialPath, dur); err != nil {
		t.Fatalf("writeTestWAV failed: %v", err)
	}

	seg := types.Segment{
		ID:      2,
		StartMs: 0,
		EndMs:   2000,
		Text:    "Stretched retry test",
	}

	take := types.Take{
		SegmentID: 2,
		Attempt:   1,
		File:      stretchedInitialPath,
		Duration:  dur,
		Fit:       types.NewFit(slot, dur),
	}

	trans := &mockTranslator{replies: []string{"Shorter line"}}
	synth := &mockSynthesizer{durations: []time.Duration{slot}}

	cfg := RewriteConfig{
		Translator:  trans,
		Synthesizer: synth,
		WorkDir:     workDir,
		InitialTake: &take,
		MaxAttempts: 2,
	}

	res, err := RepairLine(context.Background(), seg, cfg)
	if err != nil {
		t.Fatalf("RepairLine failed: %v", err)
	}

	if len(res.Attempts) != 2 {
		t.Fatalf("got %d attempts, want 2", len(res.Attempts))
	}
	if res.Attempts[0].Repair != types.RepairRewrite {
		t.Errorf("attempt 1 repair = %v, want RepairRewrite", res.Attempts[0].Repair)
	}
	if res.Attempts[0].RepairDetail != "Source take is already stretched." {
		t.Errorf("attempt 1 repair detail = %q", res.Attempts[0].RepairDetail)
	}
	if res.Attempts[0].Stretched {
		t.Errorf("attempt 1 Stretched = true, want false")
	}
	if res.Attempts[1].Repair != types.RepairNone {
		t.Errorf("attempt 2 repair = %v, want RepairNone", res.Attempts[1].Repair)
	}
	if res.Flagged {
		t.Errorf("res.Flagged = true, want false")
	}
	if res.ChosenTake.Attempt != 2 {
		t.Errorf("chosen take attempt = %d, want 2", res.ChosenTake.Attempt)
	}
}

func TestCapTotalStretchLimit(t *testing.T) {
	cleanMsg := cleanStretchErrorMessage(ErrTotalStretchExceeded)
	if cleanMsg != "Total stretch correction exceeds maximum limit." {
		t.Errorf("clean message = %q", cleanMsg)
	}
	assertProseStyle(t, cleanMsg)

	// Verify that stretch limits exceeding MaxStretchLimit are rejected.
	_, err := NewRepairer(RewriteConfig{
		Limits: StretchLimits{Short: MaxStretchLimit + 0.05, Long: MaxStretchLimit + 0.05},
	})
	if !errors.Is(err, ErrLimits) {
		t.Errorf("NewRepairer err = %v, want errors.Is ErrLimits", err)
	}

	// Verify that overrun exceeding MaxStretchLimit routes to RepairRewrite.
	slot := 2000 * time.Millisecond
	overrunDur := time.Duration(float64(slot) * (1.0 + MaxStretchLimit + 0.05))
	fit := types.NewFit(slot, overrunDur)
	plan := PlanStretchWithLimits(fit, DefaultStretchLimits())
	if plan.Repair != types.RepairRewrite {
		t.Errorf("plan.Repair = %v, want RepairRewrite for overrun exceeding MaxStretchLimit", plan.Repair)
	}
}

func TestContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	seg := types.Segment{
		ID:      30,
		StartMs: 0,
		EndMs:   2000,
		Text:    "Cancel test",
	}
	cfg := RewriteConfig{
		Translator:  &mockTranslator{},
		Synthesizer: &mockSynthesizer{},
		WorkDir:     t.TempDir(),
	}

	_, err := RepairLine(ctx, seg, cfg)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestCustomPathBuilder(t *testing.T) {
	customDir := t.TempDir()
	slot := 2000 * time.Millisecond
	seg := types.Segment{
		ID:      40,
		StartMs: 0,
		EndMs:   2000,
		Text:    "Custom builder",
	}

	customBuilder := func(segID, attempt int, stretched bool) string {
		tag := "raw"
		if stretched {
			tag = "stretched"
		}
		return filepath.Join(customDir, fmt.Sprintf("custom_line_%d_att_%d_%s.wav", segID, attempt, tag))
	}

	cfg := RewriteConfig{
		Translator:  &mockTranslator{replies: []string{"Line"}},
		Synthesizer: &mockSynthesizer{durations: []time.Duration{slot}},
		PathBuilder: customBuilder,
	}

	res, err := RepairLine(context.Background(), seg, cfg)
	if err != nil {
		t.Fatalf("RepairLine failed: %v", err)
	}

	wantName := "custom_line_40_att_1_raw.wav"
	if filepath.Base(res.ChosenTake.File) != wantName {
		t.Errorf("chosen take file = %s, want %s", filepath.Base(res.ChosenTake.File), wantName)
	}
}

func TestNewRepairerInterface(t *testing.T) {
	workDir := t.TempDir()
	slot := 2000 * time.Millisecond
	seg := types.Segment{
		ID:      50,
		StartMs: 0,
		EndMs:   2000,
		Text:    "Interface test",
	}

	cfg := RewriteConfig{
		Translator:  &mockTranslator{replies: []string{"Interface text"}},
		Synthesizer: &mockSynthesizer{durations: []time.Duration{slot}},
		WorkDir:     workDir,
	}

	repairer, err := NewRepairer(cfg)
	if err != nil {
		t.Fatalf("NewRepairer failed: %v", err)
	}

	var lr LineRepairer = repairer
	res, err := lr.RepairLine(context.Background(), seg)
	if err != nil {
		t.Fatalf("lr.RepairLine failed: %v", err)
	}
	if res.Flagged {
		t.Errorf("res.Flagged = true, want false")
	}
}

func TestInitialTakeSupport(t *testing.T) {
	workDir := t.TempDir()
	slot := 2000 * time.Millisecond
	initialPath := filepath.Join(workDir, "initial_try1.wav")
	if err := writeTestWAV(initialPath, slot); err != nil {
		t.Fatalf("writeTestWAV failed: %v", err)
	}

	seg := types.Segment{
		ID:      60,
		StartMs: 0,
		EndMs:   2000,
		Text:    "Initial take test",
	}

	take := types.Take{
		SegmentID: 60,
		Attempt:   1,
		File:      initialPath,
		Duration:  slot,
		Fit:       types.NewFit(slot, slot),
	}

	cfg := RewriteConfig{
		WorkDir:     workDir,
		InitialTake: &take,
	}

	res, err := RepairLine(context.Background(), seg, cfg)
	if err != nil {
		t.Fatalf("RepairLine failed: %v", err)
	}
	if res.Flagged {
		t.Errorf("res.Flagged = true, want false")
	}
	if res.ChosenTake.File != initialPath {
		t.Errorf("chosen take file = %s, want %s", res.ChosenTake.File, initialPath)
	}
}

func TestStretchLimitsValidation(t *testing.T) {
	// Zero value limits default cleanly.
	rep, err := NewRepairer(RewriteConfig{Limits: StretchLimits{}})
	if err != nil {
		t.Fatalf("NewRepairer with zero limits failed: %v", err)
	}
	if rep.cfg.Limits != DefaultStretchLimits() {
		t.Errorf("NewRepairer zero limits = %+v, want %+v", rep.cfg.Limits, DefaultStretchLimits())
	}

	// Invalid limits in NewRepairer must return ErrLimits.
	invalidLimits := []StretchLimits{
		{Short: 0.25, Long: 0.25},
		{Short: -0.05, Long: 0.08},
		{Short: 0.05, Long: -0.08},
		{Short: 0.05, Long: 0.20},
		{Short: 0.0, Long: 0.08},
	}
	for _, lim := range invalidLimits {
		_, err := NewRepairer(RewriteConfig{Limits: lim})
		if !errors.Is(err, ErrLimits) {
			t.Errorf("NewRepairer with limits %+v err = %v, want errors.Is ErrLimits", lim, err)
		}
	}

	// Invalid limits in RepairLine must return ErrLimits.
	seg := types.Segment{
		ID:      1,
		StartMs: 0,
		EndMs:   2000,
		Text:    "Limits test",
	}
	for _, lim := range invalidLimits {
		cfg := RewriteConfig{
			Limits:  lim,
			WorkDir: t.TempDir(),
		}
		_, err := RepairLine(context.Background(), seg, cfg)
		if !errors.Is(err, ErrLimits) {
			t.Errorf("RepairLine with limits %+v err = %v, want errors.Is ErrLimits", lim, err)
		}
	}

	// Clean stretch error message handles ErrLimits cleanly.
	cleanMsg := cleanStretchErrorMessage(ErrLimits)
	if cleanMsg != "Stretch limits sit outside the valid range." {
		t.Errorf("clean message = %q", cleanMsg)
	}
	assertProseStyle(t, cleanMsg)
}

func TestSignedDeltasDerived(t *testing.T) {
	res := LineResult{
		Attempts: []LineAttempt{
			{
				Attempt: 1,
				Fit: types.Fit{
					Slot:     5000 * time.Millisecond,
					Measured: 5500 * time.Millisecond,
					Delta:    0,
				},
			},
			{
				Attempt: 2,
				Fit: types.Fit{
					Slot:     5000 * time.Millisecond,
					Measured: 4700 * time.Millisecond,
					Delta:    0,
				},
			},
		},
	}

	deltas := res.SignedDeltas()
	if len(deltas) != 2 {
		t.Fatalf("got %d signed deltas, want 2", len(deltas))
	}
	if deltas[0] != 500*time.Millisecond {
		t.Errorf("deltas[0] = %v, want 500ms", deltas[0])
	}
	if deltas[1] != -300*time.Millisecond {
		t.Errorf("deltas[1] = %v, want -300ms", deltas[1])
	}

	ms := res.SignedDeltaMs()
	if len(ms) != 2 {
		t.Fatalf("got %d signed delta ms, want 2", len(ms))
	}
	if ms[0] != 500 {
		t.Errorf("ms[0] = %d, want 500", ms[0])
	}
	if ms[1] != -300 {
		t.Errorf("ms[1] = %d, want -300", ms[1])
	}
}
