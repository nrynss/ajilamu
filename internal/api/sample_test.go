package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSampleHandlerCreatesListedProject(t *testing.T) {
	storage := filepath.Join(t.TempDir(), "uploads")
	handler := NewSampleHandler(storage)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/dubs/sample", nil))

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusCreated, recorder.Body.String())
	}
	var upload Upload
	if err := json.NewDecoder(recorder.Body).Decode(&upload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(upload.ID) != 32 {
		t.Fatalf("project id length = %d, want 32", len(upload.ID))
	}
	if upload.Language != sampleLanguage {
		t.Fatalf("language = %q, want %q", upload.Language, sampleLanguage)
	}
	if upload.Video.Name != sampleTitle {
		t.Fatalf("video name = %q, want %q", upload.Video.Name, sampleTitle)
	}
	if filepath.IsAbs(upload.Video.Path) || strings.Contains(upload.Video.Path, "..") {
		t.Fatalf("video path = %q, want a safe relative path", upload.Video.Path)
	}

	storedPath := filepath.Join(storage, filepath.FromSlash(upload.Video.Path))
	assertFileMode(t, storedPath, 0o600)
	assertFileMode(t, filepath.Join(storage, upload.ID, uploadRecordName), 0o600)
	source := openCommittedSampleForTest(t)
	defer source.Close()
	stored, err := os.Open(storedPath)
	if err != nil {
		t.Fatalf("open stored sample: %v", err)
	}
	defer stored.Close()
	sourceHash, sourceBytes := hashFile(t, source)
	storedHash, storedBytes := hashFile(t, stored)
	if sourceHash != storedHash || sourceBytes != storedBytes {
		t.Fatalf("stored sample differs from committed clip")
	}
	if upload.Video.Bytes != sourceBytes {
		t.Fatalf("response bytes = %d, want %d", upload.Video.Bytes, sourceBytes)
	}

	summaries := ListUploadSummaries(storage)
	if len(summaries) != 1 || summaries[0].ID != upload.ID {
		t.Fatalf("summaries = %+v, want sample project %q", summaries, upload.ID)
	}
	if summaries[0].Title != sampleTitle {
		t.Fatalf("summary title = %q, want %q", summaries[0].Title, sampleTitle)
	}
	if got := strings.Join(summaries[0].Languages, ","); got != sampleLanguage {
		t.Fatalf("summary languages = %q, want %q", got, sampleLanguage)
	}
}

func TestSampleHandlerRejectsOtherMethods(t *testing.T) {
	recorder := httptest.NewRecorder()
	NewSampleHandler(t.TempDir()).ServeHTTP(
		recorder,
		httptest.NewRequest(http.MethodGet, "/api/dubs/sample", nil),
	)
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusMethodNotAllowed)
	}
	if got := recorder.Header().Get("Allow"); got != http.MethodPost {
		t.Fatalf("Allow = %q, want POST", got)
	}
}

func TestOpenSampleVideoHonorsOverride(t *testing.T) {
	override := filepath.Join(t.TempDir(), "sample.mp4")
	want := []byte("configured sample")
	if err := os.WriteFile(override, want, 0o600); err != nil {
		t.Fatalf("write override: %v", err)
	}
	t.Setenv("AJILAMU_SAMPLE_CLIP", override)

	file, err := openSampleVideo()
	if err != nil {
		t.Fatalf("open override: %v", err)
	}
	defer file.Close()
	got, err := io.ReadAll(file)
	if err != nil {
		t.Fatalf("read override: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("override bytes = %q, want %q", got, want)
	}
}

func TestSampleHandlerLogsMissingConfiguredClip(t *testing.T) {
	missingPath := filepath.Join(t.TempDir(), "missing.mp4")
	t.Setenv("AJILAMU_SAMPLE_CLIP", missingPath)
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	recorder := httptest.NewRecorder()
	NewSampleHandler(t.TempDir()).ServeHTTP(
		recorder,
		httptest.NewRequest(http.MethodPost, "/api/dubs/sample", nil),
	)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
	if body := recorder.Body.String(); body != "We could not find the sample video. Upload a video instead.\n" {
		t.Fatalf("body = %q, want creator-facing sentence", body)
	}
	logStr := logs.String()
	if !strings.Contains(logStr, "sample video could not be opened") {
		t.Fatalf("logs = %q, want missing sample failure", logStr)
	}
	if !strings.Contains(logStr, "AJILAMU_SAMPLE_CLIP") {
		t.Fatalf("logs = %q, want override variable name in log", logStr)
	}
}

func TestSampleHandlerLogsMissingUnsetClip(t *testing.T) {
	t.Setenv("AJILAMU_SAMPLE_CLIP", "")
	// Run from isolated directory where clip.mp4 does not exist.
	workDir := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(workDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	recorder := httptest.NewRecorder()
	NewSampleHandler(t.TempDir()).ServeHTTP(
		recorder,
		httptest.NewRequest(http.MethodPost, "/api/dubs/sample", nil),
	)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
	logStr := logs.String()
	if !strings.Contains(logStr, "clip not found in searched locations") {
		t.Fatalf("logs = %q, want candidate search paths in error log", logStr)
	}
}

func TestSampleHandlerLogsUnconfiguredStorage(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	recorder := httptest.NewRecorder()
	NewSampleHandler("").ServeHTTP(
		recorder,
		httptest.NewRequest(http.MethodPost, "/api/dubs/sample", nil),
	)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
	if !strings.Contains(logs.String(), "Sample storage directory is not configured") {
		t.Fatalf("logs = %q, want unconfigured storage log", logs.String())
	}
}

func openCommittedSampleForTest(t *testing.T) *os.File {
	t.Helper()
	file, err := os.Open(filepath.Join("..", "..", "testdata", "clip.mp4"))
	if err != nil {
		t.Fatalf("open committed sample: %v", err)
	}
	return file
}

func assertFileMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s permissions = %o, want %o", path, got, want)
	}
}

func hashFile(t *testing.T, file io.Reader) ([sha256.Size]byte, int64) {
	t.Helper()
	hash := sha256.New()
	bytes, err := io.Copy(hash, file)
	if err != nil {
		t.Fatalf("hash file: %v", err)
	}
	var sum [sha256.Size]byte
	copy(sum[:], hash.Sum(nil))
	return sum, bytes
}
