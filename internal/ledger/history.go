package ledger

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
)

const insertTimelineState = "INSERT INTO timeline_state_raw (commit_id, project_id, dub_id, owner_id, language, version_seq, segment_index, start_ms, end_ms, speaker, emotion, source_text, text, take_id) FORMAT JSONEachRow"

const selectTimelineAt = "SELECT * FROM timeline_at_commit(dub_id = {dub_id:String}, language = {language:String}, commit_id = {commit_id:String}) FORMAT JSONEachRow"

const selectBranchCost = "WITH RECURSIVE ancestry AS (SELECT commit_id, parent_commit_id, branch FROM commits WHERE dub_id = {dub_id:String} AND commit_id = {commit_id:String} UNION ALL SELECT c.commit_id, c.parent_commit_id, c.branch FROM commits AS c INNER JOIN ancestry AS a ON c.commit_id = a.parent_commit_id WHERE c.dub_id = {dub_id:String}) SELECT (SELECT branch FROM ancestry WHERE commit_id = {commit_id:String}) AS branch, coalesce((SELECT sum(cost_usd) FROM charges WHERE dub_id = {dub_id:String} AND language = {language:String} AND commit_id IN (SELECT commit_id FROM ancestry)), 0) AS cost_usd FORMAT JSONEachRow"

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
	resp, err := c.postClickHouse(ctx, selectTimelineAt, dubID, language, commitID)
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
// It walks parent_commit_id then keeps the newest version_seq per segment, matching timeline_at_commit.
// Production time travel queries the view. This function exists to pin replay against that query.
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
		if !ok || snap.VersionSeq >= prev.VersionSeq {
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
	resp, err := c.postClickHouse(ctx, selectBranchCost, dubID, language, commitID)
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

func (c *Client) postClickHouse(ctx context.Context, statement, dubID, language, commitID string) (*http.Response, error) {
	if c == nil || c.endpoint == nil || c.http == nil {
		return nil, errors.New("ledger client is nil")
	}
	requestURL := *c.endpoint
	query := requestURL.Query()
	query.Set("database", c.database)
	query.Set("query", statement)
	query.Set("param_dub_id", dubID)
	query.Set("param_language", language)
	query.Set("param_commit_id", commitID)
	requestURL.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL.String(), bytes.NewReader(nil))
	if err != nil {
		return nil, fmt.Errorf("create ClickHouse query: %w", err)
	}
	req.SetBasicAuth(c.user, c.password)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send ClickHouse query: %w", err)
	}
	if resp.StatusCode/100 != 2 {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		return nil, fmt.Errorf("ClickHouse returned %s: %s", resp.Status, strings.TrimSpace(string(detail)))
	}
	return resp, nil
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
