package media

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// Run executes an ffmpeg command and captures standard error.
func Run(args ...string) error {
	cmd := exec.Command("ffmpeg", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return fmt.Errorf("ffmpeg %s failed: %s (%w)", strings.Join(args, " "), msg, err)
		}
		return fmt.Errorf("ffmpeg %s failed: %w", strings.Join(args, " "), err)
	}
	return nil
}

// Demux extracts 16 kHz mono audio for speech analysis.
func Demux(video, wav string) error {
	return Run("-y", "-i", video, "-vn", "-ac", "1", "-ar", "16000", "-c:a", "pcm_s16le", wav)
}

// Atempo applies time stretching while preserving audio pitch.
// The ratio must stay within the supported range of 0.5 to 2.0.
func Atempo(in, out string, ratio float64) error {
	if ratio < 0.5 || ratio > 2.0 {
		return fmt.Errorf("atempo ratio %g outside valid range [0.5, 2.0]", ratio)
	}
	filter := fmt.Sprintf("atempo=%.4f", ratio)
	return Run("-y", "-i", in, "-filter:a", filter, out)
}
