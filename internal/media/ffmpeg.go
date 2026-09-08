package media

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// waitDelay bounds how long Wait blocks on an inherited pipe after the context kills a child.
// Five seconds lets ffmpeg flush a partial file and keeps shutdown prompt.
const waitDelay = 5 * time.Second

// Command builds an ffmpeg or ffprobe command bound to ctx.
// The context kills the child, and waitDelay bounds any inherited pipe wait.
func Command(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = waitDelay
	return cmd
}

// Run executes an ffmpeg command and captures standard error.
func Run(ctx context.Context, args ...string) error {
	cmd := Command(ctx, "ffmpeg", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("ffmpeg %s cancelled: %w", strings.Join(args, " "), ctxErr)
		}
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return fmt.Errorf("ffmpeg %s failed: %s (%w)", strings.Join(args, " "), msg, err)
		}
		return fmt.Errorf("ffmpeg %s failed: %w", strings.Join(args, " "), err)
	}
	return nil
}

// Demux extracts 16 kHz mono audio for speech analysis.
func Demux(ctx context.Context, video, wav string) error {
	return Run(ctx, "-y", "-i", video, "-vn", "-ac", "1", "-ar", "16000", "-c:a", "pcm_s16le", wav)
}

// Atempo applies time stretching while preserving audio pitch.
// The ratio must stay within the supported range of 0.5 to 2.0.
func Atempo(ctx context.Context, in, out string, ratio float64) error {
	if ratio < 0.5 || ratio > 2.0 {
		return fmt.Errorf("atempo ratio %g outside valid range [0.5, 2.0]", ratio)
	}
	filter := fmt.Sprintf("atempo=%.4f", ratio)
	return Run(ctx, "-y", "-i", in, "-filter:a", filter, out)
}
