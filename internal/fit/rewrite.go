package fit

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nrynss/ajilamu/internal/gemini"
	"github.com/nrynss/ajilamu/internal/media"
	"github.com/nrynss/ajilamu/internal/tts"
	"github.com/nrynss/ajilamu/internal/types"
)

// DefaultMaxAttempts sets the maximum consecutive attempts per dialogue line.
const DefaultMaxAttempts = 3

var (
	// ErrAlreadyStretched reports a source take that is already stretched.
	ErrAlreadyStretched = errors.New("source take is already stretched")

	// ErrTotalStretchExceeded reports cumulative stretch exceeding the maximum limit.
	ErrTotalStretchExceeded = errors.New("cumulative stretch exceeds maximum limit")

	// ErrTranslatorRequired reports a missing translator when rewrite needs one.
	ErrTranslatorRequired = errors.New("translator is required")

	// ErrSynthesizerRequired reports a missing synthesizer when synthesis needs one.
	ErrSynthesizerRequired = errors.New("synthesizer is required")
)

// PathBuilder resolves the destination audio path for an attempt.
type PathBuilder func(segmentID, attempt int, stretched bool) string

// DefaultTakeName returns the standard take filename for an attempt.
func DefaultTakeName(segmentID, attempt int, stretched bool) string {
	if stretched {
		return fmt.Sprintf("seg_%d_try%d_stretched.wav", segmentID, attempt)
	}
	return fmt.Sprintf("seg_%d_try%d.wav", segmentID, attempt)
}

// RewriteConfig configures dialogue line rewrite and repair attempts.
type RewriteConfig struct {
	Translator  gemini.Translator
	Synthesizer tts.Synthesizer
	Limits      StretchLimits
	WorkDir     string
	PathBuilder PathBuilder
	MaxAttempts int
	InitialTake *types.Take
	InitialText string
}

// LineAttempt records one recorded attempt for a dialogue line.
type LineAttempt struct {
	Attempt      int
	Mode         gemini.TranslateMode
	Text         string
	AudioPath    string
	Fit          types.Fit
	Repair       types.Repair
	RepairDetail string
	Stretched    bool
	Ratio        float64
}

// Take converts this attempt into a types.Take.
func (a LineAttempt) Take(segmentID int) types.Take {
	return types.Take{
		SegmentID: segmentID,
		Attempt:   a.Attempt,
		File:      a.AudioPath,
		Duration:  a.Fit.Measured,
		Fit:       a.Fit,
	}
}

// LineResult holds the outcome of repairing one dialogue line.
type LineResult struct {
	Segment          types.Segment
	Flagged          bool
	ChosenTake       types.Take
	Attempts         []LineAttempt
	NotificationCopy string
}

// SignedDeltas returns the signed delta duration for each attempt.
func (r LineResult) SignedDeltas() []time.Duration {
	deltas := make([]time.Duration, len(r.Attempts))
	for i, a := range r.Attempts {
		deltas[i] = a.Fit.Measured - a.Fit.Slot
	}
	return deltas
}

// SignedDeltaMs returns the signed delta in milliseconds for each attempt.
func (r LineResult) SignedDeltaMs() []int64 {
	ms := make([]int64, len(r.Attempts))
	for i, a := range r.Attempts {
		ms[i] = (a.Fit.Measured - a.Fit.Slot).Milliseconds()
	}
	return ms
}

// LineRepairer defines the interface for repairing dialogue lines.
type LineRepairer interface {
	RepairLine(ctx context.Context, seg types.Segment) (LineResult, error)
}

// Repairer implements LineRepairer using a stored configuration.
type Repairer struct {
	cfg RewriteConfig
}

// NewRepairer constructs a LineRepairer with validated configuration.
func NewRepairer(cfg RewriteConfig) (*Repairer, error) {
	if cfg.Limits == (StretchLimits{}) {
		cfg.Limits = DefaultStretchLimits()
	} else if !cfg.Limits.Valid() {
		return nil, fmt.Errorf("%w: got short=%v long=%v", ErrLimits, cfg.Limits.Short, cfg.Limits.Long)
	}
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = DefaultMaxAttempts
	}
	return &Repairer{cfg: cfg}, nil
}

// RepairLine executes dialogue line repairs under stored configuration.
func (r *Repairer) RepairLine(ctx context.Context, seg types.Segment) (LineResult, error) {
	return RepairLine(ctx, seg, r.cfg)
}

// isStretchedTake reports whether path indicates an already stretched take.
func isStretchedTake(path string) bool {
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	return strings.HasSuffix(stem, "_stretched") || strings.Contains(stem, "stretched")
}

// cleanRepairRewriteDetail describes a rewrite decision in clear prose.
func cleanRepairRewriteDetail(f types.Fit, lim StretchLimits) string {
	delta := f.Measured - f.Slot
	deltaMs := delta.Milliseconds()
	if delta > 0 {
		return fmt.Sprintf("overrun of %+dms exceeds stretch budget of %.1f percent", deltaMs, lim.Long*100)
	}
	return fmt.Sprintf("underrun of %+dms exceeds stretch budget of %.1f percent", deltaMs, lim.Short*100)
}

// cleanStretchErrorMessage maps stretch sentinel errors to clean user sentences.
func cleanStretchErrorMessage(err error) string {
	switch {
	case errors.Is(err, ErrDeadBand):
		return "Take already sits inside the stretch dead band."
	case errors.Is(err, ErrOverrunTooLarge):
		return "Overrun exceeds the time stretch budget."
	case errors.Is(err, ErrUnderrunTooLarge):
		return "Underrun exceeds the time stretch budget."
	case errors.Is(err, ErrLandedOutside):
		return "Stretched take landed outside the slot tolerance."
	case errors.Is(err, ErrRenderFailed):
		return "Audio rendering failed during time stretch."
	case errors.Is(err, ErrOutputExists):
		return "Output audio file already exists."
	case errors.Is(err, ErrSourceMissing):
		return "Source audio take does not exist."
	case errors.Is(err, ErrSourceUnreadable):
		return "Source audio could not be probed."
	case errors.Is(err, ErrSameFile):
		return "Output take cannot share a path with its source."
	case errors.Is(err, ErrPathRequired):
		return "Audio take path is required."
	case errors.Is(err, ErrNoSlot):
		return "Slot duration must be positive."
	case errors.Is(err, ErrLimits):
		return "Stretch limits sit outside the valid range."
	case errors.Is(err, ErrRatioRange):
		return "Stretch ratio sits outside the supported range."
	case errors.Is(err, ErrAlreadyStretched):
		return "Source take is already stretched."
	case errors.Is(err, ErrTotalStretchExceeded):
		return "Total stretch correction exceeds maximum limit."
	default:
		return "Unexpected error occurred during time stretch."
	}
}

// formatSuccessCopy builds notification copy for a successful take.
func formatSuccessCopy(segID, attempt int, fit types.Fit, repair types.Repair, ratio float64) string {
	slotMs := fit.Slot.Milliseconds()
	deltaMs := (fit.Measured - fit.Slot).Milliseconds()
	switch repair {
	case types.RepairNone:
		return fmt.Sprintf("Line %d fits slot (%dms) cleanly on attempt %d with delta %+dms.",
			segID, slotMs, attempt, deltaMs)
	case types.RepairAtempo:
		return fmt.Sprintf("Line %d fits slot (%dms) on attempt %d after time stretch at ratio %.4f.",
			segID, slotMs, attempt, ratio)
	default:
		return fmt.Sprintf("Line %d fits slot (%dms) on attempt %d with delta %+dms.",
			segID, slotMs, attempt, deltaMs)
	}
}

// formatFlaggedCopy builds notification copy when all attempts fail.
func formatFlaggedCopy(segID int, slot time.Duration, attempts []LineAttempt, bestIdx int) string {
	best := attempts[bestIdx]
	bestDeltaMs := (best.Fit.Measured - best.Fit.Slot).Milliseconds()

	var deltaParts []string
	for _, a := range attempts {
		dMs := (a.Fit.Measured - a.Fit.Slot).Milliseconds()
		deltaParts = append(deltaParts, fmt.Sprintf("try %d (%+dms)", a.Attempt, dMs))
	}
	deltasSummary := strings.Join(deltaParts, ", ")

	s1 := fmt.Sprintf("Line %d flagged for creator review after %d attempts failed to fit the %dms slot.",
		segID, len(attempts), slot.Milliseconds())
	s2 := fmt.Sprintf("Pipeline kept attempt %d as the closest take with signed delta %+dms.",
		best.Attempt, bestDeltaMs)
	s3 := fmt.Sprintf("Attempt history records signed deltas: %s.", deltasSummary)
	s4 := "Adjust dialogue boundaries or text in the editor."

	return fmt.Sprintf("%s %s %s %s", s1, s2, s3, s4)
}

// RepairLine runs up to three attempts to fit a dialogue line.
func RepairLine(ctx context.Context, seg types.Segment, cfg RewriteConfig) (LineResult, error) {
	if seg.ID < 0 {
		return LineResult{}, errors.New("segment ID cannot be negative")
	}
	slot := seg.SlotDuration()
	if slot <= 0 {
		return LineResult{}, fmt.Errorf("%w: got %v", ErrNoSlot, slot)
	}
	if cfg.Limits == (StretchLimits{}) {
		cfg.Limits = DefaultStretchLimits()
	} else if !cfg.Limits.Valid() {
		return LineResult{}, fmt.Errorf("%w: got short=%v long=%v", ErrLimits, cfg.Limits.Short, cfg.Limits.Long)
	}
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = DefaultMaxAttempts
	}

	resolvePath := func(attempt int, stretched bool) string {
		if cfg.PathBuilder != nil {
			return cfg.PathBuilder(seg.ID, attempt, stretched)
		}
		filename := DefaultTakeName(seg.ID, attempt, stretched)
		if cfg.WorkDir != "" {
			return filepath.Join(cfg.WorkDir, filename)
		}
		return filename
	}

	attempts := make([]LineAttempt, 0, cfg.MaxAttempts)
	result := LineResult{Segment: seg}

	for attemptNum := 1; attemptNum <= cfg.MaxAttempts; attemptNum++ {
		if err := ctx.Err(); err != nil {
			return result, err
		}

		var mode gemini.TranslateMode
		if attemptNum == 1 {
			mode = gemini.ModeNormal
		} else {
			prevFit := attempts[attemptNum-2].Fit
			miss := prevFit.Measured - prevFit.Slot
			if miss > 0 {
				mode = gemini.ModeShorter
			} else {
				mode = gemini.ModeFuller
			}
		}

		var text string
		var audioPath string
		var fit types.Fit

		if attemptNum == 1 && cfg.InitialTake != nil {
			audioPath = cfg.InitialTake.File
			text = cfg.InitialText
			measured, err := media.Duration(audioPath)
			if err != nil {
				return result, fmt.Errorf("%w: %s: %w", ErrSourceUnreadable, audioPath, err)
			}
			fit = types.NewFit(slot, measured)
		} else {
			if attemptNum == 1 && cfg.InitialText != "" {
				text = cfg.InitialText
			} else {
				if cfg.Translator == nil {
					return result, ErrTranslatorRequired
				}
				req := gemini.TranslateRequest{
					SegmentID:  seg.ID,
					Text:       seg.Text,
					TargetSlot: slot,
					Emotion:    seg.Emotion,
					Mode:       mode,
				}
				translated, err := cfg.Translator.Translate(ctx, req)
				if err != nil {
					return result, fmt.Errorf("translate line %d attempt %d: %w", seg.ID, attemptNum, err)
				}
				text = translated
			}

			if cfg.Synthesizer == nil {
				return result, ErrSynthesizerRequired
			}
			rawPath := resolvePath(attemptNum, false)
			if err := os.MkdirAll(filepath.Dir(rawPath), 0o755); err != nil {
				return result, fmt.Errorf("create directory for %s: %w", rawPath, err)
			}
			synthReq := tts.SynthesizeRequest{
				SegmentID: seg.ID,
				Text:      text,
				Speaker:   seg.Speaker,
				OutPath:   rawPath,
			}
			if err := cfg.Synthesizer.Synthesize(ctx, synthReq); err != nil {
				return result, fmt.Errorf("synthesize line %d attempt %d: %w", seg.ID, attemptNum, err)
			}
			audioPath = rawPath
			measured, err := media.Duration(audioPath)
			if err != nil {
				return result, fmt.Errorf("%w: %s: %w", ErrSourceUnreadable, audioPath, err)
			}
			fit = types.NewFit(slot, measured)
		}

		plan := PlanStretchWithLimits(fit, cfg.Limits)

		switch plan.Repair {
		case types.RepairNone:
			att := LineAttempt{
				Attempt:      attemptNum,
				Mode:         mode,
				Text:         text,
				AudioPath:    audioPath,
				Fit:          fit,
				Repair:       types.RepairNone,
				RepairDetail: "fits slot within dead band",
				Stretched:    false,
			}
			attempts = append(attempts, att)
			result.Attempts = attempts
			result.ChosenTake = att.Take(seg.ID)
			result.Flagged = false
			result.NotificationCopy = formatSuccessCopy(seg.ID, attemptNum, fit, types.RepairNone, 0)
			return result, nil

		case types.RepairAtempo:
			if isStretchedTake(audioPath) {
				att := LineAttempt{
					Attempt:      attemptNum,
					Mode:         mode,
					Text:         text,
					AudioPath:    audioPath,
					Fit:          fit,
					Repair:       types.RepairRewrite,
					RepairDetail: cleanStretchErrorMessage(ErrAlreadyStretched),
					Stretched:    false,
				}
				attempts = append(attempts, att)
				continue
			}

			stretchedPath := resolvePath(attemptNum, true)
			if err := os.MkdirAll(filepath.Dir(stretchedPath), 0o755); err != nil {
				return result, fmt.Errorf("create directory for %s: %w", stretchedPath, err)
			}

			res, stretchErr := StretchWithLimits(audioPath, stretchedPath, slot, cfg.Limits)
			if stretchErr == nil {
				att := LineAttempt{
					Attempt:      attemptNum,
					Mode:         mode,
					Text:         text,
					AudioPath:    stretchedPath,
					Fit:          res.After,
					Repair:       types.RepairAtempo,
					RepairDetail: fmt.Sprintf("atempo stretch applied at ratio %.4f", res.Ratio),
					Stretched:    true,
					Ratio:        res.Ratio,
				}
				attempts = append(attempts, att)
				result.Attempts = attempts
				result.ChosenTake = att.Take(seg.ID)
				result.Flagged = false
				result.NotificationCopy = formatSuccessCopy(seg.ID, attemptNum, res.After, types.RepairAtempo, res.Ratio)
				return result, nil
			}

			if errors.Is(stretchErr, ErrDeadBand) {
				att := LineAttempt{
					Attempt:      attemptNum,
					Mode:         mode,
					Text:         text,
					AudioPath:    audioPath,
					Fit:          fit,
					Repair:       types.RepairNone,
					RepairDetail: "fits slot within dead band",
					Stretched:    false,
				}
				attempts = append(attempts, att)
				result.Attempts = attempts
				result.ChosenTake = att.Take(seg.ID)
				result.Flagged = false
				result.NotificationCopy = formatSuccessCopy(seg.ID, attemptNum, fit, types.RepairNone, 0)
				return result, nil
			}

			if errors.Is(stretchErr, ErrLandedOutside) {
				att := LineAttempt{
					Attempt:      attemptNum,
					Mode:         mode,
					Text:         text,
					AudioPath:    audioPath,
					Fit:          fit,
					Repair:       types.RepairRewrite,
					RepairDetail: cleanStretchErrorMessage(stretchErr),
					Stretched:    false,
				}
				attempts = append(attempts, att)
				continue
			}

			return result, fmt.Errorf("stretch take attempt %d: %w", attemptNum, stretchErr)

		default:
			att := LineAttempt{
				Attempt:      attemptNum,
				Mode:         mode,
				Text:         text,
				AudioPath:    audioPath,
				Fit:          fit,
				Repair:       types.RepairRewrite,
				RepairDetail: cleanRepairRewriteDetail(fit, cfg.Limits),
				Stretched:    false,
			}
			attempts = append(attempts, att)
		}
	}

	result.Attempts = attempts
	result.Flagged = true

	bestIdx := 0
	minDelta := absDuration(attempts[0].Fit.Measured - attempts[0].Fit.Slot)
	for i := 1; i < len(attempts); i++ {
		d := absDuration(attempts[i].Fit.Measured - attempts[i].Fit.Slot)
		if d < minDelta {
			minDelta = d
			bestIdx = i
		}
	}
	best := attempts[bestIdx]
	result.ChosenTake = best.Take(seg.ID)
	result.NotificationCopy = formatFlaggedCopy(seg.ID, slot, attempts, bestIdx)
	return result, nil
}
