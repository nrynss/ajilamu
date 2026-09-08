package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nrynss/ajilamu/internal/api"
)

// standInHistoryReader answers the history routes without a ledger.
// It records each call, so a rejected request can prove it never read.
type standInHistoryReader struct {
	commits       []api.Commit
	segments      []api.TimelineEntry
	comparison    api.BranchComparison
	err           error
	listCalls     int
	timelineCalls int
	compareCalls  int
}

func (s *standInHistoryReader) ListCommits(context.Context, string) ([]api.Commit, error) {
	s.listCalls++
	if s.err != nil {
		return nil, s.err
	}
	return s.commits, nil
}

func (s *standInHistoryReader) TimelineAt(context.Context, string, string, string) ([]api.TimelineEntry, error) {
	s.timelineCalls++
	if s.err != nil {
		return nil, s.err
	}
	return s.segments, nil
}

func (s *standInHistoryReader) CompareBranches(context.Context, string, string, string, string) (api.BranchComparison, error) {
	s.compareCalls++
	if s.err != nil {
		return api.BranchComparison{}, s.err
	}
	return s.comparison, nil
}

func (s *standInHistoryReader) calls() int {
	return s.listCalls + s.timelineCalls + s.compareCalls
}

// serveHistory runs one handler and asserts the headers every route shares.
func serveHistory(t *testing.T, handler http.Handler, target string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, target, nil)
	request.SetPathValue("id", "dub-1")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if got := response.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Fatalf("Content-Type = %q, want application/json; charset=utf-8", got)
	}
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	return response
}

// decodeHistoryBody decodes a JSON body and returns the raw text.
func decodeHistoryBody(t *testing.T, response *httptest.ResponseRecorder, target any) string {
	t.Helper()
	raw := response.Body.String()
	if err := json.Unmarshal([]byte(raw), target); err != nil {
		t.Fatalf("decode body %q: %v", raw, err)
	}
	return raw
}

func TestHistoryRouteServesCommits(t *testing.T) {
	reader := &standInHistoryReader{commits: []api.Commit{
		{
			CommitID:       "commit-2",
			ParentCommitID: "commit-1",
			VersionNumber:  2,
			CreatedAt:      "2026-09-08T10:00:00Z",
			Action:         api.ActionTakeRendered,
			Author:         api.AuthorAgent,
			Instruction:    "render line 3",
		},
	}}
	response := serveHistory(t, api.HistoryHandlerFrom(reader, nil), "/api/dubs/dub-1/history")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}

	var body api.DubHistory
	decodeHistoryBody(t, response, &body)
	if len(body.Commits) != 1 {
		t.Fatalf("commits = %d, want 1", len(body.Commits))
	}
	want := api.Commit{
		CommitID:       "commit-2",
		ParentCommitID: "commit-1",
		VersionNumber:  2,
		CreatedAt:      "2026-09-08T10:00:00Z",
		Action:         api.ActionTakeRendered,
		Author:         api.AuthorAgent,
		Instruction:    "render line 3",
	}
	if body.Commits[0] != want {
		t.Fatalf("commit = %+v, want %+v", body.Commits[0], want)
	}
	if reader.listCalls != 1 {
		t.Fatalf("list calls = %d, want 1", reader.listCalls)
	}
}

func TestHistoryRouteServesEmptyCommitList(t *testing.T) {
	response := serveHistory(t, api.HistoryHandlerFrom(&standInHistoryReader{}, nil), "/api/dubs/dub-1/history")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	if got := strings.TrimSpace(response.Body.String()); got != `{"commits":[]}` {
		t.Fatalf("body = %q, want an empty commit list", got)
	}
}

func TestTimelineRouteServesSegments(t *testing.T) {
	reader := &standInHistoryReader{segments: []api.TimelineEntry{
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
	}}
	response := serveHistory(t, api.TimelineHandlerFrom(reader, nil), "/api/dubs/dub-1/timeline?language=ml&commit=commit-1")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}

	var body api.TimelineView
	decodeHistoryBody(t, response, &body)
	if body.CommitID != "commit-1" || body.Language != "ml" {
		t.Fatalf("view = %+v, want commit-1 in ml", body)
	}
	if len(body.Segments) != 1 {
		t.Fatalf("segments = %d, want 1", len(body.Segments))
	}
	want := api.TimelineEntry{
		SegmentIndex: 3,
		StartMs:      1000,
		EndMs:        2500,
		Speaker:      "Narrator",
		Emotion:      "calm",
		SourceText:   "hello there",
		Text:         "namaskaram",
		TakeID:       "take-3",
		VersionSeq:   7,
	}
	if body.Segments[0] != want {
		t.Fatalf("segment = %+v, want %+v", body.Segments[0], want)
	}
	if reader.timelineCalls != 1 {
		t.Fatalf("timeline calls = %d, want 1", reader.timelineCalls)
	}
}

func TestBranchCompareRouteServesBothHeads(t *testing.T) {
	reader := &standInHistoryReader{comparison: api.BranchComparison{
		A: api.BranchSummary{CommitID: "commit-a", Branch: "main", SlotMs: 1200, TakeCount: 2, AttributedCostUSD: "1.230000"},
		B: api.BranchSummary{CommitID: "commit-b", Branch: "shorter", SlotMs: 1100, TakeCount: 3, AttributedCostUSD: "0.750000"},
	}}
	response := serveHistory(t, api.BranchCompareHandlerFrom(reader, nil), "/api/dubs/dub-1/branches?language=ml&a=commit-a&b=commit-b")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}

	var body api.BranchComparison
	decodeHistoryBody(t, response, &body)
	want := api.BranchComparison{
		A: api.BranchSummary{CommitID: "commit-a", Branch: "main", SlotMs: 1200, TakeCount: 2, AttributedCostUSD: "1.230000"},
		B: api.BranchSummary{CommitID: "commit-b", Branch: "shorter", SlotMs: 1100, TakeCount: 3, AttributedCostUSD: "0.750000"},
	}
	if body != want {
		t.Fatalf("comparison = %+v, want %+v", body, want)
	}
	if reader.compareCalls != 1 {
		t.Fatalf("compare calls = %d, want 1", reader.compareCalls)
	}
}

func TestHistoryRoutesRejectMissingParameters(t *testing.T) {
	tests := []struct {
		name      string
		handler   func(api.HistoryReader) http.Handler
		target    string
		parameter string
	}{
		{"timeline missing language", func(reader api.HistoryReader) http.Handler {
			return api.TimelineHandlerFrom(reader, nil)
		}, "/api/dubs/dub-1/timeline?commit=commit-1", "language"},
		{"timeline blank language", func(reader api.HistoryReader) http.Handler {
			return api.TimelineHandlerFrom(reader, nil)
		}, "/api/dubs/dub-1/timeline?language=%20&commit=commit-1", "language"},
		{"timeline missing commit", func(reader api.HistoryReader) http.Handler {
			return api.TimelineHandlerFrom(reader, nil)
		}, "/api/dubs/dub-1/timeline?language=ml", "commit"},
		{"timeline blank commit", func(reader api.HistoryReader) http.Handler {
			return api.TimelineHandlerFrom(reader, nil)
		}, "/api/dubs/dub-1/timeline?language=ml&commit=%20", "commit"},
		{"branches missing language", func(reader api.HistoryReader) http.Handler {
			return api.BranchCompareHandlerFrom(reader, nil)
		}, "/api/dubs/dub-1/branches?a=commit-a&b=commit-b", "language"},
		{"branches missing a", func(reader api.HistoryReader) http.Handler {
			return api.BranchCompareHandlerFrom(reader, nil)
		}, "/api/dubs/dub-1/branches?language=ml&b=commit-b", "a"},
		{"branches blank a", func(reader api.HistoryReader) http.Handler {
			return api.BranchCompareHandlerFrom(reader, nil)
		}, "/api/dubs/dub-1/branches?language=ml&a=%20&b=commit-b", "a"},
		{"branches missing b", func(reader api.HistoryReader) http.Handler {
			return api.BranchCompareHandlerFrom(reader, nil)
		}, "/api/dubs/dub-1/branches?language=ml&a=commit-a", "b"},
		{"branches blank b", func(reader api.HistoryReader) http.Handler {
			return api.BranchCompareHandlerFrom(reader, nil)
		}, "/api/dubs/dub-1/branches?language=ml&a=commit-a&b=%20", "b"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reader := &standInHistoryReader{}
			response := serveHistory(t, test.handler(reader), test.target)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", response.Code)
			}
			var body struct {
				Error string `json:"error"`
			}
			decodeHistoryBody(t, response, &body)
			if !strings.Contains(body.Error, test.parameter) {
				t.Fatalf("error = %q, want it to name %q", body.Error, test.parameter)
			}
			if reader.calls() != 0 {
				t.Fatalf("reader calls = %d, want 0 for a rejected request", reader.calls())
			}
		})
	}
}

func TestHistoryRoutesUnavailableWithoutReader(t *testing.T) {
	tests := []struct {
		name    string
		handler http.Handler
		target  string
	}{
		{"history", api.HistoryHandlerFrom(nil, nil), "/api/dubs/dub-1/history"},
		{"timeline", api.TimelineHandlerFrom(nil, nil), "/api/dubs/dub-1/timeline?language=ml&commit=commit-1"},
		{"branches", api.BranchCompareHandlerFrom(nil, nil), "/api/dubs/dub-1/branches?language=ml&a=commit-a&b=commit-b"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := serveHistory(t, test.handler, test.target)
			if response.Code != http.StatusServiceUnavailable {
				t.Fatalf("status = %d, want 503", response.Code)
			}
			var body struct {
				Error string `json:"error"`
			}
			decodeHistoryBody(t, response, &body)
			if body.Error != "Ledger history is unavailable." {
				t.Fatalf("error = %q", body.Error)
			}
		})
	}
}

func TestHistoryRoutesHideReaderErrors(t *testing.T) {
	internal := errors.New("clickhouse rejected the credential")
	tests := []struct {
		name    string
		handler func(api.HistoryReader, *slog.Logger) http.Handler
		target  string
	}{
		{"history", api.HistoryHandlerFrom, "/api/dubs/dub-1/history"},
		{"timeline", api.TimelineHandlerFrom, "/api/dubs/dub-1/timeline?language=ml&commit=commit-1"},
		{"branches", api.BranchCompareHandlerFrom, "/api/dubs/dub-1/branches?language=ml&a=commit-a&b=commit-b"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var logs bytes.Buffer
			logger := slog.New(slog.NewTextHandler(&logs, nil))
			reader := &standInHistoryReader{err: internal}
			response := serveHistory(t, test.handler(reader, logger), test.target)
			if response.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want 500", response.Code)
			}
			var body struct {
				Error string `json:"error"`
			}
			raw := decodeHistoryBody(t, response, &body)
			if body.Error != "Could not read the ledger history." {
				t.Fatalf("error = %q", body.Error)
			}
			if strings.Contains(raw, internal.Error()) {
				t.Fatalf("response leaked the internal error: %q", raw)
			}
			if !strings.Contains(logs.String(), internal.Error()) {
				t.Fatalf("log = %q, want the internal detail", logs.String())
			}
		})
	}
}
