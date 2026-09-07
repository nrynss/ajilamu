package assemble

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestFixtureTakePeaks(t *testing.T) {
	dir := filepath.Join("..", "..", "testdata", "takes")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var files int
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		files++
		path := filepath.Join(dir, entry.Name())
		got, err := Peaks(path)
		if err != nil {
			t.Fatalf("%s: %v", entry.Name(), err)
		}
		if len(got) < 64 || len(got) > 128 {
			t.Fatalf("%s: length %d outside [64, 128]", entry.Name(), len(got))
		}
		again, err := Peaks(path)
		if err != nil {
			t.Fatalf("%s second read: %v", entry.Name(), err)
		}
		if !slices.Equal(got, again) {
			t.Fatalf("%s: repeat Peaks differed", entry.Name())
		}
		want := independentPeaks(t, path, len(got))
		if maxPeakDelta(got, want) > 1 {
			t.Fatalf("%s: peaks drift from independent buckets: got %v want %v", entry.Name(), got, want)
		}
		if slices.Max(got) == 0 {
			t.Fatalf("%s: expected waveform energy, got a zero vector", entry.Name())
		}
	}
	if files != 10 {
		t.Fatalf("expected 10 takes, found %d", files)
	}
}

func TestSyntheticPeakDynamics(t *testing.T) {
	dir := t.TempDir()
	silence := filepath.Join(dir, "silence.wav")
	runAudioTool(t, "ffmpeg", "-v", "error", "-f", "lavfi", "-i",
		"anullsrc=r=16000:cl=mono:d=1", "-c:a", "pcm_s16le", silence)
	got, err := Peaks(silence)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != PeakCount {
		t.Fatalf("silence length %d, want %d", len(got), PeakCount)
	}
	for i, v := range got {
		if v != 0 {
			t.Fatalf("silence bin %d = %d, want 0", i, v)
		}
	}

	tone := filepath.Join(dir, "tone.wav")
	runAudioTool(t, "ffmpeg", "-v", "error", "-f", "lavfi", "-i",
		"aevalsrc=sin(2*PI*440*t):s=16000:d=1", "-c:a", "pcm_f32le", tone)
	got, err = Peaks(tone)
	if err != nil {
		t.Fatal(err)
	}
	for i, v := range got {
		if v < 250 {
			t.Fatalf("full-scale tone bin %d = %d, want near 255", i, v)
		}
	}

	mixed := filepath.Join(dir, "tone_then_silence.wav")
	synthToneSilence(t, mixed, 440, 0.5, 0.5)
	got, err = Peaks(mixed)
	if err != nil {
		t.Fatal(err)
	}
	early := meanPeak(got[:PeakCount/3])
	late := meanPeak(got[PeakCount*2/3:])
	if early < 200 {
		t.Fatalf("tone half mean %d, want high peaks", early)
	}
	if late > 5 {
		t.Fatalf("silence half mean %d, want a fall toward zero", late)
	}
	if !laterBinsFall(got) {
		t.Fatalf("tone then silence did not fall: %v", got)
	}
}

func TestPeaksRejectsInvalidPath(t *testing.T) {
	if _, err := Peaks(""); err == nil {
		t.Fatal("empty path must fail")
	}
	if _, err := Peaks(filepath.Join(t.TempDir(), "missing.wav")); err == nil {
		t.Fatal("missing take must fail")
	}
	if _, err := Peaks(t.TempDir()); err == nil {
		t.Fatal("directory path must fail")
	}
}

func independentPeaks(t *testing.T, path string, bins int) []uint8 {
	t.Helper()
	channels := probeChannels(t, path)
	samples := decodedAudio(t, path)
	frames := len(samples) / channels
	absFrames := make([]float32, frames)
	for frame := 0; frame < frames; frame++ {
		var peak float32
		base := frame * channels
		for ch := 0; ch < channels; ch++ {
			v := samples[base+ch]
			if v < 0 {
				v = -v
			}
			if v > peak {
				peak = v
			}
		}
		absFrames[frame] = peak
	}
	buckets := make([]float32, bins)
	for i := 0; i < bins; i++ {
		start := i * frames / bins
		end := (i + 1) * frames / bins
		var peak float32
		for _, v := range absFrames[start:end] {
			if v > peak {
				peak = v
			}
		}
		buckets[i] = peak
	}
	var peak float32
	for _, v := range buckets {
		if v > peak {
			peak = v
		}
	}
	out := make([]uint8, bins)
	if peak == 0 {
		return out
	}
	scale := 255 / float64(peak)
	for i, v := range buckets {
		n := math.Round(float64(v) * scale)
		if n > 255 {
			n = 255
		}
		out[i] = uint8(n)
	}
	return out
}

func probeChannels(t *testing.T, path string) int {
	t.Helper()
	data := runAudioTool(t, "ffprobe", "-v", "error", "-select_streams", "a:0",
		"-show_entries", "stream=channels", "-of", "json", path)
	var result struct {
		Streams []struct {
			Channels int `json:"channels"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Streams) != 1 || result.Streams[0].Channels <= 0 {
		t.Fatalf("channel probe failed: %s", data)
	}
	return result.Streams[0].Channels
}

func maxPeakDelta(got, want []uint8) int {
	if len(got) != len(want) {
		return math.MaxInt
	}
	var max int
	for i := range got {
		d := int(got[i]) - int(want[i])
		if d < 0 {
			d = -d
		}
		if d > max {
			max = d
		}
	}
	return max
}

func meanPeak(values []uint8) int {
	if len(values) == 0 {
		return 0
	}
	var sum int
	for _, v := range values {
		sum += int(v)
	}
	return sum / len(values)
}

func laterBinsFall(values []uint8) bool {
	n := len(values)
	if n < 4 {
		return false
	}
	return meanPeak(values[n/2:]) < meanPeak(values[:n/2])/2
}

func TestPeakCountBound(t *testing.T) {
	if PeakCount < 64 || PeakCount > 128 {
		t.Fatalf("PeakCount %d outside [64, 128]", PeakCount)
	}
}
