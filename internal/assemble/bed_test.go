package assemble

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func runAudioTool(t *testing.T, name string, args ...string) []byte {
	t.Helper()
	cmd := exec.Command(name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("%s failed: %v: %s", name, err, stderr.String())
	}
	return out
}

func probeBed(t *testing.T, path, rate, layout string, channels int) {
	t.Helper()
	data := runAudioTool(t, "ffprobe", "-v", "error", "-select_streams", "a:0",
		"-show_entries", "stream=sample_rate,channels,channel_layout", "-of", "json", path)
	var result struct {
		Streams []struct {
			SampleRate string `json:"sample_rate"`
			Channels   int    `json:"channels"`
			Layout     string `json:"channel_layout"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Streams) != 1 {
		t.Fatalf("expected one audio stream: %s", data)
	}
	s := result.Streams[0]
	// Basic WAV headers imply mono or stereo without an explicit channel mask.
	if s.Layout == "" {
		if s.Channels == 1 {
			s.Layout = "mono"
		} else if s.Channels == 2 {
			s.Layout = "stereo"
		}
	}
	if s.SampleRate != rate || s.Channels != channels || s.Layout != layout {
		t.Fatalf("unexpected audio format: %s", data)
	}
}

func decodedAudio(t *testing.T, path string) []float32 {
	t.Helper()
	data := runAudioTool(t, "ffmpeg", "-nostdin", "-v", "error", "-i", path,
		"-map", "0:a:0", "-f", "f32le", "-c:a", "pcm_f32le", "-")
	values := make([]float32, len(data)/4)
	for i := range values {
		values[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[i*4:]))
	}
	return values
}

func audioRMS(samples []float32) float64 {
	var energy float64
	for _, sample := range samples {
		energy += float64(sample) * float64(sample)
	}
	return math.Sqrt(energy / float64(len(samples)))
}

func TestBedPreservesFixture(t *testing.T) {
	source := "../../testdata/clip.mp4"
	output := filepath.Join(t.TempDir(), "bed.wav")
	bed, err := BuildBed(source, "", output)
	if err != nil {
		t.Fatal(err)
	}
	if bed.SeparateMusic {
		t.Fatal("source bed marked as separate music")
	}
	probeBed(t, output, "44100", "stereo", 2)
	want, got := decodedAudio(t, source), decodedAudio(t, output)
	seconds := float64(len(got)) / (44100 * 2)
	if math.Abs(seconds-75.008267) > 0.001 {
		t.Fatalf("bed duration %.9f does not match film", seconds)
	}
	for _, interval := range [][2]int{{0, 54}, {54, 75}} {
		start, end := interval[0]*44100*2, interval[1]*44100*2
		delta := 20 * math.Log10(audioRMS(got[start:end])/audioRMS(want[start:end]))
		t.Logf("interval %d-%d seconds measured level delta %.6f dB", interval[0], interval[1], delta)
		if math.Abs(delta) > 0.5 || math.IsNaN(delta) {
			t.Fatalf("bed level differs by %.6f dB", delta)
		}
		for i := start; i < end; i++ {
			if got[i] != want[i] {
				t.Fatalf("bed changed decoded source sample %d", i)
			}
		}
	}
	for _, name := range []string{"seg_1_try1.wav", "seg_2_try1.wav", "seg_3_try1.wav", "seg_3_stretched.wav", "seg_4_try1.wav", "seg_4_stretched.wav", "seg_5_try1.wav", "seg_6_try1.wav", "seg_7_try1.wav", "seg_8_try1.wav"} {
		t.Run(name, func(t *testing.T) {
			input := filepath.Join("../../testdata/takes", name)
			converted := filepath.Join(t.TempDir(), name)
			if err := bed.PrepareTake(input, converted); err != nil {
				t.Fatal(err)
			}
			probeBed(t, converted, "44100", "stereo", 2)
			original, prepared := decodedAudio(t, input), decodedAudio(t, converted)
			if math.Abs(float64(len(original))/16000-float64(len(prepared))/(44100*2)) > 1.0/16000 {
				t.Fatal("resampling changed take duration")
			}
			delta := 20 * math.Log10(audioRMS(prepared)/audioRMS(original))
			if math.Abs(delta) > 0.1 || math.IsNaN(delta) {
				t.Fatalf("take level changed %.6f dB", delta)
			}
			for i := 0; i < len(prepared); i += 2 {
				if prepared[i] != prepared[i+1] {
					t.Fatal("mono speech did not duplicate into both stereo channels")
				}
			}
		})
	}
}

func TestSeparateMusicAndNativeFormat(t *testing.T) {
	dir := t.TempDir()
	source, music, output := filepath.Join(dir, "source.wav"), filepath.Join(dir, "music.wav"), filepath.Join(dir, "bed.wav")
	runAudioTool(t, "ffmpeg", "-v", "error", "-f", "lavfi", "-i",
		"aevalsrc=0.1|0.2|0.3|0.4|0.5|0.6:s=48000:d=2:c=5.1", "-c:a", "pcm_f32le", source)
	runAudioTool(t, "ffmpeg", "-v", "error", "-f", "lavfi", "-i",
		"aevalsrc=0.06|0.05|0.04|0.03|0.02|0.01:s=48000:d=1:c=5.1", "-c:a", "pcm_f32le", music)
	bed, err := BuildBed(source, music, output)
	if err != nil {
		t.Fatal(err)
	}
	if !bed.SeparateMusic {
		t.Fatal("separate music must bypass later ducking")
	}
	probeBed(t, output, "48000", "5.1", 6)
	want, got := decodedAudio(t, music), decodedAudio(t, output)
	if len(got) != 48000*2*6 {
		t.Fatalf("music bed has %d samples", len(got))
	}
	for i, sample := range got {
		expected := float32(0)
		if i < len(want) {
			expected = want[i]
		}
		if sample != expected {
			t.Fatalf("music changed or padding was not silent at sample %d", i)
		}
	}
	// Swapping the inputs exercises trimming without mixing the source soundtrack.
	if _, err := BuildBed(music, source, output); err != nil {
		t.Fatal(err)
	}
	want, got = decodedAudio(t, source), decodedAudio(t, output)
	if len(got) != 48000*6 {
		t.Fatalf("long music did not trim to the film duration: %d", len(got))
	}
	for i, sample := range got {
		if sample != want[i] {
			t.Fatalf("long music changed at sample %d", i)
		}
	}
}

func TestBedPreservesSourceTimestamps(t *testing.T) {
	for _, tc := range []struct {
		name   string
		filter string
	}{
		{"delayed", "asetpts=PTS+1/TB"},
		{"gap", "aselect='not(between(t,0.5,1.5))',asetpts=PTS+1/TB"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			source, output := filepath.Join(dir, "source.mkv"), filepath.Join(dir, "bed.wav")
			runAudioTool(t, "ffmpeg", "-v", "error", "-f", "lavfi", "-i",
				"color=black:s=32x32:r=10:d=4", "-f", "lavfi", "-i",
				"sine=frequency=440:sample_rate=44100:duration=2", "-af", tc.filter,
				"-map", "0:v", "-map", "1:a", "-ac", "2", "-c:v", "ffv1", "-c:a", "pcm_s16le", source)
			if _, err := BuildBed(source, "", output); err != nil {
				t.Fatal(err)
			}
			probeBed(t, output, "44100", "stereo", 2)
			samples := decodedAudio(t, output)
			if len(samples) != 4*44100*2 {
				t.Fatalf("bed has %d samples", len(samples))
			}
			for _, span := range []struct {
				start, end float64
				silent     bool
			}{
				{0, 0.9, true},
				{1.1, 1.4, false},
				{1.6, 2.4, tc.name == "gap"},
				{2.6, 2.9, false},
				{3.1, 4, true},
			} {
				rms := audioRMS(samples[int(span.start*44100)*2 : int(span.end*44100)*2])
				t.Logf("%.1f-%.1f seconds measured RMS %.6f", span.start, span.end, rms)
				if (span.silent && rms > 0.000001) || (!span.silent && rms < 0.05) {
					t.Fatalf("%.1f-%.1f seconds: RMS %.6f, want silent=%t", span.start, span.end, rms, span.silent)
				}
			}
		})
	}
}

func TestBedPreservesShortSourceTimestampGap(t *testing.T) {
	dir := t.TempDir()
	source, output := filepath.Join(dir, "shortgap.mkv"), filepath.Join(dir, "bed.wav")
	runAudioTool(t, "ffmpeg", "-v", "error", "-f", "lavfi", "-i",
		"color=black:s=32x32:r=100:d=3", "-f", "lavfi", "-i",
		"sine=frequency=440:sample_rate=48000:duration=2", "-af",
		"asetnsamples=n=480,asetpts=PTS+gte(T\\,1)*0.05/TB",
		"-map", "0:v", "-map", "1:a", "-ac", "2", "-c:v", "ffv1", "-c:a", "pcm_s16le", source)
	packets := runAudioTool(t, "ffprobe", "-v", "error", "-select_streams", "a:0",
		"-show_entries", "packet=pts_time,duration_time", "-of", "json", source)
	var timing struct {
		Packets []struct {
			PTS      string `json:"pts_time"`
			Duration string `json:"duration_time"`
		} `json:"packets"`
	}
	if err := json.Unmarshal(packets, &timing); err != nil {
		t.Fatal(err)
	}
	if len(timing.Packets) != 200 || timing.Packets[99].PTS != "0.990000" ||
		timing.Packets[99].Duration != "0.010000" || timing.Packets[100].PTS != "1.050000" {
		t.Fatalf("source packets do not establish the 50 ms gap: %s", packets)
	}
	if _, err := BuildBed(source, "", output); err != nil {
		t.Fatal(err)
	}
	probeBed(t, output, "48000", "stereo", 2)
	want, got := decodedAudio(t, source), decodedAudio(t, output)
	const samplesPerSecond = 48000 * 2
	const gapSamples = samplesPerSecond / 20
	if len(want) != 2*samplesPerSecond || len(got) != 3*samplesPerSecond {
		t.Fatalf("source has %d samples, bed has %d", len(want), len(got))
	}
	gapRMS := audioRMS(got[samplesPerSecond : samplesPerSecond+gapSamples])
	lateRMS := audioRMS(got[2*samplesPerSecond : 2*samplesPerSecond+gapSamples])
	t.Logf("1.000-1.050 seconds RMS %.9f, 2.000-2.050 seconds RMS %.9f", gapRMS, lateRMS)
	if gapRMS != 0 || lateRMS < 0.05 {
		t.Errorf("gap RMS %.9f must be zero and later audio RMS %.9f must exceed 0.05", gapRMS, lateRMS)
	}
	// Compare every decoded sample against the source with exactly 50 ms inserted.
	for i, sample := range got {
		expected := float32(0)
		switch {
		case i < samplesPerSecond:
			expected = want[i]
		case i >= samplesPerSecond+gapSamples && i < len(want)+gapSamples:
			expected = want[i-gapSamples]
		}
		if sample != expected {
			t.Fatalf("sample %d at %.9f seconds is %g, want %g", i, float64(i)/samplesPerSecond, sample, expected)
		}
	}
}

func TestSeparateMonoMusicPreservesLevel(t *testing.T) {
	dir := t.TempDir()
	source, music, output := filepath.Join(dir, "source.wav"), filepath.Join(dir, "mono.wav"), filepath.Join(dir, "bed.wav")
	runAudioTool(t, "ffmpeg", "-v", "error", "-f", "lavfi", "-i",
		"anullsrc=r=44100:cl=stereo", "-t", "2", source)
	runAudioTool(t, "ffmpeg", "-v", "error", "-f", "lavfi", "-i",
		"sine=frequency=440:sample_rate=16000:duration=2", "-c:a", "pcm_s16le", music)
	if _, err := BuildBed(source, music, output); err != nil {
		t.Fatal(err)
	}
	probeBed(t, output, "44100", "stereo", 2)
	want, got := decodedAudio(t, music), decodedAudio(t, output)
	if len(got) != 2*44100*2 {
		t.Fatalf("music bed has %d samples", len(got))
	}
	delta := 20 * math.Log10(audioRMS(got)/audioRMS(want))
	t.Logf("separate mono music measured level delta %.6f dB", delta)
	if math.Abs(delta) > 0.1 || math.IsNaN(delta) {
		t.Fatalf("separate mono music level changed %.6f dB", delta)
	}
	for i := 0; i < len(got); i += 2 {
		if got[i] != got[i+1] {
			t.Fatal("mono music did not duplicate into both stereo channels")
		}
	}
}

func TestBedRejectsInvalidInputsAndPreservesOutputs(t *testing.T) {
	dir := t.TempDir()
	source := "../../testdata/clip.mp4"
	output := filepath.Join(dir, "bed.wav")
	if _, err := BuildBed(source, "", source); err == nil {
		t.Fatal("allowed overwriting source")
	}
	// A hard link cannot cross a filesystem boundary. t.TempDir often sits on a
	// different filesystem from the repository, so linking the fixture directly
	// fails with EXDEV on both Linux and macOS. Copy the clip into the temporary
	// directory first, then link the copy beside it.
	localSource := filepath.Join(dir, "source.mp4")
	clip, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(localSource, clip, 0600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(dir, "alias.mp4")
	if err := os.Link(localSource, alias); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildBed(localSource, "", alias); err == nil {
		t.Fatal("allowed overwriting source alias")
	}
	if err := os.WriteFile(output, []byte("previous render"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildBed(source, filepath.Join(dir, "missing.wav"), output); err == nil {
		t.Fatal("accepted missing music")
	}
	data, err := os.ReadFile(output)
	if err != nil || string(data) != "previous render" {
		t.Fatal("failed rendering changed existing output")
	}
	entries, err := filepath.Glob(filepath.Join(dir, ".assemble-*"))
	if err != nil || len(entries) != 0 {
		t.Fatal("failed rendering leaked temporary files")
	}
	if err := (Bed{}).PrepareTake(source, output); err == nil {
		t.Fatal("accepted uninitialized bed")
	}
}
