package command

import (
	"reflect"
	"testing"
)

func TestDocumentedCommandsParseIntoSafeMutations(t *testing.T) {
	timeline := commandTimeline()

	tests := []struct {
		name    string
		input   string
		want    Mutation
		segment Segment
	}{
		{
			name:    "move padded timecode",
			input:   "move wav 1 to 0:0005 to right.",
			want:    Mutation{Kind: Move, SegmentID: 1, PositionMs: 5_000, Anchor: Right},
			segment: Segment{ID: 1, StartMs: 4_000, EndMs: 5_000, Speaker: "Ada"},
		},
		{
			name:    "shift right",
			input:   "shift line 3 right by 200ms.",
			want:    Mutation{Kind: Shift, SegmentID: 3, DeltaMs: 200, Anchor: Right},
			segment: Segment{ID: 3, StartMs: 7_200, EndMs: 8_200, Speaker: "Maya"},
		},
		{
			name:    "change speaker by unique prefix",
			input:   "change speaker for line 7 to Mark.",
			want:    Mutation{Kind: ChangeSpeaker, SegmentID: 7, Speaker: "Mark"},
			segment: Segment{ID: 7, StartMs: 12_000, EndMs: 13_000, Speaker: "Mark Vande Hei"},
		},
		{
			name:    "shorten decimal seconds",
			input:   "shorten line 4 by 0.5s.",
			want:    Mutation{Kind: Shorten, SegmentID: 4, DeltaMs: 500},
			segment: Segment{ID: 4, StartMs: 16_000, EndMs: 17_500, Speaker: "Ada"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := Parse(test.input)
			if err != nil {
				t.Fatalf("Parse(%q): %v", test.input, err)
			}
			if got != test.want {
				t.Fatalf("Parse(%q) = %#v, want %#v", test.input, got, test.want)
			}
			if err := Validate(timeline, got); err != nil {
				t.Fatalf("Validate(%q): %v", test.input, err)
			}
			updated, err := Apply(timeline, got)
			if err != nil {
				t.Fatalf("Apply(%q): %v", test.input, err)
			}
			index, ok := segmentAt(updated, test.segment.ID)
			if !ok || updated.Segments[index] != test.segment {
				t.Fatalf("Apply(%q) line %d = %#v, want %#v", test.input, test.segment.ID, updated.Segments[index], test.segment)
			}
		})
	}
}

func TestParseRejectsMalformedCommands(t *testing.T) {
	tests := []string{
		"",
		"move wav 1 to 0:60 to right",
		"move wav 1 to 0:0005 to sideways",
		"shift line 3 right by zero",
		"change speaker for line 7 Mark",
		"shorten line 4 by 0s",
	}

	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			if _, err := Parse(input); err == nil {
				t.Fatalf("Parse(%q) succeeded", input)
			}
		})
	}
}

func TestValidateRejectsUnsafeReferencesAndLeavesTimelineUnchanged(t *testing.T) {
	tests := []struct {
		name     string
		mutation Mutation
	}{
		{
			name:     "unknown line",
			mutation: Mutation{Kind: Shift, SegmentID: 99, DeltaMs: 200, Anchor: Right},
		},
		{
			name:     "unknown speaker",
			mutation: Mutation{Kind: ChangeSpeaker, SegmentID: 7, Speaker: "Nora"},
		},
		{
			name:     "ambiguous speaker",
			mutation: Mutation{Kind: ChangeSpeaker, SegmentID: 7, Speaker: "M"},
		},
		{
			name:     "past picture end",
			mutation: Mutation{Kind: Move, SegmentID: 1, PositionMs: 20_000, Anchor: Left},
		},
		{
			name:     "collision",
			mutation: Mutation{Kind: Shift, SegmentID: 3, DeltaMs: 4_500, Anchor: Right},
		},
		{
			name:     "empty duration",
			mutation: Mutation{Kind: Shorten, SegmentID: 4, DeltaMs: 2_000},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			original := commandTimeline()
			if err := Validate(original, test.mutation); err == nil {
				t.Fatalf("Validate(%#v) succeeded", test.mutation)
			}
			updated, err := Apply(original, test.mutation)
			if err == nil {
				t.Fatalf("Apply(%#v) succeeded with %#v", test.mutation, updated)
			}
			if !reflect.DeepEqual(original, commandTimeline()) {
				t.Fatalf("Apply(%#v) changed the supplied timeline: %#v", test.mutation, original)
			}
		})
	}
}

func commandTimeline() Timeline {
	return Timeline{
		DurationMs: 20_000,
		Segments: []Segment{
			{ID: 1, StartMs: 1_000, EndMs: 2_000, Speaker: "Ada"},
			{ID: 2, StartMs: 3_000, EndMs: 4_000, Speaker: "Mark Vande Hei"},
			{ID: 3, StartMs: 7_000, EndMs: 8_000, Speaker: "Maya"},
			{ID: 4, StartMs: 16_000, EndMs: 18_000, Speaker: "Ada"},
			{ID: 7, StartMs: 12_000, EndMs: 13_000, Speaker: "Ada"},
			{ID: 8, StartMs: 14_000, EndMs: 15_000, Speaker: "Mina"},
		},
	}
}
