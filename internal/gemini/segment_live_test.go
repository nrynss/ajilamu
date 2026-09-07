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
	"golang.org/x/oauth2/google"
)

// TestSegmentLive probes the real Vertex AI endpoint with the sample clip.
// Run it with: go test -tags live ./internal/gemini/
// It never runs in CI and skips without Google Cloud credentials.
func TestSegmentLive(t *testing.T) {
	loadDotEnv(t, filepath.Join("..", "..", ".env"))
	project := os.Getenv("GOOGLE_CLOUD_PROJECT")
	credsPath := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS")
	if project == "" || credsPath == "" {
		t.Skip("GOOGLE_CLOUD_PROJECT or GOOGLE_APPLICATION_CREDENTIALS is unset")
	}

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}

	tmp := t.TempDir()
	wav := filepath.Join(tmp, "speech_16k.wav")
	mp3 := filepath.Join(tmp, "speech.mp3")
	if err := media.Demux(filepath.Join(root, "testdata", "clip.mp4"), wav); err != nil {
		t.Fatalf("demux clip: %v", err)
	}
	if err := media.Run("-y", "-i", wav, "-b:a", "64k", mp3); err != nil {
		t.Fatalf("encode mp3: %v", err)
	}
	audio, err := os.ReadFile(mp3)
	if err != nil {
		t.Fatalf("read mp3: %v", err)
	}

	cfg := &config.Config{
		GeminiModel:                  envDefault(t, "GEMINI_MODEL", config.DefaultGeminiModel),
		GoogleCloudProject:           project,
		GoogleCloudLocation:          envDefault(t, "GOOGLE_CLOUD_LOCATION", config.DefaultGoogleLocation),
		GoogleApplicationCredentials: credsPath,
	}
	creds, err := google.FindDefaultCredentials(context.Background(), "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		t.Skipf("application default credentials unusable: %v", err)
	}
	ts := creds.TokenSource
	ledger := cost.NewLedger()
	seg, err := NewSegmenter(cfg, ts, ledger, nil, cost.DefaultRateCard())
	if err != nil {
		t.Fatalf("build segmenter: %v", err)
	}

	started := time.Now()
	segments, err := seg.Segment(context.Background(), Input{Data: audio, MIMEType: "audio/mp3"})
	if err != nil {
		// A live failure is evidence. Report it instead of masking it.
		t.Fatalf("live segmentation failed: %v", err)
	}
	endpoint, err := endpointURL(cfg)
	if err != nil {
		t.Fatalf("build endpoint URL: %v", err)
	}
	fmt.Printf("live probe: model=%s location=%s endpoint=%s\n", cfg.GeminiModel, cfg.GoogleCloudLocation, endpoint)
	fmt.Printf("\n--- live transcript (%d segments, %s) ---\n", len(segments), time.Since(started).Round(time.Millisecond))
	for _, s := range segments {
		fmt.Printf("  #%d [%dms -> %dms (%dms)] %s (%s): %q\n",
			s.ID, s.StartMs, s.EndMs, s.EndMs-s.StartMs, s.Speaker.Name, s.Emotion, s.Text)
	}
	for _, c := range ledger.Charges() {
		fmt.Printf("  charge: kind=%v take=%d units=%d unitPrice=%v total=%v\n",
			c.Kind, c.TakeID, c.Units, c.UnitPrice, c.Total())
	}

	fixtureRaw, err := os.ReadFile(filepath.Join(root, "testdata", "segments.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	want, err := decodeSegments(string(fixtureRaw))
	if err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	fmt.Printf("  count: live=%d fixture=%d\n", len(segments), len(want))

	// Structural checks enforce the ordering and bounds the downstream loop needs.
	// They rerun the client validation on every live row and keep segments inside the clip.
	clipDur, err := media.Duration(filepath.Join(root, "testdata", "clip.mp4"))
	if err != nil {
		t.Fatalf("probe clip duration: %v", err)
	}
	clipMs := clipDur.Milliseconds()
	for i, s := range segments {
		if i > 0 && s.ID <= segments[i-1].ID {
			t.Errorf("segment %d: id %d does not increase past %d", s.ID, s.ID, segments[i-1].ID)
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
			t.Errorf("segment %d: %v", s.ID, err)
		}
		if s.StartMs < 0 || s.EndMs > clipMs {
			t.Errorf("segment %d: range [%d, %d] escapes clip length %d ms", s.ID, s.StartMs, s.EndMs, clipMs)
		}
		if i > 0 && s.StartMs < segments[i-1].EndMs {
			t.Errorf("segment %d: start %d overlaps previous end %d", s.ID, s.StartMs, segments[i-1].EndMs)
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
			t.Errorf("live transcript misses fixture speaker %q", name)
		}
	}
	for name := range gotSpeakers {
		if !wantSpeakers[name] {
			t.Errorf("live transcript adds unknown speaker %q", name)
		}
	}

	// Drift logging records per-id timing drift without gating on it.
	// Generative runs vary, so the table informs readers instead of failing them.
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

// envDefault reads an environment variable or falls back.
func envDefault(t *testing.T, key, fallback string) string {
	t.Helper()
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

// absDiff returns the absolute distance between two millisecond stamps.
func absDiff(a, b int64) int64 {
	if a > b {
		return a - b
	}
	return b - a
}
