package api

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestUploadHandlerStoresCompleteProject(t *testing.T) {
	storage := t.TempDir()
	handler := NewUploadHandler(storage)
	video := []byte("video bytes")
	music := []byte("music bytes")
	req := uploadRequest(t, func(form *multipart.Writer) {
		writeUploadFile(t, form, "video", "../../launch.MP4", video)
		writeUploadFile(t, form, "music", "../score.WAV", music)
		writeUploadField(t, form, "source_language", "en-US")
		writeUploadField(t, form, "language", "ml")
	})
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusCreated, recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Fatalf("content type = %q, want application/json", got)
	}
	var upload Upload
	if err := json.NewDecoder(recorder.Body).Decode(&upload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(upload.ID) != 32 {
		t.Fatalf("project id length = %d, want 32", len(upload.ID))
	}
	if upload.SourceLanguage != "en-US" {
		t.Fatalf("source language = %q, want en-US", upload.SourceLanguage)
	}
	if upload.Language != "ml" {
		t.Fatalf("language = %q, want ml", upload.Language)
	}
	if upload.Music == nil {
		t.Fatal("music response is missing")
	}
	assertStoredUpload(t, storage, upload.Video, "launch.MP4", video)
	assertStoredUpload(t, storage, *upload.Music, "score.WAV", music)

	entries, err := os.ReadDir(storage)
	if err != nil {
		t.Fatalf("read storage: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != upload.ID {
		t.Fatalf("storage entries = %#v, want only %q", entries, upload.ID)
	}

	summaries := ListUploadSummaries(storage)
	if len(summaries) != 1 {
		t.Fatalf("summaries = %d, want 1", len(summaries))
	}
	if summaries[0].ID != upload.ID {
		t.Fatalf("summary id = %q, want %q", summaries[0].ID, upload.ID)
	}
	if summaries[0].Title != "launch.MP4" {
		t.Fatalf("summary title = %q, want launch.MP4", summaries[0].Title)
	}
	if got := strings.Join(summaries[0].Languages, ","); got != "ml" {
		t.Fatalf("summary languages = %q, want ml", got)
	}
	if summaries[0].Readiness != ReadinessPending {
		t.Fatalf("summary readiness = %q, want %s", summaries[0].Readiness, ReadinessPending)
	}
	if _, err := time.Parse(time.RFC3339, summaries[0].CreatedAt); err != nil {
		t.Fatalf("summary created_at = %q, want RFC 3339: %v", summaries[0].CreatedAt, err)
	}
	record := readUploadRecord(t, storage, upload.ID)
	if record.SourceLanguage != "en-US" || record.Language != "ml" {
		t.Fatalf("stored record languages = %q to %q, want en-US to ml", record.SourceLanguage, record.Language)
	}
}

// TestUploadHandlerAcceptsMissingSourceLanguage proves an upload that names no
// source language still stores a record. A record written before T7.5b carries
// none, so the read paths must keep working with an empty value.
func TestUploadHandlerAcceptsMissingSourceLanguage(t *testing.T) {
	storage := t.TempDir()
	handler := NewUploadHandler(storage)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, uploadRequest(t, func(form *multipart.Writer) {
		writeUploadFile(t, form, "video", "clip.mp4", []byte("video"))
		writeUploadField(t, form, "language", "ml")
	}))

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusCreated, recorder.Body.String())
	}
	var upload Upload
	if err := json.NewDecoder(recorder.Body).Decode(&upload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if upload.SourceLanguage != "" {
		t.Fatalf("source language = %q, want empty", upload.SourceLanguage)
	}
	record := readUploadRecord(t, storage, upload.ID)
	if record.SourceLanguage != "" {
		t.Fatalf("stored source language = %q, want empty", record.SourceLanguage)
	}
	if record.Language != "ml" {
		t.Fatalf("stored language = %q, want ml", record.Language)
	}
}

// readUploadRecord reads the persisted record of one project.
func readUploadRecord(t *testing.T, storage, id string) uploadRecord {
	t.Helper()
	payload, err := os.ReadFile(filepath.Join(storage, id, uploadRecordName))
	if err != nil {
		t.Fatalf("read upload record: %v", err)
	}
	var record uploadRecord
	if err := json.Unmarshal(payload, &record); err != nil {
		t.Fatalf("decode upload record: %v", err)
	}
	return record
}

func TestUploadHandlerCleansUpRejectedUploads(t *testing.T) {
	tests := []struct {
		name     string
		maxBytes int64
		build    func(t *testing.T, form *multipart.Writer)
		want     int
	}{
		{
			name: "unknown file field",
			build: func(t *testing.T, form *multipart.Writer) {
				writeUploadFile(t, form, "video", "video.mp4", []byte("video"))
				writeUploadFile(t, form, "subtitle", "captions.srt", []byte("caption"))
			},
			want: http.StatusBadRequest,
		},
		{
			name:     "request exceeds limit",
			maxBytes: 512,
			build: func(t *testing.T, form *multipart.Writer) {
				writeUploadFile(t, form, "video", "video.mp4", bytes.Repeat([]byte("x"), 1024))
				writeUploadField(t, form, "language", "ml")
			},
			want: http.StatusRequestEntityTooLarge,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			storage := t.TempDir()
			handler := NewUploadHandler(storage)
			handler.MaxBytes = test.maxBytes
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, uploadRequest(t, func(form *multipart.Writer) {
				test.build(t, form)
			}))

			if recorder.Code != test.want {
				t.Fatalf("status = %d, want %d: %s", recorder.Code, test.want, recorder.Body.String())
			}
			entries, err := os.ReadDir(storage)
			if err != nil {
				t.Fatalf("read storage: %v", err)
			}
			if len(entries) != 0 {
				t.Fatalf("rejected upload left storage entries: %#v", entries)
			}
		})
	}
}

func TestUploadHandlerRejectsInvalidRequests(t *testing.T) {
	handler := NewUploadHandler(t.TempDir())
	tests := []struct {
		name    string
		request *http.Request
		want    int
	}{
		{
			name:    "method",
			request: httptest.NewRequest(http.MethodGet, "/api/dubs/new", nil),
			want:    http.StatusMethodNotAllowed,
		},
		{
			name:    "content type",
			request: httptest.NewRequest(http.MethodPost, "/api/dubs/new", strings.NewReader("video")),
			want:    http.StatusUnsupportedMediaType,
		},
		{
			name: "missing video",
			request: uploadRequest(t, func(form *multipart.Writer) {
				writeUploadField(t, form, "language", "ml")
			}),
			want: http.StatusBadRequest,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, test.request)
			if recorder.Code != test.want {
				t.Fatalf("status = %d, want %d: %s", recorder.Code, test.want, recorder.Body.String())
			}
		})
	}
}

func uploadRequest(t *testing.T, fill func(*multipart.Writer)) *http.Request {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	fill(form)
	if err := form.Close(); err != nil {
		t.Fatalf("close multipart form: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/dubs/new", &body)
	req.Header.Set("Content-Type", form.FormDataContentType())
	return req
}

func writeUploadFile(t *testing.T, form *multipart.Writer, field, name string, contents []byte) {
	t.Helper()
	part, err := form.CreateFormFile(field, name)
	if err != nil {
		t.Fatalf("create %s part: %v", field, err)
	}
	if _, err := part.Write(contents); err != nil {
		t.Fatalf("write %s part: %v", field, err)
	}
}

func writeUploadField(t *testing.T, form *multipart.Writer, field, value string) {
	t.Helper()
	if err := form.WriteField(field, value); err != nil {
		t.Fatalf("write %s field: %v", field, err)
	}
}

func assertStoredUpload(t *testing.T, storage string, file UploadFile, wantName string, wantContents []byte) {
	t.Helper()
	if file.Name != wantName {
		t.Fatalf("file name = %q, want %q", file.Name, wantName)
	}
	if filepath.IsAbs(file.Path) || strings.Contains(file.Path, "..") || strings.Contains(file.Path, storage) {
		t.Fatalf("response path leaks storage location: %q", file.Path)
	}
	contents, err := os.ReadFile(filepath.Join(storage, file.Path))
	if err != nil {
		t.Fatalf("read stored upload: %v", err)
	}
	if !bytes.Equal(contents, wantContents) {
		t.Fatalf("stored contents = %q, want %q", contents, wantContents)
	}
	info, err := os.Stat(filepath.Join(storage, file.Path))
	if err != nil {
		t.Fatalf("stat stored upload: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("stored upload permissions = %o, want 600", got)
	}
	if file.Bytes != int64(len(wantContents)) {
		t.Fatalf("response bytes = %d, want %d", file.Bytes, len(wantContents))
	}
}

// TestUploadHandlerStoresCreatorTitle proves a named upload stores that name
// in the record the index and the workspace both read.
func TestUploadHandlerStoresCreatorTitle(t *testing.T) {
	storage := t.TempDir()
	recorder := httptest.NewRecorder()

	NewUploadHandler(storage).ServeHTTP(recorder, uploadRequest(t, func(form *multipart.Writer) {
		writeUploadFile(t, form, "video", "clip.mp4", []byte("video"))
		writeUploadField(t, form, "language", "ml")
		writeUploadField(t, form, "title", "  Launch Film  ")
	}))

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusCreated, recorder.Body.String())
	}
	var upload Upload
	if err := json.NewDecoder(recorder.Body).Decode(&upload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if upload.Title != "Launch Film" {
		t.Fatalf("response title = %q, want Launch Film", upload.Title)
	}
	record := readUploadRecord(t, storage, upload.ID)
	if record.Title != "Launch Film" {
		t.Fatalf("stored title = %q, want Launch Film", record.Title)
	}
	summaries := ListUploadSummaries(storage)
	if len(summaries) != 1 || summaries[0].Title != "Launch Film" {
		t.Fatalf("index titles = %#v, want one row named Launch Film", summaries)
	}
}

// TestUploadHandlerNamesProjectFromVideoWithoutTitle proves the filename
// fallback survives the naming feature.
func TestUploadHandlerNamesProjectFromVideoWithoutTitle(t *testing.T) {
	storage := t.TempDir()
	recorder := httptest.NewRecorder()

	NewUploadHandler(storage).ServeHTTP(recorder, uploadRequest(t, func(form *multipart.Writer) {
		writeUploadFile(t, form, "video", "clip.mp4", []byte("video"))
		writeUploadField(t, form, "language", "ml")
	}))

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusCreated, recorder.Body.String())
	}
	var upload Upload
	if err := json.NewDecoder(recorder.Body).Decode(&upload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if upload.Title != "clip.mp4" {
		t.Fatalf("response title = %q, want clip.mp4", upload.Title)
	}
	if record := readUploadRecord(t, storage, upload.ID); record.Title != "clip.mp4" {
		t.Fatalf("stored title = %q, want clip.mp4", record.Title)
	}
}

// TestUploadHandlerKeepsTitleMarkupAsText proves a name is stored as plain
// text rather than interpreted as markup.
func TestUploadHandlerKeepsTitleMarkupAsText(t *testing.T) {
	storage := t.TempDir()
	recorder := httptest.NewRecorder()
	const title = `<b>Final</b> & "best" cut`

	NewUploadHandler(storage).ServeHTTP(recorder, uploadRequest(t, func(form *multipart.Writer) {
		writeUploadFile(t, form, "video", "clip.mp4", []byte("video"))
		writeUploadField(t, form, "language", "ml")
		writeUploadField(t, form, "title", title)
	}))

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusCreated, recorder.Body.String())
	}
	var upload Upload
	if err := json.NewDecoder(recorder.Body).Decode(&upload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if record := readUploadRecord(t, storage, upload.ID); record.Title != title {
		t.Fatalf("stored title = %q, want %q", record.Title, title)
	}
}

// TestUploadHandlerRejectsOverlongTitle proves a name past the limit is
// refused before any project is written.
func TestUploadHandlerRejectsOverlongTitle(t *testing.T) {
	storage := t.TempDir()
	recorder := httptest.NewRecorder()

	NewUploadHandler(storage).ServeHTTP(recorder, uploadRequest(t, func(form *multipart.Writer) {
		writeUploadFile(t, form, "video", "clip.mp4", []byte("video"))
		writeUploadField(t, form, "language", "ml")
		writeUploadField(t, form, "title", strings.Repeat("a", maxProjectTitleRunes+1))
	}))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
	if got := strings.TrimSpace(recorder.Body.String()); got != tooLongTitleSentence() {
		t.Fatalf("sentence = %q, want %q", got, tooLongTitleSentence())
	}
	entries, err := os.ReadDir(storage)
	if err != nil {
		t.Fatalf("read storage: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("rejected upload left storage entries: %#v", entries)
	}
}

// TestRenameHandlerStoresNewTitle proves one rename updates the record the
// index and the workspace read, and leaves every other field alone.
func TestRenameHandlerStoresNewTitle(t *testing.T) {
	storage := t.TempDir()
	id := storeTestUpload(t, storage)
	before := readUploadRecord(t, storage, id)
	recorder := httptest.NewRecorder()

	NewRenameHandler(storage).ServeHTTP(recorder, renameRequestFor(t, id, `{"title":"  Festival Cut  "}`))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var answer ProjectTitle
	if err := json.NewDecoder(recorder.Body).Decode(&answer); err != nil {
		t.Fatalf("decode answer: %v", err)
	}
	if answer.ID != id || answer.Title != "Festival Cut" {
		t.Fatalf("answer = %#v, want id %q named Festival Cut", answer, id)
	}
	record := readUploadRecord(t, storage, id)
	if record.Title != "Festival Cut" {
		t.Fatalf("stored title = %q, want Festival Cut", record.Title)
	}
	if record.Language != before.Language || record.SourceLanguage != before.SourceLanguage || record.CreatedAt != before.CreatedAt {
		t.Fatalf("rename changed another field: before %#v after %#v", before, record)
	}
	summaries := ListUploadSummaries(storage)
	if len(summaries) != 1 || summaries[0].Title != "Festival Cut" {
		t.Fatalf("index titles = %#v, want one row named Festival Cut", summaries)
	}
}

// TestRenameHandlerKeepsMarkupAsText proves a renamed project stores the name
// as plain text.
func TestRenameHandlerKeepsMarkupAsText(t *testing.T) {
	storage := t.TempDir()
	id := storeTestUpload(t, storage)
	const title = `<script>alert(1)</script>`
	recorder := httptest.NewRecorder()

	NewRenameHandler(storage).ServeHTTP(recorder, renameRequestFor(t, id, `{"title":"`+title+`"}`))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if record := readUploadRecord(t, storage, id); record.Title != title {
		t.Fatalf("stored title = %q, want %q", record.Title, title)
	}
}

// TestRenameHandlerRejectsBlankTitle proves a blank name changes nothing and
// answers a plain sentence.
func TestRenameHandlerRejectsBlankTitle(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "empty", body: `{"title":""}`},
		{name: "spaces", body: `{"title":"   "}`},
		{name: "whitespace", body: "{\"title\":\" \\t\\n \"}"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			storage := t.TempDir()
			id := storeTestUpload(t, storage)
			recorder := httptest.NewRecorder()

			NewRenameHandler(storage).ServeHTTP(recorder, renameRequestFor(t, id, test.body))

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
			}
			if got := strings.TrimSpace(recorder.Body.String()); got != renameBlankSentence {
				t.Fatalf("sentence = %q, want %q", got, renameBlankSentence)
			}
			if record := readUploadRecord(t, storage, id); record.Title != "clip.mp4" {
				t.Fatalf("stored title = %q, want clip.mp4", record.Title)
			}
		})
	}
}

// TestRenameHandlerRejectsOverlongTitle proves a name past the limit changes
// nothing and answers a plain sentence.
func TestRenameHandlerRejectsOverlongTitle(t *testing.T) {
	storage := t.TempDir()
	id := storeTestUpload(t, storage)
	recorder := httptest.NewRecorder()
	body := `{"title":"` + strings.Repeat("a", maxProjectTitleRunes+1) + `"}`

	NewRenameHandler(storage).ServeHTTP(recorder, renameRequestFor(t, id, body))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
	if got := strings.TrimSpace(recorder.Body.String()); got != tooLongTitleSentence() {
		t.Fatalf("sentence = %q, want %q", got, tooLongTitleSentence())
	}
	if record := readUploadRecord(t, storage, id); record.Title != "clip.mp4" {
		t.Fatalf("stored title = %q, want clip.mp4", record.Title)
	}
}

// TestRenameHandlerRejectsUnknownProject proves a rename cannot invent a
// record for an id no upload wrote.
func TestRenameHandlerRejectsUnknownProject(t *testing.T) {
	storage := t.TempDir()
	recorder := httptest.NewRecorder()

	NewRenameHandler(storage).ServeHTTP(recorder, renameRequestFor(t, "absent", `{"title":"New"}`))

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusNotFound, recorder.Body.String())
	}
}

// TestRenameHandlerRejectsInvalidRequests proves the route is POST only and
// refuses a body that is not JSON.
func TestRenameHandlerRejectsInvalidRequests(t *testing.T) {
	storage := t.TempDir()
	handler := NewRenameHandler(storage)
	get := httptest.NewRequest(http.MethodGet, "/api/dubs/absent/title", nil)
	get.SetPathValue("id", "absent")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, get)
	if recorder.Code != http.StatusMethodNotAllowed || recorder.Header().Get("Allow") != http.MethodPost {
		t.Fatalf("GET = %d allow %q, want 405 POST", recorder.Code, recorder.Header().Get("Allow"))
	}

	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, renameRequestFor(t, "absent", "not json"))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("bad body = %d, want 400: %s", recorder.Code, recorder.Body.String())
	}
}

// storeTestUpload writes one stored project and returns its id.
func storeTestUpload(t *testing.T, storage string) string {
	t.Helper()
	recorder := httptest.NewRecorder()
	NewUploadHandler(storage).ServeHTTP(recorder, uploadRequest(t, func(form *multipart.Writer) {
		writeUploadFile(t, form, "video", "clip.mp4", []byte("video"))
		writeUploadField(t, form, "language", "ml")
	}))
	if recorder.Code != http.StatusCreated {
		t.Fatalf("upload status = %d, want 201: %s", recorder.Code, recorder.Body.String())
	}
	var upload Upload
	if err := json.NewDecoder(recorder.Body).Decode(&upload); err != nil {
		t.Fatalf("decode upload: %v", err)
	}
	return upload.ID
}

// renameRequestFor builds one rename request bound to a project id.
func renameRequestFor(t *testing.T, id, body string) *http.Request {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/dubs/"+id+"/title", strings.NewReader(body))
	request.SetPathValue("id", id)
	request.Header.Set("Content-Type", "application/json")
	return request
}

// TestUploadHandlerAnswersTitlePastFieldGuard proves a name past the field
// byte guard still reads as the naming limit rather than a store failure.
func TestUploadHandlerAnswersTitlePastFieldGuard(t *testing.T) {
	storage := t.TempDir()
	recorder := httptest.NewRecorder()

	NewUploadHandler(storage).ServeHTTP(recorder, uploadRequest(t, func(form *multipart.Writer) {
		writeUploadFile(t, form, "video", "clip.mp4", []byte("video"))
		writeUploadField(t, form, "language", "ml")
		writeUploadField(t, form, "title", strings.Repeat("a", 9<<10))
	}))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
	if got := strings.TrimSpace(recorder.Body.String()); got != tooLongTitleSentence() {
		t.Fatalf("sentence = %q, want %q", got, tooLongTitleSentence())
	}
}

// TestRenameHandlerAnswersTitlePastBodyGuard proves a rename body past the
// byte guard still reads as the naming limit rather than a read failure.
func TestRenameHandlerAnswersTitlePastBodyGuard(t *testing.T) {
	storage := t.TempDir()
	id := storeTestUpload(t, storage)
	recorder := httptest.NewRecorder()
	body := `{"title":"` + strings.Repeat("a", 9<<10) + `"}`

	NewRenameHandler(storage).ServeHTTP(recorder, renameRequestFor(t, id, body))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
	if got := strings.TrimSpace(recorder.Body.String()); got != tooLongTitleSentence() {
		t.Fatalf("sentence = %q, want %q", got, tooLongTitleSentence())
	}
	if record := readUploadRecord(t, storage, id); record.Title != "clip.mp4" {
		t.Fatalf("stored title = %q, want clip.mp4", record.Title)
	}
}
