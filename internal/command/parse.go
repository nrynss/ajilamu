// Package command turns editor instructions into safe timeline mutations.
package command

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// Kind identifies the edit that a mutation requests.
type Kind string

const (
	// Move places one segment at an absolute timeline boundary.
	Move Kind = "move"
	// Shift moves one segment relative to its current position.
	Shift Kind = "shift"
	// ChangeSpeaker assigns a known speaker to one segment.
	ChangeSpeaker Kind = "change_speaker"
	// Shorten reduces a segment's duration at its right boundary.
	Shorten Kind = "shorten"
)

// Anchor identifies which segment boundary an absolute move positions.
type Anchor string

const (
	// Left positions a segment's start boundary.
	Left Anchor = "left"
	// Right positions a segment's end boundary.
	Right Anchor = "right"
)

// Segment is the mutable timeline data needed by the command validator.
type Segment struct {
	ID      int    `json:"id"`
	StartMs int64  `json:"start_ms"`
	EndMs   int64  `json:"end_ms"`
	Speaker string `json:"speaker"`
}

// Timeline supplies the boundary and reference data for an edit.
type Timeline struct {
	DurationMs int64     `json:"duration_ms"`
	Segments   []Segment `json:"segments"`
}

// Mutation is the structured action a model may propose for confirmation.
// Values express user intent. Apply calculates the resulting timeline values.
type Mutation struct {
	Kind       Kind   `json:"kind"`
	SegmentID  int    `json:"segment_id"`
	Anchor     Anchor `json:"anchor,omitempty"`
	PositionMs int64  `json:"position_ms,omitempty"`
	DeltaMs    int64  `json:"delta_ms,omitempty"`
	Speaker    string `json:"speaker,omitempty"`
}

// Parse converts one supported command into a structured mutation.
// It does not inspect a timeline or perform any timing arithmetic.
func Parse(input string) (Mutation, error) {
	words := strings.Fields(strings.TrimSpace(strings.TrimSuffix(input, ".")))
	if len(words) == 0 {
		return Mutation{}, fmt.Errorf("editor command is empty")
	}

	switch strings.ToLower(words[0]) {
	case "move":
		return parseMove(words)
	case "shift":
		return parseShift(words)
	case "change":
		return parseSpeaker(words)
	case "shorten":
		return parseShorten(words)
	default:
		return Mutation{}, fmt.Errorf("cannot understand command %q", input)
	}
}

// Validate rejects references and values that cannot safely change timeline.
//
// It rejects an overlap the mutation would introduce or deepen. It accepts an
// overlap the stored timeline already holds, because the creator confirms one
// through the boundary editor and the result is valid stored state.
func Validate(timeline Timeline, mutation Mutation) error {
	if err := validateTimeline(timeline); err != nil {
		return err
	}
	index, ok := segmentAt(timeline, mutation.SegmentID)
	if !ok {
		return fmt.Errorf("line %d does not exist", mutation.SegmentID)
	}

	switch mutation.Kind {
	case Move:
		if mutation.Anchor != Left && mutation.Anchor != Right {
			return fmt.Errorf("move line %d needs a left or right boundary", mutation.SegmentID)
		}
		if mutation.PositionMs < 0 || mutation.PositionMs > timeline.DurationMs {
			return fmt.Errorf("move position %s is outside this timeline", formatTime(mutation.PositionMs))
		}
	case Shift:
		if mutation.Anchor != Left && mutation.Anchor != Right {
			return fmt.Errorf("shift line %d needs a left or right direction", mutation.SegmentID)
		}
		if mutation.DeltaMs <= 0 {
			return fmt.Errorf("shift amount must be greater than zero")
		}
	case ChangeSpeaker:
		if strings.TrimSpace(mutation.Speaker) == "" {
			return fmt.Errorf("speaker name is required for line %d", mutation.SegmentID)
		}
		if _, err := resolveSpeaker(timeline, mutation.Speaker); err != nil {
			return err
		}
	case Shorten:
		if mutation.DeltaMs <= 0 {
			return fmt.Errorf("shorten amount must be greater than zero")
		}
	default:
		return fmt.Errorf("unsupported editor mutation %q", mutation.Kind)
	}

	candidate := mutate(timeline, index, mutation)
	if candidate.StartMs < 0 || candidate.EndMs > timeline.DurationMs || candidate.EndMs <= candidate.StartMs {
		return fmt.Errorf("line %d would fall outside this timeline", candidate.ID)
	}
	if overlap, ok := overlapping(timeline.Segments, index, candidate); ok {
		return fmt.Errorf("line %d would overlap line %d", candidate.ID, overlap.ID)
	}

	return nil
}

// Apply validates a confirmed mutation, calculates its effect, and returns a copy.
// The supplied timeline remains unchanged when validation fails.
func Apply(timeline Timeline, mutation Mutation) (Timeline, error) {
	if err := Validate(timeline, mutation); err != nil {
		return Timeline{}, err
	}

	result := Timeline{DurationMs: timeline.DurationMs, Segments: append([]Segment(nil), timeline.Segments...)}
	index, _ := segmentAt(result, mutation.SegmentID)
	result.Segments[index] = mutate(result, index, mutation)
	return result, nil
}

// Describe returns the confirmation sentence for a validated mutation.
func Describe(timeline Timeline, mutation Mutation) (string, error) {
	if err := Validate(timeline, mutation); err != nil {
		return "", err
	}

	switch mutation.Kind {
	case Move:
		boundary := "start"
		if mutation.Anchor == Right {
			boundary = "end"
		}
		return fmt.Sprintf("Move line %d so its %s is at %s.", mutation.SegmentID, boundary, formatTime(mutation.PositionMs)), nil
	case Shift:
		return fmt.Sprintf("Shift line %d %s by %s.", mutation.SegmentID, mutation.Anchor, formatDuration(mutation.DeltaMs)), nil
	case ChangeSpeaker:
		speaker, _ := resolveSpeaker(timeline, mutation.Speaker)
		return fmt.Sprintf("Change line %d speaker to %s.", mutation.SegmentID, speaker), nil
	case Shorten:
		return fmt.Sprintf("Shorten line %d by %s.", mutation.SegmentID, formatDuration(mutation.DeltaMs)), nil
	default:
		return "", fmt.Errorf("unsupported editor mutation %q", mutation.Kind)
	}
}

func parseMove(words []string) (Mutation, error) {
	if len(words) != 7 || (strings.ToLower(words[1]) != "wav" && strings.ToLower(words[1]) != "line") || strings.ToLower(words[3]) != "to" || strings.ToLower(words[5]) != "to" {
		return Mutation{}, fmt.Errorf("use: move wav 1 to 0:0005 to right")
	}
	id, err := parseLineID(words[2])
	if err != nil {
		return Mutation{}, err
	}
	position, err := parseTime(words[4])
	if err != nil {
		return Mutation{}, err
	}
	anchor, err := parseAnchor(words[6])
	if err != nil {
		return Mutation{}, err
	}
	return Mutation{Kind: Move, SegmentID: id, PositionMs: position, Anchor: anchor}, nil
}

func parseShift(words []string) (Mutation, error) {
	if len(words) != 6 || strings.ToLower(words[1]) != "line" || strings.ToLower(words[3]) != "left" && strings.ToLower(words[3]) != "right" || strings.ToLower(words[4]) != "by" {
		return Mutation{}, fmt.Errorf("use: shift line 3 right by 200ms")
	}
	id, err := parseLineID(words[2])
	if err != nil {
		return Mutation{}, err
	}
	delta, err := parseDuration(words[5])
	if err != nil {
		return Mutation{}, err
	}
	anchor, _ := parseAnchor(words[3])
	return Mutation{Kind: Shift, SegmentID: id, DeltaMs: delta, Anchor: anchor}, nil
}

func parseSpeaker(words []string) (Mutation, error) {
	if len(words) < 7 || strings.ToLower(words[1]) != "speaker" || strings.ToLower(words[2]) != "for" || strings.ToLower(words[3]) != "line" || strings.ToLower(words[5]) != "to" {
		return Mutation{}, fmt.Errorf("use: change speaker for line 7 to Mark")
	}
	id, err := parseLineID(words[4])
	if err != nil {
		return Mutation{}, err
	}
	speaker := strings.TrimSpace(strings.Join(words[6:], " "))
	if speaker == "" {
		return Mutation{}, fmt.Errorf("speaker name is required for line %d", id)
	}
	return Mutation{Kind: ChangeSpeaker, SegmentID: id, Speaker: speaker}, nil
}

func parseShorten(words []string) (Mutation, error) {
	if len(words) != 5 || strings.ToLower(words[1]) != "line" || strings.ToLower(words[3]) != "by" {
		return Mutation{}, fmt.Errorf("use: shorten line 4 by 0.5s")
	}
	id, err := parseLineID(words[2])
	if err != nil {
		return Mutation{}, err
	}
	delta, err := parseDuration(words[4])
	if err != nil {
		return Mutation{}, err
	}
	return Mutation{Kind: Shorten, SegmentID: id, DeltaMs: delta}, nil
}

func parseLineID(raw string) (int, error) {
	id, err := strconv.Atoi(raw)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("line reference %q must be a positive number", raw)
	}
	return id, nil
}

func parseAnchor(raw string) (Anchor, error) {
	switch strings.ToLower(raw) {
	case string(Left):
		return Left, nil
	case string(Right):
		return Right, nil
	default:
		return "", fmt.Errorf("direction %q must be left or right", raw)
	}
}

func parseTime(raw string) (int64, error) {
	parts := strings.Split(raw, ":")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return 0, fmt.Errorf("time %q must use minutes:seconds", raw)
	}
	minutes, err := positiveNumber(parts[0])
	if err != nil {
		return 0, fmt.Errorf("time %q has invalid minutes", raw)
	}
	seconds, err := decimalMilliseconds(parts[1])
	if err != nil || seconds >= 60_000 {
		return 0, fmt.Errorf("time %q has invalid seconds", raw)
	}
	if minutes > (int64(^uint64(0)>>1)-seconds)/60_000 {
		return 0, fmt.Errorf("time %q is too large", raw)
	}
	return minutes*60_000 + seconds, nil
}

func parseDuration(raw string) (int64, error) {
	raw = strings.ToLower(raw)
	if strings.HasSuffix(raw, "ms") {
		milliseconds, err := positiveNumber(strings.TrimSuffix(raw, "ms"))
		if err != nil || milliseconds <= 0 {
			return 0, fmt.Errorf("duration %q must be greater than zero", raw)
		}
		return milliseconds, nil
	}
	if strings.HasSuffix(raw, "s") {
		milliseconds, err := decimalMilliseconds(strings.TrimSuffix(raw, "s"))
		if err != nil || milliseconds <= 0 {
			return 0, fmt.Errorf("duration %q must be greater than zero", raw)
		}
		return milliseconds, nil
	}
	return 0, fmt.Errorf("duration %q must end in ms or s", raw)
}

func positiveNumber(raw string) (int64, error) {
	if raw == "" {
		return 0, fmt.Errorf("empty number")
	}
	for _, char := range raw {
		if !unicode.IsDigit(char) {
			return 0, fmt.Errorf("not a number")
		}
	}
	return strconv.ParseInt(raw, 10, 64)
}

func decimalMilliseconds(raw string) (int64, error) {
	whole, fraction, hasFraction := strings.Cut(raw, ".")
	if strings.Contains(fraction, ".") {
		return 0, fmt.Errorf("too many decimal points")
	}
	seconds, err := positiveNumber(whole)
	if err != nil {
		return 0, err
	}
	if !hasFraction {
		if seconds > int64(^uint64(0)>>1)/1_000 {
			return 0, fmt.Errorf("number is too large")
		}
		return seconds * 1_000, nil
	}
	if len(fraction) == 0 || len(fraction) > 3 {
		return 0, fmt.Errorf("fraction must have one to three digits")
	}
	for _, char := range fraction {
		if !unicode.IsDigit(char) {
			return 0, fmt.Errorf("invalid fraction")
		}
	}
	milliseconds, _ := positiveNumber(fraction)
	for len(fraction) < 3 {
		milliseconds *= 10
		fraction += "0"
	}
	if seconds > (int64(^uint64(0)>>1)-milliseconds)/1_000 {
		return 0, fmt.Errorf("number is too large")
	}
	return seconds*1_000 + milliseconds, nil
}

func validateTimeline(timeline Timeline) error {
	if timeline.DurationMs <= 0 {
		return fmt.Errorf("timeline duration must be greater than zero")
	}
	seen := make(map[int]struct{}, len(timeline.Segments))
	for _, segment := range timeline.Segments {
		if segment.ID <= 0 || segment.StartMs < 0 || segment.EndMs <= segment.StartMs || segment.EndMs > timeline.DurationMs {
			return fmt.Errorf("line %d has invalid timeline bounds", segment.ID)
		}
		if _, exists := seen[segment.ID]; exists {
			return fmt.Errorf("timeline repeats line %d", segment.ID)
		}
		seen[segment.ID] = struct{}{}
	}
	return nil
}

func segmentAt(timeline Timeline, id int) (int, bool) {
	for index, segment := range timeline.Segments {
		if segment.ID == id {
			return index, true
		}
	}
	return 0, false
}

func resolveSpeaker(timeline Timeline, requested string) (string, error) {
	requested = strings.TrimSpace(requested)
	var matches []string
	for _, segment := range timeline.Segments {
		name := strings.TrimSpace(segment.Speaker)
		if name == "" {
			continue
		}
		if strings.EqualFold(name, requested) {
			return name, nil
		}
		if strings.HasPrefix(strings.ToLower(name), strings.ToLower(requested)) && !contains(matches, name) {
			matches = append(matches, name)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("speaker %q is ambiguous", requested)
	}
	return "", fmt.Errorf("speaker %q does not exist in this timeline", requested)
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func mutate(timeline Timeline, index int, mutation Mutation) Segment {
	segment := timeline.Segments[index]
	duration := segment.EndMs - segment.StartMs

	switch mutation.Kind {
	case Move:
		if mutation.Anchor == Left {
			segment.StartMs = mutation.PositionMs
			segment.EndMs = mutation.PositionMs + duration
		} else {
			segment.EndMs = mutation.PositionMs
			segment.StartMs = mutation.PositionMs - duration
		}
	case Shift:
		if mutation.Anchor == Left {
			segment.StartMs -= mutation.DeltaMs
			segment.EndMs -= mutation.DeltaMs
		} else {
			segment.StartMs += mutation.DeltaMs
			segment.EndMs += mutation.DeltaMs
		}
	case ChangeSpeaker:
		segment.Speaker, _ = resolveSpeaker(timeline, mutation.Speaker)
	case Shorten:
		segment.EndMs -= mutation.DeltaMs
	}

	return segment
}

// overlapping returns the first line the candidate would newly overlap or
// overlap more deeply than the stored segment already does.
//
// The comparison runs per line rather than against a flat rule. A speaker change
// moves no boundary, so every comparison holds equal and the mutation passes. A
// move or a shift that reaches into another line grows one comparison and fails.
// A mutation that shrinks a confirmed overlap also passes.
func overlapping(segments []Segment, current int, candidate Segment) (Segment, bool) {
	stored := segments[current]
	for index, other := range segments {
		if index == current {
			continue
		}
		if overlapMs(candidate, other) > overlapMs(stored, other) {
			return other, true
		}
	}
	return Segment{}, false
}

// overlapMs returns the milliseconds two slots share. Slots that only touch at a
// boundary share nothing, so the function returns zero for them.
func overlapMs(first, second Segment) int64 {
	start := max(first.StartMs, second.StartMs)
	end := min(first.EndMs, second.EndMs)
	if end <= start {
		return 0
	}
	return end - start
}

func formatTime(milliseconds int64) string {
	minutes := milliseconds / 60_000
	seconds := milliseconds % 60_000
	return fmt.Sprintf("%d:%06.3f", minutes, float64(seconds)/1_000)
}

func formatDuration(milliseconds int64) string {
	return fmt.Sprintf("%.3fs", float64(milliseconds)/1_000)
}
