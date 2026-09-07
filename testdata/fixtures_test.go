package testdata_test

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/nrynss/ajilamu/internal/media"
)

// Segment represents a dialogue line from segments.json.
type Segment struct {
	ID         int    `json:"id"`
	StartMs    int64  `json:"start_ms"`
	EndMs      int64  `json:"end_ms"`
	DurationMs int64  `json:"duration_ms"`
	Text       string `json:"text"`
	Speaker    string `json:"speaker"`
	Emotion    string `json:"emotion"`
}

// ExpectedMetric defines golden measurements for a dialogue segment.
type ExpectedMetric struct {
	SegmentID          int              `json:"segment_id"`
	SlotMs             int64            `json:"slot_ms"`
	TakeMs             int64            `json:"take_ms"`
	DeltaMs            int64            `json:"delta_ms"`
	DeltaPct           float64          `json:"delta_pct"`
	Repair             string           `json:"repair"`
	RepairedDurationMs int64            `json:"repaired_duration_ms"`
	Outcome            string           `json:"outcome"`
	TakeFile           string           `json:"take_file"`
	RepairedTakeFile   string           `json:"repaired_take_file"`
	Takes              map[string]int64 `json:"takes"`
}

// resolvePath locates a fixture path across working directory variations.
func resolvePath(t *testing.T, rel string) string {
	t.Helper()
	candidates := []string{
		rel,
		filepath.Join("testdata", rel),
		filepath.Join("..", "testdata", rel),
		filepath.Join("..", rel),
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	t.Fatalf("fixture file not found: %s", rel)
	return ""
}

// TestSegments verifies eight dialogue segments in segments.json.
func TestSegments(t *testing.T) {
	path := resolvePath(t, "segments.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read segments.json: %v", err)
	}

	var segments []Segment
	if err := json.Unmarshal(data, &segments); err != nil {
		t.Fatalf("unmarshal segments.json: %v", err)
	}

	const wantSegments = 8
	if len(segments) != wantSegments {
		t.Fatalf("got %d segments, want %d", len(segments), wantSegments)
	}

	for i, seg := range segments {
		expectedID := i + 1
		if seg.ID != expectedID {
			t.Errorf("segment[%d] ID = %d, want %d", i, seg.ID, expectedID)
		}
		if seg.DurationMs <= 0 {
			t.Errorf("segment %d duration_ms = %d, want positive", seg.ID, seg.DurationMs)
		}
		if seg.EndMs <= seg.StartMs {
			t.Errorf("segment %d end_ms (%d) <= start_ms (%d)", seg.ID, seg.EndMs, seg.StartMs)
		}
		if seg.Text == "" {
			t.Errorf("segment %d text is empty", seg.ID)
		}
		if seg.Speaker == "" {
			t.Errorf("segment %d speaker is empty", seg.ID)
		}
	}
}

// TestTakeDurations checks all ten WAV takes against golden metrics.
// The test permits a five millisecond tolerance.
func TestTakeDurations(t *testing.T) {
	metricsPath := resolvePath(t, filepath.Join("expected", "metrics.json"))
	data, err := os.ReadFile(metricsPath)
	if err != nil {
		t.Fatalf("read metrics.json: %v", err)
	}

	var metrics []ExpectedMetric
	if err := json.Unmarshal(data, &metrics); err != nil {
		t.Fatalf("unmarshal metrics.json: %v", err)
	}

	expectedTakes := make(map[string]int64)
	for _, m := range metrics {
		for file, dur := range m.Takes {
			expectedTakes[file] = dur
		}
	}

	const wantTakes = 10
	if len(expectedTakes) != wantTakes {
		t.Fatalf("collected %d expected takes, want %d", len(expectedTakes), wantTakes)
	}

	takesDir := resolvePath(t, "takes")
	entries, err := os.ReadDir(takesDir)
	if err != nil {
		t.Fatalf("read takes dir: %v", err)
	}

	var measuredCount int
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".wav" {
			continue
		}
		name := entry.Name()
		wantMs, ok := expectedTakes[name]
		if !ok {
			t.Errorf("unexpected take file in takes dir: %s", name)
			continue
		}

		filePath := filepath.Join(takesDir, name)
		dur, err := media.Duration(filePath)
		if err != nil {
			t.Errorf("Duration(%s) error: %v", filePath, err)
			continue
		}

		gotMs := dur.Milliseconds()
		diff := int64(math.Abs(float64(gotMs - wantMs)))
		const maxToleranceMs = 5
		if diff > maxToleranceMs {
			t.Errorf("take %s duration = %d ms, want %d ms (diff %d ms > tolerance %d ms)",
				name, gotMs, wantMs, diff, maxToleranceMs)
		}
		measuredCount++
	}

	if measuredCount != wantTakes {
		t.Errorf("measured %d take files, want %d", measuredCount, wantTakes)
	}
}

// TestClipDuration verifies clip.mp4 duration within ten milliseconds.
func TestClipDuration(t *testing.T) {
	clipPath := resolvePath(t, "clip.mp4")
	dur, err := media.Duration(clipPath)
	if err != nil {
		t.Fatalf("Duration(%s) error: %v", clipPath, err)
	}

	gotMs := dur.Milliseconds()
	const wantMs = int64(75008)
	diff := int64(math.Abs(float64(gotMs - wantMs)))
	const maxToleranceMs = 10
	if diff > maxToleranceMs {
		t.Errorf("clip.mp4 duration = %d ms, want %d ms (diff %d ms > tolerance %d ms)",
			gotMs, wantMs, diff, maxToleranceMs)
	}
}
