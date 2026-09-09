package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"

	editorcommand "github.com/nrynss/ajilamu/internal/command"
)

// Edit kind names the two creator confirmations this route records.
const (
	// EditKindBoundary records a dragged segment boundary.
	EditKindBoundary = "boundary"
	// EditKindCommand records a confirmed command-bar instruction.
	EditKindCommand = "command"
)

// Edit failure sentences. A failure never carries internal detail, except the
// command validator, whose sentence names the rejected instruction.
const (
	editsUnavailable  = "Timeline editing is unavailable."
	editsBadBody      = "The edit request is not valid JSON."
	editsNoLanguage   = "The edit needs a target language."
	editsBadKind      = "That edit kind is not supported."
	editsBadCommand   = "The edit needs a command to apply."
	editsNoLine       = "That line does not exist in this project."
	editsNoTimeline   = "This project has no timeline to edit."
	editsBadTiming    = "Those boundaries are not valid for this line."
	editsOverlap      = "That boundary would overlap another line."
	editsReadFailed   = "Could not read the project ledger."
	editsRecordFailed = "The edit could not be saved."
)

// EditRecorder records one confirmed timeline edit as a ledger commit.
//
// The adapter lives in cmd/ajilamu/main.go, so this package never imports
// internal/ledger. The route mints the identity fields before it calls here,
// and it holds the dub's commit lock across the call. A recorder may therefore
// read the head and append its child without forking the commit DAG.
type EditRecorder interface {
	// RecordEdit writes one commit, one action and one timeline snapshot.
	RecordEdit(ctx context.Context, edit EditRecord) error
}

// commitLocks serializes every writer that reads a dub head and appends a
// child commit. One lock per dub spans the head read, the derivation, the
// append and the final flush, so two writers of one dub never compute from one
// head. The commit DAG stays a chain and the head timeline keeps every commit.
type commitLocks struct {
	mu   sync.Mutex
	held map[string]*sync.Mutex
}

// newCommitLocks returns an empty per-dub lock set.
func newCommitLocks() *commitLocks {
	return &commitLocks{held: make(map[string]*sync.Mutex)}
}

// lock acquires one dub's lock and returns the release function.
func (l *commitLocks) lock(dubID string) func() {
	l.mu.Lock()
	entry, ok := l.held[dubID]
	if !ok {
		entry = &sync.Mutex{}
		l.held[dubID] = entry
	}
	l.mu.Unlock()
	entry.Lock()
	return entry.Unlock
}

// dubCommitLocks is the one per-dub lock set every head reader and appender
// shares. EditsHandler takes it, and the run recorder in cmd/ajilamu takes it
// around the run and re-render append.
var dubCommitLocks = newCommitLocks()

// LockDubCommit acquires the process-wide commit lock for one dub and returns
// the release function. Every writer that reads a dub head and appends a child
// holds it across both and flushes before it releases, so the next writer reads
// the commit that landed last.
func LockDubCommit(dubID string) func() {
	return dubCommitLocks.lock(dubID)
}

// EditSnapshot is one complete timeline row. The adapter copies every field
// forward, because the ledger reader never merges two rows.
type EditSnapshot struct {
	// SegmentIndex numbers the line inside the dub.
	SegmentIndex int
	// StartMs locates the slot start in the film.
	StartMs int64
	// EndMs locates the slot end in the film.
	EndMs int64
	// Speaker names the person talking.
	Speaker string
	// Emotion describes how the line is spoken.
	Emotion string
	// SourceText is the transcribed source line.
	SourceText string
	// Text is the target-language line.
	Text string
	// TakeID names the active take.
	TakeID string
}

// EditRecord is one confirmed edit ready for the ledger.
type EditRecord struct {
	// CommitID identifies the commit this edit appends.
	CommitID string
	// ProjectID groups the edit rows. The dub id is the project id here.
	ProjectID string
	// DubID identifies the project the line belongs to.
	DubID string
	// OwnerID names the creator who owns the dub.
	OwnerID string
	// Language names the target language track the snapshot belongs to.
	Language string
	// Action names what the commit changed.
	Action string
	// Author names who caused the change.
	Author string
	// Prompt keeps a command-bar instruction verbatim. It is empty otherwise.
	Prompt string
	// Message is the commit sentence.
	Message string
	// BeforeValue is JSON text that explains the state before the edit.
	BeforeValue string
	// AfterValue is JSON text that explains the state after the edit.
	AfterValue string
	// Segment is the timeline snapshot at the new head.
	Segment EditSnapshot
}

// EditBody is the JSON body of the edit route.
// A boundary sends start_ms and end_ms. A command sends the instruction.
type EditBody struct {
	// Kind is boundary or command.
	Kind string `json:"kind"`
	// Language names the target language track to edit.
	Language string `json:"language"`
	// SegmentID numbers the edited line.
	SegmentID int `json:"segment_id,omitempty"`
	// StartMs is the dragged slot start. Absent for a command.
	StartMs *int64 `json:"start_ms,omitempty"`
	// EndMs is the dragged slot end. Absent for a command.
	EndMs *int64 `json:"end_ms,omitempty"`
	// Command is the confirmed instruction verbatim.
	Command string `json:"command,omitempty"`
	// DurationMs is the timeline length the command validator bounds against.
	DurationMs int64 `json:"duration_ms,omitempty"`
}

// EditSegment is the resulting line state the page applies.
type EditSegment struct {
	// ID numbers the line inside the dub.
	ID int `json:"id"`
	// StartMs locates the slot start in the film.
	StartMs int64 `json:"start_ms"`
	// EndMs locates the slot end in the film.
	EndMs int64 `json:"end_ms"`
	// DurationMs is the slot length, end minus start.
	DurationMs int64 `json:"duration_ms"`
	// Speaker names the person talking.
	Speaker string `json:"speaker"`
}

// EditResponse is the 201 body of the edit route.
type EditResponse struct {
	// CommitID names the commit the edit was recorded under.
	CommitID string `json:"commit_id"`
	// Action names what the commit changed.
	Action string `json:"action"`
	// Author names who caused the change.
	Author string `json:"author"`
	// Segment is the resulting line state.
	Segment EditSegment `json:"segment"`
	// Sentence describes the result in one line of prose.
	Sentence string `json:"sentence"`
}

// editValues is the JSON text the ledger stores in before_value and after_value.
type editValues struct {
	StartMs int64  `json:"start_ms"`
	EndMs   int64  `json:"end_ms"`
	Speaker string `json:"speaker"`
}

// EditsHandler serves POST /api/dubs/{id}/edits.
//
// It holds the dub's shared commit lock across the head read, the derivation
// and the recorder call, so no edit of one dub computes from a head another
// writer can move. It records one commit, one action and one timeline
// snapshot. A boundary drag writes the manual_ui author. A command writes the
// command_bar author and keeps the instruction as the prompt. The route never
// trusts a browser result. It validates a boundary against the head timeline
// and derives a command from the deterministic parser.
func EditsHandler(recorder EditRecorder, history HistoryReader, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if recorder == nil || history == nil {
			writeHistoryFailure(w, http.StatusServiceUnavailable, editsUnavailable)
			return
		}
		dubID := strings.TrimSpace(r.PathValue("id"))
		if !safeProjectID(dubID) {
			writeHistoryFailure(w, http.StatusNotFound, workspaceNotFound)
			return
		}
		body, err := decodeEditBody(w, r)
		if err != nil {
			writeHistoryFailure(w, http.StatusBadRequest, editsBadBody)
			return
		}
		language := strings.TrimSpace(body.Language)
		if language == "" {
			writeHistoryFailure(w, http.StatusBadRequest, editsNoLanguage)
			return
		}
		// One commit writer at a time per dub. The lock spans the head read,
		// the derivation and the recorder, so the commit parent and version_seq
		// come from a head no other writer can move.
		unlock := LockDubCommit(dubID)
		defer unlock()

		commits, err := history.ListCommits(r.Context(), dubID)
		if err != nil {
			logHistoryFailure(logger, "read the edit head", err)
			writeHistoryFailure(w, http.StatusInternalServerError, editsReadFailed)
			return
		}
		if len(commits) == 0 {
			writeHistoryFailure(w, http.StatusNotFound, editsNoTimeline)
			return
		}
		head := commits[len(commits)-1].CommitID
		entries, err := history.TimelineAt(r.Context(), dubID, language, head)
		if err != nil {
			logHistoryFailure(logger, "read the edit timeline", err)
			writeHistoryFailure(w, http.StatusInternalServerError, editsReadFailed)
			return
		}

		before, after, action, author, prompt, message, problem, status := editOutcome(body, entries)
		if problem != "" {
			writeHistoryFailure(w, status, problem)
			return
		}
		record := EditRecord{
			CommitID:    newRunID(),
			ProjectID:   dubID,
			DubID:       dubID,
			OwnerID:     localOwnerID,
			Language:    language,
			Action:      action,
			Author:      author,
			Prompt:      prompt,
			Message:     message,
			BeforeValue: editValuesJSON(before),
			AfterValue:  editValuesJSON(after),
			Segment: EditSnapshot{
				SegmentIndex: after.SegmentIndex,
				StartMs:      after.StartMs,
				EndMs:        after.EndMs,
				Speaker:      after.Speaker,
				Emotion:      after.Emotion,
				SourceText:   after.SourceText,
				Text:         after.Text,
				TakeID:       after.TakeID,
			},
		}
		if err := recorder.RecordEdit(r.Context(), record); err != nil {
			logHistoryFailure(logger, "record the timeline edit", err)
			writeHistoryFailure(w, http.StatusInternalServerError, editsRecordFailed)
			return
		}
		writeHistoryJSON(w, http.StatusCreated, EditResponse{
			CommitID: record.CommitID,
			Action:   action,
			Author:   author,
			Segment: EditSegment{
				ID:         after.SegmentIndex,
				StartMs:    after.StartMs,
				EndMs:      after.EndMs,
				DurationMs: after.EndMs - after.StartMs,
				Speaker:    after.Speaker,
			},
			Sentence: editSentence(action, after),
		})
	})
}

// editOutcome derives the before and after snapshots and the ledger provenance
// for one edit body. A non-empty problem is the failure sentence, and status is
// the HTTP code that carries it.
func editOutcome(body EditBody, entries []TimelineEntry) (before, after TimelineEntry, action, author, prompt, message, problem string, status int) {
	switch strings.ToLower(strings.TrimSpace(body.Kind)) {
	case EditKindBoundary:
		if body.StartMs == nil || body.EndMs == nil {
			return before, after, "", "", "", "", editsBadTiming, http.StatusBadRequest
		}
		existing, target, problem := boundaryEntry(entries, body.SegmentID, *body.StartMs, *body.EndMs)
		if problem != "" {
			return before, after, "", "", "", "", problem, http.StatusBadRequest
		}
		return existing, target, ActionBoundaryNudged, AuthorManualUI, "",
			"Nudged the boundary of line " + strconv.Itoa(body.SegmentID) + ".", "", 0
	case EditKindCommand:
		command := strings.TrimSpace(body.Command)
		if command == "" {
			return before, after, "", "", "", "", editsBadCommand, http.StatusBadRequest
		}
		mutation, err := editorcommand.Parse(command)
		if err != nil {
			return before, after, "", "", "", "", err.Error(), http.StatusUnprocessableEntity
		}
		updated, err := editorcommand.Apply(commandTimeline(entries, body.DurationMs), mutation)
		if err != nil {
			return before, after, "", "", "", "", err.Error(), http.StatusUnprocessableEntity
		}
		applied, ok := commandSegment(updated, mutation.SegmentID)
		if !ok {
			return before, after, "", "", "", "", editsNoLine, http.StatusNotFound
		}
		existing, ok := findTimelineEntry(entries, mutation.SegmentID)
		if !ok {
			return before, after, "", "", "", "", editsNoLine, http.StatusNotFound
		}
		after = existing
		after.StartMs = applied.StartMs
		after.EndMs = applied.EndMs
		after.Speaker = applied.Speaker
		return existing, after, ActionUserCommand, AuthorCommandBar, command,
			"Applied the command " + command, "", 0
	default:
		return before, after, "", "", "", "", editsBadKind, http.StatusBadRequest
	}
}

// boundaryEntry validates a dragged boundary against the head timeline and
// returns the state before and after. A drag may not invert its slot or
// overlap a neighbour. The rest of the row copies forward.
func boundaryEntry(entries []TimelineEntry, segmentID int, startMs, endMs int64) (TimelineEntry, TimelineEntry, string) {
	target, ok := findTimelineEntry(entries, segmentID)
	if !ok {
		return TimelineEntry{}, TimelineEntry{}, editsNoLine
	}
	if startMs < 0 || endMs <= startMs {
		return TimelineEntry{}, TimelineEntry{}, editsBadTiming
	}
	for _, other := range entries {
		if other.SegmentIndex == segmentID {
			continue
		}
		if startMs < other.EndMs && endMs > other.StartMs {
			return TimelineEntry{}, TimelineEntry{}, editsOverlap
		}
	}
	after := target
	after.StartMs = startMs
	after.EndMs = endMs
	return target, after, ""
}

// findTimelineEntry returns one line of the head timeline.
func findTimelineEntry(entries []TimelineEntry, segmentID int) (TimelineEntry, bool) {
	for _, entry := range entries {
		if entry.SegmentIndex == segmentID {
			return entry, true
		}
	}
	return TimelineEntry{}, false
}

// commandTimeline maps the head timeline onto the command validator shape.
// A supplied duration wins, and the longest line extends it, so an existing
// line never fails validation on a short browser duration.
func commandTimeline(entries []TimelineEntry, durationMs int64) editorcommand.Timeline {
	segments := make([]editorcommand.Segment, 0, len(entries))
	for _, entry := range entries {
		segments = append(segments, editorcommand.Segment{
			ID:      entry.SegmentIndex,
			StartMs: entry.StartMs,
			EndMs:   entry.EndMs,
			Speaker: entry.Speaker,
		})
		if entry.EndMs > durationMs {
			durationMs = entry.EndMs
		}
	}
	return editorcommand.Timeline{DurationMs: durationMs, Segments: segments}
}

// editValuesJSON encodes the timing and speaker provenance the ledger stores.
func editValuesJSON(entry TimelineEntry) string {
	payload, err := json.Marshal(editValues{
		StartMs: entry.StartMs,
		EndMs:   entry.EndMs,
		Speaker: entry.Speaker,
	})
	if err != nil {
		return ""
	}
	return string(payload)
}

// editSentence describes one recorded edit in prose.
func editSentence(action string, entry TimelineEntry) string {
	verb := "Saved the boundary."
	if action == ActionUserCommand {
		verb = "Saved the command."
	}
	return fmt.Sprintf("%s Line %d now runs from %s to %s.",
		verb, entry.SegmentIndex, seconds(entry.StartMs), seconds(entry.EndMs))
}

// seconds renders one timeline position with millisecond precision.
func seconds(ms int64) string {
	return strconv.FormatFloat(float64(ms)/1000, 'f', 3, 64) + "s"
}

// decodeEditBody reads one JSON object. An empty body is rejected, because an
// edit needs a kind and a language.
func decodeEditBody(w http.ResponseWriter, r *http.Request) (EditBody, error) {
	if r.Body == nil {
		return EditBody{}, errors.New("edit body is empty")
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxCommandPreviewBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var body EditBody
	if err := decoder.Decode(&body); err != nil {
		return EditBody{}, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return EditBody{}, errors.New("edit request must contain one JSON object")
	}
	return body, nil
}
