package assemble

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/nrynss/ajilamu/internal/media"
)

// DubbedReplaced is the Overlay mix muxed onto the source video.
const DubbedReplaced = "dubbed_replaced.mp4"

// DubbedDucked is the Duck mix muxed onto the source video.
const DubbedDucked = "dubbed_ducked.mp4"

// Export muxes a finished mix onto the source video.
// Video packets are copied. Audio encodes at the film rate and channel count.
// The output lasts as long as the video. The command does not use -shortest.
func Export(ctx context.Context, video, audio, output string) error {
	if video == "" || audio == "" {
		return fmt.Errorf("export needs a video file and a mix")
	}
	if err := distinctOutput(output, video, audio); err != nil {
		return err
	}
	if err := requireVideoStream(ctx, video); err != nil {
		return err
	}
	format, err := filmAudioFormat(ctx, video, audio)
	if err != nil {
		return err
	}
	if err := validateBedFormat(format); err != nil {
		return err
	}
	duration, err := probeFormatSeconds(ctx, video)
	if err != nil {
		return fmt.Errorf("probe video duration: %w", err)
	}
	if duration <= 0 {
		return fmt.Errorf("video duration must be positive")
	}
	return renderExport(ctx, video, audio, output, format, duration)
}

func filmAudioFormat(ctx context.Context, video, audio string) (media.Format, error) {
	if format, err := media.AudioFormat(ctx, video); err == nil {
		return format, nil
	}
	format, err := media.AudioFormat(ctx, audio)
	if err != nil {
		return media.Format{}, fmt.Errorf("probe export audio: %w", err)
	}
	return format, nil
}

func requireVideoStream(ctx context.Context, path string) error {
	out, err := probeTool(ctx, "-v", "error", "-select_streams", "v:0",
		"-show_entries", "stream=codec_type", "-of", "csv=p=0", path)
	if err != nil {
		return fmt.Errorf("probe export video: %w", err)
	}
	if out != "video" {
		return fmt.Errorf("export source %q has no video stream", path)
	}
	return nil
}

func probeFormatSeconds(ctx context.Context, path string) (float64, error) {
	raw, err := probeTool(ctx, "-v", "error", "-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1", path)
	if err != nil {
		return 0, err
	}
	if raw == "" || raw == "N/A" {
		return 0, fmt.Errorf("duration missing for %s", path)
	}
	sec, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("parse duration %q for %s: %w", raw, path, err)
	}
	return sec, nil
}

func probeTool(ctx context.Context, args ...string) (string, error) {
	cmd := media.Command(ctx, "ffprobe", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", fmt.Errorf("ffprobe cancelled: %w", ctxErr)
		}
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return "", fmt.Errorf("ffprobe failed: %s (%w)", msg, err)
		}
		return "", fmt.Errorf("ffprobe failed: %w", err)
	}
	return strings.TrimSpace(stdout.String()), nil
}

func renderExport(ctx context.Context, video, audio, output string, format media.Format, duration float64) error {
	ext := filepath.Ext(output)
	if ext == "" {
		ext = ".mp4"
	}
	tmp, err := os.CreateTemp(filepath.Dir(output), ".assemble-*"+ext)
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Close(); err != nil {
		return err
	}
	args := []string{
		"-nostdin", "-hide_banner", "-loglevel", "error", "-y",
		"-i", video, "-i", audio,
		"-map", "0:v", "-map", "1:a",
		"-c:v", "copy",
		"-c:a", "aac",
		"-ar", strconv.Itoa(format.SampleRate),
		"-ac", strconv.Itoa(format.Channels),
	}
	if format.ChannelLayout != "" && format.ChannelLayout != "unknown" {
		args = append(args, "-channel_layout", format.ChannelLayout)
	}
	args = append(args, "-t", strconv.FormatFloat(duration, 'f', 9, 64), tmpPath)
	if err := media.Run(ctx, args...); err != nil {
		return fmt.Errorf("export mux: %w", err)
	}
	return os.Rename(tmpPath, output)
}
