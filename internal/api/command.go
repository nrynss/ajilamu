package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	editorcommand "github.com/nrynss/ajilamu/internal/command"
)

const maxCommandPreviewBytes int64 = 64 << 10

// CommandPreviewRequest carries a proposed command and the current timeline.
// The server treats the browser timeline as untrusted input and validates it.
type CommandPreviewRequest struct {
	Command  string                 `json:"command"`
	Timeline editorcommand.Timeline `json:"timeline"`
}

// CommandPreview supplies the creator-facing explanation and Go-derived result.
// The browser applies Segment only after the creator confirms this preview.
type CommandPreview struct {
	Command  string                 `json:"command"`
	Summary  string                 `json:"summary"`
	Mutation editorcommand.Mutation `json:"mutation"`
	Segment  CommandPreviewSegment  `json:"segment"`
}

// CommandPreviewSegment is the complete mutable state returned by the parser.
type CommandPreviewSegment struct {
	ID         int    `json:"id"`
	StartMs    int64  `json:"start_ms"`
	EndMs      int64  `json:"end_ms"`
	DurationMs int64  `json:"duration_ms"`
	Speaker    string `json:"speaker"`
}

// NewCommandPreviewHandler builds the deterministic command-preview endpoint.
// It parses, validates, describes, and applies with the Go command package.
func NewCommandPreviewHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "Method not allowed.", http.StatusMethodNotAllowed)
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, maxCommandPreviewBytes)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		var request CommandPreviewRequest
		if err := decoder.Decode(&request); err != nil {
			writeCommandPreviewError(w, err)
			return
		}
		if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
			writeCommandPreviewError(w, errors.New("request must contain one JSON object"))
			return
		}

		mutation, err := editorcommand.Parse(request.Command)
		if err != nil {
			writeCommandPreviewError(w, err)
			return
		}
		if err := editorcommand.Validate(request.Timeline, mutation); err != nil {
			writeCommandPreviewError(w, err)
			return
		}
		summary, err := editorcommand.Describe(request.Timeline, mutation)
		if err != nil {
			writeCommandPreviewError(w, err)
			return
		}
		updated, err := editorcommand.Apply(request.Timeline, mutation)
		if err != nil {
			writeCommandPreviewError(w, err)
			return
		}
		segment, ok := commandSegment(updated, mutation.SegmentID)
		if !ok {
			writeCommandPreviewError(w, errors.New("updated line is missing from this timeline"))
			return
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(CommandPreview{
			Command:  request.Command,
			Summary:  summary,
			Mutation: mutation,
			Segment: CommandPreviewSegment{
				ID:         segment.ID,
				StartMs:    segment.StartMs,
				EndMs:      segment.EndMs,
				DurationMs: segment.EndMs - segment.StartMs,
				Speaker:    segment.Speaker,
			},
		})
	})
}

func commandSegment(timeline editorcommand.Timeline, id int) (editorcommand.Segment, bool) {
	for _, segment := range timeline.Segments {
		if segment.ID == id {
			return segment, true
		}
	}
	return editorcommand.Segment{}, false
}

func writeCommandPreviewError(w http.ResponseWriter, err error) {
	http.Error(w, err.Error(), http.StatusUnprocessableEntity)
}
