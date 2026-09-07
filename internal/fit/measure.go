// Package fit measures rendered takes against their allocated slots.
package fit

import (
	"fmt"
	"time"

	"github.com/nrynss/ajilamu/internal/media"
	"github.com/nrynss/ajilamu/internal/types"
)

// Measure probes a rendered WAV take and returns its Fit against slot.
// Signed delta is positive when the take runs long and negative when it runs short.
// An empty path, a missing file, or an ffprobe failure returns a zero Fit and an error.
func Measure(path string, slot time.Duration) (types.Fit, error) {
	if path == "" {
		return types.Fit{}, fmt.Errorf("take path is required")
	}
	measured, err := media.Duration(path)
	if err != nil {
		return types.Fit{}, err
	}
	return types.NewFit(slot, measured), nil
}
