package assemble

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestDuckOutputMatchesBedDuration(t *testing.T) {
	for _, seconds := range []float64{0.5, 1, 2, 6} {
		t.Run(fmt.Sprintf("%.1fs", seconds), func(t *testing.T) {
			dir := t.TempDir()
			source := filepath.Join(dir, "source.wav")
			runAudioTool(t, "ffmpeg", "-v", "error", "-f", "lavfi", "-i",
				fmt.Sprintf("sine=frequency=80:sample_rate=44100:duration=%g", seconds),
				"-ac", "2", "-af", "volume=0.2", "-c:a", "pcm_f32le", source)
			bedFile := filepath.Join(dir, "bed.wav")
			bed, err := BuildBed(t.Context(), source, "", bedFile)
			if err != nil {
				t.Fatal(err)
			}
			speech := filepath.Join(dir, "speech.wav")
			runAudioTool(t, "ffmpeg", "-v", "error", "-f", "lavfi", "-i",
				fmt.Sprintf("sine=frequency=2000:sample_rate=44100:duration=%g", seconds),
				"-ac", "2", "-af", "volume=0.4", "-c:a", "pcm_f32le", speech)
			mixFile := filepath.Join(dir, "mix.wav")
			if err := bed.Duck(t.Context(), speech, mixFile); err != nil {
				t.Fatal(err)
			}
			overlayFile := filepath.Join(dir, "overlay.wav")
			if err := bed.Overlay(t.Context(), speech, overlayFile); err != nil {
				t.Fatal(err)
			}

			bedDur := probeFormatDuration(t, bedFile)
			mixDur := probeFormatDuration(t, mixFile)
			overlayDur := probeFormatDuration(t, overlayFile)
			t.Logf("ffprobe duration bed=%.9f duck=%.9f overlay=%.9f", bedDur, mixDur, overlayDur)
			if math.Abs(overlayDur-bedDur) > 1.0/44100 {
				t.Fatalf("overlay duration %.9f does not match bed %.9f", overlayDur, bedDur)
			}
			if math.Abs(mixDur-bedDur) > 1.0/44100 {
				t.Fatalf("duck duration %.9f does not match bed %.9f", mixDur, bedDur)
			}

			bedSamples := decodedAudio(t, bedFile)
			mixSamples := decodedAudio(t, mixFile)
			if len(mixSamples) != len(bedSamples) {
				t.Fatalf("duck pcm samples %d do not match bed %d", len(mixSamples), len(bedSamples))
			}
		})
	}
}

func TestDuckDipsBedUnderSpeech(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.wav")
	runAudioTool(t, "ffmpeg", "-v", "error", "-f", "lavfi", "-i",
		"sine=frequency=80:sample_rate=44100:duration=4", "-ac", "2", "-af", "volume=0.2",
		"-c:a", "pcm_f32le", source)
	bedFile := filepath.Join(dir, "bed.wav")
	bed, err := BuildBed(t.Context(), source, "", bedFile)
	if err != nil {
		t.Fatal(err)
	}
	if bed.SeparateMusic {
		t.Fatal("source bed marked as separate music")
	}
	speech := filepath.Join(dir, "speech.wav")
	runAudioTool(t, "ffmpeg", "-v", "error",
		"-f", "lavfi", "-i", "anullsrc=r=44100:cl=stereo:d=1",
		"-f", "lavfi", "-i", "sine=frequency=2000:sample_rate=44100:duration=1",
		"-f", "lavfi", "-i", "anullsrc=r=44100:cl=stereo:d=2",
		"-filter_complex", "[1]aformat=channel_layouts=stereo,volume=0.4[s];[0][s][2]concat=n=3:v=0:a=1",
		"-c:a", "pcm_f32le", speech)
	mixFile := filepath.Join(dir, "mix.wav")
	if err := bed.Duck(t.Context(), speech, mixFile); err != nil {
		t.Fatal(err)
	}
	probeBed(t, mixFile, "44100", "stereo", 2)

	bedLP := filteredAudio(t, bedFile, "lowpass=f=200")
	mixLP := filteredAudio(t, mixFile, "lowpass=f=200")
	speechHP := filteredAudio(t, speech, "highpass=f=800")
	mixHP := filteredAudio(t, mixFile, "highpass=f=800")

	for _, gap := range [][2]float64{{0.2, 0.8}, {2.5, 3.5}} {
		delta := levelDelta(span(mixLP, gap[0], gap[1]), span(bedLP, gap[0], gap[1]))
		t.Logf("silent %.1f-%.1f seconds bed band delta %.6f dB", gap[0], gap[1], delta)
		if math.Abs(delta) > 0.5 || math.IsNaN(delta) {
			t.Fatalf("silent interval changed the bed by %.6f dB", delta)
		}
	}
	dip := levelDelta(span(mixLP, 1.2, 1.8), span(bedLP, 1.2, 1.8))
	t.Logf("speech 1.2-1.8 seconds bed band dip %.6f dB", dip)
	if dip > -2 || math.IsNaN(dip) {
		t.Fatalf("bed under speech did not dip: %.6f dB", dip)
	}
	voice := levelDelta(span(mixHP, 1.2, 1.8), span(speechHP, 1.2, 1.8))
	t.Logf("speech 1.2-1.8 seconds voice band delta %.6f dB", voice)
	if math.Abs(voice) > 0.5 || math.IsNaN(voice) {
		t.Fatalf("speech in the mix changed by %.6f dB", voice)
	}
}

func TestFixtureDuckPreservesGapsAndVoices(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join("..", "..", "testdata", "clip.mp4")
	bedFile := filepath.Join(dir, "bed.wav")
	bed, err := BuildBed(t.Context(), source, "", bedFile)
	if err != nil {
		t.Fatal(err)
	}
	placed, err := bed.Place(t.Context(), fixtureClips(t), filepath.Join(dir, "speech.wav"))
	if err != nil {
		t.Fatal(err)
	}
	mixFile := filepath.Join(dir, "ducked.wav")
	if err := bed.Duck(t.Context(), placed.File, mixFile); err != nil {
		t.Fatal(err)
	}
	probeBed(t, mixFile, "44100", "stereo", 2)

	bedSamples := decodedAudio(t, bedFile)
	speech := decodedAudio(t, placed.File)
	mix := decodedAudio(t, mixFile)
	if len(mix) != len(bedSamples) || len(speech) != len(bedSamples) {
		t.Fatalf("layer lengths bed=%d speech=%d mix=%d", len(bedSamples), len(speech), len(mix))
	}

	for _, gap := range [][2]float64{{0, 5.5}, {54.5, 75}} {
		delta := levelDelta(span(mix, gap[0], gap[1]), span(bedSamples, gap[0], gap[1]))
		t.Logf("fixture gap %.1f-%.1f seconds mix vs bed %.6f dB", gap[0], gap[1], delta)
		if math.Abs(delta) > 0.5 || math.IsNaN(delta) {
			t.Fatalf("gap %.1f-%.1f seconds left the bed by %.6f dB", gap[0], gap[1], delta)
		}
	}
	for _, window := range [][2]float64{{8.5, 12.5}, {39.5, 44}} {
		duckedBed := subSamples(span(mix, window[0], window[1]), span(speech, window[0], window[1]))
		dip := levelDelta(duckedBed, span(bedSamples, window[0], window[1]))
		voice := levelDelta(span(mix, window[0], window[1]), span(speech, window[0], window[1]))
		t.Logf("fixture dialogue %.1f-%.1f seconds bed dip %.6f dB, mix vs speech %.6f dB",
			window[0], window[1], dip, voice)
		if dip > -2 || math.IsNaN(dip) {
			t.Fatalf("dialogue %.1f-%.1f seconds did not duck the bed: %.6f dB", window[0], window[1], dip)
		}
		if voice < -3 || math.IsNaN(voice) {
			t.Fatalf("dialogue %.1f-%.1f seconds attenuated speech by %.6f dB", window[0], window[1], voice)
		}
	}
}

func TestSeparateMusicSkipsDucking(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.wav")
	music := filepath.Join(dir, "music.wav")
	runAudioTool(t, "ffmpeg", "-v", "error", "-f", "lavfi", "-i",
		"anullsrc=r=44100:cl=stereo:d=3", "-c:a", "pcm_f32le", source)
	runAudioTool(t, "ffmpeg", "-v", "error", "-f", "lavfi", "-i",
		"sine=frequency=80:sample_rate=44100:duration=3", "-ac", "2", "-af", "volume=0.15",
		"-c:a", "pcm_f32le", music)
	bedFile := filepath.Join(dir, "bed.wav")
	bed, err := BuildBed(t.Context(), source, music, bedFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bed.SeparateMusic {
		t.Fatal("separate music must skip ducking")
	}
	speech := filepath.Join(dir, "speech.wav")
	runAudioTool(t, "ffmpeg", "-v", "error",
		"-f", "lavfi", "-i", "anullsrc=r=44100:cl=stereo:d=1",
		"-f", "lavfi", "-i", "sine=frequency=2000:sample_rate=44100:duration=1",
		"-f", "lavfi", "-i", "anullsrc=r=44100:cl=stereo:d=1",
		"-filter_complex", "[1]aformat=channel_layouts=stereo,volume=0.4[s];[0][s][2]concat=n=3:v=0:a=1",
		"-c:a", "pcm_f32le", speech)
	duckedFile := filepath.Join(dir, "ducked.wav")
	overlayFile := filepath.Join(dir, "overlay.wav")
	if err := bed.Duck(t.Context(), speech, duckedFile); err != nil {
		t.Fatal(err)
	}
	if err := bed.Overlay(t.Context(), speech, overlayFile); err != nil {
		t.Fatal(err)
	}
	probeBed(t, duckedFile, "44100", "stereo", 2)

	ducked := decodedAudio(t, duckedFile)
	overlay := decodedAudio(t, overlayFile)
	bedSamples := decodedAudio(t, bedFile)
	speechSamples := decodedAudio(t, speech)
	if len(ducked) != len(overlay) {
		t.Fatalf("duck has %d samples, overlay has %d", len(ducked), len(overlay))
	}
	for i, sample := range ducked {
		if sample != overlay[i] {
			t.Fatalf("separate music duck differed from overlay at sample %d", i)
		}
	}
	for _, gap := range [][2]float64{{0.2, 0.8}, {2.2, 2.8}} {
		delta := levelDelta(span(ducked, gap[0], gap[1]), span(bedSamples, gap[0], gap[1]))
		t.Logf("separate music gap %.1f-%.1f seconds %.6f dB", gap[0], gap[1], delta)
		if math.Abs(delta) > 0.5 || math.IsNaN(delta) {
			t.Fatalf("separate music gap changed by %.6f dB", delta)
		}
	}
	under := subSamples(span(ducked, 1.2, 1.8), span(speechSamples, 1.2, 1.8))
	dip := levelDelta(under, span(bedSamples, 1.2, 1.8))
	t.Logf("separate music under speech bed delta %.6f dB", dip)
	if math.Abs(dip) > 0.5 || math.IsNaN(dip) {
		t.Fatalf("separate music ducked the bed by %.6f dB", dip)
	}
}

func TestDuckRejectsInvalidInputsAndPreservesOutputs(t *testing.T) {
	dir := t.TempDir()
	bed := synthBed(t, dir, 1)
	take := synthTone(t, filepath.Join(dir, "take.wav"), 440, 0.2)
	output := filepath.Join(dir, "mix.wav")
	if err := os.WriteFile(output, []byte("previous render"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := bed.Duck(t.Context(), filepath.Join(dir, "missing.wav"), output); err == nil {
		t.Fatal("accepted a missing speech layer")
	}
	data, err := os.ReadFile(output)
	if err != nil || string(data) != "previous render" {
		t.Fatal("failed ducking changed existing output")
	}
	if err := (Bed{}).Duck(t.Context(), take, output); err == nil {
		t.Fatal("accepted an uninitialized bed")
	}
	if err := bed.Duck(t.Context(), "", output); err == nil {
		t.Fatal("accepted empty speech")
	}
	if err := bed.Duck(t.Context(), take, bed.File); err == nil {
		t.Fatal("allowed overwriting the bed")
	}
	if err := bed.Duck(t.Context(), take, take); err == nil {
		t.Fatal("allowed overwriting speech")
	}
	alias := filepath.Join(dir, "alias.wav")
	if err := os.Link(bed.File, alias); err != nil {
		t.Fatal(err)
	}
	if err := bed.Duck(t.Context(), take, alias); err == nil {
		t.Fatal("allowed overwriting the bed alias")
	}
	entries, err := filepath.Glob(filepath.Join(dir, ".assemble-*"))
	if err != nil || len(entries) != 0 {
		t.Fatal("failed ducking leaked temporary files")
	}
}

func probeFormatDuration(t *testing.T, path string) float64 {
	t.Helper()
	data := runAudioTool(t, "ffprobe", "-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1", path)
	sec, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64)
	if err != nil {
		t.Fatalf("parse duration %q: %v", data, err)
	}
	return sec
}

func filteredAudio(t *testing.T, path, filter string) []float32 {
	t.Helper()
	out := filepath.Join(t.TempDir(), "filtered.wav")
	runAudioTool(t, "ffmpeg", "-v", "error", "-i", path, "-af", filter, "-c:a", "pcm_f32le", out)
	return decodedAudio(t, out)
}

func levelDelta(got, want []float32) float64 {
	return 20 * math.Log10(audioRMS(got)/audioRMS(want))
}

func subSamples(a, b []float32) []float32 {
	out := make([]float32, len(a))
	for i := range a {
		out[i] = a[i] - b[i]
	}
	return out
}
