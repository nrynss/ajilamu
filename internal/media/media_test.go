package media

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func findFixture(t *testing.T, rel string) string {
	t.Helper()
	candidates := []string{
		rel,
		filepath.Join("..", "..", rel),
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	t.Fatalf("fixture file not found: %s", rel)
	return ""
}

func TestDurationSeg3Try1(t *testing.T) {
	path := findFixture(t, "testdata/takes/seg_3_try1.wav")
	dur, err := Duration(t.Context(), path)
	if err != nil {
		t.Fatalf("Duration(%s) failed: %v", path, err)
	}
	// ffprobe reports 5.720375 seconds for the committed fixture.
	const wantMs = 5720
	if gotMs := dur.Milliseconds(); gotMs != wantMs {
		t.Errorf("Duration(%s) = %d ms, want %d ms", path, gotMs, wantMs)
	}
}

func TestDurationSeg3Stretched(t *testing.T) {
	path := findFixture(t, "testdata/takes/seg_3_stretched.wav")
	dur, err := Duration(t.Context(), path)
	if err != nil {
		t.Fatalf("Duration(%s) failed: %v", path, err)
	}
	// ffprobe reports 5.337563 seconds for the committed fixture.
	const wantMs = 5338
	if gotMs := dur.Milliseconds(); gotMs != wantMs {
		t.Errorf("Duration(%s) = %d ms, want %d ms", path, gotMs, wantMs)
	}
}

func TestAtempoSlowDownSeg8(t *testing.T) {
	in := findFixture(t, "testdata/takes/seg_8_try1.wav")
	origDur, err := Duration(t.Context(), in)
	if err != nil {
		t.Fatalf("Duration(%s) failed: %v", in, err)
	}

	out := filepath.Join(t.TempDir(), "seg_8_slow.wav")
	// Apply ratio below 1.0 to slow down the take.
	const ratio = 0.8
	if err := Atempo(t.Context(), in, out, ratio); err != nil {
		t.Fatalf("Atempo failed: %v", err)
	}

	slowDur, err := Duration(t.Context(), out)
	if err != nil {
		t.Fatalf("Duration(%s) failed: %v", out, err)
	}

	if slowDur <= origDur {
		t.Errorf("slow take duration %v not longer than original %v", slowDur, origDur)
	}
}

func TestAtempoRatioLimits(t *testing.T) {
	in := findFixture(t, "testdata/takes/seg_8_try1.wav")
	tmpDir := t.TempDir()

	tests := []struct {
		name    string
		ratio   float64
		wantErr bool
	}{
		{name: "below_minimum", ratio: 0.49, wantErr: true},
		{name: "above_maximum", ratio: 2.01, wantErr: true},
		{name: "at_minimum", ratio: 0.5, wantErr: false},
		{name: "at_maximum", ratio: 2.0, wantErr: false},
		{name: "unity_speed", ratio: 1.0, wantErr: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out := filepath.Join(tmpDir, tc.name+".wav")
			err := Atempo(t.Context(), in, out, tc.ratio)
			if tc.wantErr && err == nil {
				t.Errorf("Atempo(ratio=%v) expected error, got nil", tc.ratio)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("Atempo(ratio=%v) unexpected error: %v", tc.ratio, err)
			}
		})
	}
}

func TestAudioFormat(t *testing.T) {
	t.Run("wav_take", func(t *testing.T) {
		path := findFixture(t, "testdata/takes/seg_3_try1.wav")
		format, err := AudioFormat(t.Context(), path)
		if err != nil {
			t.Fatalf("AudioFormat(%s) failed: %v", path, err)
		}
		if format.Codec != "pcm_s16le" {
			t.Errorf("Codec = %q, want pcm_s16le", format.Codec)
		}
		if format.SampleRate != 16000 {
			t.Errorf("SampleRate = %d, want 16000", format.SampleRate)
		}
		if format.Channels != 1 {
			t.Errorf("Channels = %d, want 1", format.Channels)
		}
		if format.ChannelLayout != "mono" {
			t.Errorf("ChannelLayout = %q, want mono", format.ChannelLayout)
		}
		if format.BitRate != 256000 {
			t.Errorf("BitRate = %d, want 256000", format.BitRate)
		}
	})

	t.Run("source_clip", func(t *testing.T) {
		path := findFixture(t, "testdata/clip.mp4")
		format, err := AudioFormat(t.Context(), path)
		if err != nil {
			t.Fatalf("AudioFormat(%s) failed: %v", path, err)
		}
		if format.Codec != "aac" {
			t.Errorf("Codec = %q, want aac", format.Codec)
		}
		if format.SampleRate != 44100 {
			t.Errorf("SampleRate = %d, want 44100", format.SampleRate)
		}
		if format.Channels != 2 {
			t.Errorf("Channels = %d, want 2", format.Channels)
		}
		if format.ChannelLayout != "stereo" {
			t.Errorf("ChannelLayout = %q, want stereo", format.ChannelLayout)
		}
		if format.BitRate <= 0 {
			t.Errorf("BitRate = %d, want positive value", format.BitRate)
		}
	})
}

func TestDemux(t *testing.T) {
	videoPath := findFixture(t, "testdata/clip.mp4")
	outWav := filepath.Join(t.TempDir(), "extracted.wav")

	if err := Demux(t.Context(), videoPath, outWav); err != nil {
		t.Fatalf("Demux failed: %v", err)
	}

	format, err := AudioFormat(t.Context(), outWav)
	if err != nil {
		t.Fatalf("AudioFormat on demuxed audio failed: %v", err)
	}

	if format.Codec != "pcm_s16le" {
		t.Errorf("Codec = %q, want pcm_s16le", format.Codec)
	}
	if format.SampleRate != 16000 {
		t.Errorf("SampleRate = %d, want 16000", format.SampleRate)
	}
	if format.Channels != 1 {
		t.Errorf("Channels = %d, want 1", format.Channels)
	}

	dur, err := Duration(t.Context(), outWav)
	if err != nil {
		t.Fatalf("Duration on demuxed audio failed: %v", err)
	}
	if dur <= 0 {
		t.Errorf("Duration = %v, want positive duration", dur)
	}
}

func TestRunError(t *testing.T) {
	err := Run(t.Context(), "-invalid_option_unknown_flag")
	if err == nil {
		t.Fatal("expected error on invalid ffmpeg options, got nil")
	}
}

// blockingProbeInput listens on a random loopback port and never writes.
// It returns a probe URL and a wait function for the incoming connection.
func blockingProbeInput(t *testing.T) (string, func() net.Conn) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { listener.Close() })
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			accepted <- conn
		}
	}()
	wait := func() net.Conn {
		t.Helper()
		select {
		case conn := <-accepted:
			return conn
		case <-time.After(10 * time.Second):
			t.Fatal("ffprobe did not connect to the blocking input")
			return nil
		}
	}
	return "tcp://" + listener.Addr().String(), wait
}

func waitProbeResult(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(10 * time.Second):
		t.Fatal("Duration did not return after the context ended")
		return nil
	}
}

func assertPeerGone(t *testing.T, conn net.Conn) {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	if _, err := conn.Read(make([]byte, 1)); err == nil {
		t.Fatal("ffprobe connection stayed open after the context ended")
	}
}

func TestDurationCancelsRunningProbe(t *testing.T) {
	url, wait := blockingProbeInput(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := Duration(ctx, url)
		done <- err
	}()
	conn := wait()
	defer conn.Close()
	start := time.Now()
	cancel()
	err := waitProbeResult(t, done)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Duration error = %v, want context.Canceled", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("Duration waited %v after cancellation", elapsed)
	}
	assertPeerGone(t, conn)
}

func TestDurationHonoursDeadline(t *testing.T) {
	url, wait := blockingProbeInput(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := Duration(ctx, url)
		done <- err
	}()
	conn := wait()
	defer conn.Close()
	err := waitProbeResult(t, done)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Duration error = %v, want context.DeadlineExceeded", err)
	}
	assertPeerGone(t, conn)
}
