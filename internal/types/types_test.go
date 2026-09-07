package types

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type metricRow struct {
	SegmentID int   `json:"segment_id"`
	SlotMs    int64 `json:"slot_ms"`
	TakeMs    int64 `json:"take_ms"`
}

func TestFitMetrics(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "expected", "metrics.json"))
	if err != nil {
		t.Fatalf("Failed to read metrics.json: %v", err)
	}

	var rows []metricRow
	if err := json.Unmarshal(b, &rows); err != nil {
		t.Fatalf("Failed to unmarshal metrics.json: %v", err)
	}

	expected := map[int]struct {
		deltaMs   time.Duration
		fits      bool
		tooLong   bool
		tooShort  bool
	}{
		1: {-140 * time.Millisecond, true, false, false},
		2: {-160 * time.Millisecond, true, false, false},
		3: {400 * time.Millisecond, true, false, false},
		4: {260 * time.Millisecond, true, false, false},
		5: {-120 * time.Millisecond, true, false, false},
		6: {-190 * time.Millisecond, true, false, false},
		7: {-240 * time.Millisecond, true, false, false},
		8: {-2910 * time.Millisecond, false, false, true},
	}

	for _, row := range rows {
		exp, ok := expected[row.SegmentID]
		if !ok {
			t.Errorf("Unexpected segment %d", row.SegmentID)
			continue
		}

		slot := time.Duration(row.SlotMs) * time.Millisecond
		take := time.Duration(row.TakeMs) * time.Millisecond
		fit := NewFit(slot, take)

		if fit.Delta != exp.deltaMs {
			t.Errorf("Seg %d: got delta %v, want %v", row.SegmentID, fit.Delta, exp.deltaMs)
		}
		if fit.Fits() != exp.fits {
			t.Errorf("Seg %d: got Fits() %v, want %v", row.SegmentID, fit.Fits(), exp.fits)
		}
		if fit.TooLong() != exp.tooLong {
			t.Errorf("Seg %d: got TooLong() %v, want %v", row.SegmentID, fit.TooLong(), exp.tooLong)
		}
		if fit.TooShort() != exp.tooShort {
			t.Errorf("Seg %d: got TooShort() %v, want %v", row.SegmentID, fit.TooShort(), exp.tooShort)
		}

		expectedRatio := float64(row.TakeMs) / float64(row.SlotMs)
		if math.Abs(fit.Ratio()-expectedRatio) > 1e-6 {
			t.Errorf("Seg %d: got Ratio() %v, want %v", row.SegmentID, fit.Ratio(), expectedRatio)
		}

		if row.SegmentID == 8 {
			t.Run("Segment 8", func(t *testing.T) {
				if !fit.TooShort() {
					t.Errorf("Expected TooShort() == true")
				}
				if fit.Delta != -2910*time.Millisecond {
					t.Errorf("Expected Delta == -2910ms, got %v", fit.Delta)
				}
			})
		}
	}
}

func TestEdgeCases(t *testing.T) {
	// Zero slot duration
	f1 := NewFit(0, 0)
	if !f1.Fits() || f1.TooLong() || f1.TooShort() {
		t.Errorf("Zero slot, zero measured should fit")
	}

	f2 := NewFit(0, 100*time.Millisecond)
	if f2.Fits() || !f2.TooLong() || f2.TooShort() {
		t.Errorf("Zero slot, non-zero measured should be too long")
	}

	// Exact fit
	f3 := NewFit(100*time.Millisecond, 100*time.Millisecond)
	if !f3.Fits() || f3.TooLong() || f3.TooShort() {
		t.Errorf("Exact fit failed")
	}

	// Exactly at 8% boundary (overrun)
	f4 := NewFit(100*time.Millisecond, 108*time.Millisecond)
	if !f4.Fits() || f4.TooLong() {
		t.Errorf("Exactly 8%% overrun should fit")
	}
	f4b := NewFit(100*time.Millisecond, 109*time.Millisecond)
	if f4b.Fits() || !f4b.TooLong() {
		t.Errorf("More than 8%% overrun should be too long")
	}

	// Exactly at 8% boundary (underrun)
	f5 := NewFit(100*time.Millisecond, 92*time.Millisecond)
	if !f5.Fits() || f5.TooShort() {
		t.Errorf("Exactly 8%% underrun should fit")
	}
	f5b := NewFit(100*time.Millisecond, 91*time.Millisecond)
	if f5b.Fits() || !f5b.TooShort() {
		t.Errorf("More than 8%% underrun should be too short")
	}
}

func TestRepairString(t *testing.T) {
	tests := []struct {
		r    Repair
		want string
	}{
		{RepairNone, "none"},
		{RepairAtempo, "atempo"},
		{RepairRewrite, "rewrite"},
		{RepairManual, "manual"},
		{Repair(99), "unknown"},
	}
	for _, tc := range tests {
		if got := tc.r.String(); got != tc.want {
			t.Errorf("Repair(%d).String() = %v, want %v", tc.r, got, tc.want)
		}
	}
}

func TestSegmentSlotDuration(t *testing.T) {
	seg := Segment{
		StartMs: 1000,
		EndMs:   2500,
	}
	want := 1500 * time.Millisecond
	if got := seg.SlotDuration(); got != want {
		t.Errorf("Segment.SlotDuration() = %v, want %v", got, want)
	}
}
