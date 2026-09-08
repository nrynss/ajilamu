package ledger

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
)

const insertTimelineState = "INSERT INTO timeline_state_raw (commit_id, project_id, dub_id, owner_id, language, version_seq, segment_index, start_ms, end_ms, speaker, emotion, source_text, text, take_id) FORMAT JSONEachRow"

const selectTimelineAt = "SELECT * FROM timeline_at_commit(dub_id = {dub_id:String}, language = {language:String}, commit_id = {commit_id:String}) FORMAT JSONEachRow"

// selectBranchCost walks the ancestry once and reads both answers off that one walk.
//
// The earlier shape named the ancestry CTE from two scalar subqueries, one for the branch
// label and one for the charge sum. ClickHouse re-ran the recursive walk for each, so the
// statement cost two walks. Joining the charges onto the ancestry rows instead lets one
// GROUP BY produce the label and the sum together.
//
// It reads commits_raw FINAL rather than the commits view, which is what timeline_at_commit
// already does. The commits view is SELECT *, ingested_at FROM commits_raw FINAL, so the row
// set is identical, but naming the table keeps the recursion from materialising every column
// at every step.
//
// any(branch) stays. A bare scalar subquery over an empty ancestry raises Code 125, so a head
// still sitting in the write queue would fail the whole compare. See commit 3c1035e. The
// aggregate returns an empty label and a zero cost for that head instead.
const selectBranchCost = "WITH RECURSIVE ancestry AS (SELECT commit_id, parent_commit_id, branch FROM commits_raw FINAL WHERE dub_id = {dub_id:String} AND commit_id = {commit_id:String} UNION ALL SELECT c.commit_id, c.parent_commit_id, c.branch FROM commits_raw AS c FINAL INNER JOIN ancestry AS a ON c.commit_id = a.parent_commit_id WHERE c.dub_id = {dub_id:String}) SELECT ifNull(any(branch), '') AS branch, coalesce(sum(cost_usd), 0) AS cost_usd FROM (SELECT if(a.commit_id = {commit_id:String}, a.branch, NULL) AS branch, ch.cost_usd AS cost_usd FROM ancestry AS a LEFT JOIN (SELECT commit_id, cost_usd FROM charges WHERE dub_id = {dub_id:String} AND language = {language:String}) AS ch ON ch.commit_id = a.commit_id) FORMAT JSONEachRow"

// maxRecursiveCTEDepth caps the ancestry walk in selectTimelineAt and selectBranchCost.
//
// ClickHouse defaults the recursive CTE evaluation depth setting to 1000. A head past 1000 ancestors
// then fails with Code 306 and the caller sees a wrapped HTTP 500. The default also belongs
// to the server, so a settings profile can lower it under a shipped feature with no warning.
// Pinning it per request takes that decision back from the profile.
//
// 5000 is the measured trade. On ClickHouse 26.8.2.7 the walk runs about 2 milliseconds per
// ancestor, so the deepest allowed head costs roughly 10 seconds against the 30 second client
// budget in client.go. A larger ceiling spends that budget instead of raising an error: at
// 10000 ancestors the same two statements measured 22 and 25 seconds. Past the ceiling the
// caller gets Code 306, which names the setting, rather than an opaque client timeout.
const maxRecursiveCTEDepth = "5000"

// TimelineSegment is one full segment snapshot at one commit.
// Writers copy every field forward. Readers never merge two rows.
type TimelineSegment struct {
	CommitID     string `json:"commit_id"`
	ProjectID    string `json:"project_id"`
	DubID        string `json:"dub_id"`
	OwnerID      string `json:"owner_id"`
	Language     string `json:"language"`
	VersionSeq   uint64 `json:"version_seq"`
	SegmentIndex int32  `json:"segment_index"`
	StartMs      int64  `json:"start_ms"`
	EndMs        int64  `json:"end_ms"`
	Speaker      string `json:"speaker"`
	Emotion      string `json:"emotion"`
	SourceText   string `json:"source_text"`
	Text         string `json:"text"`
	TakeID       string `json:"take_id"`
}

// BranchView is duration and cost for one head of a language track.
type BranchView struct {
	CommitID  string
	Branch    string
	Segments  []TimelineSegment
	SlotMs    int64
	TakeCount int
	CostUSD   string
}

// BranchCompare holds metrics for two heads of one language track.
type BranchCompare struct {
	A BranchView
	B BranchView
}

type timelineAtRow struct {
	SegmentIndex    int32  `json:"segment_index"`
	StartMs         int64  `json:"start_ms"`
	EndMs           int64  `json:"end_ms"`
	Speaker         string `json:"speaker"`
	Emotion         string `json:"emotion"`
	SourceText      string `json:"source_text"`
	Text            string `json:"text"`
	TakeID          string `json:"take_id"`
	StateVersionSeq uint64 `json:"state_version_seq"`
}

type branchCostRow struct {
	Branch  string      `json:"branch"`
	CostUSD json.Number `json:"cost_usd"`
}

// RecordSegmentState enqueues one timeline_state_raw snapshot.
// The caller supplies the full row. This function does not fill missing fields from earlier commits.
func (c *Client) RecordSegmentState(ctx context.Context, segment TimelineSegment) error {
	if err := validateTimelineSegment(segment); err != nil {
		return err
	}
	return c.EnqueueJSON(ctx, insertTimelineState, segment)
}

// TimelineAt returns segment state at commitID by querying timeline_at_commit.
// It binds dub_id, language, and commit_id and does not walk parents in Go.
func (c *Client) TimelineAt(ctx context.Context, dubID, language, commitID string) ([]TimelineSegment, error) {
	if err := validateHistoryQuery(dubID, language, commitID); err != nil {
		return nil, err
	}
	resp, err := c.queryClickHouse(ctx, selectTimelineAt, map[string]string{"dub_id": dubID, "language": language, "commit_id": commitID})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	rows, err := decodeJSONEachRow[timelineAtRow](resp.Body)
	if err != nil {
		return nil, fmt.Errorf("decode timeline_at_commit: %w", err)
	}
	out := make([]TimelineSegment, 0, len(rows))
	for _, row := range rows {
		out = append(out, timelineFromView(dubID, language, row))
	}
	slices.SortFunc(out, cmpTimelineSegment)
	return out, nil
}

// ReplayAt reconstructs timeline state at commitID from an in-memory DAG and snapshot log.
// It walks parent_commit_id then keeps the highest (version_seq, commit_id) per segment.
// Production time travel queries the view. This function exists to pin replay against that query.
//
// ReplayAt and timeline_at_commit return the same rows on an acyclic ancestry of at most
// maxRecursiveCTEDepth commits whose snapshots this DAG and this database both hold. Both
// order on the same tuple, so a tie in version_seq no longer splits them.
//
// They part on three inputs the view cannot serve. An ancestry longer than the ceiling raises
// Code 306 through the view and replays here. A cycle in commits_raw does the same, because
// commitAncestry breaks on a repeat and the database does not. A snapshot the caller never
// wrote to timeline_state_raw is invisible to the view and visible here.
func ReplayAt(commits []Commit, snapshots []TimelineSegment, dubID, language, commitID string) ([]TimelineSegment, error) {
	if err := validateHistoryQuery(dubID, language, commitID); err != nil {
		return nil, err
	}
	ancestors := commitAncestry(commits, dubID, commitID)
	best := make(map[int32]TimelineSegment)
	for _, snap := range snapshots {
		if snap.DubID != dubID || snap.Language != language {
			continue
		}
		if _, ok := ancestors[snap.CommitID]; !ok {
			continue
		}
		prev, ok := best[snap.SegmentIndex]
		if !ok || newerSnapshot(snap, prev) {
			best[snap.SegmentIndex] = snap
		}
	}
	out := make([]TimelineSegment, 0, len(best))
	for _, snap := range best {
		out = append(out, TimelineSegment{
			DubID:        dubID,
			Language:     language,
			VersionSeq:   snap.VersionSeq,
			SegmentIndex: snap.SegmentIndex,
			StartMs:      snap.StartMs,
			EndMs:        snap.EndMs,
			Speaker:      snap.Speaker,
			Emotion:      snap.Emotion,
			SourceText:   snap.SourceText,
			Text:         snap.Text,
			TakeID:       snap.TakeID,
		})
	}
	slices.SortFunc(out, cmpTimelineSegment)
	return out, nil
}

// CompareBranches reports duration and cost for two heads of one language track.
// Duration comes from reconstructed slots. Cost sums the charges view over each head's ancestry.
func (c *Client) CompareBranches(ctx context.Context, dubID, language, commitA, commitB string) (BranchCompare, error) {
	if err := validateHistoryQuery(dubID, language, commitA); err != nil {
		return BranchCompare{}, err
	}
	if strings.TrimSpace(commitB) == "" {
		return BranchCompare{}, errors.New("ledger history commit_id is empty")
	}
	left, err := c.TimelineAt(ctx, dubID, language, commitA)
	if err != nil {
		return BranchCompare{}, err
	}
	right, err := c.TimelineAt(ctx, dubID, language, commitB)
	if err != nil {
		return BranchCompare{}, err
	}
	leftCost, err := c.branchCost(ctx, dubID, language, commitA)
	if err != nil {
		return BranchCompare{}, err
	}
	rightCost, err := c.branchCost(ctx, dubID, language, commitB)
	if err != nil {
		return BranchCompare{}, err
	}
	return BranchCompare{
		A: newBranchView(commitA, left, leftCost),
		B: newBranchView(commitB, right, rightCost),
	}, nil
}

func (c *Client) branchCost(ctx context.Context, dubID, language, commitID string) (branchCostRow, error) {
	resp, err := c.queryClickHouse(ctx, selectBranchCost, map[string]string{"dub_id": dubID, "language": language, "commit_id": commitID})
	if err != nil {
		return branchCostRow{}, err
	}
	defer resp.Body.Close()
	rows, err := decodeJSONEachRow[branchCostRow](resp.Body)
	if err != nil {
		return branchCostRow{}, fmt.Errorf("decode branch cost: %w", err)
	}
	if len(rows) != 1 {
		return branchCostRow{}, fmt.Errorf("ClickHouse returned %d branch cost rows, want 1", len(rows))
	}
	return rows[0], nil
}

func decodeJSONEachRow[T any](r io.Reader) ([]T, error) {
	decoder := json.NewDecoder(r)
	var rows []T
	for {
		var row T
		if err := decoder.Decode(&row); errors.Is(err, io.EOF) {
			return rows, nil
		} else if err != nil {
			return nil, fmt.Errorf("decode ClickHouse JSONEachRow: %w", err)
		}
		rows = append(rows, row)
	}
}

func timelineFromView(dubID, language string, row timelineAtRow) TimelineSegment {
	return TimelineSegment{
		DubID:        dubID,
		Language:     language,
		VersionSeq:   row.StateVersionSeq,
		SegmentIndex: row.SegmentIndex,
		StartMs:      row.StartMs,
		EndMs:        row.EndMs,
		Speaker:      row.Speaker,
		Emotion:      row.Emotion,
		SourceText:   row.SourceText,
		Text:         row.Text,
		TakeID:       row.TakeID,
	}
}

func newBranchView(commitID string, segments []TimelineSegment, cost branchCostRow) BranchView {
	view := BranchView{
		CommitID: commitID,
		Branch:   cost.Branch,
		Segments: segments,
		CostUSD:  cost.CostUSD.String(),
	}
	if view.CostUSD == "" {
		view.CostUSD = "0"
	}
	for _, segment := range segments {
		view.SlotMs += segment.EndMs - segment.StartMs
		if segment.TakeID != "" {
			view.TakeCount++
		}
	}
	return view
}

func commitAncestry(commits []Commit, dubID, commitID string) map[string]struct{} {
	byID := make(map[string]Commit, len(commits))
	for _, commit := range commits {
		if commit.DubID != dubID {
			continue
		}
		byID[commit.CommitID] = commit
	}
	seen := make(map[string]struct{})
	current := commitID
	for current != "" {
		if _, ok := seen[current]; ok {
			break
		}
		commit, ok := byID[current]
		if !ok {
			break
		}
		seen[current] = struct{}{}
		current = commit.ParentCommitID
	}
	return seen
}

// newerSnapshot reports whether a outranks b under the ordering timeline_at_commit uses.
//
// The view runs argMax over the tuple (version_seq, commit_id). version_seq alone is not a
// total order, because nothing forces a snapshot's version_seq to match its commit row, and
// two snapshots in one ancestry can carry the same number for one segment. commit_id is
// unique per dub, so the pair always separates them.
//
// ClickHouse compares String by bytes and so does Go, so both sides pick the same row.
func newerSnapshot(a, b TimelineSegment) bool {
	if a.VersionSeq != b.VersionSeq {
		return a.VersionSeq > b.VersionSeq
	}
	return a.CommitID > b.CommitID
}

func cmpTimelineSegment(a, b TimelineSegment) int {
	return cmp.Compare(a.SegmentIndex, b.SegmentIndex)
}

func validateHistoryQuery(dubID, language, commitID string) error {
	for _, field := range []struct {
		name  string
		value string
	}{
		{name: "dub_id", value: dubID},
		{name: "language", value: language},
		{name: "commit_id", value: commitID},
	} {
		if strings.TrimSpace(field.value) == "" {
			return errors.New("ledger history " + field.name + " is empty")
		}
	}
	return nil
}

func validateTimelineSegment(segment TimelineSegment) error {
	for _, field := range []struct {
		name  string
		value string
	}{
		{name: "commit_id", value: segment.CommitID},
		{name: "project_id", value: segment.ProjectID},
		{name: "dub_id", value: segment.DubID},
		{name: "owner_id", value: segment.OwnerID},
		{name: "language", value: segment.Language},
	} {
		if strings.TrimSpace(field.value) == "" {
			return errors.New("ledger timeline segment " + field.name + " is empty")
		}
	}
	if segment.VersionSeq == 0 {
		return errors.New("ledger timeline segment version_seq is zero")
	}
	if segment.SegmentIndex < 0 {
		return fmt.Errorf("ledger timeline segment segment_index %d is negative", segment.SegmentIndex)
	}
	if segment.EndMs <= segment.StartMs {
		return fmt.Errorf("ledger timeline segment slot [%d, %d] is empty or inverted", segment.StartMs, segment.EndMs)
	}
	return nil
}
