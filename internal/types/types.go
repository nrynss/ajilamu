package types

import (
	"time"
)

// Segment represents a discrete portion of dialogue.
type Segment struct {
	ID      int
	StartMs int64
	EndMs   int64
	Text    string
	Speaker Speaker
	Emotion string
}

// SlotDuration calculates the allowed time for this segment.
func (s Segment) SlotDuration() time.Duration {
	return time.Duration(s.EndMs-s.StartMs) * time.Millisecond
}

// Speaker identifies the person talking.
type Speaker struct {
	Name string
}

// Take records a single recorded attempt for a segment.
type Take struct {
	SegmentID int
	Attempt   int
	File      string
	Duration  time.Duration
	Fit       Fit
}

// Repair specifies the strategy to fix timing issues.
type Repair int

const (
	// RepairNone indicates no repair is needed.
	RepairNone Repair = iota
	// RepairAtempo applies time stretching to the audio.
	RepairAtempo
	// RepairRewrite requires a script modification.
	RepairRewrite
	// RepairManual delegates the fix to an operator.
	RepairManual
)

// String returns a textual representation of the repair strategy.
func (r Repair) String() string {
	switch r {
	case RepairNone:
		return "none"
	case RepairAtempo:
		return "atempo"
	case RepairRewrite:
		return "rewrite"
	case RepairManual:
		return "manual"
	default:
		return "unknown"
	}
}

// FitThreshold defines the maximum acceptable relative deviation.
const FitThreshold = 0.08

// Fit describes how well a measured duration matches an allocated slot.
type Fit struct {
	Slot     time.Duration
	Measured time.Duration
	Delta    time.Duration // signed: positive = long, negative = short
}

// NewFit creates a Fit record from slot and measured durations.
func NewFit(slot, measured time.Duration) Fit {
	return Fit{
		Slot:     slot,
		Measured: measured,
		Delta:    measured - slot,
	}
}

// Ratio computes the proportion of measured time against the slot.
func (f Fit) Ratio() float64 {
	if f.Slot == 0 {
		if f.Measured == 0 {
			return 1.0
		}
		return 0.0
	}
	return float64(f.Measured) / float64(f.Slot)
}

// Fits determines if the duration lies within the acceptable threshold.
func (f Fit) Fits() bool {
	if f.Slot == 0 {
		return f.Measured == 0
	}
	ratio := float64(f.Delta) / float64(f.Slot)
	if ratio < 0 {
		ratio = -ratio
	}
	return ratio <= FitThreshold
}

// TooLong returns true when the measured duration exceeds the slot beyond tolerance.
func (f Fit) TooLong() bool {
	if f.Slot == 0 {
		return f.Measured > 0
	}
	return f.Delta > 0 && !f.Fits()
}

// TooShort returns true when the measured duration falls short of the slot beyond tolerance.
func (f Fit) TooShort() bool {
	if f.Slot == 0 {
		return false
	}
	return f.Delta < 0 && !f.Fits()
}
