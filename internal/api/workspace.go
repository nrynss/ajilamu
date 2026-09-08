package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// WorkspaceReader reads the ledger facts one workspace payload needs.
// It names api wire types only, so this package never imports internal/ledger.
type WorkspaceReader interface {
	// WorkspaceTakes returns one track per target language.
	WorkspaceTakes(ctx context.Context, dubID string) ([]LanguageTrack, error)
	// WholePassCharges returns the charges no single take owns.
	WholePassCharges(ctx context.Context, dubID string) ([]Charge, error)
	// RunningTotal returns the exact running sum and what it covers.
	RunningTotal(ctx context.Context, dubID string) (Total, error)
	// Languages returns the target language codes.
	Languages(ctx context.Context, dubID string) ([]string, error)
	// ProjectMetadata returns the identity and the timestamps.
	ProjectMetadata(ctx context.Context, dubID string) (DubSummary, error)
}

// ProjectLookup returns the stored upload record for one project id.
// It reports the title and the record creation time. The last result is
// false when no upload record exists.
type ProjectLookup func(id string) (title, createdAt string, ok bool)

// RunActive reports whether a run is in flight for one project id.
// The server supplies it from the run registry, which this package does
// not own. A nil check leaves the route at its ledger-derived state.
type RunActive func(dubID string) bool

// Workspace failure sentences. A read failure never carries internal text.
const (
	workspaceUnavailable = "The project ledger is unavailable, so this workspace cannot load."
	workspaceReadFailed  = "Could not read the project ledger."
	workspaceNotFound    = "This project does not exist."
)

// WorkspaceHandlerFrom serves one Dub assembled from the ledger.
//
// history supplies the source segments and the commit DAG. lookup supplies the
// title the ledger does not store. active reports an in-flight run so the route
// can answer running. A nil reader answers 503, matching the history routes, so
// a clone with no credentials cannot claim a project.
func WorkspaceHandlerFrom(reader WorkspaceReader, history HistoryReader, lookup ProjectLookup, active RunActive, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if reader == nil {
			writeHistoryFailure(w, http.StatusServiceUnavailable, workspaceUnavailable)
			return
		}
		dubID := strings.TrimSpace(r.PathValue("id"))
		if !safeProjectID(dubID) {
			writeHistoryFailure(w, http.StatusNotFound, workspaceNotFound)
			return
		}
		running := active != nil && active(dubID)
		dub, err := assembleWorkspace(r.Context(), reader, history, lookup, running, dubID)
		if err != nil {
			logWorkspaceFailure(logger, err)
			writeHistoryFailure(w, http.StatusInternalServerError, workspaceReadFailed)
			return
		}
		if dub == nil {
			writeHistoryFailure(w, http.StatusNotFound, workspaceNotFound)
			return
		}
		writeHistoryJSON(w, http.StatusOK, *dub)
	})
}

// assembleWorkspace builds one Dub from the ledger reads.
//
// A nil result means no upload record and no ledger row names this project, so
// the route answers 404. Every slice is non-nil, because the wire declares each
// one as an array.
func assembleWorkspace(ctx context.Context, reader WorkspaceReader, history HistoryReader, lookup ProjectLookup, running bool, dubID string) (*Dub, error) {
	tracks, err := reader.WorkspaceTakes(ctx, dubID)
	if err != nil {
		return nil, fmt.Errorf("read workspace takes: %w", err)
	}
	wholePass, err := reader.WholePassCharges(ctx, dubID)
	if err != nil {
		return nil, fmt.Errorf("read whole-pass charges: %w", err)
	}
	total, err := reader.RunningTotal(ctx, dubID)
	if err != nil {
		return nil, fmt.Errorf("read the running total: %w", err)
	}
	languages, err := reader.Languages(ctx, dubID)
	if err != nil {
		return nil, fmt.Errorf("read the language list: %w", err)
	}
	metadata, err := reader.ProjectMetadata(ctx, dubID)
	if err != nil {
		return nil, fmt.Errorf("read the project metadata: %w", err)
	}
	commits, err := workspaceCommits(ctx, history, dubID)
	if err != nil {
		return nil, err
	}
	segments, err := workspaceSegments(ctx, history, dubID, languages, commits)
	if err != nil {
		return nil, err
	}

	title := ""
	recordCreatedAt := ""
	stored := false
	if lookup != nil {
		title, recordCreatedAt, stored = lookup(dubID)
	}
	if !stored && len(commits) == 0 && len(languages) == 0 && len(tracks) == 0 {
		return nil, nil
	}
	if tracks == nil {
		tracks = []LanguageTrack{}
	}
	if wholePass == nil {
		wholePass = []Charge{}
	}
	createdAt := metadata.CreatedAt
	updatedAt := metadata.UpdatedAt
	if createdAt == "" {
		createdAt = recordCreatedAt
	}
	if updatedAt == "" {
		updatedAt = recordCreatedAt
	}
	return &Dub{
		ID:        dubID,
		Title:     title,
		Readiness: workspaceReadiness(tracks, segments, running),
		Segments:  segments,
		Languages: tracks,
		Charges:   wholePass,
		Total:     total,
		Commits:   commits,
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}, nil
}

// workspaceCommits reads the commit DAG oldest first. Without a history reader
// the DAG stays an empty array rather than failing the whole payload.
func workspaceCommits(ctx context.Context, history HistoryReader, dubID string) ([]Commit, error) {
	if history == nil {
		return []Commit{}, nil
	}
	commits, err := history.ListCommits(ctx, dubID)
	if err != nil {
		return nil, fmt.Errorf("read the commit history: %w", err)
	}
	if commits == nil {
		return []Commit{}, nil
	}
	return commits, nil
}

// workspaceSegments reads the source track at the head commit.
//
// The ledger stores segment timing and source text on the timeline, per
// language. The first target language carries the same copied source fields, so
// the route reads that track at the newest commit. Without a commit or a
// language there is nothing to read and the segment list stays empty.
func workspaceSegments(ctx context.Context, history HistoryReader, dubID string, languages []string, commits []Commit) ([]Segment, error) {
	if history == nil || len(languages) == 0 || len(commits) == 0 {
		return []Segment{}, nil
	}
	head := commits[len(commits)-1].CommitID
	entries, err := history.TimelineAt(ctx, dubID, languages[0], head)
	if err != nil {
		return nil, fmt.Errorf("read the source timeline: %w", err)
	}
	segments := make([]Segment, 0, len(entries))
	for _, entry := range entries {
		segments = append(segments, Segment{
			ID:         entry.SegmentIndex,
			StartMs:    entry.StartMs,
			EndMs:      entry.EndMs,
			DurationMs: entry.EndMs - entry.StartMs,
			Text:       entry.SourceText,
			Speaker:    entry.Speaker,
			Emotion:    entry.Emotion,
		})
	}
	return segments, nil
}

// workspaceReadiness derives the wire readiness from the assembled tracks.
//
// The upload record stores no readiness and the ledger stores none either, so
// the route derives it. An in-flight run wins, because the wire says running
// means a run is active right now. No take at all means the project waits for
// its first run. A flagged line needs the creator. A segment with no rendered
// line is not finished, because the wire says ready means finished and
// playable. Every line fitting and every segment covered means ready.
func workspaceReadiness(tracks []LanguageTrack, segments []Segment, running bool) string {
	if running {
		return ReadinessRunning
	}
	takes := 0
	rendered := 0
	flagged := false
	for _, track := range tracks {
		for _, line := range track.Lines {
			if line.Flagged {
				flagged = true
			}
			if len(line.Takes) > 0 {
				rendered++
			}
			takes += len(line.Takes)
		}
	}
	switch {
	case takes == 0:
		return ReadinessPending
	case flagged:
		return ReadinessReview
	case len(segments) > 0 && rendered < len(segments)*len(tracks):
		return ReadinessReview
	default:
		return ReadinessReady
	}
}

// UploadProjectLookup reads the stored upload record for one project id.
// It returns the same title and timestamp ListUploadSummaries reads, so the
// workspace and the index agree on a stored project.
func UploadProjectLookup(storageDir string) ProjectLookup {
	return func(id string) (string, string, bool) {
		if storageDir == "" || !safeProjectID(id) {
			return "", "", false
		}
		payload, err := os.ReadFile(filepath.Join(storageDir, id, uploadRecordName))
		if err != nil {
			return "", "", false
		}
		var record uploadRecord
		if err := json.Unmarshal(payload, &record); err != nil || record.ID == "" {
			return "", "", false
		}
		return record.Title, record.CreatedAt, true
	}
}

// safeProjectID rejects an id that could name another file. A stored project
// id is hex, so a separator or a parent reference is never a real project.
func safeProjectID(id string) bool {
	if id == "" || id == "." || id == ".." {
		return false
	}
	return !strings.ContainsAny(id, `/\`)
}

// logWorkspaceFailure records the detail the response hides.
func logWorkspaceFailure(logger *slog.Logger, err error) {
	if logger == nil {
		logger = slog.Default()
	}
	logger.Error("ledger workspace read failed", "error", err)
}
