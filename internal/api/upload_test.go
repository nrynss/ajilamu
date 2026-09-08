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
)

func TestUploadHandlerStoresCompleteProject(t *testing.T) {
	storage := t.TempDir()
	handler := NewUploadHandler(storage)
	video := []byte("video bytes")
	music := []byte("music bytes")
	req := uploadRequest(t, func(form *multipart.Writer) {
		writeUploadFile(t, form, "video", "../../launch.MP4", video)
		writeUploadFile(t, form, "music", "../score.WAV", music)
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
