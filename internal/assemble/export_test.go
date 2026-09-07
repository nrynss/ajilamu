package assemble

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExportTestdata(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join("..", "..", "testdata", "clip.mp4")
	bedFile := filepath.Join(dir, "bed.wav")
	bed, err := BuildBed(source, "", bedFile)
	if err != nil {
		t.Fatal(err)
	}
	placed, err := bed.Place(fixtureClips(t), filepath.Join(dir, "speech.wav"))
	if err != nil {
		t.Fatal(err)
	}
	replacedMix := filepath.Join(dir, "replaced.wav")
	duckedMix := filepath.Join(dir, "ducked.wav")
	if err := bed.Overlay(placed.File, replacedMix); err != nil {
		t.Fatal(err)
	}
	if err := bed.Duck(placed.File, duckedMix); err != nil {
		t.Fatal(err)
	}

	replaced := filepath.Join(dir, DubbedReplaced)
	ducked := filepath.Join(dir, DubbedDucked)
	if err := Export(source, replacedMix, replaced); err != nil {
		t.Fatal(err)
	}
	if err := Export(source, duckedMix, ducked); err != nil {
		t.Fatal(err)
	}

	srcVideo := probeVideoStream(t, source)
	srcDur := probeFormatDuration(t, source)
	srcHash := videoStreamHash(t, source)
	srcTail := audioRMS(span(decodedAudio(t, source), 54.5, 75))

	for _, out := range []struct {
		name string
		path string
	}{
		{DubbedReplaced, replaced},
		{DubbedDucked, ducked},
	} {
		if filepath.Base(out.path) != out.name {
			t.Fatalf("export name %q", filepath.Base(out.path))
		}
		probeBed(t, out.path, "44100", "stereo", 2)
		gotDur := probeFormatDuration(t, out.path)
		t.Logf("%s ffprobe duration %.9f source %.9f", out.name, gotDur, srcDur)
		if math.Abs(gotDur-srcDur) > 0.010 {
			t.Fatalf("%s duration %.9f is not within 10 ms of source %.9f", out.name, gotDur, srcDur)
		}

		gotVideo := probeVideoStream(t, out.path)
		t.Logf("%s video codec=%s tag=%s frames=%s", out.name, gotVideo.Codec, gotVideo.Tag, gotVideo.Frames)
		if gotVideo != srcVideo {
			t.Fatalf("%s video %+v does not match source %+v", out.name, gotVideo, srcVideo)
		}
		gotHash := videoStreamHash(t, out.path)
		if gotHash != srcHash {
			t.Fatalf("%s video bitstream %s does not match source %s", out.name, gotHash, srcHash)
		}

		samples := decodedAudio(t, out.path)
		tail := audioRMS(span(samples, 54.5, 75))
		tailDelta := 20 * math.Log10(tail/srcTail)
		t.Logf("%s tail 54.5-75 s RMS %.9f source %.9f delta %.6f dB", out.name, tail, srcTail, tailDelta)
		if tail < 0.01 || math.IsNaN(tail) {
			t.Fatalf("%s tail is digital silence: RMS %.9f", out.name, tail)
		}
		if math.Abs(tailDelta) > 2 || math.IsNaN(tailDelta) {
			t.Fatalf("%s tail left the source by %.6f dB", out.name, tailDelta)
		}
		for _, window := range [][2]float64{{8.5, 12.5}, {39.5, 44}} {
			voice := audioRMS(span(samples, window[0], window[1]))
			t.Logf("%s dialogue %.1f-%.1f s RMS %.9f", out.name, window[0], window[1], voice)
			if voice < 0.02 || math.IsNaN(voice) {
				t.Fatalf("%s dialogue %.1f-%.1f s lacks speech energy: RMS %.9f",
					out.name, window[0], window[1], voice)
			}
		}
	}
}

func TestExportKeepsVideoWhenMixLengthDiffers(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "film.mp4")
	synthFilm(t, source, 2)
	srcVideo := probeVideoStream(t, source)
	srcDur := probeFormatDuration(t, source)
	srcHash := videoStreamHash(t, source)

	for _, seconds := range []float64{0.5, 2.5} {
		t.Run(fmt.Sprintf("%.1fs", seconds), func(t *testing.T) {
			mix := filepath.Join(t.TempDir(), "mix.wav")
			synthMix(t, mix, seconds)
			out := filepath.Join(t.TempDir(), "dubbed_replaced.mp4")
			if err := Export(source, mix, out); err != nil {
				t.Fatal(err)
			}
			gotDur := probeFormatDuration(t, out)
			t.Logf("mix %.1f s export duration %.9f source %.9f", seconds, gotDur, srcDur)
			if math.Abs(gotDur-srcDur) > 0.010 {
				t.Fatalf("duration %.9f is not within 10 ms of source %.9f", gotDur, srcDur)
			}
			gotVideo := probeVideoStream(t, out)
			if gotVideo != srcVideo {
				t.Fatalf("video %+v does not match source %+v", gotVideo, srcVideo)
			}
			if videoStreamHash(t, out) != srcHash {
				t.Fatal("video bitstream changed")
			}
		})
	}
}

func TestExportRejectsInvalidInputsAndPreservesOutputs(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "film.mp4")
	synthFilm(t, source, 1)
	mix := filepath.Join(dir, "mix.wav")
	synthMix(t, mix, 1)
	output := filepath.Join(dir, DubbedReplaced)
	if err := os.WriteFile(output, []byte("previous render"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Export("", mix, output); err == nil {
		t.Fatal("accepted empty video")
	}
	if err := Export(source, "", output); err == nil {
		t.Fatal("accepted empty mix")
	}
	if err := Export(source, filepath.Join(dir, "missing.wav"), output); err == nil {
		t.Fatal("accepted a missing mix")
	}
	data, err := os.ReadFile(output)
	if err != nil || string(data) != "previous render" {
		t.Fatal("failed export changed existing output")
	}
	audioOnly := filepath.Join(dir, "audio-only.wav")
	synthMix(t, audioOnly, 1)
	if err := Export(audioOnly, mix, output); err == nil {
		t.Fatal("accepted a source with no video stream")
	}
	if err := Export(source, mix, source); err == nil {
		t.Fatal("allowed overwriting the video")
	}
	if err := Export(source, mix, mix); err == nil {
		t.Fatal("allowed overwriting the mix")
	}
	alias := filepath.Join(dir, "alias.mp4")
	if err := os.Link(source, alias); err != nil {
		t.Fatal(err)
	}
	if err := Export(source, mix, alias); err == nil {
		t.Fatal("allowed overwriting the video alias")
	}
	entries, err := filepath.Glob(filepath.Join(dir, ".assemble-*"))
	if err != nil || len(entries) != 0 {
		t.Fatal("failed export leaked temporary files")
	}
}

func TestExportNamesNeverClean(t *testing.T) {
	if DubbedReplaced != "dubbed_replaced.mp4" {
		t.Fatalf("replaced name %q", DubbedReplaced)
	}
	if DubbedDucked != "dubbed_ducked.mp4" {
		t.Fatalf("ducked name %q", DubbedDucked)
	}
	for _, name := range []string{DubbedReplaced, DubbedDucked} {
		if strings.Contains(strings.ToLower(name), "clean") {
			t.Fatalf("export name %q uses clean", name)
		}
	}
}

type videoInfo struct {
	Codec  string
	Tag    string
	Frames string
}

func probeVideoStream(t *testing.T, path string) videoInfo {
	t.Helper()
	data := runAudioTool(t, "ffprobe", "-v", "error", "-select_streams", "v:0",
		"-show_entries", "stream=codec_name,codec_tag_string,nb_frames", "-of", "json", path)
	var result struct {
		Streams []struct {
			Codec  string `json:"codec_name"`
			Tag    string `json:"codec_tag_string"`
			Frames string `json:"nb_frames"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Streams) != 1 {
		t.Fatalf("expected one video stream: %s", data)
	}
	s := result.Streams[0]
	return videoInfo{Codec: s.Codec, Tag: s.Tag, Frames: s.Frames}
}

func videoStreamHash(t *testing.T, path string) string {
	t.Helper()
	data := runAudioTool(t, "ffmpeg", "-nostdin", "-v", "error", "-i", path,
		"-map", "0:v", "-c", "copy", "-f", "hash", "-hash", "md5", "-")
	return strings.TrimSpace(string(data))
}

func synthFilm(t *testing.T, path string, seconds float64) {
	t.Helper()
	dur := fmt.Sprintf("%g", seconds)
	runAudioTool(t, "ffmpeg", "-v", "error",
		"-f", "lavfi", "-i", "color=black:s=64x64:r=30:d="+dur,
		"-f", "lavfi", "-i", "sine=frequency=220:sample_rate=44100:duration="+dur,
		"-map", "0:v", "-map", "1:a", "-ac", "2", "-ar", "44100",
		"-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", path)
}

func synthMix(t *testing.T, path string, seconds float64) {
	t.Helper()
	runAudioTool(t, "ffmpeg", "-v", "error", "-f", "lavfi", "-i",
		"sine=frequency=440:sample_rate=44100:duration="+fmt.Sprintf("%g", seconds),
		"-ac", "2", "-c:a", "pcm_f32le", path)
}
