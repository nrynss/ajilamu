package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	editorcommand "github.com/nrynss/ajilamu/internal/command"
	"github.com/nrynss/ajilamu/internal/config"
)

func TestCommandPreviewHandlerReturnsGoDerivedSegment(t *testing.T) {
	request := CommandPreviewRequest{
		Command:  "shift line 3 right by 200ms.",
		Timeline: commandPreviewTimeline(),
	}
	response := previewCommandRequest(t, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	var preview CommandPreview
	if err := json.NewDecoder(response.Body).Decode(&preview); err != nil {
		t.Fatalf("decode preview: %v", err)
	}
	if preview.Mutation != (editorcommand.Mutation{Kind: editorcommand.Shift, SegmentID: 3, Anchor: editorcommand.Right, DeltaMs: 200}) {
		t.Fatalf("mutation = %#v", preview.Mutation)
	}
	if preview.Segment != (CommandPreviewSegment{ID: 3, StartMs: 7_200, EndMs: 8_200, DurationMs: 1_000, Speaker: "Maya"}) {
		t.Fatalf("segment = %#v", preview.Segment)
	}
	if preview.Summary != "Shift line 3 right by 0.200s." {
		t.Fatalf("summary = %q", preview.Summary)
	}
}

func TestCommandPreviewHandlerRejectsUnsafeTimelineMutations(t *testing.T) {
	tests := []struct {
		name    string
		command string
		want    string
	}{
		{name: "unknown line", command: "shorten line 99 by 0.5s.", want: "line 99 does not exist"},
		{name: "overlap", command: "shift line 3 right by 4500ms.", want: "would overlap line 7"},
		{name: "unknown speaker", command: "change speaker for line 7 to Nora.", want: "does not exist in this timeline"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := previewCommandRequest(t, CommandPreviewRequest{
				Command:  test.command,
				Timeline: commandPreviewTimeline(),
			})
			if response.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusUnprocessableEntity, response.Body.String())
			}
			if !strings.Contains(response.Body.String(), test.want) {
				t.Fatalf("body = %q, want %q", response.Body.String(), test.want)
			}
		})
	}
}

func TestCommandPreviewHandlerIsMountedOnTheServer(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("<main>fixture workspace</main>"), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}
	server, err := NewServer(&config.Config{Env: "development", Port: "0"}, ServerOptions{FrontendRoot: root})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)

	body, err := json.Marshal(CommandPreviewRequest{
		Command:  "shift line 3 right by 200ms.",
		Timeline: commandPreviewTimeline(),
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	response, err := http.Post(httpServer.URL+"/api/editor/commands/preview", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST preview: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusOK)
	}
	var preview CommandPreview
	if err := json.NewDecoder(response.Body).Decode(&preview); err != nil {
		t.Fatalf("decode preview: %v", err)
	}
	if preview.Segment != (CommandPreviewSegment{ID: 3, StartMs: 7_200, EndMs: 8_200, DurationMs: 1_000, Speaker: "Maya"}) {
		t.Fatalf("mounted segment = %#v", preview.Segment)
	}
}

func TestCommandPreviewHandlerRejectsBadRequests(t *testing.T) {
	handler := NewCommandPreviewHandler()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/editor/commands/preview", nil))
	if response.Code != http.StatusMethodNotAllowed || response.Header().Get("Allow") != http.MethodPost {
		t.Fatalf("GET status and Allow = %d, %q", response.Code, response.Header().Get("Allow"))
	}

	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/editor/commands/preview", strings.NewReader(`{"command":"shorten line 4 by 0.5s.","extra":true}`)))
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("unknown field status = %d, want %d", response.Code, http.StatusUnprocessableEntity)
	}
}

func previewCommandRequest(t *testing.T, input CommandPreviewRequest) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	response := httptest.NewRecorder()
	NewCommandPreviewHandler().ServeHTTP(response, httptest.NewRequest(
		http.MethodPost,
		"/api/editor/commands/preview",
		bytes.NewReader(body),
	))
	return response
}

func commandPreviewTimeline() editorcommand.Timeline {
	return editorcommand.Timeline{
		DurationMs: 20_000,
		Segments: []editorcommand.Segment{
			{ID: 1, StartMs: 1_000, EndMs: 2_000, Speaker: "Ada"},
			{ID: 2, StartMs: 3_000, EndMs: 4_000, Speaker: "Mark Vande Hei"},
			{ID: 3, StartMs: 7_000, EndMs: 8_000, Speaker: "Maya"},
			{ID: 4, StartMs: 16_000, EndMs: 18_000, Speaker: "Ada"},
			{ID: 7, StartMs: 12_000, EndMs: 13_000, Speaker: "Ada"},
		},
	}
}
