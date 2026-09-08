package assemble

import (
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/nrynss/ajilamu/internal/media"
	"github.com/nrynss/ajilamu/internal/types"
)

// Policy names the overrun decision recorded for one placed take.
type Policy string

const (
	// PolicyFit keeps a take that ends inside its slot.
	PolicyFit Policy = "fit"
	// PolicyGap lets a take occupy trailing silence before the next take.
	PolicyGap Policy = "gap"
	// PolicyTruncate drops colliding tail silence at the next take start.
	PolicyTruncate Policy = "truncate"
	// PolicyCrossfade overlaps colliding speech through the fade window.
	PolicyCrossfade Policy = "crossfade"
)

// Crossfade is the fade window applied when voiced speech still collides.
const Crossfade = 40 * time.Millisecond

// voicedFloor treats samples below about -40 dB as silence.
const voicedFloor = 0.01

// Clip is one take and the segment that owns its slot.
type Clip struct {
	Segment types.Segment
	File    string
}

// Event records how one take used its slot, the trailing gap, and any cut.
type Event struct {
	SegmentID int
	Policy    Policy
	Start     time.Duration
	End       time.Duration
	Slot      time.Duration
	Measured  time.Duration
	Overrun   time.Duration
	GapUsed   time.Duration
	Truncated time.Duration
	Crossfade time.Duration
}

// Placement is a full-length speech layer matching the bed format.
type Placement struct {
	File     string
	Format   media.Format
	Duration time.Duration
	Events   []Event
}

type plannedClip struct {
	clip     Clip
	prepared string
	delay    int
	keep     int
	fadeIn   int
	fadeOut  int
	event    Event
}

// Place writes each take at its segment start on a silent canvas.
// A take may occupy the gap before the next take.
// Voiced collision overlaps through a fade and always records the cut.
func (b Bed) Place(ctx context.Context, clips []Clip, output string) (Placement, error) {
	if err := validateBedFormat(b.Format); err != nil {
		return Placement{}, err
	}
	if b.Duration <= 0 || b.File == "" {
		return Placement{}, fmt.Errorf("place needs a built bed")
	}
	if err := distinctOutput(output, append(clipFiles(clips), b.File)...); err != nil {
		return Placement{}, err
	}
	bedFrames, err := audioFrames(ctx, b.File, b.Format.Channels)
	if err != nil {
		return Placement{}, fmt.Errorf("probe bed length: %w", err)
	}
	if bedFrames <= 0 {
		return Placement{}, fmt.Errorf("bed has no audio frames")
	}
	sorted, err := sortClips(clips)
	if err != nil {
		return Placement{}, err
	}
	work, err := os.MkdirTemp(filepath.Dir(output), ".assemble-place-*")
	if err != nil {
		return Placement{}, err
	}
	defer os.RemoveAll(work)

	planned := make([]plannedClip, len(sorted))
	for i, clip := range sorted {
		prepared := filepath.Join(work, fmt.Sprintf("take-%d.wav", i))
		if err := b.PrepareTake(ctx, clip.File, prepared); err != nil {
			return Placement{}, fmt.Errorf("prepare segment %d: %w", clip.Segment.ID, err)
		}
		samples, err := decodeFloat32(ctx, prepared)
		if err != nil {
			return Placement{}, fmt.Errorf("decode segment %d: %w", clip.Segment.ID, err)
		}
		measured := len(samples) / b.Format.Channels
		if measured <= 0 {
			return Placement{}, fmt.Errorf("segment %d take has no audio frames", clip.Segment.ID)
		}
		voiced := voicedFrames(samples, b.Format.Channels)
		delay := msFrames(clip.Segment.StartMs, b.Format.SampleRate)
		if delay >= bedFrames {
			return Placement{}, fmt.Errorf("segment %d starts after the bed", clip.Segment.ID)
		}
		next := bedFrames
		if i+1 < len(sorted) {
			next = msFrames(sorted[i+1].Segment.StartMs, b.Format.SampleRate)
		}
		if next <= delay {
			return Placement{}, fmt.Errorf("segment %d collides with the next start", clip.Segment.ID)
		}
		slot := msFrames(clip.Segment.EndMs, b.Format.SampleRate) - delay
		if slot < 0 {
			slot = 0
		}
		available := next - delay
		choice := decidePolicy(slot, measured, voiced, available, framesFor(Crossfade, b.Format.SampleRate))
		choice = clampKeep(choice, measured, slot, bedFrames-delay)
		choice.event.SegmentID = clip.Segment.ID
		choice.event.Start = framesDuration(delay, b.Format.SampleRate)
		choice.event.End = framesDuration(delay+choice.keep, b.Format.SampleRate)
		choice.event.Slot = framesDuration(slot, b.Format.SampleRate)
		choice.event.Measured = framesDuration(measured, b.Format.SampleRate)
		choice.event.Overrun = framesDuration(choice.overrun, b.Format.SampleRate)
		choice.event.GapUsed = framesDuration(choice.gapUsed, b.Format.SampleRate)
		choice.event.Truncated = framesDuration(choice.truncated, b.Format.SampleRate)
		choice.event.Crossfade = framesDuration(choice.fadeOut, b.Format.SampleRate)
		if i > 0 && planned[i-1].event.Policy == PolicyCrossfade {
			choice.fadeIn = planned[i-1].fadeOut
			if choice.fadeIn > choice.keep {
				choice.fadeIn = choice.keep / 2
			}
		}
		if choice.keep <= 0 {
			return Placement{}, fmt.Errorf("segment %d has no room on the bed", clip.Segment.ID)
		}
		planned[i] = plannedClip{
			clip:     clip,
			prepared: prepared,
			delay:    delay,
			keep:     choice.keep,
			fadeIn:   choice.fadeIn,
			fadeOut:  choice.fadeOut,
			event:    choice.event,
		}
		logPlacement(choice.event)
	}
	filter := buildPlaceFilter(b.Format, bedFrames, planned)
	inputs := make([]string, len(planned))
	for i, item := range planned {
		inputs[i] = item.prepared
	}
	if err := renderMix(ctx, inputs, output, b.Format, filter); err != nil {
		return Placement{}, fmt.Errorf("place takes: %w", err)
	}
	events := make([]Event, len(planned))
	for i, item := range planned {
		events[i] = item.event
	}
	format := b.Format
	format.Codec = "pcm_f32le"
	format.BitRate = int64(format.SampleRate) * int64(format.Channels) * 32
	return Placement{
		File:     output,
		Format:   format,
		Duration: framesDuration(bedFrames, b.Format.SampleRate),
		Events:   events,
	}, nil
}

// Overlay mixes a speech layer over the bed without changing either level.
func (b Bed) Overlay(ctx context.Context, speech, output string) error {
	if err := validateBedFormat(b.Format); err != nil {
		return err
	}
	if b.File == "" || speech == "" {
		return fmt.Errorf("overlay needs a bed and a speech layer")
	}
	if err := distinctOutput(output, b.File, speech); err != nil {
		return err
	}
	filter := "[0:a][1:a]amix=inputs=2:duration=first:normalize=0:dropout_transition=0[out]"
	return renderMix(ctx, []string{b.File, speech}, output, b.Format, filter)
}

type policyChoice struct {
	keep      int
	fadeIn    int
	fadeOut   int
	overrun   int
	gapUsed   int
	truncated int
	event     Event
}

func decidePolicy(slot, measured, voiced, available, fade int) policyChoice {
	if available < 0 {
		available = 0
	}
	if voiced < 0 {
		voiced = 0
	}
	if voiced > measured {
		voiced = measured
	}
	overrun := measured - slot
	if overrun < 0 {
		overrun = 0
	}
	choice := policyChoice{overrun: overrun}
	switch {
	case measured <= available:
		choice.keep = measured
		if measured <= slot {
			choice.event.Policy = PolicyFit
		} else {
			choice.event.Policy = PolicyGap
			choice.gapUsed = measured - slot
		}
	case voiced <= available:
		choice.keep = available
		choice.truncated = measured - available
		choice.event.Policy = PolicyTruncate
		if choice.keep > slot {
			choice.gapUsed = choice.keep - slot
		}
	default:
		// Keep past the next start so the fade window overlaps the later take.
		choice.keep = available + fade
		if choice.keep > measured {
			choice.keep = measured
		}
		choice.truncated = measured - choice.keep
		choice.event.Policy = PolicyCrossfade
		choice.fadeOut = choice.keep - available
		if choice.fadeOut > fade {
			choice.fadeOut = fade
		}
		if choice.fadeOut >= choice.keep {
			choice.fadeOut = choice.keep / 2
		}
		if choice.fadeOut < 1 && choice.keep > 1 {
			choice.fadeOut = 1
		}
		if choice.keep > slot {
			choice.gapUsed = choice.keep - slot
		}
	}
	return choice
}

func clampKeep(choice policyChoice, measured, slot, maxKeep int) policyChoice {
	if maxKeep < 0 {
		maxKeep = 0
	}
	if choice.keep <= maxKeep {
		return choice
	}
	choice.keep = maxKeep
	choice.truncated = measured - choice.keep
	if choice.truncated < 0 {
		choice.truncated = 0
	}
	if choice.fadeOut >= choice.keep {
		choice.fadeOut = choice.keep / 2
	}
	if choice.keep > slot {
		choice.gapUsed = choice.keep - slot
	} else {
		choice.gapUsed = 0
	}
	return choice
}

func sortClips(clips []Clip) ([]Clip, error) {
	seen := make(map[int]struct{}, len(clips))
	sorted := append([]Clip(nil), clips...)
	for i, clip := range sorted {
		if clip.File == "" {
			return nil, fmt.Errorf("clip %d is missing a take file", i)
		}
		if clip.Segment.ID == 0 {
			return nil, fmt.Errorf("clip %d is missing a segment id", i)
		}
		if clip.Segment.EndMs <= clip.Segment.StartMs {
			return nil, fmt.Errorf("segment %d has an empty slot", clip.Segment.ID)
		}
		if clip.Segment.StartMs < 0 {
			return nil, fmt.Errorf("segment %d starts before the bed", clip.Segment.ID)
		}
		if _, ok := seen[clip.Segment.ID]; ok {
			return nil, fmt.Errorf("segment %d appears more than once", clip.Segment.ID)
		}
		seen[clip.Segment.ID] = struct{}{}
	}
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Segment.StartMs == sorted[j].Segment.StartMs {
			return sorted[i].Segment.ID < sorted[j].Segment.ID
		}
		return sorted[i].Segment.StartMs < sorted[j].Segment.StartMs
	})
	return sorted, nil
}

func buildPlaceFilter(format media.Format, bedFrames int, clips []plannedClip) string {
	var b strings.Builder
	fmt.Fprintf(&b, "anullsrc=r=%d:cl=%s,atrim=end_sample=%d,asetpts=PTS-STARTPTS[base]",
		format.SampleRate, format.ChannelLayout, bedFrames)
	labels := make([]string, 0, len(clips)+1)
	labels = append(labels, "[base]")
	for i, clip := range clips {
		label := fmt.Sprintf("t%d", i)
		fmt.Fprintf(&b, ";[%d:a]atrim=end_sample=%d,asetpts=PTS-STARTPTS", i, clip.keep)
		if clip.fadeIn > 0 {
			fmt.Fprintf(&b, ",afade=t=in:ss=0:ns=%d", clip.fadeIn)
		}
		if clip.fadeOut > 0 {
			start := clip.keep - clip.fadeOut
			if start < 0 {
				start = 0
			}
			fmt.Fprintf(&b, ",afade=t=out:ss=%d:ns=%d", start, clip.fadeOut)
		}
		if clip.delay > 0 {
			fmt.Fprintf(&b, ",adelay=delays=%dS:all=1", clip.delay)
		}
		fmt.Fprintf(&b, "[%s]", label)
		labels = append(labels, "["+label+"]")
	}
	if len(clips) == 0 {
		b.WriteString(";[base]anull[out]")
		return b.String()
	}
	fmt.Fprintf(&b, ";%samix=inputs=%d:duration=first:normalize=0:dropout_transition=0[out]",
		strings.Join(labels, ""), len(labels))
	return b.String()
}

func renderMix(ctx context.Context, inputs []string, output string, format media.Format, filter string) error {
	tmp, err := os.CreateTemp(filepath.Dir(output), ".assemble-*.wav")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Close(); err != nil {
		return err
	}
	args := []string{"-nostdin", "-hide_banner", "-loglevel", "error", "-y"}
	for _, input := range inputs {
		args = append(args, "-i", input)
	}
	args = append(args,
		"-filter_complex", filter, "-map", "[out]",
		"-ar", strconv.Itoa(format.SampleRate), "-ac", strconv.Itoa(format.Channels),
		"-channel_layout", format.ChannelLayout, "-c:a", "pcm_f32le", "-f", "wav", tmpPath)
	if err := media.Run(ctx, args...); err != nil {
		return err
	}
	return os.Rename(tmpPath, output)
}

func logPlacement(event Event) {
	if event.Policy != PolicyTruncate && event.Policy != PolicyCrossfade {
		return
	}
	log.Printf("assemble place: segment=%d policy=%s start_ms=%d end_ms=%d overrun_ms=%d truncated_ms=%d crossfade_ms=%d gap_used_ms=%d",
		event.SegmentID, event.Policy,
		event.Start.Milliseconds(), event.End.Milliseconds(),
		event.Overrun.Milliseconds(), event.Truncated.Milliseconds(),
		event.Crossfade.Milliseconds(), event.GapUsed.Milliseconds())
}

func clipFiles(clips []Clip) []string {
	files := make([]string, 0, len(clips))
	for _, clip := range clips {
		if clip.File != "" {
			files = append(files, clip.File)
		}
	}
	return files
}

func msFrames(ms int64, rate int) int {
	return int(math.Round(float64(ms) * float64(rate) / 1000.0))
}

func framesFor(d time.Duration, rate int) int {
	return int(math.Round(d.Seconds() * float64(rate)))
}

func framesDuration(frames, rate int) time.Duration {
	if rate <= 0 || frames <= 0 {
		return 0
	}
	return time.Duration(math.Round(float64(frames) / float64(rate) * 1e9))
}

func audioFrames(ctx context.Context, path string, channels int) (int, error) {
	samples, err := decodeFloat32(ctx, path)
	if err != nil {
		return 0, err
	}
	if channels <= 0 {
		return 0, fmt.Errorf("audio needs a positive channel count")
	}
	return len(samples) / channels, nil
}

func voicedFrames(samples []float32, channels int) int {
	if channels <= 0 {
		return 0
	}
	frames := len(samples) / channels
	for frame := frames - 1; frame >= 0; frame-- {
		for ch := 0; ch < channels; ch++ {
			if abs32(samples[frame*channels+ch]) >= voicedFloor {
				return frame + 1
			}
		}
	}
	return 0
}

func decodeFloat32(ctx context.Context, path string) ([]float32, error) {
	cmd := media.Command(ctx, "ffmpeg", "-nostdin", "-hide_banner", "-loglevel", "error",
		"-i", path, "-map", "0:a:0", "-f", "f32le", "-c:a", "pcm_f32le", "-")
	data, err := cmd.Output()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("ffmpeg decode %s cancelled: %w", path, ctxErr)
		}
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	values := make([]float32, len(data)/4)
	for i := range values {
		values[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[i*4:]))
	}
	return values, nil
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}
