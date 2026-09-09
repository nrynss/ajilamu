package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nrynss/ajilamu/internal/api"
)

// editRecorderFunc adapts a function to api.EditRecorder.
type editRecorderFunc func(ctx context.Context, edit api.EditRecord) error

// RecordEdit invokes the wrapped function.
func (f editRecorderFunc) RecordEdit(ctx context.Context, edit api.EditRecord) error {
	return f(ctx, edit)
}

// editHistory is the stand-in head timeline every edit test reads.
func editHistory() *standInWorkspaceHistory {
	return &standInWorkspaceHistory{
		commits: []api.Commit{{CommitID: "c1", VersionNumber: 1}},
		segments: []api.TimelineEntry{
			{SegmentIndex: 1, StartMs: 0, EndMs: 5000, Speaker: "Mark", Emotion: "Warm", SourceText: "one", Text: "onnu", TakeID: "t1"},
			{SegmentIndex: 2, StartMs: 6000, EndMs: 11000, Speaker: "Suni", Emotion: "Calm", SourceText: "two", Text: "randu", TakeID: "t2"},
		},
	}
}

// postEdit posts one edit body and returns the response.
func postEdit(t *testing.T, base, dubID, body string) *http.Response {
	t.Helper()
	target := base + "/api/dubs/" + dubID + "/edits"
	response, err := http.Post(target, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("post edit: %v", err)
	}
	return response
}

// decodeEdit reads one edit body and closes the response.
func decodeEdit(t *testing.T, response *http.Response) api.EditResponse {
	t.Helper()
	defer response.Body.Close()
	var body api.EditResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode edit body: %v", err)
	}
	return body
}

// editError reads one failure sentence and closes the response.
func editError(t *testing.T, response *http.Response) string {
	t.Helper()
	defer response.Body.Close()
	var body struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode edit failure: %v", err)
	}
	return body.Error
}

// TestEditBoundaryRecordsOneCommitActionAndSnapshot proves a boundary drag
// reaches the recorder with the manual_ui author, no prompt, and the whole
// timeline row copied forward around the new bounds.
func TestEditBoundaryRecordsOneCommitActionAndSnapshot(t *testing.T) {
	var got api.EditRecord
	calls := 0
	recorder := editRecorderFunc(func(_ context.Context, edit api.EditRecord) error {
		calls++
		got = edit
		return nil
	})
	base := newRunTestServer(t, api.ServerOptions{Edits: recorder, History: editHistory()})

	response := postEdit(t, base, "dub-boundary", `{"kind":"boundary","language":"ml","segment_id":1,"start_ms":500,"end_ms":4800}`)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("edit status = %d, want 201", response.StatusCode)
	}
	body := decodeEdit(t, response)

	if calls != 1 {
		t.Errorf("recorder calls = %d, want 1", calls)
	}
	if got.CommitID == "" {
		t.Error("record carried no commit id")
	}
	if got.DubID != "dub-boundary" || got.ProjectID != "dub-boundary" {
		t.Errorf("record ids = %q/%q, want dub-boundary", got.DubID, got.ProjectID)
	}
	if got.OwnerID != "local" {
		t.Errorf("owner = %q, want local", got.OwnerID)
	}
	if got.Language != "ml" {
		t.Errorf("language = %q, want ml", got.Language)
	}
	if got.Action != api.ActionBoundaryNudged {
		t.Errorf("action = %q, want %q", got.Action, api.ActionBoundaryNudged)
	}
	if got.Author != api.AuthorManualUI {
		t.Errorf("author = %q, want %q", got.Author, api.AuthorManualUI)
	}
	if got.Prompt != "" {
		t.Errorf("prompt = %q, want empty for a boundary", got.Prompt)
	}
	wantBefore := `{"start_ms":0,"end_ms":5000,"speaker":"Mark"}`
	if got.BeforeValue != wantBefore {
		t.Errorf("before_value = %q, want %q", got.BeforeValue, wantBefore)
	}
	wantAfter := `{"start_ms":500,"end_ms":4800,"speaker":"Mark"}`
	if got.AfterValue != wantAfter {
		t.Errorf("after_value = %q, want %q", got.AfterValue, wantAfter)
	}
	snapshot := got.Segment
	if snapshot.SegmentIndex != 1 || snapshot.StartMs != 500 || snapshot.EndMs != 4800 {
		t.Errorf("snapshot bounds = line %d %d..%d, want line 1 500..4800", snapshot.SegmentIndex, snapshot.StartMs, snapshot.EndMs)
	}
	if snapshot.Speaker != "Mark" || snapshot.Emotion != "Warm" || snapshot.SourceText != "one" || snapshot.Text != "onnu" || snapshot.TakeID != "t1" {
		t.Errorf("snapshot lost copied fields: %+v", snapshot)
	}
	if body.CommitID != got.CommitID || body.Action != api.ActionBoundaryNudged || body.Author != api.AuthorManualUI {
		t.Errorf("response provenance = %+v, want the recorded commit", body)
	}
	if body.Segment.ID != 1 || body.Segment.StartMs != 500 || body.Segment.EndMs != 4800 || body.Segment.DurationMs != 4300 {
		t.Errorf("response segment = %+v, want line 1 500..4800 over 4300ms", body.Segment)
	}
}

// TestEditCommandRecordsUserCommandWithVerbatimPrompt proves a confirmed
// command reaches the recorder with the command_bar author, the instruction as
// the prompt, and the deterministic mutation applied to the head timeline.
func TestEditCommandRecordsUserCommandWithVerbatimPrompt(t *testing.T) {
	var got api.EditRecord
	calls := 0
	recorder := editRecorderFunc(func(_ context.Context, edit api.EditRecord) error {
		calls++
		got = edit
		return nil
	})
	base := newRunTestServer(t, api.ServerOptions{Edits: recorder, History: editHistory()})

	response := postEdit(t, base, "dub-command", `{"kind":"command","language":"ml","command":"shift line 1 right by 250ms","duration_ms":75008}`)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("edit status = %d, want 201", response.StatusCode)
	}
	body := decodeEdit(t, response)

	if calls != 1 {
		t.Errorf("recorder calls = %d, want 1", calls)
	}
	if got.Action != api.ActionUserCommand {
		t.Errorf("action = %q, want %q", got.Action, api.ActionUserCommand)
	}
	if got.Author != api.AuthorCommandBar {
		t.Errorf("author = %q, want %q", got.Author, api.AuthorCommandBar)
	}
	if got.Prompt != "shift line 1 right by 250ms" {
		t.Errorf("prompt = %q, want the instruction verbatim", got.Prompt)
	}
	if got.Segment.StartMs != 250 || got.Segment.EndMs != 5250 {
		t.Errorf("snapshot bounds = %d..%d, want 250..5250", got.Segment.StartMs, got.Segment.EndMs)
	}
	if body.Segment.StartMs != 250 || body.Segment.EndMs != 5250 || body.Segment.DurationMs != 5000 {
		t.Errorf("response segment = %+v, want 250..5250 over 5000ms", body.Segment)
	}
}

// TestEditCommandRecordsASpeakerChange proves a confirmed speaker command
// records the resolved speaker and leaves the timing alone.
func TestEditCommandRecordsASpeakerChange(t *testing.T) {
	var got api.EditRecord
	recorder := editRecorderFunc(func(_ context.Context, edit api.EditRecord) error {
		got = edit
		return nil
	})
	base := newRunTestServer(t, api.ServerOptions{Edits: recorder, History: editHistory()})

	response := postEdit(t, base, "dub-speaker", `{"kind":"command","language":"ml","command":"change speaker for line 1 to Suni","duration_ms":75008}`)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("edit status = %d, want 201", response.StatusCode)
	}
	body := decodeEdit(t, response)

	if got.Segment.Speaker != "Suni" {
		t.Errorf("snapshot speaker = %q, want Suni", got.Segment.Speaker)
	}
	if got.Segment.StartMs != 0 || got.Segment.EndMs != 5000 {
		t.Errorf("snapshot bounds = %d..%d, want 0..5000 unchanged", got.Segment.StartMs, got.Segment.EndMs)
	}
	if body.Segment.Speaker != "Suni" {
		t.Errorf("response speaker = %q, want Suni", body.Segment.Speaker)
	}
}

// TestEditCommandRejectsAnInvalidInstruction proves the validator runs before
// the recorder, so an unapplied command writes nothing.
func TestEditCommandRejectsAnInvalidInstruction(t *testing.T) {
	calls := 0
	recorder := editRecorderFunc(func(context.Context, api.EditRecord) error {
		calls++
		return nil
	})
	base := newRunTestServer(t, api.ServerOptions{Edits: recorder, History: editHistory()})

	response := postEdit(t, base, "dub-invalid", `{"kind":"command","language":"ml","command":"shift line 9 right by 250ms","duration_ms":75008}`)
	if response.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("invalid command status = %d, want 422", response.StatusCode)
	}
	if sentence := editError(t, response); !strings.Contains(sentence, "line 9") {
		t.Errorf("failure sentence = %q, want it to name line 9", sentence)
	}
	if calls != 0 {
		t.Errorf("recorder calls = %d, want 0 for an invalid command", calls)
	}
}

// TestEditBoundaryRejectsOverlap proves a drag that would overlap a neighbour
// fails before the recorder sees a row.
func TestEditBoundaryRejectsOverlap(t *testing.T) {
	calls := 0
	recorder := editRecorderFunc(func(context.Context, api.EditRecord) error {
		calls++
		return nil
	})
	base := newRunTestServer(t, api.ServerOptions{Edits: recorder, History: editHistory()})

	response := postEdit(t, base, "dub-overlap", `{"kind":"boundary","language":"ml","segment_id":1,"start_ms":0,"end_ms":6500}`)
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("overlap status = %d, want 400", response.StatusCode)
	}
	if sentence := editError(t, response); sentence != "That boundary would overlap another line." {
		t.Errorf("failure sentence = %q, want the overlap sentence", sentence)
	}
	if calls != 0 {
		t.Errorf("recorder calls = %d, want 0 for an overlap", calls)
	}
}

// TestEditFailureDoesNotReportSuccess proves a failed ledger write answers 500
// with an honest sentence and no commit id.
func TestEditFailureDoesNotReportSuccess(t *testing.T) {
	recorder := editRecorderFunc(func(context.Context, api.EditRecord) error {
		return errors.New("ClickHouse refused the row")
	})
	base := newRunTestServer(t, api.ServerOptions{Edits: recorder, History: editHistory()})

	response := postEdit(t, base, "dub-fail", `{"kind":"boundary","language":"ml","segment_id":1,"start_ms":500,"end_ms":4800}`)
	if response.StatusCode != http.StatusInternalServerError {
		t.Fatalf("failed write status = %d, want 500", response.StatusCode)
	}
	if sentence := editError(t, response); sentence != "The edit could not be saved." {
		t.Errorf("failure sentence = %q, want the save failure sentence", sentence)
	}
}

// TestEditWithoutATimelineWritesNothing proves a project with no commit cannot
// record an edit.
func TestEditWithoutATimelineWritesNothing(t *testing.T) {
	calls := 0
	recorder := editRecorderFunc(func(context.Context, api.EditRecord) error {
		calls++
		return nil
	})
	history := &standInWorkspaceHistory{}
	base := newRunTestServer(t, api.ServerOptions{Edits: recorder, History: history})

	response := postEdit(t, base, "dub-empty", `{"kind":"boundary","language":"ml","segment_id":1,"start_ms":0,"end_ms":100}`)
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("empty project status = %d, want 404", response.StatusCode)
	}
	if calls != 0 {
		t.Errorf("recorder calls = %d, want 0 without a timeline", calls)
	}
}

// headReadDelay widens the window a forked head read needs. Without the edit
// lock every concurrent request reads the head before any append lands.
const headReadDelay = 25 * time.Millisecond

// racingEditLedger is the stand-in ledger for the serialization pin. It holds
// one commit chain and one timeline per commit, so a forked append shows up as
// two commits at one version. It records the peak number of head reads in
// flight, because that overlap is the fork the edit lock closes.
type racingEditLedger struct {
	mu            sync.Mutex
	commits       []api.Commit
	timelines     map[string][]api.TimelineEntry
	headReads     int
	peakHeadReads int
}

// newRacingEditLedger seeds the root commit and its two-line timeline.
func newRacingEditLedger() *racingEditLedger {
	return &racingEditLedger{
		commits:   []api.Commit{{CommitID: "c1", VersionNumber: 1}},
		timelines: map[string][]api.TimelineEntry{"c1": editHistory().segments},
	}
}

// ListCommits serves the chain and holds the read open long enough for a
// second unsynchronized read to overlap it.
func (l *racingEditLedger) ListCommits(context.Context, string) ([]api.Commit, error) {
	l.mu.Lock()
	l.headReads++
	if l.headReads > l.peakHeadReads {
		l.peakHeadReads = l.headReads
	}
	commits := append([]api.Commit(nil), l.commits...)
	l.mu.Unlock()
	time.Sleep(headReadDelay)
	l.mu.Lock()
	l.headReads--
	l.mu.Unlock()
	return commits, nil
}

// TimelineAt serves the timeline stored at one commit.
func (l *racingEditLedger) TimelineAt(_ context.Context, _, _, commitID string) ([]api.TimelineEntry, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	entries, ok := l.timelines[commitID]
	if !ok {
		return nil, fmt.Errorf("unknown commit %q", commitID)
	}
	return append([]api.TimelineEntry(nil), entries...), nil
}

// CompareBranches is unused by the edit route.
func (l *racingEditLedger) CompareBranches(context.Context, string, string, string, string) (api.BranchComparison, error) {
	return api.BranchComparison{}, nil
}

// RecordEdit stands in for the ledger adapter. It reads the head, then
// appends a child at the next version, and stores the edited row as the new
// timeline. The pause between the read and the append is the window the real
// adapter leaves open, because nothing serializes it on its own.
func (l *racingEditLedger) RecordEdit(_ context.Context, edit api.EditRecord) error {
	l.mu.Lock()
	head := l.commits[len(l.commits)-1]
	l.mu.Unlock()
	time.Sleep(headReadDelay)

	l.mu.Lock()
	defer l.mu.Unlock()
	entries := append([]api.TimelineEntry(nil), l.timelines[head.CommitID]...)
	for i := range entries {
		if entries[i].SegmentIndex == edit.Segment.SegmentIndex {
			entries[i].StartMs = edit.Segment.StartMs
			entries[i].EndMs = edit.Segment.EndMs
			entries[i].Speaker = edit.Segment.Speaker
		}
	}
	l.commits = append(l.commits, api.Commit{
		CommitID:       edit.CommitID,
		ParentCommitID: head.CommitID,
		VersionNumber:  head.VersionNumber + 1,
	})
	l.timelines[edit.CommitID] = entries
	return nil
}

// postEditStatus posts one edit body and returns the status code.
func postEditStatus(base, dubID, body string) (int, error) {
	target := base + "/api/dubs/" + dubID + "/edits"
	response, err := http.Post(target, "application/json", strings.NewReader(body))
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	return response.StatusCode, nil
}

// TestEditsSerializePerDub proves two edits of one dub never read one head.
// Twelve concurrent posts must chain, because the route holds the dub's edit
// lock across the head read and the recorder call. Without the lock every
// request appends a child of c1 at version 2, and the head timeline keeps one
// of the twelve edits.
func TestEditsSerializePerDub(t *testing.T) {
	for _, testCase := range []struct {
		name string
		body string
	}{
		{"boundary", `{"kind":"boundary","language":"ml","segment_id":1,"start_ms":500,"end_ms":4800}`},
		{"command", `{"kind":"command","language":"ml","command":"shift line 2 right by 250ms","duration_ms":75008}`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			ledger := newRacingEditLedger()
			base := newRunTestServer(t, api.ServerOptions{Edits: ledger, History: ledger})
			const edits = 12
			statuses := make([]int, edits)
			failures := make([]error, edits)
			var wait sync.WaitGroup
			start := make(chan struct{})
			for i := range statuses {
				wait.Add(1)
				go func(i int) {
					defer wait.Done()
					<-start
					statuses[i], failures[i] = postEditStatus(base, "dub-race", testCase.body)
				}(i)
			}
			close(start)
			wait.Wait()

			for i, err := range failures {
				if err != nil {
					t.Fatalf("edit %d: %v", i, err)
				}
				if statuses[i] != http.StatusCreated {
					t.Errorf("edit %d status = %d, want 201", i, statuses[i])
				}
			}
			ledger.mu.Lock()
			peak := ledger.peakHeadReads
			commits := append([]api.Commit(nil), ledger.commits...)
			ledger.mu.Unlock()

			if peak != 1 {
				t.Errorf("peak concurrent head reads = %d, want 1", peak)
			}
			if len(commits) != edits+1 {
				t.Fatalf("commits = %d, want %d", len(commits), edits+1)
			}
			versions := make(map[int]int, len(commits))
			for _, commit := range commits {
				versions[commit.VersionNumber]++
			}
			for version := 1; version <= edits+1; version++ {
				if versions[version] != 1 {
					t.Errorf("version %d appears %d times, want once", version, versions[version])
				}
			}
			for i := 1; i < len(commits); i++ {
				if commits[i].ParentCommitID != commits[i-1].CommitID {
					t.Errorf("commit %d parent = %q, want %q", i, commits[i].ParentCommitID, commits[i-1].CommitID)
				}
			}

			// The head the route serves is the greatest version, then commit
			// id. Its ancestry must reach every commit, or an edit vanished.
			head := commits[0]
			for _, commit := range commits[1:] {
				if commit.VersionNumber > head.VersionNumber ||
					(commit.VersionNumber == head.VersionNumber && commit.CommitID > head.CommitID) {
					head = commit
				}
			}
			parent := make(map[string]string, len(commits))
			for _, commit := range commits {
				parent[commit.CommitID] = commit.ParentCommitID
			}
			held := 0
			for id := head.CommitID; id != ""; id = parent[id] {
				held++
			}
			if held != len(commits) {
				t.Errorf("head ancestry = %d commits, want %d", held, len(commits))
			}
		})
	}
}

// appendRunCommit stands in for runRecorder.persist on the shared lock. It
// takes the dub's commit lock, reads the head, then appends a run commit at the
// next version. The pause between the read and the append is the window a run
// commit leaves open without the shared lock.
func (l *racingEditLedger) appendRunCommit(ctx context.Context, dubID, commitID string) error {
	unlock := api.LockDubCommit(dubID)
	defer unlock()
	commits, err := l.ListCommits(ctx, dubID)
	if err != nil {
		return err
	}
	head := commits[len(commits)-1]
	time.Sleep(headReadDelay)
	l.mu.Lock()
	defer l.mu.Unlock()
	l.commits = append(l.commits, api.Commit{
		CommitID:       commitID,
		ParentCommitID: head.CommitID,
		VersionNumber:  head.VersionNumber + 1,
	})
	l.timelines[commitID] = append([]api.TimelineEntry(nil), l.timelines[head.CommitID]...)
	return nil
}

// TestRunCommitAndEditsShareDubLock proves the run writer and the edit route
// take one per-dub commit lock. A run commit and twelve concurrent edits must
// chain, because EditsHandler and the run writer both hold LockDubCommit across
// the head read and the append. Without the shared lock the run commit and the
// edits append children of one head at one version, and the head timeline
// keeps one branch.
func TestRunCommitAndEditsShareDubLock(t *testing.T) {
	ledger := newRacingEditLedger()
	base := newRunTestServer(t, api.ServerOptions{Edits: ledger, History: ledger})
	const edits = 12
	statuses := make([]int, edits)
	failures := make([]error, edits)
	runResult := make(chan error, 1)
	var wait sync.WaitGroup
	start := make(chan struct{})
	wait.Add(1)
	go func() {
		defer wait.Done()
		<-start
		runResult <- ledger.appendRunCommit(context.Background(), "dub-run-race", "run-commit")
	}()
	for i := range statuses {
		wait.Add(1)
		go func(i int) {
			defer wait.Done()
			<-start
			statuses[i], failures[i] = postEditStatus(base, "dub-run-race",
				`{"kind":"boundary","language":"ml","segment_id":1,"start_ms":500,"end_ms":4800}`)
		}(i)
	}
	close(start)
	wait.Wait()

	if err := <-runResult; err != nil {
		t.Fatalf("run commit: %v", err)
	}
	for i, err := range failures {
		if err != nil {
			t.Fatalf("edit %d: %v", i, err)
		}
		if statuses[i] != http.StatusCreated {
			t.Errorf("edit %d status = %d, want 201", i, statuses[i])
		}
	}
	ledger.mu.Lock()
	peak := ledger.peakHeadReads
	commits := append([]api.Commit(nil), ledger.commits...)
	ledger.mu.Unlock()

	if peak != 1 {
		t.Errorf("peak concurrent head reads = %d, want 1", peak)
	}
	wantCommits := edits + 2
	if len(commits) != wantCommits {
		t.Fatalf("commits = %d, want %d", len(commits), wantCommits)
	}
	versions := make(map[int]int, len(commits))
	for _, commit := range commits {
		versions[commit.VersionNumber]++
	}
	for version := 1; version <= wantCommits; version++ {
		if versions[version] != 1 {
			t.Errorf("version %d appears %d times, want once", version, versions[version])
		}
	}
	parent := make(map[string]string, len(commits))
	for _, commit := range commits {
		parent[commit.CommitID] = commit.ParentCommitID
	}
	head := commits[0]
	for _, commit := range commits[1:] {
		if commit.VersionNumber > head.VersionNumber ||
			(commit.VersionNumber == head.VersionNumber && commit.CommitID > head.CommitID) {
			head = commit
		}
	}
	held := 0
	runHeld := false
	for id := head.CommitID; id != ""; id = parent[id] {
		held++
		if id == "run-commit" {
			runHeld = true
		}
	}
	if held != wantCommits {
		t.Errorf("head ancestry = %d commits, want %d", held, wantCommits)
	}
	if !runHeld {
		t.Error("the head timeline dropped the run commit")
	}
}

// TestEditsWaitForTheSharedDubCommitLock proves EditsHandler takes the
// process-wide per-dub commit lock. The test holds that lock, posts one edit,
// and proves the route reads no head until the lock is released. A
// handler-local lock lets the edit finish while the shared lock is held.
func TestEditsWaitForTheSharedDubCommitLock(t *testing.T) {
	ledger := newRacingEditLedger()
	base := newRunTestServer(t, api.ServerOptions{Edits: ledger, History: ledger})
	const dubID = "dub-wait"
	unlock := api.LockDubCommit(dubID)
	released := false
	defer func() {
		if !released {
			unlock()
		}
	}()

	status := make(chan int, 1)
	failures := make(chan error, 1)
	go func() {
		code, err := postEditStatus(base, dubID,
			`{"kind":"boundary","language":"ml","segment_id":1,"start_ms":500,"end_ms":4800}`)
		if err != nil {
			failures <- err
			return
		}
		status <- code
	}()

	select {
	case err := <-failures:
		t.Fatalf("edit: %v", err)
	case code := <-status:
		t.Fatalf("edit finished with status %d while the shared lock was held", code)
	case <-time.After(5 * headReadDelay):
	}
	ledger.mu.Lock()
	reads := ledger.headReads
	ledger.mu.Unlock()
	if reads != 0 {
		t.Fatalf("head reads while the shared lock was held = %d, want 0", reads)
	}
	unlock()
	released = true
	select {
	case err := <-failures:
		t.Fatalf("edit: %v", err)
	case code := <-status:
		if code != http.StatusCreated {
			t.Errorf("edit status = %d, want 201", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("edit did not finish after the shared lock was released")
	}
}
