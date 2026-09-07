// Package assemble builds audio layers at the source film's native format.
package assemble

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/nrynss/ajilamu/internal/media"
)

// Bed describes a full-length, unity-gain background WAV.
type Bed struct {
	File          string
	Format        media.Format
	Duration      time.Duration
	SeparateMusic bool
}

// BuildBed preserves source audio, or substitutes creator-supplied music when music is nonempty.
// Output is a float PCM WAV with the source sample rate, channels, and layout.
// Short music receives silence padding. Long music ends at the source duration.
// SeparateMusic tells the mixer to skip ducking for creator-supplied tracks.
func BuildBed(source, music, output string) (Bed, error) {
	format, err := media.AudioFormat(source)
	if err != nil {
		return Bed{}, fmt.Errorf("probe bed source: %w", err)
	}
	if err := validateBedFormat(format); err != nil {
		return Bed{}, err
	}
	duration, err := media.Duration(source)
	if err != nil {
		return Bed{}, fmt.Errorf("probe bed duration: %w", err)
	}
	if duration <= 0 {
		return Bed{}, fmt.Errorf("bed source duration must be positive")
	}
	input := source
	// Materialize source timestamps as silence before padding the film's tail.
	// Lower the default 100 ms threshold so short gaps also retain their placement.
	filter := "aresample=async=1:first_pts=0:min_hard_comp=0"
	if music != "" {
		input = music
		musicFormat, err := media.AudioFormat(music)
		if err != nil {
			return Bed{}, fmt.Errorf("probe bed music: %w", err)
		}
		filter = unityChannelFilter(musicFormat, format)
	}
	if err := distinctOutput(output, source, input); err != nil {
		return Bed{}, err
	}
	filter += ",apad,atrim=duration=" + strconv.FormatFloat(duration.Seconds(), 'f', 9, 64)
	if err := renderBedAudio(input, output, format, filter); err != nil {
		return Bed{}, fmt.Errorf("build bed: %w", err)
	}
	// Format describes the intermediate WAV rather than the source codec.
	format.Codec = "pcm_f32le"
	format.BitRate = int64(format.SampleRate) * int64(format.Channels) * 32
	return Bed{File: output, Format: format, Duration: duration, SeparateMusic: music != ""}, nil
}

// PrepareTake converts a take to the bed's format without changing its duration.
// Mono speech duplicates into stereo at unity gain to preserve voice levels.
func (b Bed) PrepareTake(input, output string) error {
	if err := validateBedFormat(b.Format); err != nil {
		return err
	}
	if err := distinctOutput(output, input, b.File); err != nil {
		return err
	}
	format, err := media.AudioFormat(input)
	if err != nil {
		return fmt.Errorf("probe take: %w", err)
	}
	filter := unityChannelFilter(format, b.Format)
	if err := renderBedAudio(input, output, b.Format, filter); err != nil {
		return fmt.Errorf("prepare take: %w", err)
	}
	return nil
}

func unityChannelFilter(input, output media.Format) string {
	if input.Channels == 1 && output.ChannelLayout == "stereo" {
		return "pan=stereo|c0=c0|c1=c0"
	}
	return "anull"
}

func validateBedFormat(format media.Format) error {
	if format.SampleRate <= 0 || format.Channels <= 0 || format.ChannelLayout == "" || format.ChannelLayout == "unknown" {
		return fmt.Errorf("source audio needs a positive sample rate, channel count, and known channel layout")
	}
	return nil
}

// distinctOutput also rejects aliases through symlinks and hard links.
func distinctOutput(output string, inputs ...string) error {
	if output == "" {
		return fmt.Errorf("audio output path is required")
	}
	absOutput, err := filepath.Abs(output)
	if err != nil {
		return err
	}
	outInfo, outErr := os.Stat(output)
	if outErr != nil && !os.IsNotExist(outErr) {
		return outErr
	}
	for _, input := range inputs {
		if input == "" {
			continue
		}
		absInput, err := filepath.Abs(input)
		if err != nil {
			return err
		}
		inInfo, inErr := os.Stat(input)
		if absInput == absOutput || (outErr == nil && inErr == nil && os.SameFile(outInfo, inInfo)) {
			return fmt.Errorf("audio output must not replace input %q", input)
		}
	}
	return nil
}

// renderBedAudio publishes only complete WAV files and keeps failed renders private.
func renderBedAudio(input, output string, format media.Format, filter string) error {
	tmp, err := os.CreateTemp(filepath.Dir(output), ".assemble-*.wav")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := media.Run("-nostdin", "-hide_banner", "-loglevel", "error", "-y",
		"-i", input, "-map", "0:a:0", "-vn", "-af", filter,
		"-ar", strconv.Itoa(format.SampleRate), "-ac", strconv.Itoa(format.Channels),
		"-channel_layout", format.ChannelLayout, "-c:a", "pcm_f32le", "-f", "wav", tmpPath); err != nil {
		return err
	}
	return os.Rename(tmpPath, output)
}
