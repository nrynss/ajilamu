// Package api defines the JSON payloads exchanged between the server and the
// web client.
package api

import (
	"time"

	"github.com/nrynss/ajilamu/internal/cost"
	"github.com/nrynss/ajilamu/internal/types"
)

// Readiness reports the processing state of a dub.
const (
	// ReadinessPending means the dub waits for its first run.
	ReadinessPending = "pending"
	// ReadinessRunning means a run is active right now.
	ReadinessRunning = "running"
	// ReadinessReview means the last run ended with flagged lines.
	ReadinessReview = "review"
	// ReadinessReady means the dub is finished and playable.
	ReadinessReady = "ready"
)

// FitState names how a take relates to its slot.
const (
	// StateFits means the take lands inside the tolerance.
	StateFits = "fits"
	// StateTooLong means the take exceeds the slot past tolerance.
	StateTooLong = "too_long"
	// StateTooShort means the take falls short of the slot past tolerance.
	StateTooShort = "too_short"
)

// RepairKind names the strategy that produced a take file.
const (
	// RepairNone means the take needed no repair.
	RepairNone = "none"
	// RepairAtempo means atempo stretched the audio to fit.
	RepairAtempo = "atempo"
	// RepairRewrite means a new translation replaced the line.
	RepairRewrite = "rewrite"
	// RepairManual means a creator fixed the take by hand.
	RepairManual = "manual"
)

// ChargeKind names the billed API operation.
const (
	// ChargeSegment bills the Gemini segmentation pass.
	ChargeSegment = "segment"
	// ChargeTranslate bills a Gemini translation.
	ChargeTranslate = "translate"
	// ChargeSynthesize bills a Chirp voice render.
	ChargeSynthesize = "synthesize"
	// ChargeAgent bills an editor agent turn.
	ChargeAgent = "agent"
)

// CommitAuthor names who caused a commit.
const (
	// AuthorAgent means the pipeline acted on its own.
	AuthorAgent = "agent"
	// AuthorCommandBar means the creator typed a command.
	AuthorCommandBar = "command_bar"
	// AuthorManualUI means the creator edited the interface.
	AuthorManualUI = "manual_ui"
)

// CommitAction names what a commit changed.
// The vocabulary follows observations.md section 5, with text_corrected
// added for transcript edits on the timeline.
const (
	// ActionSegmentCreated adds detected segments.
	ActionSegmentCreated = "segment_created"
	// ActionBoundaryNudged moves a segment boundary.
	ActionBoundaryNudged = "boundary_nudged"
	// ActionSpeakerReassigned changes a segment speaker.
	ActionSpeakerReassigned = "speaker_reassigned"
	// ActionTextCorrected edits transcribed text.
	ActionTextCorrected = "text_corrected"
	// ActionTakeRendered adds a rendered take.
	ActionTakeRendered = "take_rendered"
	// ActionAtempoStretched applies an atempo repair.
	ActionAtempoStretched = "atempo_stretched"
	// ActionLineRewritten replaces a translated line.
	ActionLineRewritten = "line_rewritten"
	// ActionUserCommand applies a command-bar instruction.
	ActionUserCommand = "user_command"
)

// EventType discriminates progress notifications.
const (
	// EventProgress reports activity inside a run.
	EventProgress = "progress"
	// EventDone reports a finished run.
	EventDone = "done"
	// EventError reports a failed run.
	EventError = "error"
)

// EventStage names the pipeline step behind an event.
const (
	// StageSegmenting detects dialogue segments.
	StageSegmenting = "segmenting"
	// StageTranslating writes target-language lines.
	StageTranslating = "translating"
	// StageSynthesizing renders voice audio.
	StageSynthesizing = "synthesizing"
	// StageMeasuring times a take against its slot.
	StageMeasuring = "measuring"
	// StageRepairing fixes takes that miss their slot.
	StageRepairing = "repairing"
	// StageAssembling mixes takes into the film audio.
	StageAssembling = "assembling"
	// StageExporting writes the final video.
	StageExporting = "exporting"
)

// DubIndex lists dubs for the home route, newest first.
type DubIndex struct {
	// Dubs holds one summary per dub.
	Dubs []DubSummary `json:"dubs"`
}

// DubSummary is one row in the dub index.
type DubSummary struct {
	// ID identifies the dub.
	ID string `json:"id"`
	// Title names the project for the creator.
	Title string `json:"title"`
	// Languages lists target language codes.
	Languages []string `json:"languages"`
	// Readiness reports the processing state.
	Readiness string `json:"readiness"`
	// TotalNanodollars is the running cost so far.
	TotalNanodollars cost.Price `json:"total_nanodollars"`
	// CreatedAt is the creation time as RFC 3339.
	CreatedAt string `json:"created_at"`
	// UpdatedAt is the last change time as RFC 3339.
	UpdatedAt string `json:"updated_at"`
}

// Dub is the full workspace payload for one project.
// One Dub payload drives the timeline and all three rail tabs.
type Dub struct {
	// ID identifies the dub.
	ID string `json:"id"`
	// Title names the project for the creator.
	Title string `json:"title"`
	// SourceLanguage is the film language code.
	SourceLanguage string `json:"source_language"`
	// Readiness reports the processing state.
	Readiness string `json:"readiness"`
	// Segments lists the source track lines.
	Segments []Segment `json:"segments"`
	// Languages holds one track per target language.
	Languages []LanguageTrack `json:"languages"`
	// Charges lists whole-pass charges that no single take owns.
	Charges []Charge `json:"charges"`
	// Total states the running cost and what it covers.
	Total Total `json:"total"`
	// Commits forms the history DAG, oldest first.
	Commits []Commit `json:"commits"`
	// CreatedAt is the creation time as RFC 3339.
	CreatedAt string `json:"created_at"`
	// UpdatedAt is the last change time as RFC 3339.
	UpdatedAt string `json:"updated_at"`
}

// Segment is one source-language dialogue block.
type Segment struct {
	// ID numbers the line inside the dub.
	ID int `json:"id"`
	// StartMs locates the slot start in the film.
	StartMs int64 `json:"start_ms"`
	// EndMs locates the slot end in the film.
	EndMs int64 `json:"end_ms"`
	// DurationMs is the slot length, end minus start.
	DurationMs int64 `json:"duration_ms"`
	// Text is the transcribed source line.
	Text string `json:"text"`
	// Speaker names the person talking.
	Speaker string `json:"speaker"`
	// Emotion describes how the line is spoken.
	Emotion string `json:"emotion"`
}

// LanguageTrack carries one target language.
type LanguageTrack struct {
	// Language is the target language code.
	Language string `json:"language"`
	// Lines holds one entry per source segment.
	Lines []Line `json:"lines"`
}

// Line is one translated line bound to a segment.
type Line struct {
	// SegmentID binds the line to a source segment.
	SegmentID int `json:"segment_id"`
	// Text is the current target-language text.
	Text string `json:"text"`
	// Flagged marks a line that no attempt could fit.
	Flagged bool `json:"flagged"`
	// Takes lists attempts oldest first. The last take is active.
	Takes []Take `json:"takes"`
}

// Take is one recorded attempt for a line.
type Take struct {
	// File is the asset path of the take audio.
	File string `json:"file"`
	// Voice names the voice used for this take.
	Voice string `json:"voice"`
	// Attempt orders takes within the line, starting at one.
	Attempt int `json:"attempt"`
	// Repair names the strategy that produced this file.
	Repair string `json:"repair"`
	// StretchFactorMilli is the atempo ratio in thousandths.
	// A value of 1075 means playback at 107.5 percent speed.
	StretchFactorMilli int64 `json:"stretch_factor_milli,omitempty"`
	// Fit measures this take against its slot.
	Fit Fit `json:"fit"`
	// Charges itemizes what this attempt cost.
	Charges []Charge `json:"charges"`
	// Peaks holds 64 to 128 waveform values when present.
	// Each value is an unsigned byte normalized to 0 through 255.
	Peaks []int `json:"peaks,omitempty"`
}

// Fit compares one take against its dialogue slot.
// It mirrors internal/types.Fit with integer milliseconds.
type Fit struct {
	// SlotMs is the allowed slot length.
	SlotMs int64 `json:"slot_ms"`
	// MeasuredMs is the take length as measured.
	MeasuredMs int64 `json:"measured_ms"`
	// DeltaMs is measured minus slot, signed.
	// Positive delta means long. Negative delta means short.
	DeltaMs int64 `json:"delta_ms"`
	// State reports fits, too_long, or too_short.
	State string `json:"state"`
}

// FitState derives the wire state from measured durations.
// It applies internal/types.Fit semantics, so a short take never
// serializes as fits.
func FitState(slotMs, measuredMs int64) string {
	slot := time.Duration(slotMs) * time.Millisecond
	measured := time.Duration(measuredMs) * time.Millisecond
	f := types.NewFit(slot, measured)
	switch {
	case f.TooLong():
		return StateTooLong
	case f.TooShort():
		return StateTooShort
	default:
		return StateFits
	}
}

// Charge is one billed API call.
// Money values are exact nanodollar integers, never floats.
type Charge struct {
	// Kind names the billed operation.
	Kind string `json:"kind"`
	// SegmentID names the line a charge serves. Null marks whole-pass work.
	SegmentID *int `json:"segment_id"`
	// TakeFile names the attempt a charge serves. Empty marks whole-pass work.
	TakeFile string `json:"take_file"`
	// Units counts billed characters or tokens.
	Units int64 `json:"units"`
	// UnitPriceNanodollars is the price of one unit.
	UnitPriceNanodollars cost.Price `json:"unit_price_nanodollars"`
	// TotalNanodollars is units times unit price, exact.
	TotalNanodollars cost.Price `json:"total_nanodollars"`
}

// Total states the running project cost.
type Total struct {
	// TotalNanodollars is the exact running sum.
	TotalNanodollars cost.Price `json:"total_nanodollars"`
	// Covers explains in words what the total includes.
	Covers string `json:"covers"`
}

// Commit is one node in the history DAG.
type Commit struct {
	// CommitID identifies the node.
	CommitID string `json:"commit_id"`
	// ParentCommitID links the previous node. Empty marks the root.
	ParentCommitID string `json:"parent_commit_id"`
	// VersionNumber counts commits along the chain.
	VersionNumber int `json:"version_number"`
	// CreatedAt is the commit time as RFC 3339.
	CreatedAt string `json:"created_at"`
	// Action names what the commit changed.
	Action string `json:"action"`
	// Author names who caused the change.
	Author string `json:"author"`
	// Instruction keeps the original command verbatim when one exists.
	Instruction string `json:"instruction"`
}

// DubHistory is the commit DAG payload for one dub.
type DubHistory struct {
	// Commits lists every commit, oldest first.
	Commits []Commit `json:"commits"`
}

// TimelineView is the timeline snapshot at one commit.
type TimelineView struct {
	// CommitID names the commit the state comes from.
	CommitID string `json:"commit_id"`
	// Language names the target language track.
	Language string `json:"language"`
	// Segments lists the line state at that commit.
	Segments []TimelineEntry `json:"segments"`
}

// TimelineEntry is one line snapshot at one commit.
type TimelineEntry struct {
	// SegmentIndex numbers the line inside the dub.
	SegmentIndex int `json:"segment_index"`
	// StartMs locates the slot start in the film.
	StartMs int64 `json:"start_ms"`
	// EndMs locates the slot end in the film.
	EndMs int64 `json:"end_ms"`
	// Speaker names the person talking.
	Speaker string `json:"speaker"`
	// Emotion describes how the line is spoken.
	Emotion string `json:"emotion"`
	// SourceText is the transcribed source line.
	SourceText string `json:"source_text"`
	// Text is the target-language line.
	Text string `json:"text"`
	// TakeID names the active take.
	TakeID string `json:"take_id"`
	// VersionSeq orders the snapshot state.
	VersionSeq uint64 `json:"version_seq"`
}

// BranchComparison holds metrics for two heads of one language track.
type BranchComparison struct {
	// A reports the first head.
	A BranchSummary `json:"a"`
	// B reports the second head.
	B BranchSummary `json:"b"`
}

// BranchSummary reports one head of a language track.
type BranchSummary struct {
	// CommitID identifies the head.
	CommitID string `json:"commit_id"`
	// Branch names the branch label.
	Branch string `json:"branch"`
	// SlotMs sums the reconstructed slot lengths.
	SlotMs int64 `json:"slot_ms"`
	// TakeCount counts the takes at the head.
	TakeCount int `json:"take_count"`
	// AttributedCostUSD sums attributed charges as a decimal string.
	AttributedCostUSD string `json:"attributed_cost_usd"`
}

// ProgressEvent is one server-sent notification body.
// Every event carries a complete sentence and the running cost.
type ProgressEvent struct {
	// Type discriminates progress, done, and error events.
	Type string `json:"type"`
	// Stage names the pipeline step behind the event.
	Stage string `json:"stage"`
	// Sentence is a complete natural-language update.
	Sentence string `json:"sentence"`
	// TotalNanodollars is the running project cost so far.
	TotalNanodollars cost.Price `json:"total_nanodollars"`
	// SegmentID names the line in progress. Zero marks none.
	SegmentID int `json:"segment_id"`
	// Language names the target language involved.
	Language string `json:"language"`
	// TakeFile names the attempt involved when one exists.
	TakeFile string `json:"take_file"`
}

// CatalogSource names where the supported-language list came from.
const (
	// CatalogSourceCommitted is the list committed to this repository.
	CatalogSourceCommitted = "committed"
	// CatalogSourceProvider is a list fetched from Cloud Text-to-Speech.
	CatalogSourceProvider = "provider"
)

// LanguageCatalog is the supported-language list the create screen offers.
// GET /api/languages serves it, and POST /api/languages/refresh replaces it.
type LanguageCatalog struct {
	// Languages lists supported BCP-47 codes, sorted.
	Languages []string `json:"languages"`
	// Source names where the list came from.
	Source string `json:"source"`
	// FetchedAt is the RFC 3339 time of the last provider fetch.
	// It is absent for the committed list.
	FetchedAt string `json:"fetched_at,omitempty"`
}
