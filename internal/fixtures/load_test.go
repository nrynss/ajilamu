package fixtures

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nrynss/ajilamu/internal/api"
	"github.com/nrynss/ajilamu/internal/cost"
)

// lineRef returns the line bound to the given segment inside the dub.
func lineRef(dub *api.Dub, segmentID int) *api.Line {
	for ti := range dub.Languages {
		for li := range dub.Languages[ti].Lines {
			if dub.Languages[ti].Lines[li].SegmentID == segmentID {
				return &dub.Languages[ti].Lines[li]
			}
		}
	}
	return nil
}

func TestLoadDubWorkspace(t *testing.T) {
	dub, err := LoadDub()
	if err != nil {
		t.Fatalf("LoadDub() error: %v", err)
	}
	if len(dub.Segments) != 8 {
		t.Errorf("got %d segments, want 8", len(dub.Segments))
	}
	if dub.Title != "NASA 75-second clip" {
		t.Errorf("title = %q, want %q", dub.Title, "NASA 75-second clip")
	}

	if dub.SourceLanguage != "en" {
		t.Errorf("source_language = %q, want %q", dub.SourceLanguage, "en")
	}
	if dub.Readiness != api.ReadinessReview {
		t.Errorf("readiness = %q, want %q", dub.Readiness, api.ReadinessReview)
	}
	if len(dub.Languages) != 1 {
		t.Fatalf("got %d language tracks, want 1", len(dub.Languages))
	}
	if dub.Languages[0].Language != "ml" {
		t.Errorf("language = %q, want %q", dub.Languages[0].Language, "ml")
	}

	takeCounts := make(map[int]int)
	for _, track := range dub.Languages {
		if len(track.Lines) != 8 {
			t.Errorf("track %s has %d lines, want 8", track.Language, len(track.Lines))
		}
		for _, line := range track.Lines {
			if line.Text == "" {
				t.Errorf("line %d text is empty", line.SegmentID)
			}
			for _, take := range line.Takes {
				if take.Voice != "ml-IN-Chirp3-HD-Zephyr" {
					t.Errorf("take %s voice = %q, want ml-IN-Chirp3-HD-Zephyr", take.File, take.Voice)
				}
			}
			takeCounts[line.SegmentID] = len(line.Takes)
		}
	}
	totalTakes := 0
	for id := 1; id <= 8; id++ {
		want := 1
		if id == 3 || id == 4 {
			want = 2
		}
		if takeCounts[id] != want {
			t.Errorf("line %d has %d takes, want %d", id, takeCounts[id], want)
		}
		totalTakes += takeCounts[id]
	}
	if totalTakes != 10 {
		t.Errorf("got %d takes, want 10", totalTakes)
	}

	for _, ts := range []string{dub.CreatedAt, dub.UpdatedAt} {
		if _, err := time.Parse(time.RFC3339, ts); err != nil {
			t.Errorf("timestamp %q is not RFC 3339: %v", ts, err)
		}
	}
	last := dub.Commits[len(dub.Commits)-1]
	if dub.UpdatedAt != last.CreatedAt {
		t.Errorf("updated_at = %q, want newest commit time %q", dub.UpdatedAt, last.CreatedAt)
	}
}

func TestLoadDubFromRepoRoot(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error: %v", err)
	}
	if filepath.Base(wd) == "fixtures" {
		t.Chdir(filepath.Join("..", ".."))
	}
	dub, err := LoadDub()
	if err != nil {
		t.Fatalf("LoadDub() from repo root error: %v", err)
	}
	if len(dub.Segments) != 8 {
		t.Errorf("got %d segments, want 8", len(dub.Segments))
	}
}

func TestLine8Flagged(t *testing.T) {
	dub, err := LoadDub()
	if err != nil {
		t.Fatalf("LoadDub() error: %v", err)
	}
	line := lineRef(dub, 8)
	if line == nil {
		t.Fatal("manifest has no line for segment 8")
	}
	if !line.Flagged {
		t.Error("line 8 is not flagged")
	}
	active := line.Takes[len(line.Takes)-1]
	if active.Fit.DeltaMs != -2910 {
		t.Errorf("line 8 active take delta_ms = %d, want -2910", active.Fit.DeltaMs)
	}
	if active.Fit.State != api.StateTooShort {
		t.Errorf("line 8 active take state = %q, want %q", active.Fit.State, api.StateTooShort)
	}
	if active.Fit.MeasuredMs != 4200 || active.Fit.SlotMs != 7110 {
		t.Errorf("line 8 fit = %+v, want slot 7110 measured 4200", active.Fit)
	}
}

func TestStretchedTakes(t *testing.T) {
	dub, err := LoadDub()
	if err != nil {
		t.Fatalf("LoadDub() error: %v", err)
	}
	tests := []struct {
		segmentID int
		try1      string
		try1Ms    int64
		try1Delta int64
		stretched string
		factor    int64
		stretchMs int64
		stretchD  int64
	}{
		{3, "seg_3_try1.wav", 5720, 400, "seg_3_stretched.wav", 1075, 5338, 18},
		{4, "seg_4_try1.wav", 5920, 260, "seg_4_stretched.wav", 1046, 5662, 2},
	}
	for _, tt := range tests {
		line := lineRef(dub, tt.segmentID)
		if line == nil {
			t.Fatalf("manifest has no line for segment %d", tt.segmentID)
		}
		if line.Flagged {
			t.Errorf("line %d is flagged, want clear", tt.segmentID)
		}
		if len(line.Takes) != 2 {
			t.Fatalf("line %d has %d takes, want 2", tt.segmentID, len(line.Takes))
		}
		first := line.Takes[0]
		if first.File != tt.try1 || first.Fit.MeasuredMs != tt.try1Ms {
			t.Errorf("line %d first take = %s (%d ms), want %s (%d ms)",
				tt.segmentID, first.File, first.Fit.MeasuredMs, tt.try1, tt.try1Ms)
		}
		if first.Repair != api.RepairNone {
			t.Errorf("line %d first take repair = %q, want %q", tt.segmentID, first.Repair, api.RepairNone)
		}
		if first.Fit.DeltaMs != tt.try1Delta {
			t.Errorf("line %d first take delta_ms = %d, want %d", tt.segmentID, first.Fit.DeltaMs, tt.try1Delta)
		}
		stretched := line.Takes[1]
		if stretched.File != tt.stretched {
			t.Errorf("line %d second take file = %q, want %q", tt.segmentID, stretched.File, tt.stretched)
		}
		if stretched.Attempt != 2 {
			t.Errorf("line %d stretched attempt = %d, want 2", tt.segmentID, stretched.Attempt)
		}
		if stretched.Repair != api.RepairAtempo {
			t.Errorf("line %d stretched repair = %q, want %q", tt.segmentID, stretched.Repair, api.RepairAtempo)
		}
		if stretched.StretchFactorMilli != tt.factor {
			t.Errorf("line %d stretch_factor_milli = %d, want %d", tt.segmentID, stretched.StretchFactorMilli, tt.factor)
		}
		if stretched.Fit.MeasuredMs != tt.stretchMs {
			t.Errorf("line %d stretched measured_ms = %d, want %d", tt.segmentID, stretched.Fit.MeasuredMs, tt.stretchMs)
		}
		if stretched.Fit.DeltaMs != tt.stretchD {
			t.Errorf("line %d stretched delta_ms = %d, want %d", tt.segmentID, stretched.Fit.DeltaMs, tt.stretchD)
		}
		if len(stretched.Charges) != 0 {
			t.Errorf("line %d stretched take carries %d charges, want 0", tt.segmentID, len(stretched.Charges))
		}
	}
}

func TestLoadRejectsMutatedManifest(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*api.Dub)
		wantErr string
	}{
		{
			name: "segment 8 delta flipped positive",
			mutate: func(dub *api.Dub) {
				line := lineRef(dub, 8)
				line.Takes[len(line.Takes)-1].Fit.DeltaMs = 2910
			},
			wantErr: "delta_ms",
		},
		{
			name: "segment 8 take serialized as fits",
			mutate: func(dub *api.Dub) {
				line := lineRef(dub, 8)
				line.Takes[len(line.Takes)-1].Fit.State = api.StateFits
			},
			wantErr: "too_short",
		},
		{
			name: "segment 8 flag cleared",
			mutate: func(dub *api.Dub) {
				lineRef(dub, 8).Flagged = false
			},
			wantErr: "not flagged",
		},
		{
			name: "segment duration drifted",
			mutate: func(dub *api.Dub) {
				dub.Segments[0].DurationMs++
			},
			wantErr: "duration_ms",
		},
		{
			name: "take dropped from line 8",
			mutate: func(dub *api.Dub) {
				lineRef(dub, 8).Takes = nil
			},
			wantErr: "lists 9 takes",
		},
		{
			name: "total no longer reconciles",
			mutate: func(dub *api.Dub) {
				dub.Total.TotalNanodollars++
			},
			wantErr: "total_nanodollars",
		},
		{
			name: "commit parent chain broken",
			mutate: func(dub *api.Dub) {
				dub.Commits[3].ParentCommitID = "broken-parent"
			},
			wantErr: "parent",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := loadMutated(t, tt.mutate)
			if err == nil {
				t.Fatal("loadFrom accepted the mutated manifest, want error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q does not name %q", err, tt.wantErr)
			}
		})
	}
}

// loadMutated writes a mutated copy of the shipped manifest and loads it.
func loadMutated(t *testing.T, mutate func(*api.Dub)) error {
	t.Helper()
	path, err := fixturePath("manifest.json")
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var dub api.Dub
	if err := json.Unmarshal(data, &dub); err != nil {
		return err
	}
	mutate(&dub)
	out, err := json.MarshalIndent(&dub, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(t.TempDir(), "manifest.json")
	if err := os.WriteFile(tmp, out, 0o644); err != nil {
		return err
	}
	_, err = loadFrom(tmp)
	return err
}

func TestTotalReconciles(t *testing.T) {
	dub, err := LoadDub()
	if err != nil {
		t.Fatalf("LoadDub() error: %v", err)
	}
	card := cost.DefaultRateCard()
	priceFor := func(kind string) (cost.Price, bool) {
		switch kind {
		case api.ChargeSegment:
			return card.SegmentPerInputChar, true
		case api.ChargeTranslate:
			return card.TranslatePerInputChar, true
		case api.ChargeSynthesize:
			return card.SynthesizePerChar, true
		}
		return 0, false
	}
	var sum cost.Price
	check := func(charges []api.Charge, where string) {
		for _, charge := range charges {
			price, ok := priceFor(charge.Kind)
			if !ok {
				t.Errorf("charge %s on %s has unknown kind %q", charge.TakeFile, where, charge.Kind)
				continue
			}
			if charge.UnitPriceNanodollars != price {
				t.Errorf("charge kind %s on %s unit price = %d, want rate card %d",
					charge.Kind, where, charge.UnitPriceNanodollars, price)
			}
			if charge.TotalNanodollars != price*cost.Price(charge.Units) {
				t.Errorf("charge kind %s on %s total = %d, want %d",
					charge.Kind, where, charge.TotalNanodollars, price*cost.Price(charge.Units))
			}
			sum += charge.TotalNanodollars
		}
	}
	check(dub.Charges, "whole pass")
	for _, track := range dub.Languages {
		for _, line := range track.Lines {
			for _, take := range line.Takes {
				check(take.Charges, take.File)
			}
		}
	}
	if sum != dub.Total.TotalNanodollars {
		t.Errorf("rate card sum = %d, want declared total %d", sum, dub.Total.TotalNanodollars)
	}
}

func TestCommitChainValid(t *testing.T) {
	dub, err := LoadDub()
	if err != nil {
		t.Fatalf("LoadDub() error: %v", err)
	}
	commits := dub.Commits
	if len(commits) != 7 {
		t.Fatalf("got %d commits, want 7", len(commits))
	}
	ids := make(map[string]bool)
	for i, commit := range commits {
		if ids[commit.CommitID] {
			t.Errorf("commit id %q repeats", commit.CommitID)
		}
		ids[commit.CommitID] = true
		if want := i + 1; commit.VersionNumber != want {
			t.Errorf("commit %d version_number = %d, want %d", i, commit.VersionNumber, want)
		}
		if i == 0 {
			if commit.ParentCommitID != "" {
				t.Errorf("root commit has parent %q, want empty", commit.ParentCommitID)
			}
			continue
		}
		if commit.ParentCommitID != commits[i-1].CommitID {
			t.Errorf("commit %d parent = %q, want %q", i, commit.ParentCommitID, commits[i-1].CommitID)
		}
	}
}
