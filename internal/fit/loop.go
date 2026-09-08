// Package fit provides audio fit measurement, time stretch, rewrite repair,
// and pipeline loop orchestration for dialogue dubbing.
package fit

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/nrynss/ajilamu/internal/api"
	"github.com/nrynss/ajilamu/internal/cost"
	"github.com/nrynss/ajilamu/internal/gemini"
	"github.com/nrynss/ajilamu/internal/tts"
	"github.com/nrynss/ajilamu/internal/types"
)

var (
	// ErrNoSegments reports missing dialogue segments for pipeline processing.
	ErrNoSegments = errors.New("no dialogue segments provided")

	// ErrSegmenterRequired reports a missing segmenter when media input is given.
	ErrSegmenterRequired = errors.New("segmenter is required when input media is provided")

	// ErrSpeakerVoiceCollision reports distinct speakers assigned the same voice.
	ErrSpeakerVoiceCollision = errors.New("distinct speakers share the same voice profile")

	// ErrMissingSpeaker reports a segment with an empty speaker name.
	ErrMissingSpeaker = errors.New("segment speaker name is required")
)

// EventListener receives pipeline progress notifications.
type EventListener interface {
	OnProgress(event api.ProgressEvent)
}

// ProgressFunc adapts a function to the EventListener interface.
type ProgressFunc func(event api.ProgressEvent)

// OnProgress invokes the wrapped function with the given event.
func (f ProgressFunc) OnProgress(event api.ProgressEvent) {
	if f != nil {
		f(event)
	}
}

// ChargeRecorder receives itemized billing charges for API invocations.
type ChargeRecorder interface {
	Add(cost.Charge)
}

// TotalReporter returns the current cumulative cost of recorded charges.
type TotalReporter interface {
	Total() cost.Price
}

// ChargesProvider returns a snapshot of recorded charges.
type ChargesProvider interface {
	Charges() []cost.Charge
}

// RecorderSetter binds a charge recorder to an API client.
type RecorderSetter interface {
	SetRecorder(ChargeRecorder)
}

// TranslatorProxy decorates a gemini.Translator with RecorderSetter support.
type TranslatorProxy struct {
	inner gemini.Translator
	rec   ChargeRecorder
}

// NewTranslatorProxy creates a decorator wrapping a translator.
func NewTranslatorProxy(inner gemini.Translator, rec ChargeRecorder) *TranslatorProxy {
	p := &TranslatorProxy{inner: inner, rec: rec}
	if rs, ok := inner.(RecorderSetter); ok {
		rs.SetRecorder(rec)
	}
	return p
}

// SetRecorder updates the active charge recorder on the proxy.
func (p *TranslatorProxy) SetRecorder(r ChargeRecorder) {
	p.rec = r
	if rs, ok := p.inner.(RecorderSetter); ok {
		rs.SetRecorder(r)
	}
}

// Translate forwards translation requests to the wrapped translator.
func (p *TranslatorProxy) Translate(ctx context.Context, req gemini.TranslateRequest) (string, error) {
	return p.inner.Translate(ctx, req)
}

// SynthesizerProxy decorates a tts.Synthesizer with RecorderSetter support.
type SynthesizerProxy struct {
	inner tts.Synthesizer
	rec   ChargeRecorder
}

// NewSynthesizerProxy creates a decorator wrapping a synthesizer.
func NewSynthesizerProxy(inner tts.Synthesizer, rec ChargeRecorder) *SynthesizerProxy {
	p := &SynthesizerProxy{inner: inner, rec: rec}
	if rs, ok := inner.(RecorderSetter); ok {
		rs.SetRecorder(rec)
	}
	return p
}

// SetRecorder updates the active charge recorder on the proxy.
func (p *SynthesizerProxy) SetRecorder(r ChargeRecorder) {
	p.rec = r
	if rs, ok := p.inner.(RecorderSetter); ok {
		rs.SetRecorder(r)
	}
}

// Synthesize forwards synthesis requests to the wrapped synthesizer.
func (p *SynthesizerProxy) Synthesize(ctx context.Context, req tts.SynthesizeRequest) error {
	return p.inner.Synthesize(ctx, req)
}

// SegmenterProxy decorates a gemini.Segmenter with RecorderSetter support.
type SegmenterProxy struct {
	inner gemini.Segmenter
	rec   ChargeRecorder
}

// NewSegmenterProxy creates a decorator wrapping a segmenter.
func NewSegmenterProxy(inner gemini.Segmenter, rec ChargeRecorder) *SegmenterProxy {
	p := &SegmenterProxy{inner: inner, rec: rec}
	if rs, ok := inner.(RecorderSetter); ok {
		rs.SetRecorder(rec)
	}
	return p
}

// SetRecorder updates the active charge recorder on the proxy.
func (p *SegmenterProxy) SetRecorder(r ChargeRecorder) {
	p.rec = r
	if rs, ok := p.inner.(RecorderSetter); ok {
		rs.SetRecorder(r)
	}
}

// Segment forwards segmentation requests to the wrapped segmenter.
func (p *SegmenterProxy) Segment(ctx context.Context, in gemini.Input) ([]types.Segment, error) {
	return p.inner.Segment(ctx, in)
}

// VoiceAssigner assigns a distinct voice profile to a speaker.
type VoiceAssigner func(speaker types.Speaker, language string) (tts.Voice, error)

// PipelineTakeName generates standard immutable take filenames.
// Every attempt and repair receives a distinct non-colliding name.
func PipelineTakeName(segmentID, attempt int, stretched bool) string {
	if stretched {
		if attempt == 1 {
			return fmt.Sprintf("seg_%d_stretched.wav", segmentID)
		}
		return fmt.Sprintf("seg_%d_try%d_stretched.wav", segmentID, attempt)
	}
	return fmt.Sprintf("seg_%d_try%d.wav", segmentID, attempt)
}

// PipelineConfig configures dubbing pipeline execution.
type PipelineConfig struct {
	// Segmenter identifies dialogue segments from audio or video input.
	Segmenter gemini.Segmenter

	// InputMedia carries raw media bytes when Segmenter runs.
	InputMedia *gemini.Input

	// Segments provides pre-segmented dialogue lines when Segmenter is omitted.
	Segments []types.Segment

	// Translator converts dialogue lines into the target language.
	Translator gemini.Translator

	// Synthesizer renders spoken dialogue into WAV audio.
	Synthesizer tts.Synthesizer

	// Language specifies the target language BCP-47 code.
	Language string

	// Limits configures time stretch ratio boundaries.
	Limits StretchLimits

	// WorkDir is the base directory where audio take files are written.
	WorkDir string

	// PathBuilder resolves destination paths for audio takes.
	PathBuilder PathBuilder

	// MaxAttempts caps retry attempts per segment.
	MaxAttempts int

	// Recorder receives itemized API expense charges.
	Recorder ChargeRecorder

	// Listener receives real-time pipeline progress events.
	Listener EventListener

	// ProgressFn provides an optional functional callback for progress events.
	ProgressFn ProgressFunc

	// Events provides an optional channel to receive progress events.
	Events chan<- api.ProgressEvent

	// VoiceAssigner optionally customizes speaker-to-voice assignment.
	VoiceAssigner VoiceAssigner
}

// PipelineResult holds the complete outcome of a dubbing pipeline run.
type PipelineResult struct {
	// Segments holds all processed dialogue segments.
	Segments []types.Segment

	// Lines holds the repair outcome for each segment.
	Lines []LineResult

	// FlaggedSegments lists IDs of segments flagged for creator review.
	FlaggedSegments []int

	// Voices maps each speaker name to their assigned voice.
	Voices map[string]tts.Voice

	// TotalCost is the cumulative price of all API operations.
	TotalCost cost.Price

	// Charges itemizes all recorded charges.
	Charges []cost.Charge
}

// IsClean reports whether all segments fit without flags.
func (r *PipelineResult) IsClean() bool {
	return len(r.FlaggedSegments) == 0
}

// FlaggedCount returns the number of flagged segments.
func (r *PipelineResult) FlaggedCount() int {
	return len(r.FlaggedSegments)
}

// Line retrieves the LineResult for a specific segment ID.
func (r *PipelineResult) Line(segmentID int) (LineResult, bool) {
	for _, line := range r.Lines {
		if line.Segment.ID == segmentID {
			return line, true
		}
	}
	return LineResult{}, false
}

// CleanErrorMessage translates an error into a clean user-facing sentence.
// It never displays raw wrapped ffmpeg banners or internal stack traces.
func CleanErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	switch {
	case errors.Is(err, context.Canceled):
		return "Pipeline run canceled by context."
	case errors.Is(err, context.DeadlineExceeded):
		return "Pipeline run timed out."
	case errors.Is(err, ErrNoSegments):
		return "No dialogue segments found for processing."
	case errors.Is(err, ErrSegmenterRequired):
		return "Segmenter client is required for media input."
	case errors.Is(err, ErrTranslatorRequired):
		return "Translator client is required for translation."
	case errors.Is(err, ErrSynthesizerRequired):
		return "Synthesizer client is required for voice generation."
	case errors.Is(err, ErrSpeakerVoiceCollision):
		return "Distinct speakers collided on the same voice profile."
	case errors.Is(err, ErrMissingSpeaker):
		return "Dialogue segment lacks a speaker name."
	case errors.Is(err, ErrDeadBand):
		return "Audio take already sits inside the stretch dead band."
	case errors.Is(err, ErrOverrunTooLarge):
		return "Overrun exceeds the time stretch budget."
	case errors.Is(err, ErrUnderrunTooLarge):
		return "Underrun exceeds the time stretch budget."
	case errors.Is(err, ErrLandedOutside):
		return "Stretched audio landed outside the slot tolerance."
	case errors.Is(err, ErrRenderFailed):
		return "Audio rendering failed during time stretch."
	case errors.Is(err, ErrOutputExists):
		return "Output audio file already exists."
	case errors.Is(err, ErrSourceMissing):
		return "Source audio take does not exist."
	case errors.Is(err, ErrSourceUnreadable):
		return "Source audio file could not be probed."
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
		return "Source audio take is already stretched."
	case errors.Is(err, ErrTotalStretchExceeded):
		return "Total stretch correction exceeds maximum limit."
	default:
		return "Unexpected error occurred during pipeline execution."
	}
}

// trackingRecorder tracks itemized charges and maintains exact total cost.
type trackingRecorder struct {
	mu      sync.Mutex
	inner   ChargeRecorder
	charges []cost.Charge
	total   cost.Price
}

func newTrackingRecorder(inner ChargeRecorder) *trackingRecorder {
	return &trackingRecorder{inner: inner}
}

func (r *trackingRecorder) Add(c cost.Charge) {
	r.mu.Lock()
	r.charges = append(r.charges, c)
	r.total += c.Total()
	r.mu.Unlock()
	if r.inner != nil {
		r.inner.Add(c)
	}
}

func (r *trackingRecorder) Total() cost.Price {
	r.mu.Lock()
	defer r.mu.Unlock()
	if tr, ok := r.inner.(TotalReporter); ok {
		return tr.Total()
	}
	return r.total
}

func (r *trackingRecorder) Charges() []cost.Charge {
	r.mu.Lock()
	defer r.mu.Unlock()
	if cp, ok := r.inner.(ChargesProvider); ok {
		return cp.Charges()
	}
	out := make([]cost.Charge, len(r.charges))
	copy(out, r.charges)
	return out
}

// eventEmitter dispatches progress events to listeners and channels.
type eventEmitter struct {
	ctx        context.Context
	listener   EventListener
	progressFn ProgressFunc
	eventsChan chan<- api.ProgressEvent
	recorder   *trackingRecorder
	language   string
}

func (e *eventEmitter) emit(event api.ProgressEvent) {
	if event.Language == "" {
		event.Language = e.language
	}
	if e.recorder != nil {
		event.TotalNanodollars = e.recorder.Total()
	}
	if e.listener != nil {
		e.listener.OnProgress(event)
	}
	if e.progressFn != nil {
		e.progressFn(event)
	}
	if e.eventsChan != nil {
		select {
		case e.eventsChan <- event:
		case <-e.ctx.Done():
		default:
		}
	}
}

// loopSegmenter wraps gemini.Segmenter to emit segmentation progress events.
type loopSegmenter struct {
	inner   gemini.Segmenter
	emitter *eventEmitter
	tracker *trackingRecorder
}

func (ls *loopSegmenter) Segment(ctx context.Context, in gemini.Input) ([]types.Segment, error) {
	ls.emitter.emit(api.ProgressEvent{
		Type:      api.EventProgress,
		Stage:     api.StageSegmenting,
		Sentence:  "Detecting dialogue segments from speech audio.",
		SegmentID: 0,
	})
	segs, err := ls.inner.Segment(ctx, in)
	if err != nil {
		ls.emitter.emit(api.ProgressEvent{
			Type:      api.EventError,
			Stage:     api.StageSegmenting,
			Sentence:  CleanErrorMessage(err),
			SegmentID: 0,
		})
		return nil, fmt.Errorf("segment media: %w", err)
	}
	ls.emitter.emit(api.ProgressEvent{
		Type:      api.EventProgress,
		Stage:     api.StageSegmenting,
		Sentence:  fmt.Sprintf("Detected %d dialogue segments from audio.", len(segs)),
		SegmentID: 0,
	})
	return segs, nil
}

// loopTranslator wraps gemini.Translator to emit translation progress events.
type loopTranslator struct {
	inner    gemini.Translator
	emitter  *eventEmitter
	tracker  *trackingRecorder
	language string
}

func (lt *loopTranslator) Translate(ctx context.Context, req gemini.TranslateRequest) (string, error) {
	var sentence string
	if req.Mode == gemini.ModeNormal {
		sentence = fmt.Sprintf("Translating line %d into Malayalam.", req.SegmentID)
	} else {
		sentence = fmt.Sprintf("Translating line %d (%s mode) into Malayalam.", req.SegmentID, req.Mode)
	}

	lt.emitter.emit(api.ProgressEvent{
		Type:      api.EventProgress,
		Stage:     api.StageTranslating,
		Sentence:  sentence,
		SegmentID: req.SegmentID,
		Language:  lt.language,
	})

	return lt.inner.Translate(ctx, req)
}

// loopSynthesizer wraps tts.Synthesizer to emit synthesis and measurement events.
type loopSynthesizer struct {
	inner    tts.Synthesizer
	emitter  *eventEmitter
	tracker  *trackingRecorder
	language string
}

func (ls *loopSynthesizer) Synthesize(ctx context.Context, req tts.SynthesizeRequest) error {
	takeFile := filepath.Base(req.OutPath)
	ls.emitter.emit(api.ProgressEvent{
		Type:      api.EventProgress,
		Stage:     api.StageSynthesizing,
		Sentence:  fmt.Sprintf("Rendering line %d in Malayalam.", req.SegmentID),
		SegmentID: req.SegmentID,
		Language:  ls.language,
		TakeFile:  takeFile,
	})

	err := ls.inner.Synthesize(ctx, req)
	if err != nil {
		return err
	}

	ls.emitter.emit(api.ProgressEvent{
		Type:      api.EventProgress,
		Stage:     api.StageMeasuring,
		Sentence:  fmt.Sprintf("Measuring audio duration for line %d.", req.SegmentID),
		SegmentID: req.SegmentID,
		Language:  ls.language,
		TakeFile:  takeFile,
	})
	return nil
}

// Pipeline orchestrates the end-to-end dubbing workflow.
type Pipeline struct {
	cfg PipelineConfig
}

// NewPipeline creates a new dubbing pipeline with validated options.
func NewPipeline(cfg PipelineConfig) (*Pipeline, error) {
	if cfg.Language == "" {
		cfg.Language = tts.Malayalam
	}
	if cfg.Limits == (StretchLimits{}) {
		cfg.Limits = DefaultStretchLimits()
	} else if !cfg.Limits.Valid() {
		return nil, fmt.Errorf("%w: got short=%v long=%v", ErrLimits, cfg.Limits.Short, cfg.Limits.Long)
	}
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = DefaultMaxAttempts
	}
	if cfg.PathBuilder == nil {
		cfg.PathBuilder = PipelineTakeName
	}
	if cfg.VoiceAssigner == nil {
		cfg.VoiceAssigner = tts.Assign
	}
	if cfg.InputMedia != nil && cfg.Segmenter == nil {
		return nil, ErrSegmenterRequired
	}
	if cfg.InputMedia == nil && len(cfg.Segments) == 0 {
		return nil, ErrNoSegments
	}
	if cfg.Translator == nil {
		return nil, ErrTranslatorRequired
	}
	if cfg.Synthesizer == nil {
		return nil, ErrSynthesizerRequired
	}
	return &Pipeline{cfg: cfg}, nil
}

// RunPipeline executes the dubbing pipeline under the given configuration.
func RunPipeline(ctx context.Context, cfg PipelineConfig) (*PipelineResult, error) {
	p, err := NewPipeline(cfg)
	if err != nil {
		return nil, err
	}
	return p.Run(ctx)
}

// Run executes the complete dubbing pipeline.
func (p *Pipeline) Run(ctx context.Context) (*PipelineResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	tracker := newTrackingRecorder(p.cfg.Recorder)
	emitter := &eventEmitter{
		ctx:        ctx,
		listener:   p.cfg.Listener,
		progressFn: p.cfg.ProgressFn,
		eventsChan: p.cfg.Events,
		recorder:   tracker,
		language:   p.cfg.Language,
	}

	if p.cfg.WorkDir != "" {
		if err := os.MkdirAll(p.cfg.WorkDir, 0o755); err != nil {
			return nil, fmt.Errorf("create work directory: %w", err)
		}
	}

	var segments []types.Segment
	if p.cfg.InputMedia != nil {
		if rs, ok := p.cfg.Segmenter.(RecorderSetter); ok {
			rs.SetRecorder(tracker)
		}
		wrappedSegmenter := &loopSegmenter{
			inner:   p.cfg.Segmenter,
			emitter: emitter,
			tracker: tracker,
		}
		segs, err := wrappedSegmenter.Segment(ctx, *p.cfg.InputMedia)
		if err != nil {
			return nil, err
		}
		segments = segs
	} else {
		segments = p.cfg.Segments
		emitter.emit(api.ProgressEvent{
			Type:      api.EventProgress,
			Stage:     api.StageSegmenting,
			Sentence:  fmt.Sprintf("Loaded %d dialogue segments for processing.", len(segments)),
			SegmentID: 0,
		})
	}

	if len(segments) == 0 {
		emitter.emit(api.ProgressEvent{
			Type:      api.EventError,
			Stage:     api.StageSegmenting,
			Sentence:  CleanErrorMessage(ErrNoSegments),
			SegmentID: 0,
		})
		return nil, ErrNoSegments
	}

	// Verify distinct speaker voice assignments.
	assignedVoices := make(map[string]tts.Voice)
	for _, seg := range segments {
		if seg.Speaker.Name == "" {
			err := fmt.Errorf("%w: segment %d", ErrMissingSpeaker, seg.ID)
			emitter.emit(api.ProgressEvent{
				Type:      api.EventError,
				Stage:     api.StageSegmenting,
				Sentence:  CleanErrorMessage(err),
				SegmentID: seg.ID,
			})
			return nil, err
		}
		if _, ok := assignedVoices[seg.Speaker.Name]; ok {
			continue
		}
		voice, err := p.cfg.VoiceAssigner(seg.Speaker, p.cfg.Language)
		if err != nil {
			emitter.emit(api.ProgressEvent{
				Type:      api.EventError,
				Stage:     api.StageSegmenting,
				Sentence:  CleanErrorMessage(err),
				SegmentID: seg.ID,
			})
			return nil, fmt.Errorf("assign voice for speaker %q: %w", seg.Speaker.Name, err)
		}
		for otherSpeaker, otherVoice := range assignedVoices {
			if otherVoice.Name == voice.Name && otherSpeaker != seg.Speaker.Name {
				err := fmt.Errorf("%w: speakers %q and %q share voice %q",
					ErrSpeakerVoiceCollision, otherSpeaker, seg.Speaker.Name, voice.Name)
				emitter.emit(api.ProgressEvent{
					Type:      api.EventError,
					Stage:     api.StageSegmenting,
					Sentence:  CleanErrorMessage(ErrSpeakerVoiceCollision),
					SegmentID: seg.ID,
				})
				return nil, err
			}
		}
		assignedVoices[seg.Speaker.Name] = voice
	}

	if rs, ok := p.cfg.Translator.(RecorderSetter); ok {
		rs.SetRecorder(tracker)
	}
	if rs, ok := p.cfg.Synthesizer.(RecorderSetter); ok {
		rs.SetRecorder(tracker)
	}

	// Wrap translator and synthesizer with event emission hooks.
	wrappedTranslator := &loopTranslator{
		inner:    p.cfg.Translator,
		emitter:  emitter,
		tracker:  tracker,
		language: p.cfg.Language,
	}

	wrappedSynthesizer := &loopSynthesizer{
		inner:    p.cfg.Synthesizer,
		emitter:  emitter,
		tracker:  tracker,
		language: p.cfg.Language,
	}

	resolveTakePath := func(segmentID, attempt int, stretched bool) string {
		filename := p.cfg.PathBuilder(segmentID, attempt, stretched)
		if p.cfg.WorkDir != "" && !filepath.IsAbs(filename) {
			return filepath.Join(p.cfg.WorkDir, filename)
		}
		return filename
	}

	wrappedPathBuilder := func(segmentID, attempt int, stretched bool) string {
		destPath := resolveTakePath(segmentID, attempt, stretched)
		if stretched {
			takeFile := filepath.Base(destPath)
			emitter.emit(api.ProgressEvent{
				Type:      api.EventProgress,
				Stage:     api.StageRepairing,
				Sentence:  fmt.Sprintf("Applying time stretch repair to line %d.", segmentID),
				SegmentID: segmentID,
				TakeFile:  takeFile,
			})
		}
		return destPath
	}

	results := make([]LineResult, 0, len(segments))
	var flaggedIDs []int

	for _, seg := range segments {
		if err := ctx.Err(); err != nil {
			emitter.emit(api.ProgressEvent{
				Type:      api.EventError,
				Stage:     api.StageMeasuring,
				Sentence:  CleanErrorMessage(err),
				SegmentID: seg.ID,
			})
			return nil, err
		}

		rewriteCfg := RewriteConfig{
			Translator:  wrappedTranslator,
			Synthesizer: wrappedSynthesizer,
			Limits:      p.cfg.Limits,
			WorkDir:     p.cfg.WorkDir,
			PathBuilder: wrappedPathBuilder,
			MaxAttempts: p.cfg.MaxAttempts,
		}

		res, err := RepairLine(ctx, seg, rewriteCfg)
		if err != nil {
			emitter.emit(api.ProgressEvent{
				Type:      api.EventError,
				Stage:     api.StageRepairing,
				Sentence:  CleanErrorMessage(err),
				SegmentID: seg.ID,
			})
			return nil, fmt.Errorf("repair line %d: %w", seg.ID, err)
		}

		results = append(results, res)
		takeFile := filepath.Base(res.ChosenTake.File)

		if res.Flagged {
			flaggedIDs = append(flaggedIDs, seg.ID)
			emitter.emit(api.ProgressEvent{
				Type:      api.EventProgress,
				Stage:     api.StageRepairing,
				Sentence:  fmt.Sprintf("Line %d flagged for creator review after %d failed attempts.", seg.ID, len(res.Attempts)),
				SegmentID: seg.ID,
				TakeFile:  takeFile,
			})
		} else {
			emitter.emit(api.ProgressEvent{
				Type:      api.EventProgress,
				Stage:     api.StageMeasuring,
				Sentence:  fmt.Sprintf("Line %d fit verified with take %s.", seg.ID, takeFile),
				SegmentID: seg.ID,
				TakeFile:  takeFile,
			})
		}
	}

	if len(flaggedIDs) == 0 {
		emitter.emit(api.ProgressEvent{
			Type:     api.EventDone,
			Stage:    api.StageMeasuring,
			Sentence: "Dubbing pipeline completed successfully.",
		})
	} else if len(flaggedIDs) == 1 {
		emitter.emit(api.ProgressEvent{
			Type:     api.EventDone,
			Stage:    api.StageRepairing,
			Sentence: "Dubbing pipeline completed with 1 flagged line.",
		})
	} else {
		emitter.emit(api.ProgressEvent{
			Type:     api.EventDone,
			Stage:    api.StageRepairing,
			Sentence: fmt.Sprintf("Dubbing pipeline completed with %d flagged lines.", len(flaggedIDs)),
		})
	}

	return &PipelineResult{
		Segments:        segments,
		Lines:           results,
		FlaggedSegments: flaggedIDs,
		Voices:          assignedVoices,
		TotalCost:       tracker.Total(),
		Charges:         tracker.Charges(),
	}, nil
}
