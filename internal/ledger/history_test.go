package ledger

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nrynss/ajilamu/internal/config"
)

const (
	fixtureDub      = "d1"
	fixtureLanguage = "ml"
	fixtureRoot     = "c1"
	fixtureChild    = "c2"
	fixtureFork     = "c3"
)

// Hard-coded timeline_at_commit JSONEachRow stand-ins. These are not produced by ReplayAt.
const viewAtRoot = `{"segment_index":1,"start_ms":1000,"end_ms":2000,"speaker":"Ada","emotion":"calm","source_text":"hello","text":"hello","take_id":"t1","state_version_seq":1}
{"segment_index":2,"start_ms":3000,"end_ms":4500,"speaker":"Ben","emotion":"warm","source_text":"world","text":"world","take_id":"t2","state_version_seq":1}
{"segment_index":3,"start_ms":5000,"end_ms":6000,"speaker":"Ada","emotion":"calm","source_text":"stay","text":"stay","take_id":"t3","state_version_seq":1}
`

const viewAtChild = `{"segment_index":1,"start_ms":1100,"end_ms":2000,"speaker":"Ada","emotion":"calm","source_text":"hello","text":"hello","take_id":"t1","state_version_seq":2}
{"segment_index":2,"start_ms":3000,"end_ms":4500,"speaker":"Ben","emotion":"warm","source_text":"world","text":"world","take_id":"t2","state_version_seq":1}
{"segment_index":3,"start_ms":5000,"end_ms":6000,"speaker":"Ada","emotion":"calm","source_text":"stay","text":"stay","take_id":"t3","state_version_seq":1}
`

const viewAtFork = `{"segment_index":1,"start_ms":1000,"end_ms":2000,"speaker":"Ada","emotion":"calm","source_text":"hello","text":"hello","take_id":"t1","state_version_seq":1}
{"segment_index":2,"start_ms":3000,"end_ms":4500,"speaker":"Ben","emotion":"warm","source_text":"world","text":"rewritten","take_id":"t2-alt","state_version_seq":2}
{"segment_index":3,"start_ms":5000,"end_ms":6000,"speaker":"Ada","emotion":"calm","source_text":"stay","text":"stay","take_id":"t3","state_version_seq":1}
`

type capturedHistoryRequest struct {
	query  string
	params url.Values
	body   []byte
}

func TestRecordSegmentStateInsertsFullSnapshot(t *testing.T) {
	t.Parallel()

	var (
		mu       sync.Mutex
		requests []capturedHistoryRequest
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureHistoryInsert(t, &requests, &mu, w, r)
	}))
	defer server.Close()

	client := newHistoryTestClient(t, server.URL)
	defer client.Close()

	segment := snapshotForTest(fixtureRoot, 1, 1, 1000, 2000, "Ada", "calm", "hello", "hello", "t1")
	if err := client.RecordSegmentState(context.Background(), segment); err != nil {
		t.Fatalf("RecordSegmentState: %v", err)
	}

	mu.Lock()
	got := append([]capturedHistoryRequest(nil), requests...)
	mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("request count = %d, want 1", len(got))
	}
	if got[0].query != insertTimelineState {
		t.Errorf("query = %q, want timeline_state_raw JSONEachRow insert", got[0].query)
	}
	if !strings.Contains(got[0].query, "INSERT INTO timeline_state_raw") {
		t.Errorf("query does not name timeline_state_raw: %s", got[0].query)
	}
	if strings.Contains(got[0].query, "INSERT INTO timeline_state ") {
		t.Errorf("query inserted into the timeline_state view: %s", got[0].query)
	}
	if !strings.Contains(strings.ToUpper(got[0].query), "FORMAT JSONEACHROW") {
		t.Errorf("query omitted JSONEachRow: %s", got[0].query)
	}

	var row map[string]any
	if err := json.Unmarshal(got[0].body, &row); err != nil {
		t.Fatalf("decode snapshot body: %v", err)
	}
	wantFields := map[string]any{
		"commit_id":     fixtureRoot,
		"project_id":    "project",
		"dub_id":        fixtureDub,
		"owner_id":      "owner",
		"language":      fixtureLanguage,
		"version_seq":   float64(1),
		"segment_index": float64(1),
		"start_ms":      float64(1000),
		"end_ms":        float64(2000),
		"speaker":       "Ada",
		"emotion":       "calm",
		"source_text":   "hello",
		"text":          "hello",
		"take_id":       "t1",
	}
	for name, want := range wantFields {
		if row[name] != want {
			t.Errorf("snapshot %s = %#v, want %#v", name, row[name], want)
		}
	}
	for _, forbidden := range []string{"created_at", "ingested_at"} {
		if _, ok := row[forbidden]; ok {
			t.Errorf("snapshot includes server-owned %s", forbidden)
		}
	}

	mu.Lock()
	beforeInvalid := len(requests)
	mu.Unlock()
	valid := segment
	for _, tc := range []struct {
		name    string
		segment TimelineSegment
	}{
		{name: "inverted slot", segment: withSlot(valid, 2000, 1000)},
		{name: "empty slot", segment: withSlot(valid, 1500, 1500)},
		{name: "negative segment_index", segment: withSegment(valid, -1)},
		{name: "empty commit_id", segment: withSnapshotCommit(valid, "")},
		{name: "empty dub_id", segment: withSnapshotDub(valid, "")},
		{name: "empty language", segment: withSnapshotLanguage(valid, "")},
		{name: "zero version_seq", segment: withSnapshotVersion(valid, 0)},
	} {
		if err := client.RecordSegmentState(context.Background(), tc.segment); err == nil {
			t.Errorf("%s succeeded, want validation error", tc.name)
		}
	}
	mu.Lock()
	afterInvalid := len(requests)
	mu.Unlock()
	if afterInvalid != beforeInvalid {
		t.Errorf("invalid snapshots sent %d requests, want none", afterInvalid-beforeInvalid)
	}
}

func TestTimelineAtQueriesParameterizedView(t *testing.T) {
	t.Parallel()

	var (
		mu       sync.Mutex
		requests []capturedHistoryRequest
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureHistoryQuery(t, &requests, &mu, w, r)
		if _, err := w.Write([]byte(viewAtChild)); err != nil {
			t.Errorf("write view payload: %v", err)
		}
	}))
	defer server.Close()

	client := newHistoryTestClient(t, server.URL)
	defer client.Close()

	got, err := client.TimelineAt(context.Background(), fixtureDub, fixtureLanguage, fixtureChild)
	if err != nil {
		t.Fatalf("TimelineAt: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("TimelineAt returned %d segments, want 3", len(got))
	}
	if got[0].SegmentIndex != 1 || got[0].StartMs != 1100 || got[0].VersionSeq != 2 {
		t.Errorf("decoded child segment 1 = %+v, want nudged slot at version 2", got[0])
	}
	if got[1].SegmentIndex != 2 || got[1].Text != "world" || got[1].VersionSeq != 1 {
		t.Errorf("decoded child segment 2 = %+v, want root text at version 1", got[1])
	}

	mu.Lock()
	captured := append([]capturedHistoryRequest(nil), requests...)
	mu.Unlock()
	if len(captured) != 1 {
		t.Fatalf("request count = %d, want 1", len(captured))
	}
	assertTimelineAtQuery(t, captured[0], fixtureChild)
}

func TestReplayAtMatchesTimelineAt(t *testing.T) {
	t.Parallel()

	commits, snapshots := historyFixture()
	payloads := map[string]string{
		fixtureRoot:  viewAtRoot,
		fixtureChild: viewAtChild,
		fixtureFork:  viewAtFork,
	}

	var (
		mu       sync.Mutex
		requests []capturedHistoryRequest
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureHistoryQuery(t, &requests, &mu, w, r)
		commitID := r.URL.Query().Get("param_commit_id")
		payload, ok := payloads[commitID]
		if !ok {
			t.Errorf("unexpected timeline commit %q", commitID)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if _, err := w.Write([]byte(payload)); err != nil {
			t.Errorf("write view payload: %v", err)
		}
	}))
	defer server.Close()

	client := newHistoryTestClient(t, server.URL)
	defer client.Close()

	for _, head := range []string{fixtureRoot, fixtureChild, fixtureFork} {
		replayed, err := ReplayAt(commits, snapshots, fixtureDub, fixtureLanguage, head)
		if err != nil {
			t.Fatalf("ReplayAt(%q): %v", head, err)
		}
		queried, err := client.TimelineAt(context.Background(), fixtureDub, fixtureLanguage, head)
		if err != nil {
			t.Fatalf("TimelineAt(%q): %v", head, err)
		}
		assertSegmentsEqual(t, head+" replay", replayed, expectedReplay(head))
		assertSegmentsEqual(t, head+" query", queried, replayed)
	}

	mu.Lock()
	captured := append([]capturedHistoryRequest(nil), requests...)
	mu.Unlock()
	if len(captured) != 3 {
		t.Fatalf("TimelineAt request count = %d, want 3", len(captured))
	}
	for i, head := range []string{fixtureRoot, fixtureChild, fixtureFork} {
		assertTimelineAtQuery(t, captured[i], head)
	}
}

func TestCompareBranchesUsesAncestryAndSumsCharges(t *testing.T) {
	t.Parallel()

	var (
		mu       sync.Mutex
		requests []capturedHistoryRequest
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureHistoryQuery(t, &requests, &mu, w, r)
		statement := r.URL.Query().Get("query")
		commitID := r.URL.Query().Get("param_commit_id")
		var payload string
		switch {
		case strings.Contains(statement, "timeline_at_commit"):
			switch commitID {
			case fixtureChild:
				payload = viewAtChild
			case fixtureFork:
				payload = viewAtFork
			default:
				t.Errorf("unexpected timeline commit %q", commitID)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
		case strings.Contains(statement, "FROM charges"):
			switch commitID {
			case fixtureChild:
				payload = `{"branch":"main","cost_usd":"0.03"}` + "\n"
			case fixtureFork:
				payload = `{"branch":"alt","cost_usd":"0.04"}` + "\n"
			default:
				t.Errorf("unexpected cost commit %q", commitID)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
		default:
			t.Errorf("unexpected query %q", statement)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if _, err := w.Write([]byte(payload)); err != nil {
			t.Errorf("write compare payload: %v", err)
		}
	}))
	defer server.Close()

	client := newHistoryTestClient(t, server.URL)
	defer client.Close()

	got, err := client.CompareBranches(context.Background(), fixtureDub, fixtureLanguage, fixtureChild, fixtureFork)
	if err != nil {
		t.Fatalf("CompareBranches: %v", err)
	}
	if got.A.CommitID != fixtureChild || got.B.CommitID != fixtureFork {
		t.Errorf("heads = %q, %q, want %s and %s", got.A.CommitID, got.B.CommitID, fixtureChild, fixtureFork)
	}
	if got.A.Branch != "main" || got.B.Branch != "alt" {
		t.Errorf("branch labels = %q, %q, want main and alt", got.A.Branch, got.B.Branch)
	}
	if got.A.CostUSD != "0.03" || got.B.CostUSD != "0.04" {
		t.Errorf("costs = %q, %q, want summed 0.03 and 0.04, not a running total", got.A.CostUSD, got.B.CostUSD)
	}
	if got.A.CostUSD == got.B.CostUSD {
		t.Error("branch costs match, want independent ancestry sums")
	}
	if got.A.SlotMs != 3400 || got.B.SlotMs != 3500 {
		t.Errorf("slot durations = %d, %d, want 3400 child and 3500 fork", got.A.SlotMs, got.B.SlotMs)
	}
	if got.A.TakeCount != 3 || got.B.TakeCount != 3 {
		t.Errorf("take counts = %d, %d, want 3 and 3", got.A.TakeCount, got.B.TakeCount)
	}

	childSeg := segmentByIndex(t, got.A.Segments, 2)
	forkSeg := segmentByIndex(t, got.B.Segments, 2)
	if childSeg.Text != "world" || forkSeg.Text != "rewritten" {
		t.Errorf("rewritten segment texts = %q, %q, want world and rewritten", childSeg.Text, forkSeg.Text)
	}
	if childSeg.TakeID == forkSeg.TakeID {
		t.Errorf("rewritten segment take_id leaked across branches: %q", childSeg.TakeID)
	}
	assertSegmentsEqual(t, "untouched segment 3",
		[]TimelineSegment{segmentByIndex(t, got.A.Segments, 3)},
		[]TimelineSegment{segmentByIndex(t, got.B.Segments, 3)},
	)
	childNudge := segmentByIndex(t, got.A.Segments, 1)
	forkOrig := segmentByIndex(t, got.B.Segments, 1)
	if childNudge.StartMs != 1100 || forkOrig.StartMs != 1000 {
		t.Errorf("segment 1 starts = %d, %d, want child nudge isolated from fork", childNudge.StartMs, forkOrig.StartMs)
	}

	mu.Lock()
	captured := append([]capturedHistoryRequest(nil), requests...)
	mu.Unlock()
	if len(captured) != 4 {
		t.Fatalf("compare request count = %d, want 2 timeline and 2 cost", len(captured))
	}
	var costQueries int
	for _, request := range captured {
		assertNoAncestryShortcuts(t, request.query)
		if strings.Contains(request.query, "timeline_at_commit") {
			assertTimelineAtQuery(t, request, request.params.Get("param_commit_id"))
			continue
		}
		costQueries++
		if !strings.Contains(request.query, "FROM charges") {
			t.Errorf("cost query omitted charges view: %s", request.query)
		}
		if strings.Contains(request.query, "charges_raw") {
			t.Errorf("cost query read charges_raw: %s", request.query)
		}
		if !strings.Contains(request.query, "sum(cost_usd)") {
			t.Errorf("cost query omitted per-ancestry sum(cost_usd): %s", request.query)
		}
		for _, running := range []string{"running_total", "total_cost", "accumulated"} {
			if strings.Contains(request.query, running) {
				t.Errorf("cost query carries running total %q: %s", running, request.query)
			}
		}
		for _, required := range []string{"{dub_id:String}", "{language:String}", "{commit_id:String}"} {
			if !strings.Contains(request.query, required) {
				t.Errorf("cost query omitted bound %s: %s", required, request.query)
			}
		}
		commitID := request.params.Get("param_commit_id")
		if commitID != fixtureChild && commitID != fixtureFork {
			t.Errorf("cost query commit_id = %q, want a head", commitID)
		}
		if strings.Contains(request.query, commitID) {
			t.Errorf("cost query interpolated commit %q", commitID)
		}
		if request.params.Get("param_branch") != "" {
			t.Errorf("cost query bound param_branch = %q", request.params.Get("param_branch"))
		}
	}
	if costQueries != 2 {
		t.Errorf("cost queries = %d, want 2", costQueries)
	}
}

func TestRecordSegmentStateReplaysFailedDelivery(t *testing.T) {
	t.Parallel()

	var (
		mu         sync.Mutex
		requests   []capturedHistoryRequest
		replayOnce sync.Once
	)
	recovery := make(chan struct{})
	replayed := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		mu.Lock()
		requests = append(requests, capturedHistoryRequest{query: r.URL.Query().Get("query"), body: body})
		mu.Unlock()
		select {
		case <-recovery:
			replayOnce.Do(func() { close(replayed) })
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer server.Close()

	client := newHistoryTestClient(t, server.URL)
	defer client.Close()
	segment := snapshotForTest(fixtureRoot, 1, 1, 1000, 2000, "Ada", "calm", "hello", "hello", "t1")
	if err := client.RecordSegmentState(context.Background(), segment); !errors.Is(err, ErrPending) {
		t.Fatalf("RecordSegmentState retry error = %v, want ErrPending", err)
	}
	if pending, err := client.Pending(); err != nil || pending != 1 {
		t.Fatalf("Pending after failed delivery = %d, %v, want 1, nil", pending, err)
	}
	files, err := os.ReadDir(client.queue.dir)
	if err != nil {
		t.Fatalf("read durable queue: %v", err)
	}
	journalEntries := 0
	for _, file := range files {
		if strings.HasSuffix(file.Name(), ".json") {
			journalEntries++
		}
	}
	if journalEntries != 1 {
		t.Fatalf("durable journal entries after failed delivery = %d, want 1", journalEntries)
	}

	close(recovery)
	if err := client.Flush(context.Background()); err != nil {
		t.Fatalf("Flush replay: %v", err)
	}
	select {
	case <-replayed:
	case <-time.After(3 * time.Second):
		t.Fatal("replay did not deliver the recovered snapshot")
	}
	if pending, err := client.Pending(); err != nil || pending != 0 {
		t.Fatalf("Pending after replay = %d, %v, want 0, nil", pending, err)
	}

	mu.Lock()
	got := append([]capturedHistoryRequest(nil), requests...)
	mu.Unlock()
	if len(got) < 2 {
		t.Fatalf("request count = %d, want failed delivery and replay", len(got))
	}
	if got[0].query != insertTimelineState || got[len(got)-1].query != insertTimelineState {
		t.Errorf("replay queries = %q, %q, want timeline_state_raw JSONEachRow insert", got[0].query, got[len(got)-1].query)
	}
	if string(got[0].body) != string(got[len(got)-1].body) {
		t.Error("replayed body differs from durable failed delivery")
	}
	var row map[string]any
	if err := json.Unmarshal(got[len(got)-1].body, &row); err != nil {
		t.Fatalf("decode replay: %v", err)
	}
	if row["take_id"] != "t1" || row["segment_index"] != float64(1) {
		t.Errorf("replayed snapshot = %#v, want original segment", row)
	}
	if _, ok := row["created_at"]; ok {
		t.Error("replayed body includes created_at")
	}
	if _, ok := row["ingested_at"]; ok {
		t.Error("replayed body includes ingested_at")
	}
}

func historyFixture() ([]Commit, []TimelineSegment) {
	commits := []Commit{
		{
			CommitID: fixtureRoot, ProjectID: "project", DubID: fixtureDub, OwnerID: "owner",
			Branch: "main", Language: fixtureLanguage, VersionSeq: 1, Message: "root",
		},
		{
			CommitID: fixtureChild, ParentCommitID: fixtureRoot, ProjectID: "project", DubID: fixtureDub, OwnerID: "owner",
			Branch: "main", Language: fixtureLanguage, VersionSeq: 2, Message: "nudge",
		},
		{
			CommitID: fixtureFork, ParentCommitID: fixtureRoot, ProjectID: "project", DubID: fixtureDub, OwnerID: "owner",
			Branch: "alt", Language: fixtureLanguage, VersionSeq: 2, Message: "rewrite",
		},
	}
	snapshots := []TimelineSegment{
		snapshotForTest(fixtureRoot, 1, 1, 1000, 2000, "Ada", "calm", "hello", "hello", "t1"),
		snapshotForTest(fixtureRoot, 1, 2, 3000, 4500, "Ben", "warm", "world", "world", "t2"),
		snapshotForTest(fixtureRoot, 1, 3, 5000, 6000, "Ada", "calm", "stay", "stay", "t3"),
		snapshotForTest(fixtureChild, 2, 1, 1100, 2000, "Ada", "calm", "hello", "hello", "t1"),
		snapshotForTest(fixtureFork, 2, 2, 3000, 4500, "Ben", "warm", "world", "rewritten", "t2-alt"),
	}
	return commits, snapshots
}

func expectedReplay(head string) []TimelineSegment {
	root1 := queriedSegment(1, 1000, 2000, "Ada", "calm", "hello", "hello", "t1", 1)
	root2 := queriedSegment(2, 3000, 4500, "Ben", "warm", "world", "world", "t2", 1)
	root3 := queriedSegment(3, 5000, 6000, "Ada", "calm", "stay", "stay", "t3", 1)
	switch head {
	case fixtureRoot:
		return []TimelineSegment{root1, root2, root3}
	case fixtureChild:
		return []TimelineSegment{
			queriedSegment(1, 1100, 2000, "Ada", "calm", "hello", "hello", "t1", 2),
			root2,
			root3,
		}
	case fixtureFork:
		return []TimelineSegment{
			root1,
			queriedSegment(2, 3000, 4500, "Ben", "warm", "world", "rewritten", "t2-alt", 2),
			root3,
		}
	default:
		return nil
	}
}

func queriedSegment(index int32, start, end int64, speaker, emotion, source, text, takeID string, version uint64) TimelineSegment {
	return TimelineSegment{
		DubID:        fixtureDub,
		Language:     fixtureLanguage,
		VersionSeq:   version,
		SegmentIndex: index,
		StartMs:      start,
		EndMs:        end,
		Speaker:      speaker,
		Emotion:      emotion,
		SourceText:   source,
		Text:         text,
		TakeID:       takeID,
	}
}

func snapshotForTest(commitID string, version uint64, index int32, start, end int64, speaker, emotion, source, text, takeID string) TimelineSegment {
	return TimelineSegment{
		CommitID:     commitID,
		ProjectID:    "project",
		DubID:        fixtureDub,
		OwnerID:      "owner",
		Language:     fixtureLanguage,
		VersionSeq:   version,
		SegmentIndex: index,
		StartMs:      start,
		EndMs:        end,
		Speaker:      speaker,
		Emotion:      emotion,
		SourceText:   source,
		Text:         text,
		TakeID:       takeID,
	}
}

func assertTimelineAtQuery(t *testing.T, request capturedHistoryRequest, commitID string) {
	t.Helper()
	statement := request.query
	if !strings.Contains(statement, "timeline_at_commit") {
		t.Errorf("query omitted timeline_at_commit: %s", statement)
	}
	if strings.Contains(statement, "timeline_state_raw") {
		t.Errorf("query selected timeline_state_raw instead of the view: %s", statement)
	}
	for _, required := range []string{"{dub_id:String}", "{language:String}", "{commit_id:String}"} {
		if !strings.Contains(statement, required) {
			t.Errorf("query omitted bound %s: %s", required, statement)
		}
	}
	if request.params.Get("param_dub_id") != fixtureDub {
		t.Errorf("param_dub_id = %q, want %s", request.params.Get("param_dub_id"), fixtureDub)
	}
	if request.params.Get("param_language") != fixtureLanguage {
		t.Errorf("param_language = %q, want %s", request.params.Get("param_language"), fixtureLanguage)
	}
	if request.params.Get("param_commit_id") != commitID {
		t.Errorf("param_commit_id = %q, want %s", request.params.Get("param_commit_id"), commitID)
	}
	if strings.Contains(statement, commitID) || strings.Contains(statement, fixtureDub) {
		t.Errorf("query interpolated bound values: %s", statement)
	}
	assertNoAncestryShortcuts(t, statement)
}

func assertNoAncestryShortcuts(t *testing.T, statement string) {
	t.Helper()
	compact := strings.ReplaceAll(statement, " ", "")
	if strings.Contains(compact, "version_seq<") || strings.Contains(compact, "version_seq>") {
		t.Errorf("query uses a version_seq inequality to select ancestry: %s", statement)
	}
	if strings.Contains(compact, "branch=") || strings.Contains(compact, "WHEREbranch") || strings.Contains(compact, "ANDbranch=") {
		t.Errorf("query filters branch to select ancestry: %s", statement)
	}
}

func assertSegmentsEqual(t *testing.T, label string, got, want []TimelineSegment) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s segments mismatch\n got %#v\nwant %#v", label, got, want)
	}
}

func segmentByIndex(t *testing.T, segments []TimelineSegment, index int32) TimelineSegment {
	t.Helper()
	for _, segment := range segments {
		if segment.SegmentIndex == index {
			return segment
		}
	}
	t.Fatalf("missing segment %d", index)
	return TimelineSegment{}
}

func captureHistoryInsert(t *testing.T, requests *[]capturedHistoryRequest, mu *sync.Mutex, w http.ResponseWriter, r *http.Request) {
	t.Helper()
	query := r.URL.Query().Get("query")
	if query != insertTimelineState {
		t.Errorf("unexpected query %q", query)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		t.Errorf("read request body: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	mu.Lock()
	*requests = append(*requests, capturedHistoryRequest{query: query, params: r.URL.Query(), body: body})
	mu.Unlock()
	w.WriteHeader(http.StatusOK)
}

func captureHistoryQuery(t *testing.T, requests *[]capturedHistoryRequest, mu *sync.Mutex, w http.ResponseWriter, r *http.Request) {
	t.Helper()
	params := r.URL.Query()
	mu.Lock()
	*requests = append(*requests, capturedHistoryRequest{query: params.Get("query"), params: params})
	mu.Unlock()
}

func newHistoryTestClient(t *testing.T, endpoint string) *Client {
	t.Helper()
	client, err := New(&config.Config{
		ClickHouseHost:     "test.invalid",
		ClickHousePort:     8123,
		ClickHouseUser:     "test-user",
		ClickHousePassword: "test-password",
		ClickHouseDatabase: "test-db",
	}, t.TempDir(), WithEndpoint(endpoint), WithHTTPClient(&http.Client{Timeout: 30 * time.Second}))
	if err != nil {
		t.Fatalf("New ledger client: %v", err)
	}
	return client
}

func withSlot(segment TimelineSegment, start, end int64) TimelineSegment {
	segment.StartMs = start
	segment.EndMs = end
	return segment
}

func withSegment(segment TimelineSegment, index int32) TimelineSegment {
	segment.SegmentIndex = index
	return segment
}

func withSnapshotCommit(segment TimelineSegment, commitID string) TimelineSegment {
	segment.CommitID = commitID
	return segment
}

func withSnapshotDub(segment TimelineSegment, dubID string) TimelineSegment {
	segment.DubID = dubID
	return segment
}

func withSnapshotLanguage(segment TimelineSegment, language string) TimelineSegment {
	segment.Language = language
	return segment
}

func withSnapshotVersion(segment TimelineSegment, version uint64) TimelineSegment {
	segment.VersionSeq = version
	return segment
}
