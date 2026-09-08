package fit

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nrynss/ajilamu/internal/media"
	"github.com/nrynss/ajilamu/internal/types"
)

// goldenTolerance bounds how far a rebuilt stretch may sit from the value the
// 2026-09-07 run recorded. That run rounded its atempo ratio to three decimals
// and media.Atempo rounds to four, so the two runs cannot agree exactly.
const goldenTolerance = 10 * time.Millisecond

// landingDrift bounds how far a rebuilt stretch may sit from the landing that
// ffmpeg and ffprobe produced outside Go on this frozen stack, ffmpeg n9.0.1.
// It is deliberately far tighter than any dead band. It exists so a widened
// band cannot widen this assertion with it.
const landingDrift = 2 * time.Millisecond

// maxLandingMiss is an independent ceiling on how far a correctly planned
// stretch may land from its slot. It reads no constant this package can move.
//
// Measured with ffprobe outside Go on the six repairable fixture segments, the
// misses are -11, +13, +3, -8, -9 and -9 ms. The worst is 13 ms. This ceiling
// sits at 20 ms, which clears the worst measured value and still fails any
// build that lands tens of milliseconds away.
const maxLandingMiss = 20 * time.Millisecond

type stretchCase struct {
	seg     int
	file    string
	slotMs  int64
	takeMs  int64
	deltaMs int64
	// bandMs is the dead band this slot carries on this take's own side,
	// written as a literal rather than read back from deadBand.
	bandMs int64
	repair types.Repair
	ratio  float64
	// landedMs is where ffmpeg put this take, measured with ffprobe outside Go
	// on 2026-09-07. It is 0 for a segment the repair band refuses.
	landedMs int64
}

// stretchGoldens carries every fixture segment with the repair this package
// plans under the default budget of 5 percent short and 8 percent long.
//
// Segments 1 and 8 route to a rewrite. Segment 8 runs 40.9 percent short.
// Segment 1 runs 7.7 percent short, which the long side would repair and the
// tighter short side refuses.
var stretchGoldens = []stretchCase{
	{1, "seg_1_try1.wav", 1820, 1680, -140, 40, types.RepairRewrite, 0, 0},
	{2, "seg_2_try1.wav", 5480, 5320, -160, 40, types.RepairAtempo, 5320.0 / 5480.0, 5469},
	{3, "seg_3_try1.wav", 5320, 5720, 400, 40, types.RepairAtempo, 5720.0 / 5320.0, 5333},
	{4, "seg_4_try1.wav", 5660, 5920, 260, 40, types.RepairAtempo, 5920.0 / 5660.0, 5663},
	{5, "seg_5_try1.wav", 4840, 4720, -120, 40, types.RepairAtempo, 4720.0 / 4840.0, 4832},
	{6, "seg_6_try1.wav", 4630, 4440, -190, 40, types.RepairAtempo, 4440.0 / 4630.0, 4621},
	{7, "seg_7_try1.wav", 7040, 6800, -240, 40, types.RepairAtempo, 6800.0 / 7040.0, 7031},
	{8, "seg_8_try1.wav", 7110, 4200, -2910, 40, types.RepairRewrite, 0, 0},
}

// copyFixtureInto copies a repository fixture into dir.
// Copying keeps the source and the working file on one filesystem, which a hard
// link cannot promise when t.TempDir lands on another device.
func copyFixtureInto(t *testing.T, dir, name string) string {
	t.Helper()
	data, err := os.ReadFile(takesPath(t, name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	dst := filepath.Join(dir, name)
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		t.Fatalf("copy fixture %s: %v", name, err)
	}
	return dst
}

// probeMs measures a file with ffprobe through internal/media.
// Every assertion about a stretched take reads this value, never a return value
// the function under test produced about its own work.
func probeMs(t *testing.T, path string) int64 {
	t.Helper()
	d, err := media.Duration(path)
	if err != nil {
		t.Fatalf("media.Duration(%s): %v", path, err)
	}
	return d.Milliseconds()
}

// mustNotExist fails when path holds a file.
func mustNotExist(t *testing.T, path, why string) {
	t.Helper()
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("%s left %s on disk, stat error %v", why, path, err)
	}
}

func TestPlanStretchCoversEveryFixtureSegment(t *testing.T) {
	for _, tc := range stretchGoldens {
		t.Run(tc.file, func(t *testing.T) {
			path := takesPath(t, tc.file)
			slot := time.Duration(tc.slotMs) * time.Millisecond

			measured := probeMs(t, path)
			if measured != tc.takeMs {
				t.Fatalf("ffprobe %s = %d ms, golden take_ms is %d", tc.file, measured, tc.takeMs)
			}

			f, err := Measure(path, slot)
			if err != nil {
				t.Fatalf("Measure(%s): %v", path, err)
			}
			if f.Delta != time.Duration(tc.deltaMs)*time.Millisecond {
				t.Fatalf("Delta = %v, want %d ms", f.Delta, tc.deltaMs)
			}

			plan := PlanStretch(f)
			if plan.Repair != tc.repair {
				t.Errorf("Repair = %s, want %s", plan.Repair, tc.repair)
			}
			// The expected band is a literal, not a call to the function that
			// produced it. Widening the band must fail this line.
			if plan.Band.Milliseconds() != tc.bandMs {
				t.Errorf("Band = %v, want %d ms", plan.Band, tc.bandMs)
			}
			wantLimit := DefaultMaxStretchShort
			if tc.deltaMs > 0 {
				wantLimit = DefaultMaxStretchLong
			}
			if plan.Limit != wantLimit {
				t.Errorf("Limit = %g, want %g", plan.Limit, wantLimit)
			}
			if tc.repair == types.RepairAtempo {
				if math.Abs(plan.Ratio-tc.ratio) > 1e-9 {
					t.Errorf("Ratio = %.9f, want %.9f", plan.Ratio, tc.ratio)
				}
				if plan.Ratio < MinAtempoRatio || plan.Ratio > MaxAtempoRatio {
					t.Errorf("Ratio %.6f outside the atempo range", plan.Ratio)
				}
			}

			relPct := float64(f.Delta) / float64(f.Slot) * 100
			t.Logf("seg %d slot_ms=%d take_ms=%d delta_ms=%d delta_pct=%.3f band_ms=%d limit=%.2f repair=%s ratio=%.6f",
				tc.seg, tc.slotMs, measured, f.Delta.Milliseconds(), relPct,
				plan.Band.Milliseconds(), plan.Limit, plan.Repair, plan.Ratio)
		})
	}
}

func TestStretchLandsEveryRepairableFixtureInsideItsSlot(t *testing.T) {
	for _, tc := range stretchGoldens {
		if tc.repair != types.RepairAtempo {
			continue
		}
		t.Run(tc.file, func(t *testing.T) {
			dir := t.TempDir()
			in := copyFixtureInto(t, dir, tc.file)
			out := filepath.Join(dir, "stretched.wav")
			slot := time.Duration(tc.slotMs) * time.Millisecond

			res, err := Stretch(in, out, slot)
			if err != nil {
				t.Fatalf("Stretch(seg %d): %v", tc.seg, err)
			}
			if math.Abs(res.Ratio-tc.ratio) > 1e-9 {
				t.Errorf("Ratio = %.9f, want %.9f", res.Ratio, tc.ratio)
			}

			// Re-probe the artifact rather than trusting res.After.
			landed := probeMs(t, out)
			if landed != res.After.Measured.Milliseconds() {
				t.Errorf("res.After.Measured = %d ms, ffprobe says %d ms",
					res.After.Measured.Milliseconds(), landed)
			}

			// Two independent expectations. Neither one asks the band what the
			// band allows.
			drift := landed - tc.landedMs
			if drift > landingDrift.Milliseconds() || drift < -landingDrift.Milliseconds() {
				t.Errorf("landed %d ms, ffprobe measured %d ms outside Go, drift %d ms beyond %v",
					landed, tc.landedMs, drift, landingDrift)
			}
			miss := landed - tc.slotMs
			if miss > maxLandingMiss.Milliseconds() || miss < -maxLandingMiss.Milliseconds() {
				t.Errorf("landed %d ms against slot %d ms, miss %d ms beyond the measured ceiling %v",
					landed, tc.slotMs, miss, maxLandingMiss)
			}

			// A stretched take must now report a fit and need no further repair.
			after, err := Measure(out, slot)
			if err != nil {
				t.Fatalf("Measure(%s): %v", out, err)
			}
			if !after.Fits() {
				t.Errorf("stretched take does not fit, delta %v", after.Delta)
			}
			if next := PlanStretch(after); next.Repair != types.RepairNone {
				t.Errorf("re-planning the stretched take gives %s, want none", next.Repair)
			}

			t.Logf("seg %d ratio=%.6f before_ms=%d after_ms=%d slot_ms=%d miss_ms=%d pinned_ms=%d",
				tc.seg, res.Ratio, res.Before.Measured.Milliseconds(), landed, tc.slotMs, miss, tc.landedMs)
		})
	}
}

func TestStretchReproducesGoldenStretchedDurations(t *testing.T) {
	cases := []struct {
		seg      int
		file     string
		slotMs   int64
		goldenMs int64
	}{
		{3, "seg_3_try1.wav", 5320, 5338},
		{4, "seg_4_try1.wav", 5660, 5662},
	}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			dir := t.TempDir()
			in := copyFixtureInto(t, dir, tc.file)
			out := filepath.Join(dir, "stretched.wav")

			res, err := Stretch(in, out, time.Duration(tc.slotMs)*time.Millisecond)
			if err != nil {
				t.Fatalf("Stretch(seg %d): %v", tc.seg, err)
			}

			got := probeMs(t, out)
			drift := got - tc.goldenMs
			if drift > goldenTolerance.Milliseconds() || drift < -goldenTolerance.Milliseconds() {
				t.Errorf("seg %d landed %d ms, golden %d ms, drift %d ms beyond %v",
					tc.seg, got, tc.goldenMs, drift, goldenTolerance)
			}

			// The committed stretched fixture must agree with what we just built.
			fixture := probeMs(t, takesPath(t, stretchedFixtureName(tc.seg)))
			if fixture != tc.goldenMs {
				t.Errorf("committed fixture measures %d ms, metrics.json records %d ms",
					fixture, tc.goldenMs)
			}

			t.Logf("seg %d ratio=%.6f rebuilt_ms=%d golden_ms=%d drift_ms=%d",
				tc.seg, res.Ratio, got, tc.goldenMs, drift)
		})
	}
}

// stretchedFixtureName names the stretched take the 2026-09-07 run committed.
func stretchedFixtureName(seg int) string {
	switch seg {
	case 3:
		return "seg_3_stretched.wav"
	case 4:
		return "seg_4_stretched.wav"
	}
	return ""
}

func TestStretchRefusesSegment8(t *testing.T) {
	dir := t.TempDir()
	in := copyFixtureInto(t, dir, "seg_8_try1.wav")
	out := filepath.Join(dir, "stretched.wav")
	slot := 7110 * time.Millisecond

	res, err := Stretch(in, out, slot)
	if !errors.Is(err, ErrUnderrunTooLarge) {
		t.Fatalf("Stretch(seg 8) error = %v, want ErrUnderrunTooLarge", err)
	}
	mustNotExist(t, out, "the refused segment 8")
	if res.Before.Delta != -2910*time.Millisecond {
		t.Errorf("Before.Delta = %v, want -2910ms", res.Before.Delta)
	}
	if res.Ratio != 0 {
		t.Errorf("Ratio = %v, want 0 for a refused take", res.Ratio)
	}

	// The atempo range alone does not protect segment 8.
	// Its corrective ratio is legal for ffmpeg. The repair band is what refuses it.
	naive := res.Before.Ratio()
	if naive < MinAtempoRatio || naive > MaxAtempoRatio {
		t.Fatalf("seg 8 ratio %.4f is outside the atempo range, so this test proves nothing", naive)
	}
	if err := checkRatio(naive); err != nil {
		t.Fatalf("checkRatio(%.4f) = %v, want nil", naive, err)
	}

	// The widest budget this package accepts still refuses segment 8.
	// No caller can talk the loop into stretching a 40 percent underrun.
	wide := StretchLimits{Short: MaxStretchLimit, Long: MaxStretchLimit}
	if _, err := StretchWithLimits(in, out, slot, wide); !errors.Is(err, ErrUnderrunTooLarge) {
		t.Errorf("StretchWithLimits(seg 8, widest budget) error = %v, want ErrUnderrunTooLarge", err)
	}
	mustNotExist(t, out, "segment 8 under the widest budget")

	t.Logf("seg 8 delta_ms=%d ratio=%.4f accepted_by_range=true refused_by_band=true",
		res.Before.Delta.Milliseconds(), naive)
}

// TestSegment1MovesToRewriteUnderTheDefaultShortBudget pins the one routing
// change the 5 percent short budget causes across the eight fixtures.
//
// Segment 1 runs 7.7 percent short. The 8 percent long budget would repair a
// miss that size. The tighter short budget sends it to a fuller rewrite.
func TestSegment1MovesToRewriteUnderTheDefaultShortBudget(t *testing.T) {
	dir := t.TempDir()
	in := copyFixtureInto(t, dir, "seg_1_try1.wav")
	out := filepath.Join(dir, "stretched.wav")
	slot := 1820 * time.Millisecond

	measured := probeMs(t, in)
	shortPct := float64(measured-slot.Milliseconds()) / float64(slot.Milliseconds()) * 100
	if shortPct > -DefaultMaxStretchShort*100 || shortPct < -DefaultMaxStretchLong*100 {
		t.Fatalf("seg 1 runs %.3f percent short, which no longer sits between the two budgets", shortPct)
	}

	res, err := Stretch(in, out, slot)
	if !errors.Is(err, ErrUnderrunTooLarge) {
		t.Fatalf("Stretch(seg 1) error = %v, want ErrUnderrunTooLarge", err)
	}
	mustNotExist(t, out, "the refused segment 1")
	if res.Ratio != 0 {
		t.Errorf("Ratio = %v, want 0 for a refused take", res.Ratio)
	}

	// Only the budget moved this take. A caller that resolves the old 8 percent
	// short budget still gets an atempo repair from the same code.
	loose := StretchLimits{Short: 0.08, Long: DefaultMaxStretchLong}
	if plan := PlanStretchWithLimits(res.Before, loose); plan.Repair != types.RepairAtempo {
		t.Errorf("under an 8 percent short budget seg 1 plans %s, want atempo", plan.Repair)
	}
	looseOut := filepath.Join(dir, "loose.wav")
	looseRes, err := StretchWithLimits(in, looseOut, slot, loose)
	if err != nil {
		t.Fatalf("StretchWithLimits(seg 1, 8 percent short): %v", err)
	}
	landed := probeMs(t, looseOut)
	if landed != looseRes.After.Measured.Milliseconds() {
		t.Errorf("res.After.Measured = %d ms, ffprobe says %d ms",
			looseRes.After.Measured.Milliseconds(), landed)
	}

	t.Logf("seg 1 short_pct=%.3f default_short=%.2f routes=rewrite; at short=0.08 routes=atempo ratio=%.4f landed_ms=%d",
		shortPct, DefaultMaxStretchShort, looseRes.Ratio, landed)
}

func TestStretchRatioRejectsRatiosOutsideTheAtempoRange(t *testing.T) {
	cases := []struct {
		name  string
		ratio float64
	}{
		{"below range", 0.49},
		{"far below range", 0.1},
		{"above range", 2.01},
		{"far above range", 4.0},
		{"not a number", math.NaN()},
		{"positive infinity", math.Inf(1)},
		{"negative infinity", math.Inf(-1)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			in := copyFixtureInto(t, dir, "seg_1_try1.wav")
			out := filepath.Join(dir, "stretched.wav")

			_, err := stretchRatio(in, out, tc.ratio, 1820*time.Millisecond, DefaultStretchLimits())
			if !errors.Is(err, ErrRatioRange) {
				t.Fatalf("stretchRatio(ratio %v) error = %v, want ErrRatioRange", tc.ratio, err)
			}
			mustNotExist(t, out, "the rejected ratio")
			t.Logf("ratio %v rejected: %v", tc.ratio, err)
		})
	}

	// Both bounds themselves stay legal.
	for _, ratio := range []float64{MinAtempoRatio, MaxAtempoRatio} {
		if err := checkRatio(ratio); err != nil {
			t.Errorf("checkRatio(%g) = %v, want nil", ratio, err)
		}
	}
}

func TestStretchRemovesATakeThatLandedOutsideItsSlot(t *testing.T) {
	// Segment 1 measures 1680 ms against an 1820 ms slot. A ratio of 1.5 speeds
	// it up instead of slowing it down, so the output lands far short of the slot.
	// ffmpeg reports success. Only the re-measure step catches the miss.
	dir := t.TempDir()
	in := copyFixtureInto(t, dir, "seg_1_try1.wav")
	out := filepath.Join(dir, "missed.wav")
	slot := 1820 * time.Millisecond

	res, err := stretchRatio(in, out, 1.5, slot, DefaultStretchLimits())
	if !errors.Is(err, ErrLandedOutside) {
		t.Fatalf("stretchRatio(1.5) error = %v, want ErrLandedOutside", err)
	}
	if res.After.Measured >= slot {
		t.Fatalf("ratio 1.5 produced %v, which does not miss the slot", res.After.Measured)
	}

	// A stretch that missed leaves no take behind. A surviving file would carry
	// a take name, a wrong length, and a path that ErrOutputExists then blocks
	// forever.
	mustNotExist(t, out, "the missed stretch")

	// The path is free again, which proves the loop can retry the same name.
	if err := checkPaths(in, out); err != nil {
		t.Errorf("checkPaths after a missed stretch = %v, want nil", err)
	}
	t.Logf("ratio=1.500 after_ms=%d slot_ms=%d band_ms=%d removed=true err=%v",
		res.After.Measured.Milliseconds(), slot.Milliseconds(), res.Band.Milliseconds(), err)

	// A near miss just outside the band fails the same way, which pins the
	// boundary rather than only the obvious case.
	near := filepath.Join(dir, "near.wav")
	nearRes, nearErr := stretchRatio(in, near, 0.85, slot, DefaultStretchLimits())
	if !errors.Is(nearErr, ErrLandedOutside) {
		t.Fatalf("stretchRatio(0.85) error = %v, want ErrLandedOutside", nearErr)
	}
	mustNotExist(t, near, "the near missed stretch")
	t.Logf("ratio=0.850 after_ms=%d slot_ms=%d removed=true err=%v",
		nearRes.After.Measured.Milliseconds(), slot.Milliseconds(), nearErr)
}

func TestStretchSkipsATakeInsideTheDeadBand(t *testing.T) {
	// Segment 5 measures 4720 ms. Against a 4700 ms slot it runs 20 ms long,
	// which is inside the 40 ms dead band.
	dir := t.TempDir()
	in := copyFixtureInto(t, dir, "seg_5_try1.wav")
	out := filepath.Join(dir, "stretched.wav")
	slot := 4700 * time.Millisecond

	res, err := Stretch(in, out, slot)
	if !errors.Is(err, ErrDeadBand) {
		t.Fatalf("Stretch inside the dead band error = %v, want ErrDeadBand", err)
	}
	mustNotExist(t, out, "the dead band take")
	if res.Before.Delta != 20*time.Millisecond {
		t.Errorf("Before.Delta = %v, want 20ms", res.Before.Delta)
	}
	if res.Band != StretchDeadBand {
		t.Errorf("Band = %v, want %v", res.Band, StretchDeadBand)
	}

	// One millisecond outside the band the same take gets stretched.
	edgeOut := filepath.Join(dir, "edge.wav")
	edgeSlot := 4679 * time.Millisecond
	edge, edgeErr := Stretch(in, edgeOut, edgeSlot)
	if edgeErr != nil {
		t.Fatalf("Stretch just outside the band: %v", edgeErr)
	}
	if edge.Before.Delta != 41*time.Millisecond {
		t.Errorf("edge Before.Delta = %v, want 41ms", edge.Before.Delta)
	}

	// The short side of the same band. The take runs 30 ms short of a 4750 ms
	// slot, which the dead band also absorbs.
	shortOut := filepath.Join(dir, "short.wav")
	shortRes, shortErr := Stretch(in, shortOut, 4750*time.Millisecond)
	if !errors.Is(shortErr, ErrDeadBand) {
		t.Fatalf("Stretch inside the short dead band error = %v, want ErrDeadBand", shortErr)
	}
	mustNotExist(t, shortOut, "the short side dead band take")
	if shortRes.Before.Delta != -30*time.Millisecond {
		t.Errorf("short Before.Delta = %v, want -30ms", shortRes.Before.Delta)
	}

	t.Logf("dead band long slot_ms=4700 delta_ms=20 skipped; short slot_ms=4750 delta_ms=-30 skipped; edge slot_ms=4679 delta_ms=41 stretched to %d ms",
		probeMs(t, edgeOut))
}

func TestPlanStretchHonoursDirectionalThresholds(t *testing.T) {
	slot := 5000 * time.Millisecond
	cases := []struct {
		name     string
		measured time.Duration
		want     types.Repair
	}{
		{"exactly the long limit", 5400 * time.Millisecond, types.RepairAtempo},
		{"one ms past the long limit", 5401 * time.Millisecond, types.RepairRewrite},
		{"exactly the short limit", 4750 * time.Millisecond, types.RepairAtempo},
		{"one ms past the short limit", 4749 * time.Millisecond, types.RepairRewrite},
		{"inside the dead band long", 5040 * time.Millisecond, types.RepairNone},
		{"inside the dead band short", 4960 * time.Millisecond, types.RepairNone},
		{"one ms outside the dead band long", 5041 * time.Millisecond, types.RepairAtempo},
		{"one ms outside the dead band short", 4959 * time.Millisecond, types.RepairAtempo},
		{"between the two budgets, short", 4680 * time.Millisecond, types.RepairRewrite},
		{"the same distance long", 5320 * time.Millisecond, types.RepairAtempo},
		{"silent take", 0, types.RepairRewrite},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan := PlanStretch(types.NewFit(slot, tc.measured))
			if plan.Repair != tc.want {
				t.Errorf("Repair = %s, want %s", plan.Repair, tc.want)
			}
		})
	}

	// A slot with no duration is an operator problem, not an atempo problem.
	if plan := PlanStretch(types.NewFit(0, time.Second)); plan.Repair != types.RepairManual {
		t.Errorf("zero slot Repair = %s, want manual", plan.Repair)
	}
	if plan := PlanStretch(types.NewFit(-time.Second, time.Second)); plan.Repair != types.RepairManual {
		t.Errorf("negative slot Repair = %s, want manual", plan.Repair)
	}
}

func TestDeadBandNeverExceedsTheRepairBand(t *testing.T) {
	// Every expected value is a literal. None of them calls deadBand.
	cases := []struct {
		slot        time.Duration
		wantShortMs int64
		wantLongMs  int64
	}{
		{100 * time.Millisecond, 5, 8},
		{300 * time.Millisecond, 15, 24},
		{500 * time.Millisecond, 25, 40},
		{800 * time.Millisecond, 40, 40},
		{1820 * time.Millisecond, 40, 40},
		{7110 * time.Millisecond, 40, 40},
	}
	lim := DefaultStretchLimits()
	for _, tc := range cases {
		gotShort := deadBand(tc.slot, -time.Millisecond, lim)
		if gotShort.Milliseconds() != tc.wantShortMs {
			t.Errorf("deadBand(%v, short) = %v, want %d ms", tc.slot, gotShort, tc.wantShortMs)
		}
		gotLong := deadBand(tc.slot, time.Millisecond, lim)
		if gotLong.Milliseconds() != tc.wantLongMs {
			t.Errorf("deadBand(%v, long) = %v, want %d ms", tc.slot, gotLong, tc.wantLongMs)
		}
		if shortLimit := time.Duration(float64(tc.slot) * lim.Short); gotShort > shortLimit {
			t.Errorf("deadBand(%v, short) = %v exceeds its own budget %v", tc.slot, gotShort, shortLimit)
		}
		if longLimit := time.Duration(float64(tc.slot) * lim.Long); gotLong > longLimit {
			t.Errorf("deadBand(%v, long) = %v exceeds its own budget %v", tc.slot, gotLong, longLimit)
		}
	}
	if deadBand(0, -time.Millisecond, lim) != 0 {
		t.Errorf("deadBand(0) = %v, want 0", deadBand(0, -time.Millisecond, lim))
	}
	// A delta of exactly zero reads the tighter short side.
	if got := deadBand(300*time.Millisecond, 0, lim); got != 15*time.Millisecond {
		t.Errorf("deadBand(300ms, exact) = %v, want 15ms", got)
	}
}

// TestDeadBandDependsOnlyOnItsOwnSide pins the seam between the two budgets.
//
// The round one reviewer set the short budget to 0.04 alone. The long side band
// on a 600 ms slot fell from 40 ms to 24 ms, and a long take changed plan. Each
// side must now read only its own number.
func TestDeadBandDependsOnlyOnItsOwnSide(t *testing.T) {
	slot := 600 * time.Millisecond
	long, short := time.Millisecond, -time.Millisecond
	base := DefaultStretchLimits()

	cases := []struct {
		name        string
		lim         StretchLimits
		wantShortMs int64
		wantLongMs  int64
	}{
		{"defaults", base, 30, 40},
		{"short side tightened alone", StretchLimits{Short: 0.04, Long: base.Long}, 24, 40},
		{"long side tightened alone", StretchLimits{Short: base.Short, Long: 0.04}, 30, 24},
		{"short side widened alone", StretchLimits{Short: MaxStretchLimit, Long: base.Long}, 40, 40},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotShort := deadBand(slot, short, tc.lim)
			if gotShort.Milliseconds() != tc.wantShortMs {
				t.Errorf("short band = %v, want %d ms", gotShort, tc.wantShortMs)
			}
			gotLong := deadBand(slot, long, tc.lim)
			if gotLong.Milliseconds() != tc.wantLongMs {
				t.Errorf("long band = %v, want %d ms", gotLong, tc.wantLongMs)
			}
		})
	}

	// The reviewer's plan flip, stated as a test. A take 30 ms long on a 600 ms
	// slot sits inside the long dead band, and tightening the short side alone
	// leaves it there.
	longTake := types.NewFit(slot, slot+30*time.Millisecond)
	tightShort := StretchLimits{Short: 0.04, Long: base.Long}
	if plan := PlanStretchWithLimits(longTake, base); plan.Repair != types.RepairNone {
		t.Fatalf("under defaults the long take plans %s, want none", plan.Repair)
	}
	if plan := PlanStretchWithLimits(longTake, tightShort); plan.Repair != types.RepairNone {
		t.Errorf("tightening the short budget alone moved a long take to %s, want none", plan.Repair)
	}

	// The mirror case. Tightening the long side alone leaves a short take alone.
	shortTake := types.NewFit(slot, slot-30*time.Millisecond)
	tightLong := StretchLimits{Short: base.Short, Long: 0.04}
	if plan := PlanStretchWithLimits(shortTake, base); plan.Repair != types.RepairNone {
		t.Fatalf("under defaults the short take plans %s, want none", plan.Repair)
	}
	if plan := PlanStretchWithLimits(shortTake, tightLong); plan.Repair != types.RepairNone {
		t.Errorf("tightening the long budget alone moved a short take to %s, want none", plan.Repair)
	}
}

// TestCallerSuppliedLimitsDriveTheRouting pins the threshold as an input.
//
// The caller resolves the budget in the fit loop. This package receives an
// already resolved value and stays ignorant of where it came from.
func TestCallerSuppliedLimitsDriveTheRouting(t *testing.T) {
	slot := 5000 * time.Millisecond
	shortTake := types.NewFit(slot, 4700*time.Millisecond) // 6 percent short
	longTake := types.NewFit(slot, 5300*time.Millisecond)  // 6 percent long

	cases := []struct {
		name      string
		lim       StretchLimits
		wantShort types.Repair
		wantLong  types.Repair
	}{
		{"project default", DefaultStretchLimits(), types.RepairRewrite, types.RepairAtempo},
		{"a slower language", StretchLimits{Short: 0.07, Long: 0.10}, types.RepairAtempo, types.RepairAtempo},
		{"a fast passage", StretchLimits{Short: 0.03, Long: 0.04}, types.RepairRewrite, types.RepairRewrite},
		{"symmetric at the old value", StretchLimits{Short: 0.08, Long: 0.08}, types.RepairAtempo, types.RepairAtempo},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := PlanStretchWithLimits(shortTake, tc.lim).Repair; got != tc.wantShort {
				t.Errorf("short take plans %s, want %s", got, tc.wantShort)
			}
			if got := PlanStretchWithLimits(longTake, tc.lim).Repair; got != tc.wantLong {
				t.Errorf("long take plans %s, want %s", got, tc.wantLong)
			}
		})
	}

	// The defaults carry the owner's decision. The short side is the tighter one.
	if DefaultMaxStretchShort >= DefaultMaxStretchLong {
		t.Errorf("DefaultMaxStretchShort %g must sit below DefaultMaxStretchLong %g",
			DefaultMaxStretchShort, DefaultMaxStretchLong)
	}
	if DefaultMaxStretchShort != 0.05 || DefaultMaxStretchLong != 0.08 {
		t.Errorf("defaults are %g short and %g long, want 0.05 and 0.08",
			DefaultMaxStretchShort, DefaultMaxStretchLong)
	}
	if lim := DefaultStretchLimits(); lim.Short != DefaultMaxStretchShort || lim.Long != DefaultMaxStretchLong {
		t.Errorf("DefaultStretchLimits() = %+v, want the two default constants", lim)
	}

	// This package does not read types.FitThreshold. The two answer different
	// questions and the short budget now sits below the domain threshold.
	if DefaultMaxStretchShort >= types.FitThreshold {
		t.Errorf("DefaultMaxStretchShort %g no longer diverges from types.FitThreshold %g",
			DefaultMaxStretchShort, types.FitThreshold)
	}
	// A take that reports Fits true can still plan a rewrite. That is intended.
	borderline := types.NewFit(slot, 4700*time.Millisecond)
	if !borderline.Fits() {
		t.Fatalf("a 6 percent short take must still report Fits true under threshold %g", types.FitThreshold)
	}
	if plan := PlanStretch(borderline); plan.Repair != types.RepairRewrite {
		t.Errorf("a 6 percent short take plans %s, want rewrite", plan.Repair)
	}
}

func TestStretchRejectsUnusableLimits(t *testing.T) {
	dir := t.TempDir()
	in := copyFixtureInto(t, dir, "seg_3_try1.wav")
	slot := 5320 * time.Millisecond

	bad := []struct {
		name string
		lim  StretchLimits
	}{
		{"zero short", StretchLimits{Short: 0, Long: 0.08}},
		{"zero long", StretchLimits{Short: 0.05, Long: 0}},
		{"negative short", StretchLimits{Short: -0.05, Long: 0.08}},
		{"not a number", StretchLimits{Short: math.NaN(), Long: 0.08}},
		{"positive infinity", StretchLimits{Short: 0.05, Long: math.Inf(1)}},
		{"wider than the slot", StretchLimits{Short: 0.05, Long: 1.5}},
		{"wide enough to reclaim segment 8", StretchLimits{Short: 0.45, Long: 0.08}},
		{"one step past the ceiling", StretchLimits{Short: 0.11, Long: 0.08}},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			if tc.lim.Valid() {
				t.Errorf("StretchLimits%+v reports Valid", tc.lim)
			}
			out := filepath.Join(dir, "bad.wav")
			if _, err := StretchWithLimits(in, out, slot, tc.lim); !errors.Is(err, ErrLimits) {
				t.Errorf("StretchWithLimits error = %v, want ErrLimits", err)
			}
			mustNotExist(t, out, "the unusable budget")
			plan := PlanStretchWithLimits(types.NewFit(slot, 5720*time.Millisecond), tc.lim)
			if plan.Repair != types.RepairManual {
				t.Errorf("PlanStretchWithLimits gives %s, want manual", plan.Repair)
			}
		})
	}

	if !DefaultStretchLimits().Valid() {
		t.Error("the project defaults must report Valid")
	}
	if !(StretchLimits{Short: MaxStretchLimit, Long: MaxStretchLimit}).Valid() {
		t.Error("the ceiling itself must report Valid")
	}
	// The ceiling must stay above both defaults.
	if MaxStretchLimit <= DefaultMaxStretchLong {
		t.Errorf("MaxStretchLimit %g leaves no room above the long default %g",
			MaxStretchLimit, DefaultMaxStretchLong)
	}
}

// TestMaxStretchLimitMatchesTheListeningLadder pins the ceiling against a
// written literal, never against the constant it tests, and proves what that
// ceiling refuses.
//
// Round 2 rendered the listening ladder outside Go and measured its lowest
// rung at ratio 0.9000, a 10 percent correction. MaxStretchLimit stops
// exactly there. The round 2 reviewer set the constant to 0.408 in a module
// copy and the whole suite still passed. A numeric assertion here must
// therefore name 0.10 directly rather than read it back from MaxStretchLimit.
func TestMaxStretchLimitMatchesTheListeningLadder(t *testing.T) {
	if MaxStretchLimit != 0.10 {
		t.Fatalf("MaxStretchLimit = %g, want 0.10 exactly", MaxStretchLimit)
	}

	// The ceiling itself must stay usable.
	if !(StretchLimits{Short: 0.10, Long: 0.10}).Valid() {
		t.Error("a budget of 0.10 on both sides must report Valid")
	}

	// A budget a fraction past the ceiling must not. Neither literal here
	// reads MaxStretchLimit, so both assertions move only if the ceiling
	// itself moves, including a widen to 0.408.
	if (StretchLimits{Short: 0.1001, Long: 0.08}).Valid() {
		t.Error("a budget of 0.1001 must report invalid, the ceiling has widened")
	}
	if (StretchLimits{Short: 0.11, Long: 0.08}).Valid() {
		t.Error("a budget of 0.11 must report invalid, the ceiling has widened")
	}

	// Round 2's finding 1 reproduction. seg_6_try1.wav measures 4440 ms.
	// Against a 5223 ms slot that is a 14.99 percent underrun, ratio 0.8501.
	// Even the widest budget this package accepts, 0.10 on both sides written
	// as a literal, still refuses it and leaves no file behind.
	dir := t.TempDir()
	in := copyFixtureInto(t, dir, "seg_6_try1.wav")
	out := filepath.Join(dir, "stretched.wav")
	slot := 5223 * time.Millisecond
	widest := StretchLimits{Short: 0.10, Long: 0.10}

	measured := probeMs(t, in)
	pct := float64(measured-slot.Milliseconds()) / float64(slot.Milliseconds()) * 100
	if pct > -14.9 || pct < -15.1 {
		t.Fatalf("seg 6 against a 5223 ms slot runs %.3f percent short, want about -14.99", pct)
	}

	res, err := StretchWithLimits(in, out, slot, widest)
	if !errors.Is(err, ErrUnderrunTooLarge) {
		t.Fatalf("StretchWithLimits(seg 6, 14.99 percent short, ceiling budget) error = %v, want ErrUnderrunTooLarge", err)
	}
	mustNotExist(t, out, "a 14.99 percent correction beyond the ceiling")
	if math.Abs(res.Before.Ratio()-0.8501) > 0.0001 {
		t.Errorf("seg 6 ratio = %.4f, want 0.8501", res.Before.Ratio())
	}

	t.Logf("seg 6 slot_ms=%d take_ms=%d pct=%.3f ratio=%.4f ceiling=0.10 refused=true",
		slot.Milliseconds(), measured, pct, res.Before.Ratio())
}

// TestStretchFailurePathsCarrySentinelsAndLeaveNoFile walks every way out of
// Stretch that is not a repair. Each one must name a sentinel and each one must
// leave the output path empty.
func TestStretchFailurePathsCarrySentinelsAndLeaveNoFile(t *testing.T) {
	dir := t.TempDir()
	in := copyFixtureInto(t, dir, "seg_1_try1.wav")
	out := filepath.Join(dir, "stretched.wav")
	slot := 1820 * time.Millisecond
	missing := filepath.Join(dir, "missing.wav")

	cases := []struct {
		name string
		call func() error
		want error
	}{
		{"empty source", func() error { _, err := Stretch("", out, slot); return err }, ErrPathRequired},
		{"empty output", func() error { _, err := Stretch(in, "", slot); return err }, ErrPathRequired},
		{"output equals source", func() error { _, err := Stretch(in, in, slot); return err }, ErrSameFile},
		{"missing source", func() error { _, err := Stretch(missing, out, slot); return err }, ErrSourceMissing},
		{"zero slot", func() error { _, err := Stretch(in, out, 0); return err }, ErrNoSlot},
		{"negative slot", func() error { _, err := Stretch(in, out, -time.Second); return err }, ErrNoSlot},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call()
			if !errors.Is(err, tc.want) {
				t.Errorf("error = %v, want %v", err, tc.want)
			}
			mustNotExist(t, out, tc.name)
			t.Logf("%s: %v", tc.name, err)
		})
	}

	// A missing take must not send the reader to ffprobe.
	_, missErr := Stretch(missing, out, slot)
	if strings.Contains(missErr.Error(), "ffprobe") {
		t.Errorf("a missing take reports %q, which names the wrong tool", missErr)
	}
	if !strings.Contains(missErr.Error(), missing) {
		t.Errorf("a missing take reports %q, which does not name the path", missErr)
	}

	// A symlink back to the source is the same file under another name.
	link := filepath.Join(dir, "alias.wav")
	if err := os.Symlink(in, link); err == nil {
		if _, err := Stretch(in, link, slot); !errors.Is(err, ErrSameFile) {
			t.Errorf("symlinked output error = %v, want ErrSameFile", err)
		}
		if err := os.Remove(link); err != nil {
			t.Fatalf("remove alias: %v", err)
		}
	}

	// An existing take is never overwritten and never disturbed.
	if err := os.WriteFile(out, []byte("existing take"), 0o644); err != nil {
		t.Fatalf("seed output: %v", err)
	}
	if _, err := Stretch(in, out, slot); !errors.Is(err, ErrOutputExists) {
		t.Errorf("existing output error = %v, want ErrOutputExists", err)
	}
	data, err := os.ReadFile(out)
	if err != nil || string(data) != "existing take" {
		t.Errorf("existing output was disturbed, got %q err %v", string(data), err)
	}

	// The two repair band refusals and the dead band refusal write nothing.
	fresh := filepath.Join(dir, "fresh.wav")
	if _, err := Stretch(in, fresh, slot); !errors.Is(err, ErrUnderrunTooLarge) {
		t.Errorf("seg 1 error = %v, want ErrUnderrunTooLarge", err)
	}
	mustNotExist(t, fresh, "the underrun refusal")

	longIn := copyFixtureInto(t, dir, "seg_3_try1.wav")
	if _, err := Stretch(longIn, fresh, 5000*time.Millisecond); !errors.Is(err, ErrOverrunTooLarge) {
		t.Errorf("an overrun beyond the budget error = %v, want ErrOverrunTooLarge", err)
	}
	mustNotExist(t, fresh, "the overrun refusal")

	if _, err := Stretch(in, fresh, 1700*time.Millisecond); !errors.Is(err, ErrDeadBand) {
		t.Errorf("a take inside the band error = %v, want ErrDeadBand", err)
	}
	mustNotExist(t, fresh, "the dead band refusal")

	// A source that is a directory cannot be probed. Round 2 found this path
	// returned no sentinel and surfaced raw ffprobe text naming the directory.
	dirOut := filepath.Join(dir, "dir_source.wav")
	if _, err := Stretch(dir, dirOut, slot); !errors.Is(err, ErrSourceUnreadable) {
		t.Errorf("directory source error = %v, want ErrSourceUnreadable", err)
	}
	mustNotExist(t, dirOut, "a directory source")

	// A source that exists but is not decodable audio gets the same sentinel.
	corrupt := filepath.Join(dir, "corrupt.wav")
	if err := os.WriteFile(corrupt, []byte("not a wav file"), 0o644); err != nil {
		t.Fatalf("write corrupt fixture: %v", err)
	}
	corruptOut := filepath.Join(dir, "corrupt_source.wav")
	if _, err := Stretch(corrupt, corruptOut, slot); !errors.Is(err, ErrSourceUnreadable) {
		t.Errorf("undecodable source error = %v, want ErrSourceUnreadable", err)
	}
	mustNotExist(t, corruptOut, "an undecodable source")

	// A missing output directory and an extensionless output both fail once
	// rendering starts, on a take this package actually stretches. Segment 3
	// against a 5320 ms slot runs 7.5 percent long, which the long budget repairs.
	missingDirOut := filepath.Join(dir, "nope", "stretched.wav")
	if _, err := Stretch(longIn, missingDirOut, 5320*time.Millisecond); !errors.Is(err, ErrRenderFailed) {
		t.Errorf("missing output directory error = %v, want ErrRenderFailed", err)
	}
	mustNotExist(t, missingDirOut, "a missing output directory")

	noExtOut := filepath.Join(dir, "noext")
	if _, err := Stretch(longIn, noExtOut, 5320*time.Millisecond); !errors.Is(err, ErrRenderFailed) {
		t.Errorf("extensionless output error = %v, want ErrRenderFailed", err)
	}
	mustNotExist(t, noExtOut, "an extensionless output")
}

// TestOnlyAnUnexportedPathCanBypassTheRepairBand keeps the repair policy bypass out of the
// package's exported surface.
//
// A round one reviewer called the exported StretchRatio on segment 8 at its own
// corrective ratio. It returned a nil error, wrote a 7085 ms file against a
// 7110 ms slot, and PlanStretch then called that output none. That is the
// 2026-09-07 defect reproduced through the repair package itself.
//
// This test states the rule the compiler now enforces. Go has no runtime test
// for an identifier that does not exist, so the enforcement is the build: an
// outside package that writes fit.StretchRatio fails to compile.
func TestOnlyAnUnexportedPathCanBypassTheRepairBand(t *testing.T) {
	// Segment 8 at its own corrective ratio is the exact call the reviewer made.
	dir := t.TempDir()
	in := copyFixtureInto(t, dir, "seg_8_try1.wav")
	out := filepath.Join(dir, "bypass.wav")
	slot := 7110 * time.Millisecond

	before, err := Measure(in, slot)
	if err != nil {
		t.Fatalf("Measure: %v", err)
	}
	ratio := before.Ratio()

	// The only exported way in refuses it and writes nothing.
	if _, err := Stretch(in, out, slot); !errors.Is(err, ErrUnderrunTooLarge) {
		t.Fatalf("Stretch(seg 8) error = %v, want ErrUnderrunTooLarge", err)
	}
	mustNotExist(t, out, "the exported repair path")

	// The unexported bypass still exists for this package's own tests. It
	// succeeds on that ratio, which is precisely why no caller may reach it.
	res, err := stretchRatio(in, out, ratio, slot, DefaultStretchLimits())
	if err != nil {
		t.Fatalf("stretchRatio(seg 8, %.4f) = %v, want nil", ratio, err)
	}
	landed := probeMs(t, out)
	if landed != res.After.Measured.Milliseconds() {
		t.Errorf("res.After.Measured = %d ms, ffprobe says %d ms",
			res.After.Measured.Milliseconds(), landed)
	}
	if plan := PlanStretch(res.After); plan.Repair != types.RepairNone {
		t.Errorf("the bypass output plans %s, want none", plan.Repair)
	}
	t.Logf("seg 8 bypass ratio=%.4f landed_ms=%d slot_ms=%d replans=%s exported=false",
		ratio, landed, slot.Milliseconds(), PlanStretch(res.After).Repair)
}

// TestPlanRoutesOnTheDerivedDeltaAndNeverOnTheField pins where the planner
// gets the miss it routes on.
//
// types.Fit carries Slot, Measured and Delta, and Delta names a value the
// other two already decide. A composite literal may omit it, and Go fills an
// omitted field with zero. A literal may also contradict it. The round 3
// reviewer reached the 2026-09-07 report through the exported plan API in one
// call, with a struct literal that simply left the redundant field out.
//
// Every case below writes a Delta the two durations disagree with. Each want
// column states what the durations mean. A build that reads the field instead
// fails on every row, which is the mutation this test exists to kill.
func TestPlanRoutesOnTheDerivedDeltaAndNeverOnTheField(t *testing.T) {
	cases := []struct {
		name       string
		fit        types.Fit
		wantRepair types.Repair
		wantLimit  float64
		wantRatio  float64
		fieldGives types.Repair
	}{
		{
			// Segment 8's real numbers, with the redundant field left out.
			name:       "segment 8 with the field omitted",
			fit:        types.Fit{Slot: 7110 * time.Millisecond, Measured: 4200 * time.Millisecond},
			wantRepair: types.RepairRewrite,
			wantLimit:  DefaultMaxStretchShort,
			fieldGives: types.RepairNone,
		},
		{
			// The same take with a stale field. Reading it sends a 40.9 percent
			// underrun to atempo at ratio 0.590717, far outside the interval the
			// exported surface is supposed to reach.
			name:       "segment 8 with a stale field",
			fit:        types.Fit{Slot: 7110 * time.Millisecond, Measured: 4200 * time.Millisecond, Delta: -100 * time.Millisecond},
			wantRepair: types.RepairRewrite,
			wantLimit:  DefaultMaxStretchShort,
			fieldGives: types.RepairAtempo,
		},
		{
			// The field names the wrong side, so it picks the wrong budget too.
			// The take runs 400 ms long, which the 8 percent long budget repairs.
			name:       "a long take whose field says short",
			fit:        types.Fit{Slot: 5000 * time.Millisecond, Measured: 5400 * time.Millisecond, Delta: -400 * time.Millisecond},
			wantRepair: types.RepairAtempo,
			wantLimit:  DefaultMaxStretchLong,
			wantRatio:  5400.0 / 5000.0,
			fieldGives: types.RepairRewrite,
		},
		{
			name:       "an exact take whose field claims a miss",
			fit:        types.Fit{Slot: 5000 * time.Millisecond, Measured: 5000 * time.Millisecond, Delta: 3000 * time.Millisecond},
			wantRepair: types.RepairNone,
			wantLimit:  DefaultMaxStretchShort,
			fieldGives: types.RepairRewrite,
		},
		{
			name:       "a short take whose field claims an exact fit",
			fit:        types.Fit{Slot: 5000 * time.Millisecond, Measured: 4700 * time.Millisecond},
			wantRepair: types.RepairRewrite,
			wantLimit:  DefaultMaxStretchShort,
			fieldGives: types.RepairNone,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.fieldGives == tc.wantRepair {
				t.Fatalf("this row cannot tell the two sources apart, both give %s", tc.wantRepair)
			}

			plan := PlanStretch(tc.fit)
			if plan.Repair != tc.wantRepair {
				t.Errorf("Repair = %s, want %s, the field would give %s",
					plan.Repair, tc.wantRepair, tc.fieldGives)
			}
			if plan.Limit != tc.wantLimit {
				t.Errorf("Limit = %g, want %g", plan.Limit, tc.wantLimit)
			}
			if math.Abs(plan.Ratio-tc.wantRatio) > 1e-9 {
				t.Errorf("Ratio = %.9f, want %.9f", plan.Ratio, tc.wantRatio)
			}

			// The same durations through types.NewFit must agree, because the
			// derivation and the constructor compute the same quantity.
			viaConstructor := PlanStretch(types.NewFit(tc.fit.Slot, tc.fit.Measured))
			if viaConstructor.Repair != plan.Repair || viaConstructor.Limit != plan.Limit {
				t.Errorf("through NewFit: repair %s limit %g, through the literal: repair %s limit %g",
					viaConstructor.Repair, viaConstructor.Limit, plan.Repair, plan.Limit)
			}

			t.Logf("%s: slot_ms=%d measured_ms=%d field_delta_ms=%d derived_delta_ms=%d repair=%s field_would_give=%s Fits=%v",
				tc.name, tc.fit.Slot.Milliseconds(), tc.fit.Measured.Milliseconds(),
				tc.fit.Delta.Milliseconds(), (tc.fit.Measured - tc.fit.Slot).Milliseconds(),
				plan.Repair, tc.fieldGives, tc.fit.Fits())
		})
	}

	// The reviewer's own reproduction, written out. This literal reports
	// Fits true and TooShort false, because types.Fit reads its own field.
	// The planner disagrees with the type, and the durations are why.
	seg8 := types.Fit{Slot: 7110 * time.Millisecond, Measured: 4200 * time.Millisecond}
	if !seg8.Fits() || seg8.TooShort() {
		t.Fatalf("types.Fit no longer reads its own field, Fits=%v TooShort=%v", seg8.Fits(), seg8.TooShort())
	}
	if plan := PlanStretch(seg8); plan.Repair != types.RepairRewrite {
		t.Errorf("the 2026-09-07 literal plans %s, want rewrite", plan.Repair)
	}
}

// TestConcurrentStretchesGiveOneWinnerAndOneFile pins the output claim.
//
// Round 3 ran two calls against one output path from an outside package. With
// two different slots, calls returned a nil error and a populated After while
// os.Stat found no file at Out, because the other call's cleanup removed it.
// With one slot on both, both calls returned nil against a single file and
// ErrOutputExists never fired.
//
// Both shapes run ten trials here. The rule under test is one sentence. A nil
// error means a file exists at Out.
func TestConcurrentStretchesGiveOneWinnerAndOneFile(t *testing.T) {
	const trials = 10

	shapes := []struct {
		name  string
		slotA time.Duration
		slotB time.Duration
	}{
		{"different slots", 5320 * time.Millisecond, 5400 * time.Millisecond},
		{"one slot on both", 5320 * time.Millisecond, 5320 * time.Millisecond},
	}

	for _, shape := range shapes {
		t.Run(shape.name, func(t *testing.T) {
			winners, losers := 0, 0
			for i := 0; i < trials; i++ {
				dir := t.TempDir()
				// Segment 3 measures 5720 ms. It runs 7.5 percent long against
				// 5320 ms and 5.9 percent long against 5400 ms, so both slots
				// route to atempo and both land inside their own band.
				in := copyFixtureInto(t, dir, "seg_3_try1.wav")
				out := filepath.Join(dir, "stretched.wav")
				slots := [2]time.Duration{shape.slotA, shape.slotB}

				var wg sync.WaitGroup
				var results [2]StretchResult
				var errs [2]error
				wg.Add(2)
				for k := 0; k < 2; k++ {
					go func(k int) {
						defer wg.Done()
						results[k], errs[k] = StretchWithLimits(in, out, slots[k], DefaultStretchLimits())
					}(k)
				}
				wg.Wait()

				won := -1
				for k, err := range errs {
					if err == nil {
						if won >= 0 {
							t.Fatalf("trial %d: both calls returned nil against one output path", i+1)
						}
						won = k
						winners++
						continue
					}
					if !errors.Is(err, ErrOutputExists) {
						t.Fatalf("trial %d: the losing call reported %v, want ErrOutputExists", i+1, err)
					}
					losers++
				}
				if won < 0 {
					t.Fatalf("trial %d: neither call succeeded, errors %v and %v", i+1, errs[0], errs[1])
				}

				// The measurement that matters. ffprobe reads the artifact, and
				// its duration must be the winner's, not the loser's.
				landed := probeMs(t, out)
				if landed != results[won].After.Measured.Milliseconds() {
					t.Errorf("trial %d: ffprobe says %d ms, the winner reported %d ms",
						i+1, landed, results[won].After.Measured.Milliseconds())
				}
				if miss := landed - slots[won].Milliseconds(); miss > 40 || miss < -40 {
					t.Errorf("trial %d: the surviving file misses the winner's slot by %d ms", i+1, miss)
				}
				t.Logf("%s trial %2d: winner=%d slot_ms=%d landed_ms=%d loser=%v",
					shape.name, i+1, won, slots[won].Milliseconds(), landed, errs[1-won])
			}

			if winners != trials || losers != trials {
				t.Errorf("across %d trials: %d nil returns and %d ErrOutputExists, want %d of each",
					trials, winners, losers, trials)
			}
		})
	}
}

// TestRenderListeningCandidates writes slow down candidates for a human ear.
//
// The short side budget needs a listener, not a measurement. Slowing speech
// sounds more artificial than speeding it, so DefaultMaxStretchShort sits below
// DefaultMaxStretchLong at a value nobody has verified by ear. This probe
// renders a ladder of slow down ratios across the plausible range and reports
// the measured duration of each one.
//
// It stays off by default. Set AJILAMU_LISTENING_DIR to an absolute output
// directory to run it. The path must be absolute, because a test runs in its own
// package directory and a relative path would land there. The probe needs ffmpeg
// and ffprobe only, and it makes no network call.
//
// The rendered WAV files stay in gitignored scratch. The measured table and this
// command live in the T2.5 handoff entry in dev-diary/PHASE-2-fit-loop.md.
//
//	AJILAMU_LISTENING_DIR=$PWD/scratch/t25-listening go test -count=1 -run ListeningCandidates -v ./internal/fit/
func TestRenderListeningCandidates(t *testing.T) {
	dir := os.Getenv("AJILAMU_LISTENING_DIR")
	if dir == "" {
		t.Skip("set AJILAMU_LISTENING_DIR to render listening candidates")
	}
	if !filepath.IsAbs(dir) {
		t.Fatalf("AJILAMU_LISTENING_DIR must be absolute, got %q", dir)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create %s: %v", dir, err)
	}

	// The ladder spans the plausible short side range. A ratio of 0.98 slows a
	// take by about 2 percent and a ratio of 0.92 slows it by about 8 percent.
	ratios := []float64{0.99, 0.98, 0.97, 0.96, 0.95, 0.94, 0.93, 0.92, 0.90}

	subjects := []struct {
		seg    int
		file   string
		slotMs int64
	}{
		{1, "seg_1_try1.wav", 1820},
		{6, "seg_6_try1.wav", 4630},
	}

	for _, sub := range subjects {
		src := takesPath(t, sub.file)
		slot := time.Duration(sub.slotMs) * time.Millisecond

		original := filepath.Join(dir, fmt.Sprintf("seg_%d_original.wav", sub.seg))
		data, err := os.ReadFile(src)
		if err != nil {
			t.Fatalf("read %s: %v", src, err)
		}
		if err := os.WriteFile(original, data, 0o644); err != nil {
			t.Fatalf("write %s: %v", original, err)
		}

		// The full correction ratio comes from the measured fit, not from the
		// plan. A take the short budget refuses carries no plan ratio, and the
		// ladder still needs to render it.
		measured := types.NewFit(slot, time.Duration(probeMs(t, src))*time.Millisecond)
		ladder := append([]float64{}, ratios...)
		ladder = append(ladder, measured.Ratio())

		for _, ratio := range ladder {
			out := filepath.Join(dir, fmt.Sprintf("seg_%d_ratio_%.4f.wav", sub.seg, ratio))
			if err := os.Remove(out); err != nil && !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("clear %s: %v", out, err)
			}
			// renderCandidate keeps the file on purpose. A candidate is not a
			// repaired take, and no exported symbol reaches this path.
			res, err := renderCandidate(src, out, ratio, slot)
			if err != nil {
				t.Fatalf("renderCandidate(seg %d, %.4f): %v", sub.seg, ratio, err)
			}
			landed := probeMs(t, out)
			if landed != res.After.Measured.Milliseconds() {
				t.Errorf("res.After.Measured = %d ms, ffprobe says %d ms",
					res.After.Measured.Milliseconds(), landed)
			}
			shortPct := float64(landed-sub.slotMs) / float64(sub.slotMs) * 100
			t.Logf("seg %d requested_ratio=%.4f measured_ms=%d slot_ms=%d delta_ms=%d delta_pct=%+.2f file=%s",
				sub.seg, ratio, landed, sub.slotMs, landed-sub.slotMs, shortPct, filepath.Base(out))
		}
		t.Logf("seg %d full_correction_ratio=%.4f original_ms=%d slot_ms=%d plan=%s",
			sub.seg, measured.Ratio(), probeMs(t, original), sub.slotMs, PlanStretch(measured).Repair)
	}
}

// TestPlanStretchRefusesNegativeMeasuredDuration verifies that negative
// measured durations return RepairManual.
//
// An arithmetic overflow on math.MinInt64 previously caused a negative
// measured duration to plan as RepairNone. Refusing negative measured durations
// stops that overflow.
func TestPlanStretchRefusesNegativeMeasuredDuration(t *testing.T) {
	cases := []struct {
		name     string
		slot     time.Duration
		measured time.Duration
	}{
		{
			name:     "one nanosecond negative",
			slot:     1000 * time.Millisecond,
			measured: -1 * time.Nanosecond,
		},
		{
			name:     "negative one hundred milliseconds",
			slot:     1000 * time.Millisecond,
			measured: -100 * time.Millisecond,
		},
		{
			name:     "min int64 offset raw nanoseconds",
			slot:     1000,
			measured: time.Duration(math.MinInt64) + 1000,
		},
		{
			name:     "min int64 offset with millisecond slot",
			slot:     1000 * time.Millisecond,
			measured: time.Duration(math.MinInt64) + 1000,
		},
		{
			name:     "min int64 exactly",
			slot:     1000 * time.Millisecond,
			measured: time.Duration(math.MinInt64),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fit := types.Fit{Slot: tc.slot, Measured: tc.measured}
			plan := PlanStretch(fit)
			if plan.Repair != types.RepairManual {
				t.Errorf("PlanStretch(Measured=%v) = %v, want %v",
					tc.measured, plan.Repair, types.RepairManual)
			}
			planWithLimits := PlanStretchWithLimits(fit, DefaultStretchLimits())
			if planWithLimits.Repair != types.RepairManual {
				t.Errorf("PlanStretchWithLimits(Measured=%v) = %v, want %v",
					tc.measured, planWithLimits.Repair, types.RepairManual)
			}
		})
	}
}

// stretchRatio applies one explicit atempo ratio through the same landing check
// and the same cleanup that StretchWithLimits uses. It skips the repair band,
// so it lives in this test file where production sibling files cannot reach it.
//
// A non-test build of package fit never includes this function.
//
// Only this package's own tests call it. They use it to pin the landing check,
// which no correctly planned repair can reach.
func stretchRatio(in, out string, ratio float64, slot time.Duration, lim StretchLimits) (StretchResult, error) {
	res, err := prepareRatio(in, out, ratio, slot, lim)
	if err != nil {
		return StretchResult{}, err
	}
	return applyStretch(res)
}

// renderCandidate renders one take at an explicit ratio and re-measures it with
// ffprobe. It applies no repair band and no landing check, and it keeps the
// output on disk on purpose.
//
// The listening harness uses it to build a ladder of slow down candidates for a
// human ear. A candidate is not a repaired take, so it must never reach the fit
// loop. It lives in this test file so sibling files in package fit cannot call it.
func renderCandidate(in, out string, ratio float64, slot time.Duration) (StretchResult, error) {
	res, err := prepareRatio(in, out, ratio, slot, DefaultStretchLimits())
	if err != nil {
		return StretchResult{}, err
	}
	return renderStretch(res)
}

// prepareRatio validates one explicit ratio call and measures its source take.
// It serves stretchRatio and renderCandidate for test and listening harness runs.
func prepareRatio(in, out string, ratio float64, slot time.Duration, lim StretchLimits) (StretchResult, error) {
	if err := checkPaths(in, out); err != nil {
		return StretchResult{}, err
	}
	if slot <= 0 {
		return StretchResult{}, fmt.Errorf("%w: got %v", ErrNoSlot, slot)
	}
	if !lim.Valid() {
		return StretchResult{}, fmt.Errorf("%w: got short %g and long %g", ErrLimits, lim.Short, lim.Long)
	}
	if err := checkRatio(ratio); err != nil {
		return StretchResult{}, err
	}

	before, err := Measure(in, slot)
	if err != nil {
		return StretchResult{}, fmt.Errorf("%w: %s: %w", ErrSourceUnreadable, in, err)
	}

	return StretchResult{
		In:     in,
		Out:    out,
		Ratio:  ratio,
		Before: before,
		Band:   deadBand(slot, delta(before), lim),
		Limits: lim,
	}, nil
}
