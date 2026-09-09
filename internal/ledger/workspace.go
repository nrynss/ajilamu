package ledger

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nrynss/ajilamu/internal/api"
	"github.com/nrynss/ajilamu/internal/cost"
)

// This file holds the ledger reads the workspace payload needs. Every read routes
// through queryClickHouse, so both pinned settings reach every statement. Every
// read binds its ids as query parameters and orders its rows deterministically.

// selectWorkspaceTakes lists every recorded take attempt for one dub.
//
// It reads the takes view, which applies FINAL, so a re-sent batch collapses
// before it reaches the reader. slot_ms, measured_ms and delta_ms leave as Int64
// so one row shape covers the wire Fit type. created_ms orders attempts that
// share an attempt number across runs.
const selectWorkspaceTakes = "SELECT language, commit_id, segment_index, attempt, voice, text, audio_path, toString(repair) AS repair, repair_detail, peaks, toInt64(slot_ms) AS slot_ms, toInt64(measured_ms) AS measured_ms, toInt64(delta_ms) AS delta_ms, toInt64(toUnixTimestamp64Milli(created_at)) AS created_ms FROM takes WHERE dub_id = {dub_id:String} ORDER BY language ASC, segment_index ASC, attempt ASC, created_ms ASC, commit_id ASC FORMAT JSONEachRow"

// selectWorkspaceTakeCharges lists the charges a single take owns.
//
// The shipped writer attributes the segmentation pass to the first take, so
// whole-pass work is not separable by take_id. It is separable by kind. A
// segment charge is whole-pass, so this statement reads translate and synthesize
// rows only and WholePassCharges reads the segment rows.
const selectWorkspaceTakeCharges = "SELECT language, commit_id, segment_index, attempt, toString(kind) AS kind, toString(unit) AS unit, toInt64(round(units)) AS units, toInt64(round(unit_price_usd * 1000000000)) AS unit_price_nanodollars FROM charges WHERE dub_id = {dub_id:String} AND kind IN ('translate', 'synthesize') ORDER BY language ASC, segment_index ASC, attempt ASC, commit_id ASC, kind ASC, unit ASC FORMAT JSONEachRow"

// selectWholePassCharges lists the charges that no single take owns.
//
// The shipped writer attributes segmentation to the first rendered take even
// though the call serves the whole dub. Agent calls own no take.
//
// Agent rows collapse into one display charge. The wire has no turn identity,
// and the Details tab keys these rows by kind and units.
const selectWholePassCharges = "SELECT commit_id, kind, unit, units, unit_price_nanodollars, total_nanodollars FROM (SELECT commit_id, toString(kind) AS kind, toString(unit) AS unit, toInt64(round(units)) AS units, toInt64(round(unit_price_usd * 1000000000)) AS unit_price_nanodollars, toInt64(round(cost_usd * 1000000000)) AS total_nanodollars FROM charges WHERE dub_id = {dub_id:String} AND kind = 'segment' UNION ALL SELECT '' AS commit_id, 'agent' AS kind, 'turns' AS unit, toInt64(uniqExact(turn_id)) AS units, toInt64(0) AS unit_price_nanodollars, toInt64(sum(toInt64(round(cost_usd * 1000000000)))) AS total_nanodollars FROM charges WHERE dub_id = {dub_id:String} AND kind = 'agent' HAVING count() > 0) ORDER BY commit_id ASC, kind ASC, unit ASC FORMAT JSONEachRow"

// selectRunningTotal sums every charge for one dub in exact nanodollars.
//
// cost_usd is the materialized product of units and unit_price_usd, so the sum
// counts each billed row once. The counts name what the total covers. A dub with
// no charges still returns one row, because every aggregate over an empty set
// answers zero.
const selectRunningTotal = "SELECT toInt64(sum(toInt64(round(cost_usd * 1000000000)))) AS total_nanodollars, toUInt64(uniqExactIf(tuple(language, commit_id, segment_index, attempt, kind, provider), kind = 'segment')) AS segment_calls, toUInt64(uniqExactIf(tuple(language, commit_id, segment_index, attempt, kind, provider), kind = 'translate')) AS translate_calls, toUInt64(uniqExactIf(tuple(language, commit_id, segment_index, attempt, kind, provider), kind = 'synthesize')) AS synthesize_calls, toUInt64(uniqExactIf(tuple(turn_id, call_index), kind = 'agent')) AS agent_calls FROM charges WHERE dub_id = {dub_id:String} FORMAT JSONEachRow"

// selectWorkspaceLanguages lists the target languages one dub holds a take for.
//
// The take count makes the row 64-bit, so the shared integer pin covers this
// statement too. Ordering by language keeps the result stable across merges.
const selectWorkspaceLanguages = "SELECT language, toUInt64(count()) AS take_count FROM takes WHERE dub_id = {dub_id:String} GROUP BY language ORDER BY language ASC FORMAT JSONEachRow"

// selectProjectMetadata reads the ledger facts about one project.
//
// The ledger stores no title and no readiness, so the wire summary carries an
// empty title and an empty readiness. The route fills both from the upload
// record. created_at and updated_at come from the commit view, and the language
// list comes from the takes view. A project with no commit still returns one row
// with a zero count.
const selectProjectMetadata = "SELECT toUInt64(commit_count) AS commit_count, toInt64(created_ms) AS created_ms, toInt64(updated_ms) AS updated_ms, languages FROM (SELECT count() AS commit_count, toUnixTimestamp64Milli(min(created_at)) AS created_ms, toUnixTimestamp64Milli(max(created_at)) AS updated_ms FROM commits WHERE dub_id = {dub_id:String}) AS c CROSS JOIN (SELECT groupArray(language) AS languages FROM (SELECT DISTINCT language FROM takes WHERE dub_id = {dub_id:String} ORDER BY language ASC)) AS l FORMAT JSONEachRow"

// workspaceTakeRow is one JSONEachRow line from selectWorkspaceTakes.
type workspaceTakeRow struct {
	Language     string  `json:"language"`
	CommitID     string  `json:"commit_id"`
	SegmentIndex int32   `json:"segment_index"`
	Attempt      uint8   `json:"attempt"`
	Voice        string  `json:"voice"`
	Text         string  `json:"text"`
	AudioPath    string  `json:"audio_path"`
	Repair       string  `json:"repair"`
	RepairDetail string  `json:"repair_detail"`
	Peaks        []uint8 `json:"peaks"`
	SlotMs       int64   `json:"slot_ms"`
	MeasuredMs   int64   `json:"measured_ms"`
	DeltaMs      int64   `json:"delta_ms"`
	CreatedMs    int64   `json:"created_ms"`
}

// workspaceChargeRow is one JSONEachRow line from selectWorkspaceTakeCharges.
type workspaceChargeRow struct {
	Language             string `json:"language"`
	CommitID             string `json:"commit_id"`
	SegmentIndex         int32  `json:"segment_index"`
	Attempt              uint8  `json:"attempt"`
	Kind                 string `json:"kind"`
	Unit                 string `json:"unit"`
	Units                int64  `json:"units"`
	UnitPriceNanodollars int64  `json:"unit_price_nanodollars"`
}

// wholePassChargeRow is one JSONEachRow line from selectWholePassCharges.
type wholePassChargeRow struct {
	CommitID             string `json:"commit_id"`
	Kind                 string `json:"kind"`
	Unit                 string `json:"unit"`
	Units                int64  `json:"units"`
	UnitPriceNanodollars int64  `json:"unit_price_nanodollars"`
	TotalNanodollars     int64  `json:"total_nanodollars"`
}

// runningTotalRow is the single JSONEachRow line from selectRunningTotal.
type runningTotalRow struct {
	TotalNanodollars int64  `json:"total_nanodollars"`
	SegmentCalls     uint64 `json:"segment_calls"`
	TranslateCalls   uint64 `json:"translate_calls"`
	SynthesizeCalls  uint64 `json:"synthesize_calls"`
	AgentCalls       uint64 `json:"agent_calls"`
}

// languageRow is one JSONEachRow line from selectWorkspaceLanguages.
type languageRow struct {
	Language  string `json:"language"`
	TakeCount uint64 `json:"take_count"`
}

// projectMetadataRow is the single JSONEachRow line from selectProjectMetadata.
type projectMetadataRow struct {
	CommitCount uint64   `json:"commit_count"`
	CreatedMs   int64    `json:"created_ms"`
	UpdatedMs   int64    `json:"updated_ms"`
	Languages   []string `json:"languages"`
}

// workspaceTakeKey identifies one take by the prefix its charges repeat.
type workspaceTakeKey struct {
	Language     string
	CommitID     string
	SegmentIndex int32
	Attempt      uint8
}

// workspaceLineKey identifies one line inside one language track.
type workspaceLineKey struct {
	Language     string
	SegmentIndex int32
}

// WorkspaceTakes returns one language track per target language the dub holds.
//
// Each line carries every take attempt for its segment, oldest attempt first, so
// the last take is active. A take carries its fit, its itemized charges, and its
// waveform peaks. A line is flagged when it holds at least one take and none of
// them fits its slot.
//
// The read binds dub_id and orders by language, segment, attempt, then creation
// time. It never orders by commit_id alone, because a commit id is random.
func (c *Client) WorkspaceTakes(ctx context.Context, dubID string) ([]api.LanguageTrack, error) {
	if err := validateWorkspaceDub(dubID); err != nil {
		return nil, err
	}
	takes, err := c.workspaceTakeRows(ctx, dubID)
	if err != nil {
		return nil, err
	}
	charges, err := c.workspaceTakeChargeRows(ctx, dubID)
	if err != nil {
		return nil, err
	}
	return buildLanguageTracks(takes, charges)
}

// WholePassCharges returns the charges that no single take owns, ordered by
// commit, segment, kind, then unit. Each charge carries a nil segment id and an
// empty take file, which is how the wire marks whole-pass work.
func (c *Client) WholePassCharges(ctx context.Context, dubID string) ([]api.Charge, error) {
	if err := validateWorkspaceDub(dubID); err != nil {
		return nil, err
	}
	resp, err := c.queryClickHouse(ctx, selectWholePassCharges, map[string]string{"dub_id": dubID})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	rows, err := decodeJSONEachRow[wholePassChargeRow](resp.Body)
	if err != nil {
		return nil, fmt.Errorf("decode whole-pass charges: %w", err)
	}
	slices.SortFunc(rows, cmpWholePassChargeRow)
	out := make([]api.Charge, 0, len(rows))
	for _, row := range rows {
		out = append(out, api.Charge{
			Kind:                 row.Kind,
			Units:                row.Units,
			UnitPriceNanodollars: cost.Price(row.UnitPriceNanodollars),
			TotalNanodollars:     cost.Price(row.TotalNanodollars),
		})
	}
	return out, nil
}

// RunningTotal returns the exact sum of every charge for one dub. Covers names
// the call kinds the total includes. A dub with no charges answers a zero total
// and a sentence that says so.
func (c *Client) RunningTotal(ctx context.Context, dubID string) (api.Total, error) {
	if err := validateWorkspaceDub(dubID); err != nil {
		return api.Total{}, err
	}
	resp, err := c.queryClickHouse(ctx, selectRunningTotal, map[string]string{"dub_id": dubID})
	if err != nil {
		return api.Total{}, err
	}
	defer resp.Body.Close()
	rows, err := decodeJSONEachRow[runningTotalRow](resp.Body)
	if err != nil {
		return api.Total{}, fmt.Errorf("decode running total: %w", err)
	}
	if len(rows) != 1 {
		return api.Total{}, fmt.Errorf("ClickHouse returned %d running total rows, want 1", len(rows))
	}
	row := rows[0]
	return api.Total{
		TotalNanodollars: cost.Price(row.TotalNanodollars),
		Covers: fmt.Sprintf("%s, %s, %s, and %s.",
			countPhrase(row.SegmentCalls, "segment call", "segment calls"),
			countPhrase(row.TranslateCalls, "translation call", "translation calls"),
			countPhrase(row.SynthesizeCalls, "render call", "render calls"),
			countPhrase(row.AgentCalls, "agent call", "agent calls")),
	}, nil
}

// countPhrase names one call count with the right number. A count of one reads
// as one call rather than one calls.
func countPhrase(count uint64, singular, plural string) string {
	if count == 1 {
		return fmt.Sprintf("%d %s", count, singular)
	}
	return fmt.Sprintf("%d %s", count, plural)
}

// Languages returns the target language codes one dub holds a take for, ordered
// by code. A dub with no takes returns an empty list.
func (c *Client) Languages(ctx context.Context, dubID string) ([]string, error) {
	if err := validateWorkspaceDub(dubID); err != nil {
		return nil, err
	}
	resp, err := c.queryClickHouse(ctx, selectWorkspaceLanguages, map[string]string{"dub_id": dubID})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	rows, err := decodeJSONEachRow[languageRow](resp.Body)
	if err != nil {
		return nil, fmt.Errorf("decode workspace languages: %w", err)
	}
	slices.SortFunc(rows, func(a, b languageRow) int { return cmp.Compare(a.Language, b.Language) })
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.Language)
	}
	return out, nil
}

// ProjectMetadata returns the ledger facts about one project as a wire summary.
//
// The ledger stores no title and no readiness, so both stay empty and the route
// fills them from the upload record. CreatedAt and UpdatedAt are RFC 3339 in UTC
// and come from the commit view. Languages come from the takes view. A project
// with no commit returns an empty summary that names only the dub id.
func (c *Client) ProjectMetadata(ctx context.Context, dubID string) (api.DubSummary, error) {
	if err := validateWorkspaceDub(dubID); err != nil {
		return api.DubSummary{}, err
	}
	resp, err := c.queryClickHouse(ctx, selectProjectMetadata, map[string]string{"dub_id": dubID})
	if err != nil {
		return api.DubSummary{}, err
	}
	defer resp.Body.Close()
	rows, err := decodeJSONEachRow[projectMetadataRow](resp.Body)
	if err != nil {
		return api.DubSummary{}, fmt.Errorf("decode project metadata: %w", err)
	}
	if len(rows) != 1 {
		return api.DubSummary{}, fmt.Errorf("ClickHouse returned %d project metadata rows, want 1", len(rows))
	}
	row := rows[0]
	summary := api.DubSummary{
		ID:        dubID,
		Languages: slices.Clone(row.Languages),
	}
	if row.CommitCount > 0 {
		summary.CreatedAt = time.UnixMilli(row.CreatedMs).UTC().Format(time.RFC3339Nano)
		summary.UpdatedAt = time.UnixMilli(row.UpdatedMs).UTC().Format(time.RFC3339Nano)
	}
	return summary, nil
}

func (c *Client) workspaceTakeRows(ctx context.Context, dubID string) ([]workspaceTakeRow, error) {
	resp, err := c.queryClickHouse(ctx, selectWorkspaceTakes, map[string]string{"dub_id": dubID})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	rows, err := decodeJSONEachRow[workspaceTakeRow](resp.Body)
	if err != nil {
		return nil, fmt.Errorf("decode workspace takes: %w", err)
	}
	slices.SortFunc(rows, cmpWorkspaceTakeRow)
	return rows, nil
}

func (c *Client) workspaceTakeChargeRows(ctx context.Context, dubID string) ([]workspaceChargeRow, error) {
	resp, err := c.queryClickHouse(ctx, selectWorkspaceTakeCharges, map[string]string{"dub_id": dubID})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	rows, err := decodeJSONEachRow[workspaceChargeRow](resp.Body)
	if err != nil {
		return nil, fmt.Errorf("decode workspace take charges: %w", err)
	}
	slices.SortFunc(rows, cmpWorkspaceChargeRow)
	return rows, nil
}

// buildLanguageTracks groups take rows into tracks and lines, then attaches the
// itemized charges each take owns. Both slices arrive ordered, so the grouping
// keeps that order and never depends on the server's row order.
func buildLanguageTracks(takes []workspaceTakeRow, charges []workspaceChargeRow) ([]api.LanguageTrack, error) {
	chargesByTake := make(map[workspaceTakeKey][]api.Charge, len(charges))
	for _, row := range charges {
		key := workspaceTakeKey{
			Language:     row.Language,
			CommitID:     row.CommitID,
			SegmentIndex: row.SegmentIndex,
			Attempt:      row.Attempt,
		}
		segment := int(row.SegmentIndex)
		chargesByTake[key] = append(chargesByTake[key], api.Charge{
			Kind:                 row.Kind,
			SegmentID:            &segment,
			Units:                row.Units,
			UnitPriceNanodollars: cost.Price(row.UnitPriceNanodollars),
			TotalNanodollars:     cost.Price(row.Units * row.UnitPriceNanodollars),
		})
	}

	tracks := make([]api.LanguageTrack, 0, 1)
	trackIndex := make(map[string]int)
	lineIndex := make(map[workspaceLineKey]int)
	for _, row := range takes {
		trackAt, ok := trackIndex[row.Language]
		if !ok {
			tracks = append(tracks, api.LanguageTrack{Language: row.Language, Lines: []api.Line{}})
			trackAt = len(tracks) - 1
			trackIndex[row.Language] = trackAt
		}
		lineKey := workspaceLineKey{Language: row.Language, SegmentIndex: row.SegmentIndex}
		lineAt, ok := lineIndex[lineKey]
		if !ok {
			tracks[trackAt].Lines = append(tracks[trackAt].Lines, api.Line{
				SegmentID: int(row.SegmentIndex),
				Text:      row.Text,
			})
			lineAt = len(tracks[trackAt].Lines) - 1
			lineIndex[lineKey] = lineAt
		}
		line := &tracks[trackAt].Lines[lineAt]
		line.Text = row.Text
		// Charges starts empty so a take with no itemized charge marshals as
		// [] rather than null, the shape testdata/wire/take.json declares.
		take := api.Take{
			File:    row.AudioPath,
			Voice:   row.Voice,
			Attempt: int(row.Attempt),
			Repair:  row.Repair,
			Charges: []api.Charge{},
			Fit: api.Fit{
				SlotMs:     row.SlotMs,
				MeasuredMs: row.MeasuredMs,
				DeltaMs:    row.DeltaMs,
				State:      api.FitState(row.SlotMs, row.MeasuredMs),
			},
		}
		if factor := atempoStretchMilli(row.Repair, row.RepairDetail); factor > 0 {
			take.StretchFactorMilli = factor
		}
		if err := ApplyTakePeaks(&take, row.Peaks); err != nil {
			return nil, fmt.Errorf("take %q peaks: %w", row.AudioPath, err)
		}
		key := workspaceTakeKey{
			Language:     row.Language,
			CommitID:     row.CommitID,
			SegmentIndex: row.SegmentIndex,
			Attempt:      row.Attempt,
		}
		for _, charge := range chargesByTake[key] {
			charge.TakeFile = take.File
			take.Charges = append(take.Charges, charge)
		}
		line.Takes = append(line.Takes, take)
	}

	for trackAt := range tracks {
		for lineAt := range tracks[trackAt].Lines {
			line := &tracks[trackAt].Lines[lineAt]
			fits := false
			for _, take := range line.Takes {
				if take.Fit.State == api.StateFits {
					fits = true
					break
				}
			}
			line.Flagged = len(line.Takes) > 0 && !fits
		}
	}
	return tracks, nil
}

// atempoStretchMilli recovers the atempo ratio from a take's repair detail.
//
// The fit rewrite writes "atempo stretch applied at ratio 1.0500". The route
// parses the ratio into thousandths, so the wire carries the speed the creator
// lost. Any other repair returns zero, which omits the wire field.
func atempoStretchMilli(repair, detail string) int64 {
	if repair != api.RepairAtempo {
		return 0
	}
	const prefix = "atempo stretch applied at ratio "
	raw, ok := strings.CutPrefix(detail, prefix)
	if !ok {
		return 0
	}
	ratio, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || ratio <= 0 {
		return 0
	}
	return int64(math.Round(ratio * 1000))
}

func cmpWorkspaceTakeRow(a, b workspaceTakeRow) int {
	if c := cmp.Compare(a.Language, b.Language); c != 0 {
		return c
	}
	if c := cmp.Compare(a.SegmentIndex, b.SegmentIndex); c != 0 {
		return c
	}
	if c := cmp.Compare(a.Attempt, b.Attempt); c != 0 {
		return c
	}
	if c := cmp.Compare(a.CreatedMs, b.CreatedMs); c != 0 {
		return c
	}
	return cmp.Compare(a.CommitID, b.CommitID)
}

func cmpWorkspaceChargeRow(a, b workspaceChargeRow) int {
	if c := cmp.Compare(a.Language, b.Language); c != 0 {
		return c
	}
	if c := cmp.Compare(a.SegmentIndex, b.SegmentIndex); c != 0 {
		return c
	}
	if c := cmp.Compare(a.Attempt, b.Attempt); c != 0 {
		return c
	}
	if c := cmp.Compare(a.CommitID, b.CommitID); c != 0 {
		return c
	}
	if c := cmp.Compare(a.Kind, b.Kind); c != 0 {
		return c
	}
	return cmp.Compare(a.Unit, b.Unit)
}

func cmpWholePassChargeRow(a, b wholePassChargeRow) int {
	if c := cmp.Compare(a.CommitID, b.CommitID); c != 0 {
		return c
	}
	if c := cmp.Compare(a.Kind, b.Kind); c != 0 {
		return c
	}
	return cmp.Compare(a.Unit, b.Unit)
}

func validateWorkspaceDub(dubID string) error {
	if strings.TrimSpace(dubID) == "" {
		return errors.New("ledger workspace dub_id is empty")
	}
	return nil
}
