package main

import (
	"testing"

	"github.com/nrynss/ajilamu/internal/api"
	"github.com/nrynss/ajilamu/internal/ledger"
)

// A nil client must stay a nil interface, because a typed nil inside
// api.HistoryReader would answer 500 instead of the 503 the tab expects.
func TestNewHistoryReaderReturnsNilForNilClient(t *testing.T) {
	if reader := newHistoryReader(nil); reader != nil {
		t.Fatalf("newHistoryReader(nil) = %T, want nil interface", reader)
	}
	client := &ledger.Client{}
	reader := newHistoryReader(client)
	if reader == nil {
		t.Fatal("newHistoryReader(client) = nil, want adapter")
	}
	adapter, ok := reader.(*historyReader)
	if !ok {
		t.Fatalf("newHistoryReader(client) = %T, want *historyReader", reader)
	}
	if adapter.client != client {
		t.Fatal("adapter did not keep the client it was given")
	}
}

func TestCommitHistoryMapsLedgerRows(t *testing.T) {
	rows := []ledger.CommitHistoryRow{
		{
			CommitID:       "commit-2",
			ParentCommitID: "commit-1",
			VersionSeq:     2,
			CreatedAt:      "2026-09-08T10:00:00Z",
			Action:         api.ActionTakeRendered,
			Author:         api.AuthorAgent,
			Instruction:    "render line 3",
		},
		{
			CommitID:       "commit-1",
			ParentCommitID: "",
			VersionSeq:     1,
			CreatedAt:      "2026-09-08T09:00:00Z",
		},
	}

	commits := commitHistory(rows)
	want := []api.Commit{
		{
			CommitID:       "commit-2",
			ParentCommitID: "commit-1",
			VersionNumber:  2,
			CreatedAt:      "2026-09-08T10:00:00Z",
			Action:         api.ActionTakeRendered,
			Author:         api.AuthorAgent,
			Instruction:    "render line 3",
		},
		{
			CommitID:       "commit-1",
			ParentCommitID: "",
			VersionNumber:  1,
			CreatedAt:      "2026-09-08T09:00:00Z",
		},
	}
	if len(commits) != len(want) {
		t.Fatalf("commit count = %d, want %d", len(commits), len(want))
	}
	for i := range want {
		if commits[i] != want[i] {
			t.Errorf("commit %d = %+v, want %+v", i, commits[i], want[i])
		}
	}
}

func TestTimelineEntriesMapLedgerSegments(t *testing.T) {
	segments := []ledger.TimelineSegment{
		{
			VersionSeq:   7,
			SegmentIndex: 3,
			StartMs:      1000,
			EndMs:        2500,
			Speaker:      "Narrator",
			Emotion:      "calm",
			SourceText:   "hello there",
			Text:         "namaskaram",
			TakeID:       "take-3",
		},
	}

	entries := timelineEntries(segments)
	want := []api.TimelineEntry{
		{
			SegmentIndex: 3,
			StartMs:      1000,
			EndMs:        2500,
			Speaker:      "Narrator",
			Emotion:      "calm",
			SourceText:   "hello there",
			Text:         "namaskaram",
			TakeID:       "take-3",
			VersionSeq:   7,
		},
	}
	if len(entries) != len(want) {
		t.Fatalf("entry count = %d, want %d", len(entries), len(want))
	}
	for i := range want {
		if entries[i] != want[i] {
			t.Errorf("entry %d = %+v, want %+v", i, entries[i], want[i])
		}
	}
}

func TestBranchComparisonMapsLedgerHeads(t *testing.T) {
	compare := ledger.BranchCompare{
		A: ledger.BranchView{
			CommitID:          "commit-a",
			Branch:            "main",
			SlotMs:            4200,
			TakeCount:         2,
			AttributedCostUSD: "1.230000",
		},
		B: ledger.BranchView{
			CommitID:          "commit-b",
			Branch:            "shorter",
			SlotMs:            4100,
			TakeCount:         3,
			AttributedCostUSD: "0.750000",
		},
	}

	got := branchComparison(compare)
	want := api.BranchComparison{
		A: api.BranchSummary{
			CommitID:          "commit-a",
			Branch:            "main",
			SlotMs:            4200,
			TakeCount:         2,
			AttributedCostUSD: "1.230000",
		},
		B: api.BranchSummary{
			CommitID:          "commit-b",
			Branch:            "shorter",
			SlotMs:            4100,
			TakeCount:         3,
			AttributedCostUSD: "0.750000",
		},
	}
	if got != want {
		t.Errorf("comparison = %+v, want %+v", got, want)
	}
}
