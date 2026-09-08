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

	"github.com/nrynss/ajilamu/internal/api"
	"github.com/nrynss/ajilamu/internal/config"
)

type capturedActionRequest struct {
	query string
	body  []byte
}

func TestRecordActionWritesRecognizedTypes(t *testing.T) {
	t.Parallel()

	var (
		mu       sync.Mutex
		requests []capturedActionRequest
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureActionRequest(t, &requests, &mu, w, r)
	}))
	defer server.Close()

	client := newActionTestClient(t, server.URL)
	defer client.Close()

	boundaryBefore := `{"start_ms":1000,"end_ms":2000}`
	boundaryAfter := `{"start_ms":1200,"end_ms":2000}`
	cases := []Action{
		actionForTest("act-segment", api.ActionSegmentCreated, api.AuthorAgent, -1, "", "", "", `{"segments":8}`),
		actionForTest("act-boundary", api.ActionBoundaryNudged, api.AuthorManualUI, 3, "", "", boundaryBefore, boundaryAfter),
		actionForTest("act-speaker", api.ActionSpeakerReassigned, api.AuthorManualUI, 2, "", "", `"Ada"`, `"Ben"`),
		actionForTest("act-text", api.ActionTextCorrected, api.AuthorManualUI, 1, "", "", `"old line"`, `"new line"`),
		actionForTest("act-take", api.ActionTakeRendered, api.AuthorAgent, 4, "take-4", "", "", `"take-4"`),
		actionForTest("act-stretch", api.ActionAtempoStretched, api.AuthorAgent, 3, "take-3-stretch", "", `{"measured_ms":5720}`, `{"measured_ms":5338}`),
		actionForTest("act-rewrite", api.ActionLineRewritten, api.AuthorAgent, 5, "take-5", "", `"first phrasing"`, `"shorter phrasing"`),
		actionForTest("act-command", api.ActionUserCommand, api.AuthorCommandBar, -1, "", "nudge line 2 left", "", ""),
	}
	if len(cases) != len(recognizedActionTypes) {
		t.Fatalf("test covers %d types, recognized set has %d", len(cases), len(recognizedActionTypes))
	}
	seen := make(map[string]struct{}, len(cases))
	for _, action := range cases {
		seen[action.ActionType] = struct{}{}
	}
	for actionType := range recognizedActionTypes {
		if _, ok := seen[actionType]; !ok {
			t.Fatalf("recognized action_type %q has no capture", actionType)
		}
	}
	for _, action := range cases {
		if err := client.RecordAction(context.Background(), action); err != nil {
			t.Fatalf("RecordAction(%q): %v", action.ActionType, err)
		}
	}

	mu.Lock()
	got := append([]capturedActionRequest(nil), requests...)
	mu.Unlock()
	if len(got) != len(cases) {
		t.Fatalf("request count = %d, want %d", len(got), len(cases))
	}
	for index, request := range got {
		if request.query != insertAction {
			t.Errorf("request %d query = %q, want actions_raw JSONEachRow insert", index, request.query)
		}
		if strings.Contains(request.query, "INSERT INTO actions ") {
			t.Errorf("request %d inserted into the actions view", index)
		}
		var row map[string]any
		if err := json.Unmarshal(request.body, &row); err != nil {
			t.Fatalf("decode request %d: %v", index, err)
		}
		want := cases[index]
		if row["action_type"] != want.ActionType {
			t.Errorf("request %d action_type = %q, want %q", index, row["action_type"], want.ActionType)
		}
		if row["author"] != want.Author {
			t.Errorf("request %d author = %q, want %q", index, row["author"], want.Author)
		}
		if row["prompt"] != want.Prompt {
			t.Errorf("request %d prompt = %q, want %q", index, row["prompt"], want.Prompt)
		}
		before, beforeOK := row["before_value"].(string)
		if !beforeOK {
			t.Fatalf("request %d before_value is %T, want string", index, row["before_value"])
		}
		if before != want.BeforeValue {
			t.Errorf("request %d before_value = %q, want %q", index, before, want.BeforeValue)
		}
		if before != "" && !json.Valid([]byte(before)) {
			t.Errorf("request %d before_value %q is not JSON text", index, before)
		}
		after, afterOK := row["after_value"].(string)
		if !afterOK {
			t.Fatalf("request %d after_value is %T, want string", index, row["after_value"])
		}
		if after != want.AfterValue {
			t.Errorf("request %d after_value = %q, want %q", index, after, want.AfterValue)
		}
		if after != "" && !json.Valid([]byte(after)) {
			t.Errorf("request %d after_value %q is not JSON text", index, after)
		}
		if row["commit_id"] != want.CommitID {
			t.Errorf("request %d commit_id = %q, want %q", index, row["commit_id"], want.CommitID)
		}
		if _, ok := row["created_at"]; ok {
			t.Errorf("request %d includes created_at", index)
		}
		if _, ok := row["ingested_at"]; ok {
			t.Errorf("request %d includes ingested_at", index)
		}
		if _, ok := row["event_key"]; ok {
			t.Errorf("request %d includes event_key", index)
		}
		if want.ActionType == api.ActionBoundaryNudged {
			if _, isObject := row["before_value"].(map[string]any); isObject {
				t.Errorf("request %d nested before_value into the row: %#v", index, row["before_value"])
			}
		}
		if want.ActionType == api.ActionAtempoStretched && row["action_type"] == "stretched" {
			t.Errorf("request %d stored stretched instead of %q", index, api.ActionAtempoStretched)
		}
	}
}

func TestRecordActionPreservesCommandBarPrompt(t *testing.T) {
	t.Parallel()

	var (
		mu       sync.Mutex
		requests []capturedActionRequest
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureActionRequest(t, &requests, &mu, w, r)
	}))
	defer server.Close()

	client := newActionTestClient(t, server.URL)
	defer client.Close()

	prompt := `  stretch line 3 to 5.2s, then say "cut!"  `
	action := actionForTest("act-verbatim", api.ActionUserCommand, api.AuthorCommandBar, -1, "", prompt, "", "")
	if err := client.RecordAction(context.Background(), action); err != nil {
		t.Fatalf("RecordAction command_bar: %v", err)
	}

	mu.Lock()
	got := append([]capturedActionRequest(nil), requests...)
	mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("request count = %d, want 1", len(got))
	}
	var row map[string]any
	if err := json.Unmarshal(got[0].body, &row); err != nil {
		t.Fatalf("decode command_bar body: %v", err)
	}
	stored, ok := row["prompt"].(string)
	if !ok {
		t.Fatalf("prompt field is %T, want string", row["prompt"])
	}
	if stored != prompt {
		t.Errorf("stored prompt = %q, want verbatim %q", stored, prompt)
	}
}

func TestRecordActionRejectsInvalidRowsBeforeTransport(t *testing.T) {
	t.Parallel()

	var (
		mu       sync.Mutex
		requests []capturedActionRequest
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureActionRequest(t, &requests, &mu, w, r)
	}))
	defer server.Close()

	client := newActionTestClient(t, server.URL)
	defer client.Close()

	valid := actionForTest("act-valid", api.ActionBoundaryNudged, api.AuthorManualUI, 1, "", "", `{"start_ms":0}`, `{"start_ms":10}`)
	cases := []struct {
		name   string
		action Action
		want   string
	}{
		{
			name:   "unknown action_type",
			action: withActionType(valid, "nudge_boundary"),
			want:   "unknown",
		},
		{
			name:   "project.md stretch_audio",
			action: withActionType(valid, "stretch_audio"),
			want:   "unknown",
		},
		{
			name:   "PHASE-4 stretched",
			action: withActionType(valid, "stretched"),
			want:   api.ActionAtempoStretched,
		},
		{
			name:   "unknown author",
			action: withAuthor(valid, "pipeline"),
			want:   "unknown",
		},
		{
			name:   "empty commit_id",
			action: withCommitID(valid, ""),
			want:   "commit_id",
		},
		{
			name:   "empty project_id",
			action: withProjectID(valid, ""),
			want:   "project_id",
		},
		{
			name:   "empty dub_id",
			action: withDubID(valid, ""),
			want:   "dub_id",
		},
		{
			name:   "empty owner_id",
			action: withOwnerID(valid, ""),
			want:   "owner_id",
		},
		{
			name:   "empty language",
			action: withLanguage(valid, ""),
			want:   "language",
		},
		{
			name:   "command_bar empty prompt",
			action: withAuthor(withActionType(valid, api.ActionUserCommand), api.AuthorCommandBar),
			want:   "prompt",
		},
		{
			name:   "segment_index below -1",
			action: withSegmentIndex(valid, -2),
			want:   "segment_index",
		},
		{
			name:   "invalid before_value JSON",
			action: withBeforeValue(valid, `{not json`),
			want:   "before_value",
		},
		{
			name:   "invalid after_value JSON",
			action: withAfterValue(valid, `{not json`),
			want:   "after_value",
		},
	}

	mu.Lock()
	before := len(requests)
	mu.Unlock()
	for _, tc := range cases {
		err := client.RecordAction(context.Background(), tc.action)
		if err == nil {
			t.Errorf("%s succeeded, want validation error", tc.name)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s error = %q, want %q", tc.name, err, tc.want)
		}
	}
	mu.Lock()
	after := len(requests)
	mu.Unlock()
	if after != before {
		t.Errorf("invalid actions sent %d requests, want none", after-before)
	}
}

func TestRecordActionReplaysFailedDelivery(t *testing.T) {
	t.Parallel()

	var (
		mu         sync.Mutex
		requests   []capturedActionRequest
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
		requests = append(requests, capturedActionRequest{query: r.URL.Query().Get("query"), body: body})
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

	client := newActionTestClient(t, server.URL)
	defer client.Close()
	action := actionForTest("act-retry", api.ActionTextCorrected, api.AuthorManualUI, 8, "", "", `"too short"`, `"still short"`)
	if err := client.RecordAction(context.Background(), action); !errors.Is(err, ErrPending) {
		t.Fatalf("RecordAction retry error = %v, want ErrPending", err)
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
		t.Fatal("replay did not deliver the recovered action")
	}
	if pending, err := client.Pending(); err != nil || pending != 0 {
		t.Fatalf("Pending after replay = %d, %v, want 0, nil", pending, err)
	}

	mu.Lock()
	got := append([]capturedActionRequest(nil), requests...)
	mu.Unlock()
	if len(got) < 2 {
		t.Fatalf("request count = %d, want failed delivery and replay", len(got))
	}
	if got[0].query != insertAction || got[len(got)-1].query != insertAction {
		t.Errorf("replay queries = %q, %q, want actions_raw JSONEachRow insert", got[0].query, got[len(got)-1].query)
	}
	if string(got[0].body) != string(got[len(got)-1].body) {
		t.Error("replayed body differs from durable failed delivery")
	}
	var row map[string]any
	if err := json.Unmarshal(got[len(got)-1].body, &row); err != nil {
		t.Fatalf("decode replay: %v", err)
	}
	if row["action_type"] != api.ActionTextCorrected {
		t.Errorf("replayed action_type = %q, want %q", row["action_type"], api.ActionTextCorrected)
	}
	if _, ok := row["created_at"]; ok {
		t.Error("replayed body includes created_at")
	}
}

func captureActionRequest(t *testing.T, requests *[]capturedActionRequest, mu *sync.Mutex, w http.ResponseWriter, r *http.Request) {
	t.Helper()
	query := r.URL.Query().Get("query")
	if query != insertAction {
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
	*requests = append(*requests, capturedActionRequest{query: query, body: body})
	mu.Unlock()
	w.WriteHeader(http.StatusOK)
}

func newActionTestClient(t *testing.T, endpoint string) *Client {
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

func actionForTest(id, actionType, author string, segment int32, takeID, prompt, before, after string) Action {
	return Action{
		ActionID:     id,
		CommitID:     "commit-1",
		ProjectID:    "project",
		DubID:        "dub",
		OwnerID:      "owner",
		Language:     "ml",
		SegmentIndex: segment,
		TakeID:       takeID,
		ActionType:   actionType,
		Author:       author,
		Prompt:       prompt,
		BeforeValue:  before,
		AfterValue:   after,
	}
}

func withActionType(action Action, actionType string) Action {
	action.ActionType = actionType
	return action
}

func withAuthor(action Action, author string) Action {
	action.Author = author
	return action
}

func withCommitID(action Action, commitID string) Action {
	action.CommitID = commitID
	return action
}

func withProjectID(action Action, projectID string) Action {
	action.ProjectID = projectID
	return action
}

func withDubID(action Action, dubID string) Action {
	action.DubID = dubID
	return action
}

func withOwnerID(action Action, ownerID string) Action {
	action.OwnerID = ownerID
	return action
}

func withLanguage(action Action, language string) Action {
	action.Language = language
	return action
}

func withSegmentIndex(action Action, segment int32) Action {
	action.SegmentIndex = segment
	return action
}

func withBeforeValue(action Action, before string) Action {
	action.BeforeValue = before
	return action
}

func withAfterValue(action Action, after string) Action {
	action.AfterValue = after
	return action
}
