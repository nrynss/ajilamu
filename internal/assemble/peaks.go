package assemble

import (
	"context"
	"fmt"
	"math"

	"github.com/nrynss/ajilamu/internal/media"
)

// PeakCount is the number of normalized waveform samples returned for every take.
const PeakCount = 128

// Peaks returns 128 uint8 values that sketch one take for the timeline.
// Each bin is the max-abs of its frames, scaled to the take peak on 0 to 255.
// A silent take is all zeros. The same path yields an identical slice.
func Peaks(ctx context.Context, path string) ([]uint8, error) {
	if path == "" {
		return nil, fmt.Errorf("take path is required")
	}
	format, err := media.AudioFormat(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("probe take peaks: %w", err)
	}
	if format.Channels <= 0 {
		return nil, fmt.Errorf("take %q needs a positive channel count", path)
	}
	samples, err := decodeFloat32(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("decode take peaks: %w", err)
	}
	return normalizePeaks(bucketPeaks(framePeaks(samples, format.Channels), PeakCount)), nil
}

func framePeaks(samples []float32, channels int) []float32 {
	frames := len(samples) / channels
	out := make([]float32, frames)
	for frame := 0; frame < frames; frame++ {
		var peak float32
		base := frame * channels
		for ch := 0; ch < channels; ch++ {
			if v := abs32(samples[base+ch]); v > peak {
				peak = v
			}
		}
		out[frame] = peak
	}
	return out
}

func bucketPeaks(frames []float32, bins int) []float32 {
	out := make([]float32, bins)
	n := len(frames)
	if n == 0 || bins <= 0 {
		return out
	}
	for i := 0; i < bins; i++ {
		start := i * n / bins
		end := (i + 1) * n / bins
		var peak float32
		for _, v := range frames[start:end] {
			if v > peak {
				peak = v
			}
		}
		out[i] = peak
	}
	return out
}

func normalizePeaks(buckets []float32) []uint8 {
	var peak float32
	for _, v := range buckets {
		if v > peak {
			peak = v
		}
	}
	out := make([]uint8, len(buckets))
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
