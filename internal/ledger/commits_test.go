package ledger

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nrynss/ajilamu/internal/config"
)

type capturedCommitRequest struct {
	query string
	body  []byte
}

func serveCommitRequest(t *testing.T, requests *[]capturedCommitRequest, mu *sync.Mutex, w http.ResponseWriter, r *http.Request) {
	t.Helper()
	query := r.URL.Query().Get("query")
	if query == selectCommit {
		parentID := r.URL.Query().Get("param_commit_id")
		mu.Lock()
		defer mu.Unlock()
		for _, request := range *requests {
			var row storedCommit
			if err := json.Unmarshal(request.body, &row); err != nil {
				t.Errorf("decode stored commit: %v", err)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			if row.CommitID == parentID {
				if err := json.NewEncoder(w).Encode(row); err != nil {
					t.Errorf("encode stored commit: %v", err)
				}
				return
			}
		}
		w.WriteHeader(http.StatusOK)
		return
	}
	if query != insertCommit {
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
	*requests = append(*requests, capturedCommitRequest{query: query, body: body})
	mu.Unlock()
	w.WriteHeader(http.StatusOK)
}

func TestAppendCommitWritesDAGWithoutClientTimestamps(t *testing.T) {
	t.Parallel()

	var (
		mu       sync.Mutex
		requests []capturedCommitRequest
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serveCommitRequest(t, &requests, &mu, w, r)
	}))
	defer server.Close()

	client := newCommitTestClient(t, server.URL)
	defer client.Close()

	root := commitForTest("root", "", "main", 1)
	child := commitForTest("child", "root", "main", 2)
	fork := commitForTest("fork", "root", "alt", 2)
	for _, commit := range []Commit{root, child, fork} {
		if err := client.AppendCommit(context.Background(), commit); err != nil {
			t.Fatalf("AppendCommit(%q): %v", commit.CommitID, err)
		}
	}

	mu.Lock()
	got := append([]capturedCommitRequest(nil), requests...)
	mu.Unlock()
	if len(got) != 3 {
		t.Fatalf("request count = %d, want 3", len(got))
	}
	for index, request := range got {
		if request.query != insertCommit {
			t.Errorf("request %d query = %q, want commits_raw JSONEachRow insert", index, request.query)
		}
		var row map[string]any
		if err := json.Unmarshal(request.body, &row); err != nil {
			t.Fatalf("decode request %d: %v", index, err)
		}
		if _, ok := row["created_at"]; ok {
			t.Errorf("request %d includes created_at", index)
		}
		if _, ok := row["ingested_at"]; ok {
			t.Errorf("request %d includes ingested_at", index)
		}
		if gotID := row["commit_id"]; gotID != []string{"root", "child", "fork"}[index] {
			t.Errorf("request %d commit_id = %q", index, gotID)
		}
		if gotParent := row["parent_commit_id"]; gotParent != []string{"", "root", "root"}[index] {
			t.Errorf("request %d parent_commit_id = %q", index, gotParent)
		}
		if gotVersion := row["version_seq"]; gotVersion != float64([]uint64{1, 2, 2}[index]) {
			t.Errorf("request %d version_seq = %v", index, gotVersion)
		}
		if gotBranch := row["branch"]; gotBranch != []string{"main", "main", "alt"}[index] {
			t.Errorf("request %d branch = %q", index, gotBranch)
		}
	}

	mu.Lock()
	beforeInvalid := len(requests)
	mu.Unlock()
	for _, commit := range []Commit{
		commitForTest("bad-root", "", "main", 2),
		commitForTest("bad-child", "root", "main", 1),
		commitForTest("self", "self", "main", 2),
		commitForTest("orphan", "missing", "main", 2),
	} {
		if err := client.AppendCommit(context.Background(), commit); err == nil {
			t.Errorf("AppendCommit(%q) succeeded, want validation error", commit.CommitID)
		}
	}
	mu.Lock()
	afterInvalid := len(requests)
	mu.Unlock()
	if afterInvalid != beforeInvalid {
		t.Errorf("invalid commits sent %d requests, want none", afterInvalid-beforeInvalid)
	}
}

func TestAppendCommitReplaysFailedDelivery(t *testing.T) {
	t.Parallel()

	var (
		mu         sync.Mutex
		requests   []capturedCommitRequest
		replayOnce sync.Once
	)
	recovery := make(chan struct{})
	replayed := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("query") == selectCommit {
			w.WriteHeader(http.StatusOK)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		mu.Lock()
		requests = append(requests, capturedCommitRequest{query: r.URL.Query().Get("query"), body: body})
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

	client := newCommitTestClient(t, server.URL)
	defer client.Close()
	if err := client.AppendCommit(context.Background(), commitForTest("retry", "", "", 1)); !errors.Is(err, ErrPending) {
		t.Fatalf("AppendCommit retry error = %v, want ErrPending", err)
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
	select {
	case <-replayed:
	case <-time.After(3 * time.Second):
		t.Fatal("automatic reconciliation did not replay the recovered delivery")
	}
	if pending, err := client.Pending(); err != nil || pending != 0 {
		t.Fatalf("Pending after replay = %d, %v, want 0, nil", pending, err)
	}

	mu.Lock()
	got := append([]capturedCommitRequest(nil), requests...)
	mu.Unlock()
	if len(got) < 2 {
		t.Fatalf("request count = %d, want failed delivery and replay", len(got))
	}
	if got[0].query != insertCommit || got[len(got)-1].query != insertCommit {
		t.Errorf("replay queries = %q, %q, want commits_raw JSONEachRow insert", got[0].query, got[len(got)-1].query)
	}
	if string(got[0].body) != string(got[len(got)-1].body) {
		t.Error("replayed body differs from durable failed delivery")
	}
	var row map[string]any
	if err := json.Unmarshal(got[len(got)-1].body, &row); err != nil {
		t.Fatalf("decode replay: %v", err)
	}
	if row["branch"] != "main" {
		t.Errorf("default branch = %q, want main", row["branch"])
	}
}

func TestAppendCommitRejectsConflictingPendingIdentity(t *testing.T) {
	t.Parallel()

	var (
		mu              sync.Mutex
		deliveries      []Commit
		successfulSends int
	)
	recoverDelivery := make(chan struct{})
	delivered := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("query") {
		case selectCommit:
			w.WriteHeader(http.StatusOK)
		case insertCommit:
			var commit Commit
			if err := json.NewDecoder(r.Body).Decode(&commit); err != nil {
				t.Errorf("decode insert: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			select {
			case <-recoverDelivery:
				mu.Lock()
				deliveries = append(deliveries, commit)
				successfulSends++
				mu.Unlock()
				select {
				case <-delivered:
				default:
					close(delivered)
				}
				w.WriteHeader(http.StatusOK)
			default:
				w.WriteHeader(http.StatusServiceUnavailable)
			}
		default:
			t.Errorf("unexpected query %q", r.URL.Query().Get("query"))
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer server.Close()

	client := newCommitTestClient(t, server.URL)
	defer client.Close()
	first := commitForTest("pending", "", "main", 1)
	first.Message = "first immutable payload"
	if err := client.AppendCommit(context.Background(), first); !errors.Is(err, ErrPending) {
		t.Fatalf("first AppendCommit error = %v, want ErrPending", err)
	}
	if pending, err := client.Pending(); err != nil || pending != 1 {
		t.Fatalf("Pending after first outage = %d, %v, want 1, nil", pending, err)
	}

	entries, err := os.ReadDir(client.queue.dir)
	if err != nil {
		t.Fatalf("read durable queue: %v", err)
	}
	var journals []string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".json") {
			journals = append(journals, entry.Name())
		}
	}
	if len(journals) != 1 {
		t.Fatalf("durable journals = %d, want 1", len(journals))
	}
	journal, err := os.ReadFile(client.queue.dir + "/" + journals[0])
	if err != nil {
		t.Fatalf("read durable journal: %v", err)
	}
	var queued Entry
	if err := json.Unmarshal(journal, &queued); err != nil {
		t.Fatalf("decode durable journal: %v", err)
	}
	var queuedCommit Commit
	if err := json.Unmarshal(queued.Body, &queuedCommit); err != nil {
		t.Fatalf("decode durable commit: %v", err)
	}
	if queuedCommit != first {
		t.Fatalf("durable commit = %#v, want %#v", queuedCommit, first)
	}

	conflict := first
	conflict.Message = "conflicting payload"
	if err := client.AppendCommit(context.Background(), conflict); err == nil {
		t.Fatal("conflicting pending AppendCommit succeeded")
	} else if !strings.Contains(err.Error(), "already queued") {
		t.Errorf("conflicting pending error = %q, want already queued", err)
	}
	if pending, err := client.Pending(); err != nil || pending != 1 {
		t.Fatalf("Pending after conflict = %d, %v, want 1, nil", pending, err)
	}

	close(recoverDelivery)
	select {
	case <-delivered:
	case <-time.After(3 * time.Second):
		t.Fatal("automatic reconciliation did not deliver the first payload")
	}
	if pending, err := client.Pending(); err != nil || pending != 0 {
		t.Fatalf("Pending after recovery = %d, %v, want 0, nil", pending, err)
	}
	mu.Lock()
	gotDeliveries, gotSuccessfulSends := append([]Commit(nil), deliveries...), successfulSends
	mu.Unlock()
	if gotSuccessfulSends != 1 || len(gotDeliveries) != 1 {
		t.Fatalf("successful deliveries = %d, bodies = %d, want one", gotSuccessfulSends, len(gotDeliveries))
	}
	if gotDeliveries[0] != first {
		t.Errorf("recovered commit = %#v, want first payload %#v", gotDeliveries[0], first)
	}
}

func TestAppendCommitRejectsLaterAndCyclicParentsBeforeInsert(t *testing.T) {
	t.Parallel()

	var (
		mu      sync.Mutex
		inserts int
	)
	parents := map[string]storedCommit{
		"later": {CommitID: "later", ParentCommitID: "child", VersionSeq: 3},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("query") == selectCommit {
			parent, ok := parents[r.URL.Query().Get("param_commit_id")]
			if ok {
				if err := json.NewEncoder(w).Encode(parent); err != nil {
					t.Errorf("encode parent: %v", err)
				}
				return
			}
			w.WriteHeader(http.StatusOK)
			return
		}
		mu.Lock()
		inserts++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := newCommitTestClient(t, server.URL)
	defer client.Close()
	for _, commit := range []Commit{
		commitForTest("orphan", "missing", "main", 2),
		commitForTest("child", "later", "main", 2),
	} {
		if err := client.AppendCommit(context.Background(), commit); err == nil {
			t.Errorf("AppendCommit(%q) succeeded, want parent validation error", commit.CommitID)
		}
	}
	mu.Lock()
	gotInserts := inserts
	mu.Unlock()
	if gotInserts != 0 {
		t.Errorf("invalid parents sent %d insert requests, want none", gotInserts)
	}
}

func TestAppendCommitRejectsDuplicateIDBeforeInsert(t *testing.T) {
	t.Parallel()

	var (
		mu      sync.Mutex
		inserts int
		lookups int
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("query") {
		case selectCommit:
			if got := r.URL.Query().Get("param_dub_id"); got != "dub" {
				t.Errorf("duplicate lookup dub_id = %q, want dub", got)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if got := r.URL.Query().Get("param_commit_id"); got != "root" {
				t.Errorf("duplicate lookup commit_id = %q, want root", got)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			mu.Lock()
			lookups++
			mu.Unlock()
			if err := json.NewEncoder(w).Encode(storedCommit{CommitID: "root", VersionSeq: 1}); err != nil {
				t.Errorf("encode stored commit: %v", err)
			}
		case insertCommit:
			mu.Lock()
			inserts++
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
		default:
			t.Errorf("unexpected query %q", r.URL.Query().Get("query"))
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer server.Close()

	client := newCommitTestClient(t, server.URL)
	defer client.Close()
	duplicate := commitForTest("root", "", "rewritten", 1)
	if err := client.AppendCommit(context.Background(), duplicate); err == nil {
		t.Fatal("AppendCommit duplicate succeeded, want identity error")
	} else if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("AppendCommit duplicate error = %q, want already exists", err)
	}

	mu.Lock()
	gotLookups, gotInserts := lookups, inserts
	mu.Unlock()
	if gotLookups != 1 {
		t.Errorf("duplicate identity lookups = %d, want 1", gotLookups)
	}
	if gotInserts != 0 {
		t.Errorf("duplicate commit sent %d INSERT requests, want 0", gotInserts)
	}
}

func TestAppendCommitSerializesConcurrentDuplicateIdentity(t *testing.T) {
	t.Parallel()

	var (
		mu             sync.Mutex
		commits        = make(map[string]storedCommit)
		inserts        []Commit
		identityChecks int
	)
	firstIdentityCheck := make(chan struct{})
	releaseFirstCheck := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("query") {
		case selectCommit:
			identity := r.URL.Query().Get("param_commit_id")
			mu.Lock()
			identityChecks++
			check := identityChecks
			stored, found := commits[identity]
			mu.Unlock()
			if check == 1 {
				close(firstIdentityCheck)
				<-releaseFirstCheck
			}
			if found {
				if err := json.NewEncoder(w).Encode(stored); err != nil {
					t.Errorf("encode stored commit: %v", err)
				}
			}
		case insertCommit:
			var commit Commit
			if err := json.NewDecoder(r.Body).Decode(&commit); err != nil {
				t.Errorf("decode inserted commit: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			mu.Lock()
			inserts = append(inserts, commit)
			commits[commit.CommitID] = storedCommit{
				CommitID:       commit.CommitID,
				ParentCommitID: commit.ParentCommitID,
				VersionSeq:     commit.VersionSeq,
			}
			mu.Unlock()
		default:
			t.Errorf("unexpected query %q", r.URL.Query().Get("query"))
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer server.Close()

	client := newCommitTestClient(t, server.URL)
	defer client.Close()
	start := make(chan struct{})
	errs := make(chan error, 2)
	for _, message := range []string{"first payload", "conflicting payload"} {
		commit := commitForTest("same", "", "main", 1)
		commit.Message = message
		go func() {
			<-start
			errs <- client.AppendCommit(context.Background(), commit)
		}()
	}
	close(start)
	select {
	case <-firstIdentityCheck:
	case <-time.After(3 * time.Second):
		t.Fatal("first concurrent identity lookup did not arrive")
	}
	close(releaseFirstCheck)

	var results []error
	for range 2 {
		select {
		case err := <-errs:
			results = append(results, err)
		case <-time.After(3 * time.Second):
			t.Fatal("concurrent AppendCommit did not return")
		}
	}
	var accepted, rejected int
	for _, err := range results {
		if err == nil {
			accepted++
			continue
		}
		if strings.Contains(err.Error(), "already exists") {
			rejected++
			continue
		}
		t.Errorf("AppendCommit concurrent error = %v, want identity error", err)
	}
	if accepted != 1 || rejected != 1 {
		t.Errorf("concurrent outcomes accepted=%d rejected=%d, want 1 and 1", accepted, rejected)
	}
	mu.Lock()
	gotInserts, gotChecks := append([]Commit(nil), inserts...), identityChecks
	mu.Unlock()
	if len(gotInserts) != 1 {
		t.Fatalf("concurrent duplicate sent %d INSERT requests, want 1", len(gotInserts))
	}
	if gotInserts[0].Message != "first payload" && gotInserts[0].Message != "conflicting payload" {
		t.Errorf("inserted message = %q, want one supplied payload", gotInserts[0].Message)
	}
	if gotChecks != 2 {
		t.Errorf("identity lookups = %d, want 2", gotChecks)
	}
}

func newCommitTestClient(t *testing.T, endpoint string) *Client {
	t.Helper()
	client, err := New(&config.Config{
		ClickHouseHost:     "test.invalid",
		ClickHousePort:     8123,
		ClickHouseUser:     "test-user",
		ClickHousePassword: "test-password",
		ClickHouseDatabase: "test-db",
	}, t.TempDir(), WithEndpoint(endpoint))
	if err != nil {
		t.Fatalf("New ledger client: %v", err)
	}
	return client
}

func commitForTest(id, parent, branch string, version uint64) Commit {
	return Commit{
		CommitID:       id,
		ParentCommitID: parent,
		ProjectID:      "project",
		DubID:          "dub",
		OwnerID:        "owner",
		Branch:         branch,
		Language:       "ml",
		VersionSeq:     version,
		Message:        "test commit",
	}
}
