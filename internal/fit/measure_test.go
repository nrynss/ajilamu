package fit

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/nrynss/ajilamu/internal/media"
	"github.com/nrynss/ajilamu/internal/types"
)

type goldenTake struct {
	seg     int
	file    string
	slotMs  int64
	takeMs  int64
	deltaMs int64
}

var try1Goldens = []goldenTake{
	{1, "seg_1_try1.wav", 1820, 1680, -140},
	{2, "seg_2_try1.wav", 5480, 5320, -160},
	{3, "seg_3_try1.wav", 5320, 5720, 400},
	{4, "seg_4_try1.wav", 5660, 5920, 260},
	{5, "seg_5_try1.wav", 4840, 4720, -120},
	{6, "seg_6_try1.wav", 4630, 4440, -190},
	{7, "seg_7_try1.wav", 7040, 6800, -240},
	{8, "seg_8_try1.wav", 7110, 4200, -2910},
}

func takesPath(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join("..", "..", "testdata", "takes", name)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("fixture %s: %v", path, err)
	}
	return path
}

func TestMeasureTry1Takes(t *testing.T) {
	for _, tc := range try1Goldens {
		t.Run(tc.file, func(t *testing.T) {
			path := takesPath(t, tc.file)
			slot := time.Duration(tc.slotMs) * time.Millisecond
			got, err := Measure(t.Context(), path, slot)
			if err != nil {
				t.Fatalf("Measure(%s): %v", path, err)
			}

			probed, err := media.Duration(t.Context(), path)
			if err != nil {
				t.Fatalf("media.Duration(%s): %v", path, err)
			}
			if got.Measured != probed {
				t.Errorf("Measured %v disagrees with media.Duration %v", got.Measured, probed)
			}

			wantTake := time.Duration(tc.takeMs) * time.Millisecond
			if probed != wantTake {
				t.Errorf("media.Duration(%s) = %v, golden take_ms is %d", path, probed, tc.takeMs)
			}

			want := types.NewFit(slot, wantTake)
			if got.Slot != want.Slot {
				t.Errorf("Slot = %v, want %v", got.Slot, want.Slot)
			}
			if got.Measured != want.Measured {
				t.Errorf("Measured = %v, want %v", got.Measured, want.Measured)
			}
			if got.Delta != want.Delta {
				t.Errorf("Delta = %v, want %v", got.Delta, want.Delta)
			}
			if got.Delta != time.Duration(tc.deltaMs)*time.Millisecond {
				t.Errorf("Delta = %v, want %d ms", got.Delta, tc.deltaMs)
			}
			if got.Fits() != want.Fits() {
				t.Errorf("Fits() = %v, want %v", got.Fits(), want.Fits())
			}
			if got.TooLong() != want.TooLong() {
				t.Errorf("TooLong() = %v, want %v", got.TooLong(), want.TooLong())
			}
			if got.TooShort() != want.TooShort() {
				t.Errorf("TooShort() = %v, want %v", got.TooShort(), want.TooShort())
			}

			t.Logf("seg %d slot_ms=%d take_ms=%d delta_ms=%d fits=%v too_short=%v too_long=%v",
				tc.seg, got.Slot.Milliseconds(), got.Measured.Milliseconds(),
				got.Delta.Milliseconds(), got.Fits(), got.TooShort(), got.TooLong())
		})
	}
}

func TestMeasureSegment8Underrun(t *testing.T) {
	path := takesPath(t, "seg_8_try1.wav")
	slot := 7110 * time.Millisecond
	got, err := Measure(t.Context(), path, slot)
	if err != nil {
		t.Fatalf("Measure(%s): %v", path, err)
	}

	if got.Slot != slot {
		t.Errorf("Slot = %v, want %v", got.Slot, slot)
	}
	if got.Measured != 4200*time.Millisecond {
		t.Errorf("Measured = %v, want 4200ms", got.Measured)
	}
	if got.Delta != -2910*time.Millisecond {
		t.Errorf("Delta = %v, want -2910ms", got.Delta)
	}
	if !got.TooShort() {
		t.Errorf("TooShort() = false, want true")
	}
	if got.Fits() {
		t.Errorf("Fits() = true, want false")
	}
	if got.TooLong() {
		t.Errorf("TooLong() = true, want false")
	}

	t.Logf("seg 8 Slot=%v Measured=%v Delta=%v TooShort=%v Fits=%v",
		got.Slot, got.Measured, got.Delta, got.TooShort(), got.Fits())
}

func TestMeasureStretchedTakes(t *testing.T) {
	cases := []struct {
		file   string
		slotMs int64
		takeMs int64
	}{
		{"seg_3_stretched.wav", 5320, 5338},
		{"seg_4_stretched.wav", 5660, 5662},
	}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			path := takesPath(t, tc.file)
			slot := time.Duration(tc.slotMs) * time.Millisecond
			got, err := Measure(t.Context(), path, slot)
			if err != nil {
				t.Fatalf("Measure(%s): %v", path, err)
			}
			probed, err := media.Duration(t.Context(), path)
			if err != nil {
				t.Fatalf("media.Duration(%s): %v", path, err)
			}
			if got.Measured != probed {
				t.Errorf("Measured %v disagrees with media.Duration %v", got.Measured, probed)
			}
			wantTake := time.Duration(tc.takeMs) * time.Millisecond
			if probed != wantTake {
				t.Errorf("media.Duration(%s) = %v, golden is %v", path, probed, wantTake)
			}
			if !got.Fits() {
				t.Errorf("Fits() = false for stretched take, delta %v", got.Delta)
			}
			t.Logf("%s measured_ms=%d slot_ms=%d delta_ms=%d",
				tc.file, got.Measured.Milliseconds(), got.Slot.Milliseconds(), got.Delta.Milliseconds())
		})
	}
}

func TestMeasureProbesRenderedWAV(t *testing.T) {
	out := filepath.Join(t.TempDir(), "tone.wav")
	cmd := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i",
		"aevalsrc=sin(2*PI*440*t):s=16000:d=0.250", "-c:a", "pcm_s16le", out)
	if err := cmd.Run(); err != nil {
		t.Fatalf("ffmpeg synth: %v", err)
	}

	slot := 200 * time.Millisecond
	got, err := Measure(t.Context(), out, slot)
	if err != nil {
		t.Fatalf("Measure(%s): %v", out, err)
	}
	probed, err := media.Duration(t.Context(), out)
	if err != nil {
		t.Fatalf("media.Duration(%s): %v", out, err)
	}
	if got.Measured != probed {
		t.Fatalf("Measured %v disagrees with media.Duration %v", got.Measured, probed)
	}
	if got.Measured != 250*time.Millisecond {
		t.Errorf("Measured = %v, want 250ms", got.Measured)
	}
	want := types.NewFit(slot, probed)
	if got != want {
		t.Errorf("Fit = %+v, want %+v", got, want)
	}
}

func TestMeasureRejectsInvalidPath(t *testing.T) {
	slot := time.Second

	got, err := Measure(t.Context(), "", slot)
	if err == nil {
		t.Fatal("empty path must fail")
	}
	if got != (types.Fit{}) {
		t.Errorf("empty path Fit = %+v, want zero", got)
	}

	missing := filepath.Join(t.TempDir(), "missing.wav")
	got, err = Measure(t.Context(), missing, slot)
	if err == nil {
		t.Fatal("missing take must fail")
	}
	if got != (types.Fit{}) {
		t.Errorf("missing take Fit = %+v, want zero", got)
	}
}
