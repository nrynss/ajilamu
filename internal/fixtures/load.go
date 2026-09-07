// Package fixtures loads the static workspace payload from testdata.
// LoadDub returns the manifest that drives the offline frontend and Go tests.
package fixtures

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/nrynss/ajilamu/internal/api"
	"github.com/nrynss/ajilamu/internal/cost"
)

// LoadDub reads testdata/manifest.json and returns the workspace payload.
// It cross-validates the manifest against the golden fixture files, so a
// manifest that disagrees with measured reality fails loudly.
func LoadDub() (*api.Dub, error) {
	path, err := fixturePath("manifest.json")
	if err != nil {
		return nil, err
	}
	return loadFrom(path)
}

// loadFrom decodes one Dub payload and validates it against the goldens.
func loadFrom(path string) (*api.Dub, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read manifest %s: %w", path, err)
	}
	var dub api.Dub
	if err := json.Unmarshal(data, &dub); err != nil {
		return nil, fmt.Errorf("decode manifest %s: %w", path, err)
	}
	if err := validateSegments(&dub); err != nil {
		return nil, err
	}
	if err := validateTakes(&dub); err != nil {
		return nil, err
	}
	if err := validateFlaggedLine(&dub); err != nil {
		return nil, err
	}
	if err := validateCommits(dub.Commits); err != nil {
		return nil, err
	}
	if err := validateTotal(&dub); err != nil {
		return nil, err
	}
	return &dub, nil
}

// fixturePath locates a file below testdata from the repo root or the
// package working directory.
func fixturePath(rel string) (string, error) {
	candidates := []string{
		rel,
		filepath.Join("testdata", rel),
		filepath.Join("..", rel),
		filepath.Join("..", "testdata", rel),
		filepath.Join("..", "..", rel),
		filepath.Join("..", "..", "testdata", rel),
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("fixture %s not found, tried %v", rel, candidates)
}

// readGoldenSegments loads testdata/segments.json as the source of truth.
func readGoldenSegments() ([]api.Segment, error) {
	path, err := fixturePath("segments.json")
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read segments.json: %w", err)
	}
	var segments []api.Segment
	if err := json.Unmarshal(data, &segments); err != nil {
		return nil, fmt.Errorf("decode segments.json: %w", err)
	}
	return segments, nil
}

// metricRow mirrors one row of the golden metrics fixture.
type metricRow struct {
	Takes map[string]int64 `json:"takes"`
}

// readGoldenTakeDurations maps every measured take file to its duration in
// milliseconds, one entry per row of testdata/expected/metrics.json.
func readGoldenTakeDurations() (map[string]int64, error) {
	path, err := fixturePath(filepath.Join("expected", "metrics.json"))
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read metrics.json: %w", err)
	}
	var rows []metricRow
	if err := json.Unmarshal(data, &rows); err != nil {
		return nil, fmt.Errorf("decode metrics.json: %w", err)
	}
	durations := make(map[string]int64)
	for _, row := range rows {
		for file, durationMs := range row.Takes {
			durations[file] = durationMs
		}
	}
	return durations, nil
}

// validateSegments compares every manifest segment with segments.json.
func validateSegments(dub *api.Dub) error {
	golden, err := readGoldenSegments()
	if err != nil {
		return err
	}
	if len(dub.Segments) != len(golden) {
		return fmt.Errorf("manifest has %d segments, want %d", len(dub.Segments), len(golden))
	}
	for i, want := range golden {
		got := dub.Segments[i]
		if got.ID != want.ID {
			return fmt.Errorf("segment %d id = %d, want %d", i+1, got.ID, want.ID)
		}
		if got.StartMs != want.StartMs {
			return fmt.Errorf("segment %d start_ms = %d, want %d", got.ID, got.StartMs, want.StartMs)
		}
		if got.EndMs != want.EndMs {
			return fmt.Errorf("segment %d end_ms = %d, want %d", got.ID, got.EndMs, want.EndMs)
		}
		if got.DurationMs != want.DurationMs {
			return fmt.Errorf("segment %d duration_ms = %d, want %d", got.ID, got.DurationMs, want.DurationMs)
		}
		if got.Text != want.Text {
			return fmt.Errorf("segment %d text differs from segments.json", got.ID)
		}
		if got.Speaker != want.Speaker {
			return fmt.Errorf("segment %d speaker = %q, want %q", got.ID, got.Speaker, want.Speaker)
		}
		if got.Emotion != want.Emotion {
			return fmt.Errorf("segment %d emotion = %q, want %q", got.ID, got.Emotion, want.Emotion)
		}
	}
	return nil
}

// segmentByID returns the manifest segment with the given id.
func segmentByID(dub *api.Dub, id int) (api.Segment, error) {
	for _, segment := range dub.Segments {
		if segment.ID == id {
			return segment, nil
		}
	}
	return api.Segment{}, fmt.Errorf("manifest has no segment %d", id)
}

// validateTakes compares every take file and measured duration with
// metrics.json and checks each fit against the domain semantics.
func validateTakes(dub *api.Dub) error {
	golden, err := readGoldenTakeDurations()
	if err != nil {
		return err
	}
	var seen int
	for _, track := range dub.Languages {
		for _, line := range track.Lines {
			segment, err := segmentByID(dub, line.SegmentID)
			if err != nil {
				return fmt.Errorf("line for segment %d: %w", line.SegmentID, err)
			}
			for _, take := range line.Takes {
				seen++
				wantMs, ok := golden[take.File]
				if !ok {
					return fmt.Errorf("take %s on segment %d has no metrics.json row", take.File, line.SegmentID)
				}
				if take.Fit.MeasuredMs != wantMs {
					return fmt.Errorf("take %s measured_ms = %d, want %d", take.File, take.Fit.MeasuredMs, wantMs)
				}
				if take.Fit.SlotMs != segment.DurationMs {
					return fmt.Errorf("take %s slot_ms = %d, want %d", take.File, take.Fit.SlotMs, segment.DurationMs)
				}
				wantDelta := take.Fit.MeasuredMs - take.Fit.SlotMs
				if take.Fit.DeltaMs != wantDelta {
					return fmt.Errorf("take %s delta_ms = %d, want %d", take.File, take.Fit.DeltaMs, wantDelta)
				}
				wantState := api.FitState(take.Fit.SlotMs, take.Fit.MeasuredMs)
				if take.Fit.State != wantState {
					return fmt.Errorf("take %s state = %q, want %q", take.File, take.Fit.State, wantState)
				}
			}
		}
	}
	if seen != len(golden) {
		return fmt.Errorf("manifest lists %d takes, metrics.json lists %d", seen, len(golden))
	}
	return nil
}

// validateFlaggedLine pins segment 8 to its flagged, too-short review state.
func validateFlaggedLine(dub *api.Dub) error {
	for _, track := range dub.Languages {
		for _, line := range track.Lines {
			if line.SegmentID != 8 {
				continue
			}
			if len(line.Takes) == 0 {
				return fmt.Errorf("segment 8 line has no takes")
			}
			if !line.Flagged {
				return fmt.Errorf("segment 8 line is not flagged")
			}
			active := line.Takes[len(line.Takes)-1]
			if active.Fit.DeltaMs != -2910 {
				return fmt.Errorf("segment 8 active take delta_ms = %d, want -2910", active.Fit.DeltaMs)
			}
			if active.Fit.State != api.StateTooShort {
				return fmt.Errorf("segment 8 active take state = %q, want %q", active.Fit.State, api.StateTooShort)
			}
			return nil
		}
	}
	return fmt.Errorf("manifest has no line for segment 8")
}

// validateCommits checks that the commits form one parent chain whose
// versions run consecutively from one.
func validateCommits(commits []api.Commit) error {
	if len(commits) == 0 {
		return fmt.Errorf("commit chain is empty")
	}
	for i, commit := range commits {
		if i == 0 {
			if commit.VersionNumber != 1 {
				return fmt.Errorf("root commit version_number = %d, want 1", commit.VersionNumber)
			}
			if commit.ParentCommitID != "" {
				return fmt.Errorf("root commit %s has parent %q, want empty", commit.CommitID, commit.ParentCommitID)
			}
			continue
		}
		previous := commits[i-1]
		if commit.ParentCommitID != previous.CommitID {
			return fmt.Errorf("commit %s parent = %q, want %q", commit.CommitID, commit.ParentCommitID, previous.CommitID)
		}
		if commit.VersionNumber != previous.VersionNumber+1 {
			return fmt.Errorf("commit %s version_number = %d, want %d", commit.CommitID, commit.VersionNumber, previous.VersionNumber+1)
		}
	}
	return nil
}

// validateTotal reconciles the declared total with every charge in the dub.
func validateTotal(dub *api.Dub) error {
	var sum cost.Price
	for _, charge := range dub.Charges {
		sum += charge.TotalNanodollars
	}
	for _, track := range dub.Languages {
		for _, line := range track.Lines {
			for _, take := range line.Takes {
				for _, charge := range take.Charges {
					sum += charge.TotalNanodollars
				}
			}
		}
	}
	if dub.Total.TotalNanodollars != sum {
		return fmt.Errorf("total_nanodollars = %d, want charge sum %d", dub.Total.TotalNanodollars, sum)
	}
	return nil
}
