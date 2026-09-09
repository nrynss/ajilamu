package ledger

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/nrynss/ajilamu/internal/api"
	"github.com/nrynss/ajilamu/internal/cost"
)

// This file holds the grouped ledger read behind the project index. The index
// serves every project on one request, so each statement binds the whole id
// list and the query count stays fixed as the project count grows.

// selectIndexTakes reads one row per recorded take for every requested project.
//
// It carries only what the readiness rule reads: the line the take belongs to,
// its slot and its measurement. The reader derives the fit in Go through
// api.FitState, so the index and the workspace apply one tolerance. Ordering
// keeps the reader independent of the server's row order.
const selectIndexTakes = "SELECT dub_id, language, segment_index, toInt64(slot_ms) AS slot_ms, toInt64(measured_ms) AS measured_ms FROM takes WHERE dub_id IN {dub_ids:Array(String)} ORDER BY dub_id ASC, language ASC, segment_index ASC, attempt ASC, created_at ASC, commit_id ASC FORMAT JSONEachRow"

// selectIndexSegmentCounts counts the head timeline segments of every
// requested project and language.
//
// The workspace reads the timeline at the newest commit, and the coverage rule
// compares the rendered lines against that count. This statement reproduces
// the count for every project at once. It takes each project's newest commit
// by (version_seq, commit_id), which is the order ListCommits ends on, walks
// its ancestry, and counts the distinct segment indexes the ancestry holds.
//
// It reads commits_raw FINAL rather than the commits view, because the
// recursive walk would otherwise materialize every view column at every step.
// selectBranchCost makes the same choice for the same reason.
const selectIndexSegmentCounts = "WITH RECURSIVE heads AS (SELECT dub_id, argMax(commit_id, (version_seq, commit_id)) AS head_id FROM commits_raw FINAL WHERE dub_id IN {dub_ids:Array(String)} GROUP BY dub_id), ancestry AS (SELECT h.dub_id AS dub_id, c.commit_id AS commit_id, c.parent_commit_id AS parent_commit_id FROM heads AS h INNER JOIN commits_raw AS c FINAL ON c.dub_id = h.dub_id AND c.commit_id = h.head_id UNION ALL SELECT a.dub_id AS dub_id, c.commit_id AS commit_id, c.parent_commit_id AS parent_commit_id FROM ancestry AS a INNER JOIN commits_raw AS c FINAL ON c.dub_id = a.dub_id AND c.commit_id = a.parent_commit_id) SELECT dub_id, language, toUInt64(uniqExact(segment_index)) AS segment_count FROM timeline_state FINAL WHERE dub_id IN {dub_ids:Array(String)} AND (dub_id, commit_id) IN (SELECT dub_id, commit_id FROM ancestry) GROUP BY dub_id, language ORDER BY dub_id ASC, language ASC FORMAT JSONEachRow"

// selectIndexTotals sums every charge of every requested project in exact
// nanodollars. It uses the same expression as selectRunningTotal, so an index
// row and a workspace payload report one number.
const selectIndexTotals = "SELECT dub_id, toInt64(sum(toInt64(round(cost_usd * 1000000000)))) AS total_nanodollars FROM charges WHERE dub_id IN {dub_ids:Array(String)} GROUP BY dub_id ORDER BY dub_id ASC FORMAT JSONEachRow"

// indexTakeRow is one JSONEachRow line from selectIndexTakes.
type indexTakeRow struct {
	DubID        string `json:"dub_id"`
	Language     string `json:"language"`
	SegmentIndex int32  `json:"segment_index"`
	SlotMs       int64  `json:"slot_ms"`
	MeasuredMs   int64  `json:"measured_ms"`
}

// indexSegmentRow is one JSONEachRow line from selectIndexSegmentCounts.
type indexSegmentRow struct {
	DubID        string `json:"dub_id"`
	Language     string `json:"language"`
	SegmentCount uint64 `json:"segment_count"`
}

// indexTotalRow is one JSONEachRow line from selectIndexTotals.
type indexTotalRow struct {
	DubID            string `json:"dub_id"`
	TotalNanodollars int64  `json:"total_nanodollars"`
}

// indexLine counts the takes of one line and whether any of them fits.
type indexLine struct {
	takes int
	fits  bool
}

// indexProject accumulates the readiness facts of one project.
type indexProject struct {
	// languages lists the target languages in ascending order, so the first
	// entry names the track whose timeline the coverage rule measures.
	languages []string
	lines     map[string]map[int32]*indexLine
}

// IndexFacts returns the ledger-derived part of the project index for every
// requested project.
//
// The read is grouped. Three statements serve any number of projects, because
// each binds the whole id list, so the query count does not grow with the
// project count. A project the ledger holds no row for is absent from the
// result, and the caller keeps the row its upload record describes.
func (c *Client) IndexFacts(ctx context.Context, dubIDs []string) (map[string]api.IndexFact, error) {
	ids := indexIDs(dubIDs)
	if len(ids) == 0 {
		return map[string]api.IndexFact{}, nil
	}
	params := map[string]string{"dub_ids": indexIDList(ids)}
	takes, err := c.indexTakeRows(ctx, params)
	if err != nil {
		return nil, err
	}
	segments, err := c.indexSegmentRows(ctx, params)
	if err != nil {
		return nil, err
	}
	totals, err := c.indexTotalRows(ctx, params)
	if err != nil {
		return nil, err
	}
	return buildIndexFacts(takes, segments, totals), nil
}

func (c *Client) indexTakeRows(ctx context.Context, params map[string]string) ([]indexTakeRow, error) {
	resp, err := c.queryClickHouse(ctx, selectIndexTakes, params)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	rows, err := decodeJSONEachRow[indexTakeRow](resp.Body)
	if err != nil {
		return nil, fmt.Errorf("decode index takes: %w", err)
	}
	return rows, nil
}

func (c *Client) indexSegmentRows(ctx context.Context, params map[string]string) ([]indexSegmentRow, error) {
	resp, err := c.queryClickHouse(ctx, selectIndexSegmentCounts, params)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	rows, err := decodeJSONEachRow[indexSegmentRow](resp.Body)
	if err != nil {
		return nil, fmt.Errorf("decode index segment counts: %w", err)
	}
	return rows, nil
}

func (c *Client) indexTotalRows(ctx context.Context, params map[string]string) ([]indexTotalRow, error) {
	resp, err := c.queryClickHouse(ctx, selectIndexTotals, params)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	rows, err := decodeJSONEachRow[indexTotalRow](resp.Body)
	if err != nil {
		return nil, fmt.Errorf("decode index totals: %w", err)
	}
	return rows, nil
}

// buildIndexFacts groups the three row sets by project and derives each
// project's readiness through api.DeriveReadiness, the same rule the workspace
// route applies.
func buildIndexFacts(takes []indexTakeRow, segments []indexSegmentRow, totals []indexTotalRow) map[string]api.IndexFact {
	projects := make(map[string]*indexProject)
	project := func(dubID string) *indexProject {
		p, ok := projects[dubID]
		if !ok {
			p = &indexProject{lines: make(map[string]map[int32]*indexLine)}
			projects[dubID] = p
		}
		return p
	}

	for _, row := range takes {
		p := project(row.DubID)
		lines, ok := p.lines[row.Language]
		if !ok {
			lines = make(map[int32]*indexLine)
			p.lines[row.Language] = lines
			p.languages = append(p.languages, row.Language)
		}
		line, ok := lines[row.SegmentIndex]
		if !ok {
			line = &indexLine{}
			lines[row.SegmentIndex] = line
		}
		line.takes++
		if api.FitState(row.SlotMs, row.MeasuredMs) == api.StateFits {
			line.fits = true
		}
	}

	segmentCounts := make(map[string]map[string]uint64)
	for _, row := range segments {
		if segmentCounts[row.DubID] == nil {
			segmentCounts[row.DubID] = make(map[string]uint64)
		}
		segmentCounts[row.DubID][row.Language] = row.SegmentCount
		project(row.DubID)
	}

	totalByProject := make(map[string]cost.Price, len(totals))
	for _, row := range totals {
		totalByProject[row.DubID] = cost.Price(row.TotalNanodollars)
		project(row.DubID)
	}

	facts := make(map[string]api.IndexFact, len(projects))
	for dubID, p := range projects {
		slices.Sort(p.languages)
		facts[dubID] = api.IndexFact{
			Readiness:        api.DeriveReadiness(indexReadinessFacts(p, segmentCounts[dubID])),
			TotalNanodollars: totalByProject[dubID],
		}
	}
	return facts
}

// indexReadinessFacts counts what api.DeriveReadiness reads for one project.
// The coverage count comes from the first language track, matching the
// workspace, which passes the first track's segments to the rule.
func indexReadinessFacts(p *indexProject, segmentCounts map[string]uint64) api.ReadinessFacts {
	facts := api.ReadinessFacts{TrackCount: len(p.languages)}
	if len(p.languages) > 0 {
		facts.SegmentCount = int(segmentCounts[p.languages[0]])
	}
	for _, lines := range p.lines {
		for _, line := range lines {
			if line.takes == 0 {
				continue
			}
			facts.Rendered++
			facts.Takes += line.takes
			if !line.fits {
				facts.Flagged = true
			}
		}
	}
	return facts
}

// indexIDs keeps the distinct non-empty project ids in request order.
func indexIDs(dubIDs []string) []string {
	ids := make([]string, 0, len(dubIDs))
	seen := make(map[string]bool, len(dubIDs))
	for _, id := range dubIDs {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	return ids
}

// indexIDList renders the ids as one ClickHouse array literal, so a single
// bound parameter carries the whole grouped read.
func indexIDList(ids []string) string {
	var b strings.Builder
	b.WriteByte('[')
	for i, id := range ids {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteByte('\'')
		for _, r := range id {
			if r == '\\' || r == '\'' {
				b.WriteByte('\\')
			}
			b.WriteRune(r)
		}
		b.WriteByte('\'')
	}
	b.WriteByte(']')
	return b.String()
}
