package ledger

import (
	"errors"
	"fmt"

	"github.com/nrynss/ajilamu/internal/api"
)

const (
	minWaveformPeaks = 64
	maxWaveformPeaks = 128
)

// ApplyTakePeaks validates stored waveform samples and copies them onto a take
// API payload. It never reads the take audio file.
//
// Empty samples keep Peaks absent from the payload. A populated vector has 64
// through 128 unsigned values, matching takes_raw.peaks Array(UInt8).
func ApplyTakePeaks(take *api.Take, peaks []uint8) error {
	if take == nil {
		return errors.New("take payload is nil")
	}
	if err := validateWaveformPeaks(peaks); err != nil {
		return err
	}
	if len(peaks) == 0 {
		take.Peaks = nil
		return nil
	}

	take.Peaks = make([]int, len(peaks))
	for i, peak := range peaks {
		take.Peaks[i] = int(peak)
	}
	return nil
}

func validateWaveformPeaks(peaks []uint8) error {
	if len(peaks) == 0 {
		return nil
	}
	if len(peaks) < minWaveformPeaks || len(peaks) > maxWaveformPeaks {
		return fmt.Errorf("waveform peaks have length %d, want empty or %d through %d", len(peaks), minWaveformPeaks, maxWaveformPeaks)
	}
	return nil
}
