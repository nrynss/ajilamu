package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	texttospeechpb "cloud.google.com/go/texttospeech/apiv1/texttospeechpb"

	"github.com/nrynss/ajilamu/internal/agent"
	"github.com/nrynss/ajilamu/internal/api"
	"github.com/nrynss/ajilamu/internal/config"
	"github.com/nrynss/ajilamu/internal/cost"
	"github.com/nrynss/ajilamu/internal/fit"
	"github.com/nrynss/ajilamu/internal/gemini"
	"github.com/nrynss/ajilamu/internal/ledger"
	"github.com/nrynss/ajilamu/internal/tts"
	"github.com/nrynss/ajilamu/internal/types"
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

// A nil client must stay a nil interface, because a typed nil inside
// api.IndexLedger would answer 500 on the index route instead of serving the
// upload rows a clone with no ClickHouse credentials can still read.
func TestNewIndexLedgerReturnsNilForNilClient(t *testing.T) {
	if reader := newIndexLedger(nil); reader != nil {
		t.Fatalf("newIndexLedger(nil) = %T, want nil interface", reader)
	}
	client := &ledger.Client{}
	reader := newIndexLedger(client)
	if reader == nil {
		t.Fatal("newIndexLedger(client) = nil, want adapter")
	}
	adapter, ok := reader.(*indexLedger)
	if !ok {
		t.Fatalf("newIndexLedger(client) = %T, want *indexLedger", reader)
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

// A nil client must stay a nil interface, because a typed nil inside
// api.RunRecorder would answer 500 instead of the 503 the route expects.
func TestNewRunRecorderReturnsNilForNilClient(t *testing.T) {
	if recorder := newRunRecorder(nil, "gemini-3.8-flash", nil); recorder != nil {
		t.Fatalf("newRunRecorder(nil) = %T, want nil interface", recorder)
	}
	client := &ledger.Client{}
	recorder := newRunRecorder(client, "gemini-3.8-flash", nil)
	if recorder == nil {
		t.Fatal("newRunRecorder(client) = nil, want adapter")
	}
	adapter, ok := recorder.(*runRecorder)
	if !ok {
		t.Fatalf("newRunRecorder(client) = %T, want *runRecorder", recorder)
	}
	if adapter.client != client {
		t.Fatal("adapter did not keep the client it was given")
	}
}

func TestNewAgentChargeRecorderReturnsNilForNilClient(t *testing.T) {
	if recorder := newAgentChargeRecorder(nil, "gemini-3.8-flash"); recorder != nil {
		t.Fatalf("newAgentChargeRecorder(nil) = %T, want nil interface", recorder)
	}
	client := &ledger.Client{}
	recorder := newAgentChargeRecorder(client, "gemini-3.8-flash")
	adapter, ok := recorder.(*agentChargeRecorder)
	if !ok || adapter.client != client {
		t.Fatalf("newAgentChargeRecorder(client) = %T, want adapter with client", recorder)
	}
}

func TestAgentChargeRecorderUsesEmptyCommitSentinel(t *testing.T) {
	client, captured := standInClickHouse(t, "")
	defer client.Close()
	recorder := newAgentChargeRecorder(client, "gemini-3.8-flash")
	err := recorder.RecordAgentTurn(context.Background(), api.AgentChargeRecord{
		TurnID: "turn-1",
		DubID:  "dub-agent",
		Charges: []cost.Charge{{
			Kind: cost.ChargeAgent, PromptTokens: 1200, CandidateTokens: 340,
			PromptUnitPrice: 150, CandidateUnitPrice: 600,
		}},
	})
	if err != nil {
		t.Fatalf("RecordAgentTurn: %v", err)
	}
	rows := insertRows(t, captured(), "charges_raw")
	if len(rows) != 2 {
		t.Fatalf("agent charge rows = %d, want prompt and candidate rows", len(rows))
	}
	for _, row := range rows {
		assertField(t, row, "turn_id", "turn-1")
		assertField(t, row, "commit_id", "")
		assertField(t, row, "project_id", "dub-agent")
		assertField(t, row, "dub_id", "dub-agent")
		assertField(t, row, "owner_id", "local")
		assertField(t, row, "segment_index", float64(-1))
		assertField(t, row, "kind", "agent")
	}
}

func TestAgentChargeRecorderJournalsBeforeFailedFlush(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	endpoint := server.URL
	server.Close()
	client, err := ledger.New(&config.Config{
		ClickHouseHost:     "fixture.invalid",
		ClickHousePort:     8443,
		ClickHouseUser:     "fixture",
		ClickHousePassword: "fixture",
		ClickHouseDatabase: "fixture",
	}, t.TempDir(), ledger.WithEndpoint(endpoint))
	if err != nil {
		t.Fatalf("new ledger client: %v", err)
	}
	defer client.Close()

	recorder := newAgentChargeRecorder(client, "gemini-3.8-flash")
	err = recorder.RecordAgentTurn(context.Background(), api.AgentChargeRecord{
		TurnID: "turn-outage",
		DubID:  "dub-agent",
		Charges: []cost.Charge{{
			Kind: cost.ChargeAgent, PromptTokens: 1200, CandidateTokens: 340,
			PromptUnitPrice: 150, CandidateUnitPrice: 600,
		}},
	})
	if err == nil {
		t.Fatal("RecordAgentTurn during outage = nil, want delivery error")
	}
	if pending, pendingErr := client.Pending(); pendingErr != nil || pending != 2 {
		t.Fatalf("Pending after outage = %d, %v, want 2, nil", pending, pendingErr)
	}
}

// A runner without its Google Cloud dependency must stay a nil interface,
// because a typed nil would answer 500 instead of the 503 the routes expect.
func TestNewPipelineRunnerReturnsNilWithoutDependencies(t *testing.T) {
	runner, err := newPipelineRunner(nil, cost.DefaultRateCard())
	if err != nil || runner != nil {
		t.Fatalf("newPipelineRunner(nil) = %T, %v, want nil and no error", runner, err)
	}
	runner, err = newPipelineRunner(&config.Config{}, cost.DefaultRateCard())
	if err != nil || runner != nil {
		t.Fatalf("newPipelineRunner(no project) = %T, %v, want nil and no error", runner, err)
	}
}

// TestPipelineRunResultMapsLines proves the adapter maps takes, voices,
// repairs, charges, flagged lines, and timeline snapshots.
func TestPipelineRunResultMapsLines(t *testing.T) {
	first := types.Segment{ID: 1, StartMs: 0, EndMs: 2000, Text: "hello", Speaker: types.Speaker{Name: "Narrator"}, Emotion: "calm"}
	second := types.Segment{ID: 2, StartMs: 2500, EndMs: 5000, Text: "world", Speaker: types.Speaker{Name: "Narrator"}, Emotion: "warm"}
	firstFit := types.NewFit(2*time.Second, 2*time.Second)
	secondFit := types.NewFit(2500*time.Millisecond, 2600*time.Millisecond)

	result := &fit.PipelineResult{
		Segments: []types.Segment{first, second},
		Lines: []fit.LineResult{
			{
				Segment:    first,
				ChosenTake: types.Take{SegmentID: 1, Attempt: 1, File: "seg_1_stretched.wav", Duration: 2 * time.Second, Fit: firstFit},
				Attempts: []fit.LineAttempt{
					{Attempt: 1, Text: "നമസ്കാരം", Repair: types.RepairAtempo, RepairDetail: "atempo 1.075", Fit: firstFit},
				},
			},
			{
				Segment:    second,
				Flagged:    true,
				ChosenTake: types.Take{SegmentID: 2, Attempt: 2, File: "seg_2_try2.wav", Duration: 2600 * time.Millisecond, Fit: secondFit},
				Attempts: []fit.LineAttempt{
					{Attempt: 1, Text: "ലോകം", Repair: types.RepairNone},
					{Attempt: 2, Text: "ലോകം വിശാലം", Repair: types.RepairRewrite, RepairDetail: "shorter line", Fit: secondFit},
				},
			},
		},
		FlaggedSegments: []int{2},
		Voices:          map[string]tts.Voice{"Narrator": {Name: "ml-IN-Chirp3-HD-Achernar"}},
		TotalCost:       1234,
		AttemptCharges: []cost.AttemptCharge{
			{Attempt: 1, Charge: cost.Charge{Kind: cost.ChargeTranslate, TakeID: 1, PromptTokens: 10, CandidateTokens: 5, PromptUnitPrice: 150, CandidateUnitPrice: 600}},
			{Attempt: 1, Charge: cost.Charge{Kind: cost.ChargeSynthesize, TakeID: 1, Units: 20, UnitPrice: 30_000}},
			{Attempt: 2, Charge: cost.Charge{Kind: cost.ChargeTranslate, TakeID: 2, PromptTokens: 11, CandidateTokens: 6, PromptUnitPrice: 150, CandidateUnitPrice: 600}},
			{Attempt: 2, Charge: cost.Charge{Kind: cost.ChargeSynthesize, TakeID: 2, Units: 21, UnitPrice: 30_000}},
			{Charge: cost.Charge{Kind: cost.ChargeSegment, TakeID: 0, PromptTokens: 100, CandidateTokens: 50, PromptUnitPrice: 150, CandidateUnitPrice: 600}},
		},
	}
	peaks := peakReader(func(_ context.Context, path string) ([]uint8, error) {
		return []uint8{uint8(len(path))}, nil
	})

	got, err := pipelineRunResult(context.Background(), result, peaks)
	if err != nil {
		t.Fatalf("pipelineRunResult: %v", err)
	}
	if len(got.Takes) != 2 || len(got.Timeline) != 2 {
		t.Fatalf("mapped %d takes and %d snapshots, want 2 and 2", len(got.Takes), len(got.Timeline))
	}
	if got.TotalCost != 1234 {
		t.Errorf("total cost = %d, want 1234", got.TotalCost)
	}
	if len(got.FlaggedSegments) != 1 || got.FlaggedSegments[0] != 2 {
		t.Errorf("flagged segments = %v, want [2]", got.FlaggedSegments)
	}
	if got.Takes[0].Voice != "ml-IN-Chirp3-HD-Achernar" {
		t.Errorf("first voice = %q", got.Takes[0].Voice)
	}
	if got.Takes[0].Repair != types.RepairAtempo || got.Takes[0].RepairDetail != "atempo 1.075" {
		t.Errorf("first repair = %v %q", got.Takes[0].Repair, got.Takes[0].RepairDetail)
	}
	if len(got.Takes[0].Charges) != 2 {
		t.Errorf("first take charges = %d, want 2", len(got.Takes[0].Charges))
	}
	if got.Takes[1].Repair != types.RepairRewrite {
		t.Errorf("second repair = %v, want rewrite", got.Takes[1].Repair)
	}
	if len(got.Takes[1].Peaks) != 1 {
		t.Errorf("second take peaks = %v, want the stub sketch", got.Takes[1].Peaks)
	}
	if len(got.WholePassCharges) != 1 || got.WholePassCharges[0].Kind != cost.ChargeSegment {
		t.Errorf("whole-pass charges = %+v, want one segment charge", got.WholePassCharges)
	}
	if got.Timeline[1].Text != "ലോകം വിശാലം" {
		t.Errorf("second timeline text = %q", got.Timeline[1].Text)
	}
	if got.Timeline[0].TakeID != "" {
		t.Errorf("timeline take id = %q, want the api package to mint it", got.Timeline[0].TakeID)
	}
}

// TestPipelineRunResultReportsPeakFailure proves a failed sketch fails the run.
func TestPipelineRunResultReportsPeakFailure(t *testing.T) {
	segment := types.Segment{ID: 1, StartMs: 0, EndMs: 2000, Text: "hello", Speaker: types.Speaker{Name: "Narrator"}}
	fitValue := types.NewFit(2*time.Second, 2*time.Second)
	result := &fit.PipelineResult{
		Lines: []fit.LineResult{{
			Segment:    segment,
			ChosenTake: types.Take{SegmentID: 1, Attempt: 1, File: "seg_1.wav", Duration: 2 * time.Second, Fit: fitValue},
			Attempts:   []fit.LineAttempt{{Attempt: 1, Text: "നമസ്കാരം"}},
		}},
	}
	peaks := peakReader(func(context.Context, string) ([]uint8, error) {
		return nil, errors.New("probe failed")
	})
	if _, err := pipelineRunResult(context.Background(), result, peaks); err == nil {
		t.Fatal("pipelineRunResult ignored a failed peak sketch")
	}
}

// TestRunRecorderPersistWritesFreshRun proves Persist writes the commit, action,
// take, charges, and snapshot for a first run in ledger order.
func TestRunRecorderPersistWritesFreshRun(t *testing.T) {
	client, captured := standInClickHouse(t, "")
	defer client.Close()
	recorder := newRunRecorder(client, "gemini-3.8-flash", slog.New(slog.NewTextHandler(io.Discard, nil)))

	request, result := persistFixture()
	if err := recorder.Persist(context.Background(), request, result); err != nil {
		t.Fatalf("Persist: %v", err)
	}

	commit := firstInsert(t, captured(), "commits_raw")
	assertField(t, commit, "commit_id", result.CommitID)
	assertField(t, commit, "parent_commit_id", "")
	assertField(t, commit, "version_seq", float64(1))
	assertField(t, commit, "dub_id", request.DubID)
	assertField(t, commit, "language", request.Language)
	if message, _ := commit["message"].(string); !strings.Contains(message, "Rendered 1 lines") {
		t.Errorf("commit message = %q", message)
	}

	action := firstInsert(t, captured(), "actions_raw")
	assertField(t, action, "commit_id", result.CommitID)
	assertField(t, action, "action_type", api.ActionTakeRendered)
	assertField(t, action, "author", api.AuthorAgent)
	assertField(t, action, "segment_index", float64(-1))

	take := firstInsert(t, captured(), "takes_raw")
	assertField(t, take, "take_id", result.Takes[0].TakeID)
	assertField(t, take, "text", "നമസ്കാരം")
	assertField(t, take, "commit_id", result.CommitID)

	charges := insertRows(t, captured(), "charges_raw")
	if len(charges) != 5 {
		t.Fatalf("charge rows = %d, want 5 itemized rows", len(charges))
	}
	wholePass := 0
	for _, charge := range charges {
		assertField(t, charge, "commit_id", result.CommitID)
		if charge["kind"] == cost.ChargeSegment.String() {
			wholePass++
			assertField(t, charge, "segment_index", float64(result.Takes[0].Segment.ID))
		}
	}
	if wholePass != 2 {
		t.Fatalf("whole-pass charge rows = %d, want 2", wholePass)
	}

	snapshot := firstInsert(t, captured(), "timeline_state_raw")
	assertField(t, snapshot, "commit_id", result.CommitID)
	assertField(t, snapshot, "version_seq", float64(1))
	assertField(t, snapshot, "take_id", result.Takes[0].TakeID)

	historyIndex := requestIndex(t, captured(), http.MethodPost, "has_action")
	commitIndex := insertIndex(t, captured(), "commits_raw")
	if historyIndex < 0 {
		t.Fatal("Persist never read the dub head before appending the commit")
	}
	if commitIndex < 0 || commitIndex < historyIndex {
		t.Fatalf("commit insert at %d, head read at %d, want the head read first", commitIndex, historyIndex)
	}
}

// TestRunRecorderPersistContinuesFromHead proves Persist names the head as the
// parent and advances the version sequence.
func TestRunRecorderPersistContinuesFromHead(t *testing.T) {
	const head = `{"commit_id":"c1","parent_commit_id":"","version_seq":1,"created_at_ms":1,"has_action":0,"action_type":"","author":"","prompt":"","action_created_at_ms":0,"event_key":""}`
	client, captured := standInClickHouse(t, head)
	defer client.Close()
	recorder := newRunRecorder(client, "gemini-3.8-flash", slog.New(slog.NewTextHandler(io.Discard, nil)))

	request, result := persistFixture()
	if err := recorder.Persist(context.Background(), request, result); err != nil {
		t.Fatalf("Persist: %v", err)
	}

	commit := firstInsert(t, captured(), "commits_raw")
	assertField(t, commit, "parent_commit_id", "c1")
	assertField(t, commit, "version_seq", float64(2))
	snapshot := firstInsert(t, captured(), "timeline_state_raw")
	assertField(t, snapshot, "version_seq", float64(2))
}

// persistFixture builds one valid run for the recorder tests.
func persistFixture() (api.RunRequest, api.RunResult) {
	segment := types.Segment{ID: 1, StartMs: 0, EndMs: 2000, Text: "hello", Speaker: types.Speaker{Name: "Narrator"}, Emotion: "calm"}
	fitValue := types.NewFit(2*time.Second, 2*time.Second)
	peaks := make([]uint8, 64)
	for i := range peaks {
		peaks[i] = uint8(i * 4)
	}
	request := api.RunRequest{DubID: "dub-fresh", Language: "ml", Source: "/source.mp4", WorkDir: "/work"}
	result := api.RunResult{
		CommitID:  "run-commit-1",
		ProjectID: "dub-fresh",
		OwnerID:   "local",
		Takes: []api.RunTake{{
			TakeID:  "take-1",
			Text:    "നമസ്കാരം",
			Segment: segment,
			Take:    types.Take{SegmentID: 1, Attempt: 1, File: "/work/seg_1.wav", Duration: 2 * time.Second, Fit: fitValue},
			Voice:   "ml-IN-Chirp3-HD-Achernar",
			Repair:  types.RepairAtempo,
			Charges: []cost.Charge{
				{Kind: cost.ChargeTranslate, TakeID: 1, PromptTokens: 10, CandidateTokens: 5, PromptUnitPrice: 150, CandidateUnitPrice: 600},
				{Kind: cost.ChargeSynthesize, TakeID: 1, Units: 20, UnitPrice: 30_000},
			},
			Peaks: peaks,
		}},
		Timeline: []api.RunSegmentState{{
			SegmentIndex: 1,
			StartMs:      0,
			EndMs:        2000,
			Speaker:      "Narrator",
			Emotion:      "calm",
			SourceText:   "hello",
			Text:         "നമസ്കാരം",
			TakeID:       "take-1",
		}},
		TotalCost: 900,
		WholePassCharges: []cost.Charge{
			{Kind: cost.ChargeSegment, TakeID: 0, PromptTokens: 100, CandidateTokens: 50, PromptUnitPrice: 150, CandidateUnitPrice: 600},
		},
	}
	return request, result
}

// capturedRequest is one HTTP request the stand-in ClickHouse received.
// Reads and inserts both arrive as POST, so insert marks the insert statements.
type capturedRequest struct {
	method string
	query  string
	insert bool
	rows   []map[string]any
}

// standInClickHouse serves the queries Persist issues. head, when non-empty,
// answers the commit history read and the parent lookup for commit c1.
func standInClickHouse(t *testing.T, head string) (*ledger.Client, func() []capturedRequest) {
	t.Helper()
	var mu sync.Mutex
	var captured []capturedRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("query")
		record := capturedRequest{method: r.Method, query: query, insert: strings.HasPrefix(strings.TrimSpace(query), "INSERT ")}
		if r.Method == http.MethodPost {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Errorf("read insert body: %v", err)
				http.Error(w, "read body", http.StatusInternalServerError)
				return
			}
			for _, line := range strings.Split(strings.TrimSpace(string(body)), "\n") {
				if line == "" {
					continue
				}
				var row map[string]any
				if err := json.Unmarshal([]byte(line), &row); err != nil {
					t.Errorf("decode row %q: %v", line, err)
					http.Error(w, "decode row", http.StatusBadRequest)
					return
				}
				record.rows = append(record.rows, row)
			}
		}
		mu.Lock()
		captured = append(captured, record)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
		switch {
		case head == "":
		case strings.Contains(query, "has_action"):
			_, _ = io.WriteString(w, head+"\n")
		case strings.Contains(query, "FROM commits_raw FINAL WHERE") &&
			r.URL.Query().Get("param_commit_id") == "c1":
			_, _ = io.WriteString(w, head+"\n")
		}
	}))
	t.Cleanup(server.Close)

	client, err := ledger.New(&config.Config{
		ClickHouseHost:     "fixture.invalid",
		ClickHousePort:     8443,
		ClickHouseUser:     "fixture",
		ClickHousePassword: "fixture",
		ClickHouseDatabase: "fixture",
	}, t.TempDir(), ledger.WithEndpoint(server.URL))
	if err != nil {
		t.Fatalf("new ledger client: %v", err)
	}
	return client, func() []capturedRequest {
		mu.Lock()
		defer mu.Unlock()
		return append([]capturedRequest(nil), captured...)
	}
}

// insertRows returns every row written to one table.
func insertRows(t *testing.T, requests []capturedRequest, table string) []map[string]any {
	t.Helper()
	var rows []map[string]any
	for _, request := range requests {
		if request.insert && strings.Contains(request.query, table) {
			rows = append(rows, request.rows...)
		}
	}
	return rows
}

// firstInsert returns the first row written to one table.
func firstInsert(t *testing.T, requests []capturedRequest, table string) map[string]any {
	t.Helper()
	rows := insertRows(t, requests, table)
	if len(rows) == 0 {
		t.Fatalf("no row written to %s", table)
	}
	return rows[0]
}

// requestIndex returns the first request index matching a method and query fragment.
func requestIndex(t *testing.T, requests []capturedRequest, method, fragment string) int {
	t.Helper()
	for i, request := range requests {
		if request.method == method && strings.Contains(request.query, fragment) {
			return i
		}
	}
	return -1
}

// insertIndex returns the first insert request index for one table.
func insertIndex(t *testing.T, requests []capturedRequest, table string) int {
	t.Helper()
	for i, request := range requests {
		if request.insert && strings.Contains(request.query, table) {
			return i
		}
	}
	return -1
}

// assertField compares one decoded JSON field.
func assertField(t *testing.T, row map[string]any, name string, want any) {
	t.Helper()
	if got := row[name]; got != want {
		t.Errorf("row field %s = %#v, want %#v", name, got, want)
	}
}

// synthAssemblyFilm renders a short film with one video and one audio stream.
func synthAssemblyFilm(t *testing.T, path string) {
	t.Helper()
	cmd := exec.Command("ffmpeg", "-v", "error",
		"-f", "lavfi", "-i", "color=black:s=64x64:r=30:d=2",
		"-f", "lavfi", "-i", "sine=frequency=220:sample_rate=44100:duration=2",
		"-map", "0:v", "-map", "1:a", "-ac", "2", "-ar", "44100",
		"-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("render assembly film: %v: %s", err, out)
	}
}

// synthAssemblyTake renders one short voiced take.
func synthAssemblyTake(t *testing.T, path string) {
	t.Helper()
	cmd := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i",
		"sine=frequency=440:sample_rate=44100:duration=1", "-c:a", "pcm_s16le", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("render assembly take: %v: %s", err, out)
	}
}

// TestAssembleEventsCarryRunCost proves every assembly and export event carries
// the cumulative run cost, so a reconnect during assembly resumes at that cost.
func TestAssembleEventsCarryRunCost(t *testing.T) {
	workDir := t.TempDir()
	source := filepath.Join(workDir, "source.mp4")
	synthAssemblyFilm(t, source)
	take := filepath.Join(workDir, "take.wav")
	synthAssemblyTake(t, take)

	request := api.RunRequest{DubID: "dub-cost", Language: "ml", Source: source, WorkDir: workDir}
	result := &fit.PipelineResult{
		Lines: []fit.LineResult{{
			Segment:    types.Segment{ID: 1, StartMs: 0, EndMs: 1000},
			ChosenTake: types.Take{SegmentID: 1, Attempt: 1, File: take, Duration: time.Second},
		}},
		TotalCost: 900,
	}

	var events []api.ProgressEvent
	if err := assembleRun(context.Background(), request, result, func(event api.ProgressEvent) {
		events = append(events, event)
	}); err != nil {
		t.Fatalf("assembleRun: %v", err)
	}
	if len(events) != 4 {
		t.Fatalf("assembly events = %d, want 4", len(events))
	}
	for i, event := range events {
		if event.TotalNanodollars != 900 {
			t.Errorf("event %d %q cost = %d, want 900", i, event.Sentence, event.TotalNanodollars)
		}
	}
	last := events[len(events)-1]
	if last.Stage != api.StageExporting || last.Sentence != "Exporting the dubbed film." {
		t.Errorf("last event = %q %q, want the export step", last.Stage, last.Sentence)
	}
}

// runnerTestSegmenter returns fixed segments and counts its calls.
type runnerTestSegmenter struct {
	segments []types.Segment
	calls    int
}

// Segment returns the fixed segments and counts the call.
func (s *runnerTestSegmenter) Segment(context.Context, gemini.Input) ([]types.Segment, error) {
	s.calls++
	return s.segments, nil
}

// runnerTestTranslator records the requests it sees and counts its calls.
type runnerTestTranslator struct {
	requests []gemini.TranslateRequest
	calls    int
}

// Translate records the request, counts the call, and echoes the source text.
func (t *runnerTestTranslator) Translate(_ context.Context, req gemini.TranslateRequest) (string, error) {
	t.calls++
	t.requests = append(t.requests, req)
	return req.Text, nil
}

// runnerTestTTSClient records the voice Cloud TTS receives and counts its calls.
type runnerTestTTSClient struct {
	audio []byte
	voice *texttospeechpb.VoiceSelectionParams
	calls int
}

// SynthesizeSpeech records the requested voice and returns canned audio.
func (c *runnerTestTTSClient) SynthesizeSpeech(_ context.Context, req *texttospeechpb.SynthesizeSpeechRequest) (*texttospeechpb.SynthesizeSpeechResponse, error) {
	c.calls++
	c.voice = req.Voice
	return &texttospeechpb.SynthesizeSpeechResponse{AudioContent: c.audio}, nil
}

// TestPipelineRunnerLanguageWiring proves the runner resolves each target
// language onto its tag, builds the synthesizer with that tag, and carries the
// tag to Cloud TTS and the display names to the translator. One case uses the
// create screen code `ml`, whose tag `ml-IN` a regression could hardcode. One
// uses the catalog code `ml-IN`, which must name Malayalam. One uses `es-ES`,
// whose tag differs from that constant.
func TestPipelineRunnerLanguageWiring(t *testing.T) {
	cases := []struct {
		name       string
		language   string
		tag        string
		target     string
		source     string
		sourceName string
		voiceName  string
	}{
		{name: "create screen code", language: "ml", tag: "ml-IN", target: "Malayalam", source: "en-US", sourceName: "English", voiceName: "ml-IN-Chirp3-HD-Achernar"},
		{name: "catalog code", language: "ml-IN", tag: "ml-IN", target: "Malayalam", source: "en-US", sourceName: "English", voiceName: "ml-IN-Chirp3-HD-Achernar"},
		{name: "full tag", language: "es-ES", tag: "es-ES", target: "Spanish", source: "en-US", sourceName: "English", voiceName: "es-ES-Chirp3-HD-Achernar"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			workDir := t.TempDir()
			source := filepath.Join(workDir, "source.mp4")
			synthAssemblyFilm(t, source)
			canned := filepath.Join(workDir, "canned.wav")
			synthAssemblyTake(t, canned)
			audio, err := os.ReadFile(canned)
			if err != nil {
				t.Fatalf("read canned take: %v", err)
			}

			segment := types.Segment{
				ID:      1,
				StartMs: 0,
				EndMs:   1000,
				Text:    "Hello from orbit",
				Speaker: types.Speaker{Name: "Suni Williams"},
			}
			segmenter := &runnerTestSegmenter{segments: []types.Segment{segment}}
			translator := &runnerTestTranslator{}
			client := &runnerTestTTSClient{audio: audio}

			var factoryLanguage string
			runner := &pipelineRunner{
				segmenter:  segmenter,
				translator: translator,
				router:     &chargeRouter{},
				newSynthesizer: func(language string, rec tts.ChargeRecorder) (tts.Synthesizer, error) {
					factoryLanguage = language
					return tts.NewSynthesizer(&config.Config{GoogleCloudProject: "test-project"}, language, rec, cost.DefaultRateCard(), client)
				},
			}

			var events []api.ProgressEvent
			_, err = runner.Run(context.Background(), api.RunRequest{
				DubID:          "dub-language",
				Language:       tc.language,
				SourceLanguage: tc.source,
				Source:         source,
				WorkDir:        workDir,
			}, func(event api.ProgressEvent) { events = append(events, event) })
			if err != nil {
				t.Fatalf("Run: %v", err)
			}

			t.Logf("factory language = %q", factoryLanguage)
			if factoryLanguage != tc.tag {
				t.Errorf("factory language = %q, want %s", factoryLanguage, tc.tag)
			}
			if len(translator.requests) == 0 {
				t.Fatal("translator saw no request")
			}
			if got := translator.requests[0].TargetLanguageName; got != tc.target {
				t.Errorf("TranslateRequest.TargetLanguageName = %q, want %s", got, tc.target)
			}
			if got := translator.requests[0].SourceLanguageName; got != tc.sourceName {
				t.Errorf("TranslateRequest.SourceLanguageName = %q, want %s", got, tc.sourceName)
			}
			if client.voice == nil {
				t.Fatal("synthesizer sent no voice to Cloud TTS")
			}
			t.Logf("Cloud TTS LanguageCode = %q, Name = %q", client.voice.LanguageCode, client.voice.Name)
			if got := client.voice.LanguageCode; got != tc.tag {
				t.Errorf("Cloud TTS LanguageCode = %q, want %s", got, tc.tag)
			}
			if got := client.voice.Name; got != tc.voiceName {
				t.Errorf("Cloud TTS Name = %q, want %s", got, tc.voiceName)
			}
			if len(events) == 0 {
				t.Fatal("runner emitted no events")
			}
			for _, event := range events {
				if event.Language != tc.tag {
					t.Errorf("event %q language = %q, want %s", event.Sentence, event.Language, tc.tag)
				}
			}
		})
	}
}

// TestLanguageNamesCoverCatalog proves every committed catalog code resolves
// to a display name rather than to its own code. A code that falls back to
// itself reaches the translation prompt as a code.
func TestLanguageNamesCoverCatalog(t *testing.T) {
	for _, code := range tts.CommittedLanguages() {
		name := resolveLanguageName(code)
		if name == "" || name == code {
			t.Errorf("resolveLanguageName(%q) = %q, want a display name", code, name)
		}
	}
	cases := []struct {
		code string
		name string
	}{
		{code: "ml-IN", name: "Malayalam"},
		{code: "ml", name: "Malayalam"},
		{code: "en", name: "English"},
		{code: "en-US", name: "English"},
	}
	for _, tc := range cases {
		if got := resolveLanguageName(tc.code); got != tc.name {
			t.Errorf("resolveLanguageName(%q) = %q, want %q", tc.code, got, tc.name)
		}
	}
	if got := resolveLanguageName(""); got != "" {
		t.Errorf("resolveLanguageName(\"\") = %q, want empty", got)
	}
	if known, err := resolveRunLanguage("ml-IN"); err != nil || known != (runLanguage{tag: "ml-IN", name: "Malayalam"}) {
		t.Errorf("resolveRunLanguage(ml-IN) = %+v, %v, want the Malayalam tag and name", known, err)
	}
}

// TestPipelineRunnerRejectsLanguageBeforeCharge proves an empty or malformed
// target language fails in resolveRunLanguage, before any billable call.
func TestPipelineRunnerRejectsLanguageBeforeCharge(t *testing.T) {
	cases := []struct {
		name     string
		language string
	}{
		{name: "empty", language: ""},
		{name: "malformed", language: "not a language"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			segmenter := &runnerTestSegmenter{}
			translator := &runnerTestTranslator{}
			client := &runnerTestTTSClient{}
			factoryCalls := 0
			runner := &pipelineRunner{
				segmenter:  segmenter,
				translator: translator,
				router:     &chargeRouter{},
				newSynthesizer: func(language string, rec tts.ChargeRecorder) (tts.Synthesizer, error) {
					factoryCalls++
					return tts.NewSynthesizer(&config.Config{GoogleCloudProject: "test-project"}, language, rec, cost.DefaultRateCard(), client)
				},
			}

			var events []api.ProgressEvent
			_, err := runner.Run(context.Background(), api.RunRequest{
				DubID:    "dub-bad-language",
				Language: tc.language,
				Source:   filepath.Join(t.TempDir(), "missing.mp4"),
				WorkDir:  t.TempDir(),
			}, func(event api.ProgressEvent) { events = append(events, event) })
			t.Logf("error = %v", err)
			if err == nil {
				t.Fatal("Run accepted a bad target language")
			}
			if factoryCalls != 0 || segmenter.calls != 0 || translator.calls != 0 || client.calls != 0 {
				t.Errorf("billable calls: factory=%d segmenter=%d translator=%d tts=%d, want 0",
					factoryCalls, segmenter.calls, translator.calls, client.calls)
			}
			if len(events) != 0 {
				t.Errorf("events = %d, want 0", len(events))
			}
		})
	}
}

// TestRunSynthesizerFactoryForwardsLanguage proves the production factory
// forwards the language it receives onto Cloud TTS. The test passes a fake
// client, so no ADC and no network are needed. The es-ES case fails a factory
// that hardcodes the ml-IN create screen tag.
func TestRunSynthesizerFactoryForwardsLanguage(t *testing.T) {
	cases := []struct {
		name      string
		language  string
		voiceName string
	}{
		{name: "create screen tag", language: "ml-IN", voiceName: "ml-IN-Chirp3-HD-Achernar"},
		{name: "full tag", language: "es-ES", voiceName: "es-ES-Chirp3-HD-Achernar"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			workDir := t.TempDir()
			canned := filepath.Join(workDir, "canned.wav")
			synthAssemblyTake(t, canned)
			audio, err := os.ReadFile(canned)
			if err != nil {
				t.Fatalf("read canned take: %v", err)
			}
			client := &runnerTestTTSClient{audio: audio}
			build := runSynthesizerFactory(&config.Config{GoogleCloudProject: "test-project"}, cost.DefaultRateCard(), client)
			synthesizer, err := build(tc.language, cost.NewLedger())
			if err != nil {
				t.Fatalf("build synthesizer: %v", err)
			}
			if err := synthesizer.Synthesize(context.Background(), tts.SynthesizeRequest{
				SegmentID: 1,
				Text:      "Hello from orbit",
				Speaker:   types.Speaker{Name: "Suni Williams"},
				OutPath:   filepath.Join(workDir, "take.wav"),
			}); err != nil {
				t.Fatalf("Synthesize: %v", err)
			}
			if client.voice == nil {
				t.Fatal("synthesizer sent no voice to Cloud TTS")
			}
			t.Logf("Cloud TTS LanguageCode = %q, Name = %q", client.voice.LanguageCode, client.voice.Name)
			if got := client.voice.LanguageCode; got != tc.language {
				t.Errorf("Cloud TTS LanguageCode = %q, want %s", got, tc.language)
			}
			if got := client.voice.Name; got != tc.voiceName {
				t.Errorf("Cloud TTS Name = %q, want %s", got, tc.voiceName)
			}
		})
	}
}

// wavOfMillis returns a silent mono 16 kHz 16-bit WAV of the requested
// length. The fit loop measures it with ffprobe, so the header must be real.
func wavOfMillis(ms int) []byte {
	const (
		sampleRate = 16000
		channels   = 1
		bitsPer    = 16
	)
	audio := make([]byte, sampleRate*ms/1000*channels*bitsPer/8)
	wav := make([]byte, 44+len(audio))
	copy(wav, "RIFF")
	binary.LittleEndian.PutUint32(wav[4:], uint32(36+len(audio)))
	copy(wav[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(wav[16:], 16)
	binary.LittleEndian.PutUint16(wav[20:], 1)
	binary.LittleEndian.PutUint16(wav[22:], channels)
	binary.LittleEndian.PutUint32(wav[24:], sampleRate)
	binary.LittleEndian.PutUint32(wav[28:], sampleRate*channels*bitsPer/8)
	binary.LittleEndian.PutUint16(wav[32:], channels*bitsPer/8)
	binary.LittleEndian.PutUint16(wav[34:], bitsPer)
	copy(wav[36:], "data")
	binary.LittleEndian.PutUint32(wav[40:], uint32(len(audio)))
	return wav
}

// TestRenderLineCorrectedTargetMissNeverTranslates drives the production
// adapter, not a copy of its decision. A corrected target line misses every
// attempt. RenderLine must set AuthoritativeText, so the loop calls no
// translator and the chosen take keeps the creator's text.
func TestRenderLineCorrectedTargetMissNeverTranslates(t *testing.T) {
	workDir := t.TempDir()
	translator := &runnerTestTranslator{}
	client := &runnerTestTTSClient{audio: wavOfMillis(10640)}
	runner := &pipelineRunner{
		translator:     translator,
		router:         &chargeRouter{},
		newSynthesizer: runSynthesizerFactory(&config.Config{GoogleCloudProject: "test-project"}, cost.DefaultRateCard(), client),
	}
	segment := types.Segment{
		ID:      3,
		StartMs: 13208,
		EndMs:   18528,
		Text:    "source line",
		Speaker: types.Speaker{Name: "Suni Williams"},
	}

	result, err := runner.RenderLine(context.Background(), api.LineRenderRequest{
		DubID:          "dub-render-line",
		Language:       "ml",
		SourceLanguage: "en-US",
		Segment:        segment,
		Text:           "corrected target line",
		WorkDir:        workDir,
		TakeFile:       filepath.Join(workDir, "seg_3_try2.wav"),
	})
	if err != nil {
		t.Fatalf("RenderLine: %v", err)
	}
	t.Logf("translation calls = %d, synthesis calls = %d, spoken text = %q, flagged = %v",
		translator.calls, client.calls, result.Text, result.Flagged)

	if translator.calls != 0 {
		t.Errorf("translation calls = %d, want 0 on the corrected target path", translator.calls)
	}
	if client.calls != fit.DefaultMaxAttempts {
		t.Errorf("synthesis calls = %d, want %d", client.calls, fit.DefaultMaxAttempts)
	}
	if result.Text != "corrected target line" {
		t.Errorf("spoken text = %q, want the creator's corrected target line", result.Text)
	}
	if !result.Flagged {
		t.Error("flagged = false, want a flagged line")
	}
}

// lockRaceCommit is one commits_raw row the stand-in store holds. The action
// fields stay zero, so the ledger reader collapses each commit to one row.
type lockRaceCommit struct {
	CommitID          string `json:"commit_id"`
	ParentCommitID    string `json:"parent_commit_id"`
	VersionSeq        uint64 `json:"version_seq"`
	CreatedAtMs       int64  `json:"created_at_ms"`
	HasAction         uint8  `json:"has_action"`
	ActionType        string `json:"action_type"`
	Author            string `json:"author"`
	Prompt            string `json:"prompt"`
	ActionCreatedAtMs int64  `json:"action_created_at_ms"`
	EventKey          string `json:"event_key"`
}

// lockRaceStore is a stateful stand-in ClickHouse for the shared-lock pin. It
// holds the commits the two real writers append and answers the head read, the
// timeline read and the identity and parent lookups from that set.
type lockRaceStore struct {
	mu       sync.Mutex
	commits  []lockRaceCommit
	requests int
}

// noteRequest counts one request the stand-in served.
func (s *lockRaceStore) noteRequest() {
	s.mu.Lock()
	s.requests++
	s.mu.Unlock()
}

// requestCount returns how many requests the stand-in served.
func (s *lockRaceStore) requestCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.requests
}

// add records one appended commit.
func (s *lockRaceStore) add(commit lockRaceCommit) {
	commit.CreatedAtMs = 1788825600000 + int64(commit.VersionSeq)
	s.mu.Lock()
	s.commits = append(s.commits, commit)
	s.mu.Unlock()
}

// rows returns the commits in the order the ledger reader expects, oldest
// first, with the greatest (version_seq, commit_id) last.
func (s *lockRaceStore) rows() []lockRaceCommit {
	s.mu.Lock()
	rows := append([]lockRaceCommit(nil), s.commits...)
	s.mu.Unlock()
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].VersionSeq != rows[j].VersionSeq {
			return rows[i].VersionSeq < rows[j].VersionSeq
		}
		return rows[i].CommitID < rows[j].CommitID
	})
	return rows
}

// newLockRaceStore seeds one root commit and serves the queries the run and
// edit writers issue. Unknown queries answer empty, which is what the run
// recorder's take, action and snapshot inserts need.
func newLockRaceStore(t *testing.T, dubID string) (*lockRaceStore, *httptest.Server) {
	t.Helper()
	store := &lockRaceStore{commits: []lockRaceCommit{{
		CommitID:    dubID + "-root",
		VersionSeq:  1,
		CreatedAtMs: 1788825600000,
	}}}
	timeline := `{"segment_index":1,"start_ms":0,"end_ms":3000,"speaker":"Mark","emotion":"Warm","source_text":"one","text":"onnu","take_id":"t1","state_version_seq":1}` + "\n" +
		`{"segment_index":2,"start_ms":3500,"end_ms":8000,"speaker":"Suni","emotion":"Calm","source_text":"two","text":"randu","take_id":"t2","state_version_seq":1}` + "\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		store.noteRequest()
		query := strings.TrimSpace(r.URL.Query().Get("query"))
		if strings.HasPrefix(query, "INSERT INTO commits_raw") {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, "read body", http.StatusInternalServerError)
				return
			}
			for _, line := range strings.Split(strings.TrimSpace(string(body)), "\n") {
				if line == "" {
					continue
				}
				var commit lockRaceCommit
				if err := json.Unmarshal([]byte(line), &commit); err != nil {
					t.Errorf("decode commit row %q: %v", line, err)
					http.Error(w, "decode row", http.StatusBadRequest)
					return
				}
				store.add(commit)
			}
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusOK)
		switch {
		case strings.Contains(query, "timeline_at_commit"):
			_, _ = io.WriteString(w, timeline)
		case strings.Contains(query, "has_action"):
			for _, row := range store.rows() {
				payload, err := json.Marshal(row)
				if err != nil {
					t.Errorf("encode commit row: %v", err)
					return
				}
				_, _ = w.Write(append(payload, '\n'))
			}
		case strings.Contains(query, "FROM commits_raw FINAL WHERE"):
			want := r.URL.Query().Get("param_commit_id")
			for _, row := range store.rows() {
				if row.CommitID != want {
					continue
				}
				payload, err := json.Marshal(row)
				if err != nil {
					t.Errorf("encode commit lookup: %v", err)
					return
				}
				_, _ = w.Write(append(payload, '\n'))
				break
			}
		}
	}))
	t.Cleanup(server.Close)
	return store, server
}

// newLockRaceClient points one durable ledger client at the stand-in store.
func newLockRaceClient(t *testing.T, endpoint string) *ledger.Client {
	t.Helper()
	client, err := ledger.New(&config.Config{
		ClickHouseHost:     "fixture.invalid",
		ClickHousePort:     8443,
		ClickHouseUser:     "fixture",
		ClickHousePassword: "fixture",
		ClickHouseDatabase: "fixture",
	}, t.TempDir(), ledger.WithEndpoint(endpoint))
	if err != nil {
		t.Fatalf("new ledger client: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

// TestRunPersistAndEditShareDubCommitLock proves the real run commit path and
// the real edit path take one shared per-dub commit lock. The test holds that
// lock, starts runRecorder.Persist and an edit POST together, and proves
// neither reaches the ledger while the lock is held. It releases the lock and
// proves both commits chain. Removing the lock from persist or from
// EditsHandler reddens it.
func TestRunPersistAndEditShareDubCommitLock(t *testing.T) {
	const dubID = "dub-fresh"
	store, ledgerServer := newLockRaceStore(t, dubID)
	runClient := newLockRaceClient(t, ledgerServer.URL)
	editClient := newLockRaceClient(t, ledgerServer.URL)
	runRecorder := newRunRecorder(runClient, "gemini-3.8-flash", slog.New(slog.NewTextHandler(io.Discard, nil)))
	server, err := api.NewServer(&config.Config{Env: "development"}, api.ServerOptions{
		History:  newHistoryReader(editClient),
		Edits:    newEditRecorder(editClient),
		Recorder: runRecorder,
		Ledger:   editClient,
	})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()
	editURL := httpServer.URL + "/api/dubs/" + dubID + "/edits"

	request, result := persistFixture()
	if request.DubID != dubID {
		t.Fatalf("persist fixture dub = %q, want %q", request.DubID, dubID)
	}
	const editBody = `{"kind":"boundary","language":"ml","segment_id":1,"start_ms":0,"end_ms":2950}`

	unlock := api.LockDubCommit(dubID)
	released := false
	defer func() {
		if !released {
			unlock()
		}
	}()

	before := store.requestCount()
	start := make(chan struct{})
	var wait sync.WaitGroup
	var persistErr error
	var editStatus int
	var editErr error
	wait.Add(2)
	go func() {
		defer wait.Done()
		<-start
		persistErr = runRecorder.Persist(context.Background(), request, result)
	}()
	go func() {
		defer wait.Done()
		<-start
		response, err := http.Post(editURL, "application/json", strings.NewReader(editBody))
		if err != nil {
			editErr = err
			return
		}
		editStatus = response.StatusCode
		response.Body.Close()
	}()
	close(start)

	reached := false
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		if store.requestCount() > before {
			reached = true
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	unlock()
	released = true
	wait.Wait()

	if reached {
		t.Fatal("a writer reached the ledger while the shared dub commit lock was held")
	}
	if editErr != nil {
		t.Fatalf("edit: %v", editErr)
	}
	if editStatus != http.StatusCreated {
		t.Fatalf("edit status = %d, want 201", editStatus)
	}
	if persistErr != nil {
		t.Fatalf("Persist: %v", persistErr)
	}
	rows := store.rows()
	if len(rows) != 3 {
		t.Fatalf("commits = %d, want the root, the edit and the run commit", len(rows))
	}
	versions := make(map[uint64]int, len(rows))
	parent := make(map[string]string, len(rows))
	for _, row := range rows {
		versions[row.VersionSeq]++
		parent[row.CommitID] = row.ParentCommitID
	}
	for version := uint64(1); version <= 3; version++ {
		if versions[version] != 1 {
			t.Errorf("version_seq %d appears %d times, want once", version, versions[version])
		}
	}
	head := rows[len(rows)-1]
	held := 0
	runOnHead := false
	editOnHead := false
	for id := head.CommitID; id != ""; id = parent[id] {
		held++
		switch id {
		case dubID + "-root":
		case result.CommitID:
			runOnHead = true
		default:
			editOnHead = true
		}
	}
	if held != len(rows) {
		t.Errorf("head ancestry = %d commits, want %d", held, len(rows))
	}
	if !runOnHead {
		t.Error("the head chain dropped the run commit")
	}
	if !editOnHead {
		t.Error("the head chain dropped the edit commit")
	}
}

func TestEditorAgentStartupGatesConstruction(t *testing.T) {
	configured := &config.Config{ClickHouseMCPURL: "http://127.0.0.1:8000/mcp", ClickHouseMCPAuthToken: "test"}
	for _, tc := range []struct {
		name      string
		cfg       *config.Config
		failure   bool
		wantCalls int
	}{
		{"absent", &config.Config{}, false, 0},
		{"partial", &config.Config{ClickHouseMCPURL: configured.ClickHouseMCPURL}, false, 0},
		{"configured", configured, false, 1},
		{"build failure", configured, true, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			built := &agent.Agent{}
			editor := newEditorAgent(context.Background(), tc.cfg, func(_ context.Context, cfg *config.Config) (*agent.Agent, error) {
				calls++
				if cfg != tc.cfg {
					t.Fatal("startup replaced config")
				}
				if tc.failure {
					return nil, errors.New("model unavailable")
				}
				return built, nil
			}, slog.New(slog.NewTextHandler(io.Discard, nil)))
			if calls != tc.wantCalls {
				t.Fatalf("builder calls %d, want %d", calls, tc.wantCalls)
			}
			if tc.wantCalls == 0 || tc.failure {
				if editor != nil {
					t.Fatalf("disabled agent is %T", editor)
				}
			} else if editor != built {
				t.Fatal("startup lost built agent")
			}
		})
	}
}
