package media

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// Format captures audio stream properties.
type Format struct {
	SampleRate    int
	Channels      int
	ChannelLayout string
	Codec         string
	BitRate       int64
}

type ffprobeStream struct {
	CodecName     string `json:"codec_name"`
	SampleRate    string `json:"sample_rate"`
	Channels      int    `json:"channels"`
	ChannelLayout string `json:"channel_layout"`
	BitRate       string `json:"bit_rate"`
	BitsPerSample int    `json:"bits_per_sample"`
}

type ffprobeJSON struct {
	Streams []ffprobeStream `json:"streams"`
	Format  struct {
		BitRate string `json:"bit_rate"`
	} `json:"format"`
}

// Duration queries container or stream duration using ffprobe.
// The result rounds to the nearest millisecond.
func Duration(ctx context.Context, path string) (time.Duration, error) {
	cmd := Command(ctx, "ffprobe",
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		path,
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return 0, fmt.Errorf("ffprobe duration %s cancelled: %w", path, ctxErr)
		}
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return 0, fmt.Errorf("ffprobe duration %s: %s (%w)", path, msg, err)
		}
		return 0, fmt.Errorf("ffprobe duration %s: %w", path, err)
	}

	raw := strings.TrimSpace(stdout.String())
	if raw == "" || raw == "N/A" {
		// Fall back to the first audio stream when container duration is missing.
		streamCmd := Command(ctx, "ffprobe",
			"-v", "error",
			"-select_streams", "a:0",
			"-show_entries", "stream=duration",
			"-of", "default=noprint_wrappers=1:nokey=1",
			path,
		)
		var streamStdout, streamStderr bytes.Buffer
		streamCmd.Stdout = &streamStdout
		streamCmd.Stderr = &streamStderr
		if err := streamCmd.Run(); err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return 0, fmt.Errorf("ffprobe stream duration %s cancelled: %w", path, ctxErr)
			}
			msg := strings.TrimSpace(streamStderr.String())
			if msg != "" {
				return 0, fmt.Errorf("ffprobe stream duration %s: %s (%w)", path, msg, err)
			}
			return 0, fmt.Errorf("ffprobe stream duration %s: %w", path, err)
		}
		raw = strings.TrimSpace(streamStdout.String())
	}

	if raw == "" || raw == "N/A" {
		return 0, fmt.Errorf("ffprobe duration %s: duration missing", path)
	}

	sec, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("parse duration %q for %s: %w", raw, path, err)
	}

	ms := math.Round(sec * 1000)
	return time.Duration(ms) * time.Millisecond, nil
}

// AudioFormat queries audio stream properties using ffprobe.
func AudioFormat(ctx context.Context, path string) (Format, error) {
	cmd := Command(ctx, "ffprobe",
		"-v", "error",
		"-select_streams", "a:0",
		"-show_streams",
		"-show_format",
		"-of", "json",
		path,
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return Format{}, fmt.Errorf("ffprobe audio format %s cancelled: %w", path, ctxErr)
		}
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return Format{}, fmt.Errorf("ffprobe audio format %s: %s (%w)", path, msg, err)
		}
		return Format{}, fmt.Errorf("ffprobe audio format %s: %w", path, err)
	}

	var parsed ffprobeJSON
	if err := json.Unmarshal(stdout.Bytes(), &parsed); err != nil {
		return Format{}, fmt.Errorf("decode ffprobe output for %s: %w", path, err)
	}

	if len(parsed.Streams) == 0 {
		return Format{}, fmt.Errorf("no audio stream found in %s", path)
	}

	stream := parsed.Streams[0]
	sampleRate, err := strconv.Atoi(stream.SampleRate)
	if err != nil {
		return Format{}, fmt.Errorf("invalid sample rate %q in %s: %w", stream.SampleRate, path, err)
	}

	layout := stream.ChannelLayout
	if layout == "" || layout == "unknown" {
		switch stream.Channels {
		case 1:
			layout = "mono"
		case 2:
			layout = "stereo"
		}
	}

	var bitRate int64
	if stream.BitRate != "" && stream.BitRate != "N/A" {
		bitRate, _ = strconv.ParseInt(stream.BitRate, 10, 64)
	}
	if bitRate == 0 && stream.BitsPerSample > 0 && sampleRate > 0 && stream.Channels > 0 {
		bitRate = int64(stream.BitsPerSample * sampleRate * stream.Channels)
	}
	if bitRate == 0 && len(parsed.Streams) == 1 && parsed.Format.BitRate != "" && parsed.Format.BitRate != "N/A" {
		bitRate, _ = strconv.ParseInt(parsed.Format.BitRate, 10, 64)
	}

	return Format{
		SampleRate:    sampleRate,
		Channels:      stream.Channels,
		ChannelLayout: layout,
		Codec:         stream.CodecName,
		BitRate:       bitRate,
	}, nil
}
