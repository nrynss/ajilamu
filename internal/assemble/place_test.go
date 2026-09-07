package assemble

import (
	"encoding/json"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nrynss/ajilamu/internal/types"
)

func TestDecidePolicy(t *testing.T) {
	for _, tc := range []struct {
		name                          string
		slot, measured, voiced, avail int
		policy                        Policy
		keep, truncated, gap, fade    int
	}{
		{name: "fit", slot: 1000, measured: 800, voiced: 780, avail: 1500, policy: PolicyFit, keep: 800},
		{name: "gap", slot: 1000, measured: 1200, voiced: 800, avail: 1500, policy: PolicyGap, keep: 1200, gap: 200},
		{name: "truncate-silence", slot: 800, measured: 1200, voiced: 800, avail: 1000, policy: PolicyTruncate, keep: 1000, truncated: 200, gap: 200},
		{name: "crossfade", slot: 1000, measured: 1500, voiced: 1500, avail: 1000, policy: PolicyCrossfade, keep: 1040, truncated: 460, fade: 40, gap: 40},
		{name: "short-crossfade", slot: 10, measured: 50, voiced: 50, avail: 10, policy: PolicyCrossfade, keep: 50, truncated: 0, fade: 40, gap: 40},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := decidePolicy(tc.slot, tc.measured, tc.voiced, tc.avail, 40)
			if got.event.Policy != tc.policy || got.keep != tc.keep || got.truncated != tc.truncated || got.gapUsed != tc.gap || got.fadeOut != tc.fade {
				t.Fatalf("policy=%s keep=%d truncated=%d gap=%d fade=%d", got.event.Policy, got.keep, got.truncated, got.gapUsed, got.fadeOut)
			}
		})
	}
}

func TestFixtureTakesPlaceOverBed(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join("..", "..", "testdata", "clip.mp4")
	bedFile := filepath.Join(dir, "bed.wav")
	bed, err := BuildBed(source, "", bedFile)
	if err != nil {
		t.Fatal(err)
	}
	clips := fixtureClips(t)
	speechFile := filepath.Join(dir, "speech.wav")
	placed, err := bed.Place(clips, speechFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(placed.Events) != 8 {
		t.Fatalf("placed %d takes", len(placed.Events))
	}
	probeBed(t, speechFile, "44100", "stereo", 2)
	speech := decodedAudio(t, speechFile)
	bedSamples := decodedAudio(t, bedFile)
	if len(speech) != len(bedSamples) {
		t.Fatalf("speech has %d samples, bed has %d", len(speech), len(bedSamples))
	}
	mixFile := filepath.Join(dir, "mix.wav")
	if err := bed.Overlay(speechFile, mixFile); err != nil {
		t.Fatal(err)
	}
	mix := decodedAudio(t, mixFile)
	if len(mix) != len(bedSamples) {
		t.Fatalf("mix has %d samples, bed has %d", len(mix), len(bedSamples))
	}

	events := map[int]Event{}
	for _, event := range placed.Events {
		events[event.SegmentID] = event
	}
	occupied := make([]bool, len(speech)/2)
	const rate = 44100
	for _, clip := range clips {
		event, ok := events[clip.Segment.ID]
		if !ok {
			t.Fatalf("missing event for segment %d", clip.Segment.ID)
		}
		if event.Policy != PolicyFit && event.Policy != PolicyGap {
			t.Fatalf("segment %d used collision policy %s", event.SegmentID, event.Policy)
		}
		prepared := filepath.Join(dir, filepath.Base(clip.File))
		if err := bed.PrepareTake(clip.File, prepared); err != nil {
			t.Fatal(err)
		}
		want := decodedAudio(t, prepared)
		start := msFrames(clip.Segment.StartMs, rate)
		if start*2+len(want) > len(speech) {
			t.Fatalf("segment %d placement overruns the bed", clip.Segment.ID)
		}
		got := speech[start*2 : start*2+len(want)]
		delta := 20 * math.Log10(audioRMS(got)/audioRMS(want))
		t.Logf("segment %d policy=%s start_ms=%d measured_ms=%d level delta %.6f dB",
			event.SegmentID, event.Policy, event.Start.Milliseconds(), event.Measured.Milliseconds(), delta)
		if math.Abs(delta) > 0.5 || math.IsNaN(delta) {
			t.Fatalf("segment %d placed level differs by %.6f dB", event.SegmentID, delta)
		}
		wantVoice := firstVoiced(want, 2)
		gotVoice := firstVoiced(got, 2)
		if absInt(gotVoice-wantVoice) > rate/100 {
			t.Fatalf("segment %d voiced start moved by %d frames", clip.Segment.ID, gotVoice-wantVoice)
		}
		for frame := 0; frame < len(want)/2 && start+frame < len(occupied); frame++ {
			occupied[start+frame] = true
		}
	}

	var gapEnergy, mixGapEnergy, bedGapEnergy float64
	var gapFrames int
	for frame, used := range occupied {
		if used {
			continue
		}
		i := frame * 2
		gapEnergy += float64(speech[i])*float64(speech[i]) + float64(speech[i+1])*float64(speech[i+1])
		mixGapEnergy += float64(mix[i])*float64(mix[i]) + float64(mix[i+1])*float64(mix[i+1])
		bedGapEnergy += float64(bedSamples[i])*float64(bedSamples[i]) + float64(bedSamples[i+1])*float64(bedSamples[i+1])
		if mix[i] != bedSamples[i] || mix[i+1] != bedSamples[i+1] {
			t.Fatalf("overlay changed bed sample %d", i)
		}
		gapFrames++
	}
	speechGapRMS := math.Sqrt(gapEnergy / float64(gapFrames*2))
	mixDelta := 20 * math.Log10(math.Sqrt(mixGapEnergy/float64(gapFrames*2))/math.Sqrt(bedGapEnergy/float64(gapFrames*2)))
	t.Logf("speech-gap RMS %.9f, overlay gap level delta %.6f dB", speechGapRMS, mixDelta)
	if speechGapRMS > 0.000001 {
		t.Fatalf("speech layer leaked into the bed gaps: RMS %.9f", speechGapRMS)
	}
	if math.Abs(mixDelta) > 0.5 || math.IsNaN(mixDelta) {
		t.Fatalf("overlay gap level differs by %.6f dB", mixDelta)
	}
}

func TestOverrunUsesTrailingGap(t *testing.T) {
	dir := t.TempDir()
	bed := synthBed(t, dir, 3)
	first := synthTone(t, filepath.Join(dir, "first.wav"), 440, 1.2)
	second := synthTone(t, filepath.Join(dir, "second.wav"), 880, 0.4)
	placed, err := bed.Place([]Clip{
		{Segment: types.Segment{ID: 1, StartMs: 0, EndMs: 1000}, File: first},
		{Segment: types.Segment{ID: 2, StartMs: 1500, EndMs: 1900}, File: second},
	}, filepath.Join(dir, "speech.wav"))
	if err != nil {
		t.Fatal(err)
	}
	if len(placed.Events) != 2 || placed.Events[0].Policy != PolicyGap {
		t.Fatalf("events=%v", placed.Events)
	}
	speech := decodedAudio(t, placed.File)
	early := audioRMS(span(speech, 0.2, 0.6))
	gap := audioRMS(span(speech, 1.05, 1.15))
	late := audioRMS(span(speech, 1.6, 1.8))
	t.Logf("gap policy RMS early=%.6f gap=%.6f late=%.6f gap_used_ms=%d",
		early, gap, late, placed.Events[0].GapUsed.Milliseconds())
	if early < 0.05 || gap < 0.05 || late < 0.05 {
		t.Fatalf("take did not occupy the trailing gap: early=%.6f gap=%.6f late=%.6f", early, gap, late)
	}
}

func TestOverrunTruncatesAtSilence(t *testing.T) {
	dir := t.TempDir()
	bed := synthBed(t, dir, 3)
	first := synthToneSilence(t, filepath.Join(dir, "first.wav"), 440, 0.8, 0.4)
	second := synthTone(t, filepath.Join(dir, "second.wav"), 880, 0.5)
	placed, err := bed.Place([]Clip{
		{Segment: types.Segment{ID: 1, StartMs: 0, EndMs: 800}, File: first},
		{Segment: types.Segment{ID: 2, StartMs: 1000, EndMs: 1500}, File: second},
	}, filepath.Join(dir, "speech.wav"))
	if err != nil {
		t.Fatal(err)
	}
	if placed.Events[0].Policy != PolicyTruncate || placed.Events[0].Truncated < 150*1e6 {
		t.Fatalf("truncate event=%+v", placed.Events[0])
	}
	speech := decodedAudio(t, placed.File)
	tone := audioRMS(span(speech, 0.2, 0.6))
	keptGap := audioRMS(span(speech, 0.85, 0.95))
	handoff := audioRMS(span(speech, 1.1, 1.3))
	t.Logf("truncate RMS tone=%.6f kept-gap=%.6f next=%.6f truncated_ms=%d",
		tone, keptGap, handoff, placed.Events[0].Truncated.Milliseconds())
	if tone < 0.05 || keptGap > 0.000001 || handoff < 0.05 {
		t.Fatalf("silence was not trimmed at the next take: tone=%.6f gap=%.6f next=%.6f", tone, keptGap, handoff)
	}
}

func TestCollisionCrossfadesAndLogs(t *testing.T) {
	dir := t.TempDir()
	bed := synthBed(t, dir, 3)
	first := synthTone(t, filepath.Join(dir, "first.wav"), 440, 1.5)
	second := synthTone(t, filepath.Join(dir, "second.wav"), 880, 1.0)
	placed, err := bed.Place([]Clip{
		{Segment: types.Segment{ID: 1, StartMs: 0, EndMs: 1000}, File: first},
		{Segment: types.Segment{ID: 2, StartMs: 1000, EndMs: 2000}, File: second},
	}, filepath.Join(dir, "speech.wav"))
	if err != nil {
		t.Fatal(err)
	}
	ev := placed.Events[0]
	if ev.Policy != PolicyCrossfade || ev.Truncated < 450*1e6 || ev.Crossfade < 30*1e6 {
		t.Fatalf("crossfade event=%+v", ev)
	}
	if ev.End < 1030*1e6 {
		t.Fatalf("earlier take did not overlap the next start: end=%s", ev.End)
	}
	if placed.Events[1].Policy != PolicyFit {
		t.Fatalf("second take policy=%s", placed.Events[1].Policy)
	}
	prepared := filepath.Join(dir, "first-prepared.wav")
	if err := bed.PrepareTake(first, prepared); err != nil {
		t.Fatal(err)
	}
	want := decodedAudio(t, prepared)
	speech := decodedAudio(t, placed.File)
	body := audioRMS(span(speech, 0.2, 0.8))
	wantBody := audioRMS(span(want, 0.2, 0.8))
	junction := audioRMS(span(speech, 0.995, 1.005))
	next := audioRMS(span(speech, 1.2, 1.6))
	t.Logf("crossfade RMS body=%.6f junction=%.6f next=%.6f truncated_ms=%d fade_ms=%d",
		body, junction, next, ev.Truncated.Milliseconds(), ev.Crossfade.Milliseconds())
	if math.Abs(20*math.Log10(body/wantBody)) > 0.5 {
		t.Fatalf("later take overwrote the earlier body")
	}
	if junction < body*0.5 {
		t.Fatalf("voiced collision left a silent trough: body=%.6f junction=%.6f", body, junction)
	}
	peak := maxAbs(span(speech, 0.995, 1.005))
	if peak < voicedFloor {
		t.Fatalf("junction peak %g is digital silence", peak)
	}
	if next < 0.05 {
		t.Fatalf("second take did not start after the logged fade")
	}
	for _, interval := range silenceIntervals(t, placed.File) {
		if interval[0] <= 1.000 && interval[1] >= 1.000 {
			t.Fatalf("silencedetect reported a trough at 1.000s: [%.6f, %.6f]", interval[0], interval[1])
		}
	}
}

func TestPlaceRejectsInvalidInputsAndPreservesOutputs(t *testing.T) {
	dir := t.TempDir()
	bed := synthBed(t, dir, 1)
	take := synthTone(t, filepath.Join(dir, "take.wav"), 440, 0.2)
	output := filepath.Join(dir, "speech.wav")
	if err := os.WriteFile(output, []byte("previous render"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := bed.Place([]Clip{{
		Segment: types.Segment{ID: 1, StartMs: 0, EndMs: 200},
		File:    filepath.Join(dir, "missing.wav"),
	}}, output); err == nil {
		t.Fatal("accepted a missing take")
	}
	data, err := os.ReadFile(output)
	if err != nil || string(data) != "previous render" {
		t.Fatal("failed placement changed existing output")
	}
	if _, err := (Bed{}).Place(nil, output); err == nil {
		t.Fatal("accepted an uninitialized bed")
	}
	if _, err := bed.Place([]Clip{{
		Segment: types.Segment{ID: 1, StartMs: 0, EndMs: 200},
		File:    take,
	}}, bed.File); err == nil {
		t.Fatal("allowed overwriting the bed")
	}
	if _, err := bed.Place([]Clip{{
		Segment: types.Segment{ID: 1, StartMs: 10, EndMs: 10},
		File:    take,
	}}, output); err == nil {
		t.Fatal("accepted an empty slot")
	}
	entries, err := filepath.Glob(filepath.Join(dir, ".assemble-*"))
	if err != nil || len(entries) != 0 {
		t.Fatal("failed placement leaked temporary files")
	}
}

func fixtureClips(t *testing.T) []Clip {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "segments.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rows []struct {
		ID      int   `json:"id"`
		StartMs int64 `json:"start_ms"`
		EndMs   int64 `json:"end_ms"`
	}
	if err := json.Unmarshal(data, &rows); err != nil {
		t.Fatal(err)
	}
	files := map[int]string{
		1: "seg_1_try1.wav",
		2: "seg_2_try1.wav",
		3: "seg_3_stretched.wav",
		4: "seg_4_stretched.wav",
		5: "seg_5_try1.wav",
		6: "seg_6_try1.wav",
		7: "seg_7_try1.wav",
		8: "seg_8_try1.wav",
	}
	clips := make([]Clip, 0, 8)
	for _, row := range rows {
		name, ok := files[row.ID]
		if !ok {
			t.Fatalf("no final take for segment %d", row.ID)
		}
		clips = append(clips, Clip{
			Segment: types.Segment{ID: row.ID, StartMs: row.StartMs, EndMs: row.EndMs},
			File:    filepath.Join("..", "..", "testdata", "takes", name),
		})
	}
	return clips
}

func synthBed(t *testing.T, dir string, seconds int) Bed {
	t.Helper()
	source := filepath.Join(dir, "source.wav")
	output := filepath.Join(dir, "bed.wav")
	runAudioTool(t, "ffmpeg", "-v", "error", "-f", "lavfi", "-i",
		"aevalsrc=0.04|0.03:s=44100:d="+strconv.Itoa(seconds)+":c=stereo", "-c:a", "pcm_f32le", source)
	bed, err := BuildBed(source, "", output)
	if err != nil {
		t.Fatal(err)
	}
	return bed
}

func synthTone(t *testing.T, path string, freq int, seconds float64) string {
	t.Helper()
	runAudioTool(t, "ffmpeg", "-v", "error", "-f", "lavfi", "-i",
		"sine=frequency="+strconv.Itoa(freq)+":sample_rate=16000:duration="+strconv.FormatFloat(seconds, 'f', 3, 64),
		"-c:a", "pcm_s16le", path)
	return path
}

func synthToneSilence(t *testing.T, path string, freq int, tone, silence float64) string {
	t.Helper()
	runAudioTool(t, "ffmpeg", "-v", "error",
		"-f", "lavfi", "-i", "sine=frequency="+strconv.Itoa(freq)+":sample_rate=16000:duration="+strconv.FormatFloat(tone, 'f', 3, 64),
		"-f", "lavfi", "-i", "anullsrc=r=16000:cl=mono:d="+strconv.FormatFloat(silence, 'f', 3, 64),
		"-filter_complex", "[0:a][1:a]concat=n=2:v=0:a=1", "-c:a", "pcm_s16le", path)
	return path
}

func firstVoiced(samples []float32, channels int) int {
	frames := len(samples) / channels
	for frame := 0; frame < frames; frame++ {
		for ch := 0; ch < channels; ch++ {
			if abs32(samples[frame*channels+ch]) >= voicedFloor {
				return frame
			}
		}
	}
	return frames
}

func span(samples []float32, start, end float64) []float32 {
	from := int(math.Round(start * 44100))
	to := int(math.Round(end * 44100))
	return samples[from*2 : to*2]
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func maxAbs(samples []float32) float32 {
	var peak float32
	for _, sample := range samples {
		if v := abs32(sample); v > peak {
			peak = v
		}
	}
	return peak
}

func silenceIntervals(t *testing.T, path string) [][2]float64 {
	t.Helper()
	cmd := exec.Command("ffmpeg", "-nostdin", "-hide_banner", "-i", path,
		"-af", "silencedetect=noise=-40dB:d=0.005", "-f", "null", "-")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("silencedetect failed: %v: %s", err, out)
	}
	var intervals [][2]float64
	var start float64
	haveStart := false
	for _, line := range strings.Split(string(out), "\n") {
		if i := strings.Index(line, "silence_start: "); i >= 0 {
			fields := strings.Fields(line[i+len("silence_start: "):])
			if len(fields) == 0 {
				continue
			}
			start, _ = strconv.ParseFloat(fields[0], 64)
			haveStart = true
		}
		if i := strings.Index(line, "silence_end: "); i >= 0 && haveStart {
			fields := strings.Fields(line[i+len("silence_end: "):])
			if len(fields) == 0 {
				continue
			}
			end, _ := strconv.ParseFloat(fields[0], 64)
			intervals = append(intervals, [2]float64{start, end})
			haveStart = false
		}
	}
	return intervals
}
