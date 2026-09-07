//go:build live

package gemini

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nrynss/ajilamu/internal/config"
	"github.com/nrynss/ajilamu/internal/cost"
	"github.com/nrynss/ajilamu/internal/media"
	"github.com/nrynss/ajilamu/internal/types"
)

// TestSegmentLive probes the real Vertex AI endpoint with the sample clip.
// Run it with: go test -tags live ./internal/gemini/
// It never runs in CI and skips only when ADC is unusable.
func TestSegmentLive(t *testing.T) {
	loadDotEnv(t, filepath.Join("..", "..", ".env"))
	os.Unsetenv("GEMINI_API_KEY")

	project := os.Getenv("GOOGLE_CLOUD_PROJECT")
	if project == "" {
		t.Fatal("GOOGLE_CLOUD_PROJECT is unset")
	}

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}

	clipPath := filepath.Join(root, "testdata", "clip.mp4")
	video, err := os.ReadFile(clipPath)
	if err != nil {
		t.Fatalf("read clip: %v", err)
	}

	cfg := &config.Config{
		GeminiModel:         config.DefaultGeminiModel,
		GoogleCloudProject:  project,
		GoogleCloudLocation: config.DefaultGoogleLocation,
	}
	client, err := newVertexClient(cfg)
	if err != nil {
		if isADCUnusable(err) {
			t.Skipf("application default credentials unusable: %v", err)
		}
		t.Fatalf("build vertex client: %v", err)
	}

	fixtureRaw, err := os.ReadFile(filepath.Join(root, "testdata", "segments.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	want, err := decodeSegments(string(fixtureRaw))
	if err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	clipDur, err := media.Duration(clipPath)
	if err != nil {
		t.Fatalf("probe clip duration: %v", err)
	}
	clipMs := clipDur.Milliseconds()

	fmt.Printf("live probe: model=%s location=%s project=%s\n", cfg.GeminiModel, cfg.GoogleCloudLocation, cfg.GoogleCloudProject)

	const attempts = 3
	var lastProblems []string
	for attempt := 1; attempt <= attempts; attempt++ {
		ledger := cost.NewLedger()
		seg, err := NewSegmenter(cfg, ledger, cost.DefaultRateCard(), client)
		if err != nil {
			t.Fatalf("build segmenter: %v", err)
		}
		started := time.Now()
		segments, err := seg.Segment(context.Background(), Input{Data: video, MIMEType: "video/mp4"})
		if err != nil {
			fmt.Printf("live probe attempt %d failed: %v\n", attempt, err)
			lastProblems = []string{err.Error()}
			if attempt == attempts {
				t.Fatalf("live segmentation failed: %v", err)
			}
			continue
		}
		fmt.Printf("\n--- live transcript attempt %d (%d segments, %s) ---\n", attempt, len(segments), time.Since(started).Round(time.Millisecond))
		for _, s := range segments {
			fmt.Printf("  #%d [%dms -> %dms (%dms)] %s (%s): %q\n",
				s.ID, s.StartMs, s.EndMs, s.EndMs-s.StartMs, s.Speaker.Name, s.Emotion, s.Text)
		}
		for _, c := range ledger.Charges() {
			fmt.Printf("  charge: kind=%v take=%d promptTokens=%d candidateTokens=%d total=%v\n",
				c.Kind, c.TakeID, c.PromptTokens, c.CandidateTokens, c.Total())
		}
		fmt.Printf("  count: live=%d fixture=%d\n", len(segments), len(want))
		printDrift(segments, want)
		lastProblems = liveStructureProblems(segments, want, clipMs)
		if len(lastProblems) == 0 {
			return
		}
		for _, p := range lastProblems {
			fmt.Printf("  structure: %s\n", p)
		}
		if attempt == attempts {
			for _, p := range lastProblems {
				t.Error(p)
			}
		}
	}
}

func liveStructureProblems(segments, want []types.Segment, clipMs int64) []string {
	var problems []string
	for i, s := range segments {
		if i > 0 && s.ID <= segments[i-1].ID {
			problems = append(problems, fmt.Sprintf("segment %d: id %d does not increase past %d", s.ID, s.ID, segments[i-1].ID))
		}
		row := wireSegment{
			ID:         s.ID,
			StartMs:    s.StartMs,
			EndMs:      s.EndMs,
			DurationMs: s.EndMs - s.StartMs,
			Text:       s.Text,
			Speaker:    s.Speaker.Name,
			Emotion:    s.Emotion,
		}
		if err := row.validate(); err != nil {
			problems = append(problems, fmt.Sprintf("segment %d: %v", s.ID, err))
		}
		if s.StartMs < 0 || s.EndMs > clipMs {
			problems = append(problems, fmt.Sprintf("segment %d: range [%d, %d] escapes clip length %d ms", s.ID, s.StartMs, s.EndMs, clipMs))
		}
		if i > 0 && s.StartMs < segments[i-1].EndMs {
			problems = append(problems, fmt.Sprintf("segment %d: start %d overlaps previous end %d", s.ID, s.StartMs, segments[i-1].EndMs))
		}
	}
	wantSpeakers := map[string]bool{}
	for _, s := range want {
		wantSpeakers[s.Speaker.Name] = true
	}
	gotSpeakers := map[string]bool{}
	for _, s := range segments {
		gotSpeakers[s.Speaker.Name] = true
	}
	for name := range wantSpeakers {
		if !gotSpeakers[name] {
			problems = append(problems, fmt.Sprintf("live transcript misses fixture speaker %q", name))
		}
	}
	for name := range gotSpeakers {
		if !wantSpeakers[name] {
			problems = append(problems, fmt.Sprintf("live transcript adds unknown speaker %q", name))
		}
	}
	return problems
}

func printDrift(segments, want []types.Segment) {
	wantByID := map[int]types.Segment{}
	for _, s := range want {
		wantByID[s.ID] = s
	}
	gotByID := map[int]types.Segment{}
	for _, s := range segments {
		gotByID[s.ID] = s
	}
	fmt.Printf("  --- drift against fixture (ms) ---\n")
	for _, s := range segments {
		w, ok := wantByID[s.ID]
		if !ok {
			fmt.Printf("  id %d: live only (start=%d end=%d)\n", s.ID, s.StartMs, s.EndMs)
			continue
		}
		fmt.Printf("  id %d: live start=%d fixture start=%d drift=%d live end=%d fixture end=%d drift=%d\n",
			s.ID, s.StartMs, w.StartMs, absDiff(s.StartMs, w.StartMs), s.EndMs, w.EndMs, absDiff(s.EndMs, w.EndMs))
	}
	for _, w := range want {
		if _, ok := gotByID[w.ID]; !ok {
			fmt.Printf("  id %d: fixture only (start=%d end=%d)\n", w.ID, w.StartMs, w.EndMs)
		}
	}
}

// loadDotEnv applies repo root .env entries missing from the process environment.
// It mirrors the setdefault behaviour of tools/validate_pipeline.py and never logs values.
func loadDotEnv(t *testing.T, path string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" {
			continue
		}
		if _, exists := os.LookupEnv(key); !exists {
			os.Setenv(key, value)
		}
	}
}

// absDiff returns the absolute distance between two millisecond stamps.
func absDiff(a, b int64) int64 {
	if a > b {
		return a - b
	}
	return b - a
}

// isADCUnusable reports whether client construction failed because ADC is missing.
func isADCUnusable(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "default credentials")
}
