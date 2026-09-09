package api_test

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nrynss/ajilamu/internal/api"
)

// takeAudioProject writes one recorded take under a project work directory and
// returns the storage root and the take bytes. The take is real WAV audio, so
// the served response carries an audio content type and a real byte count.
func takeAudioProject(t *testing.T, dubID string) (string, []byte) {
	t.Helper()
	storage := t.TempDir()
	workDir := filepath.Join(storage, dubID, "work", "ml-IN")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("create work directory: %v", err)
	}
	take := wavOfMs(250)
	if err := os.WriteFile(filepath.Join(workDir, "seg_3_try1.wav"), take, 0o644); err != nil {
		t.Fatalf("write take: %v", err)
	}
	return storage, take
}

// getTake issues one take request against the mounted server. The path is sent
// as written, so an encoded escape reaches the route as one segment.
func getTake(t *testing.T, base, path string) *http.Response {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, base+path, nil)
	if err != nil {
		t.Fatalf("build take request: %v", err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("get take: %v", err)
	}
	return response
}

// TestTakeAudioServesRecordedTake proves a real project's take reaches the
// browser from the served route, with an audio content type and every byte.
func TestTakeAudioServesRecordedTake(t *testing.T) {
	storage, take := takeAudioProject(t, "dub-audio")
	base := newRunTestServer(t, api.ServerOptions{StorageDir: storage})

	response := getTake(t, base, "/api/dubs/dub-audio/takes/ml-IN/seg_3_try1.wav")
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("take status = %d, want 200", response.StatusCode)
	}
	if contentType := response.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "audio/") {
		t.Errorf("take content type = %q, want an audio type", contentType)
	}
	if ranges := response.Header.Get("Accept-Ranges"); ranges != "bytes" {
		t.Errorf("accept ranges = %q, want bytes", ranges)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read take body: %v", err)
	}
	if !bytes.Equal(body, take) {
		t.Errorf("take body = %d bytes, want the recorded %d bytes", len(body), len(take))
	}
}

// TestTakeAudioAnswersRangeRequest proves a scrub gets 206 with Content-Range
// and only the requested bytes, so scrubbing never re-downloads the take.
func TestTakeAudioAnswersRangeRequest(t *testing.T) {
	storage, take := takeAudioProject(t, "dub-range")
	base := newRunTestServer(t, api.ServerOptions{StorageDir: storage})

	request, err := http.NewRequest(http.MethodGet, base+"/api/dubs/dub-range/takes/ml-IN/seg_3_try1.wav", nil)
	if err != nil {
		t.Fatalf("build range request: %v", err)
	}
	request.Header.Set("Range", "bytes=0-99")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("range request: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusPartialContent {
		t.Fatalf("range status = %d, want 206", response.StatusCode)
	}
	wantRange := fmt.Sprintf("bytes 0-99/%d", len(take))
	if got := response.Header.Get("Content-Range"); got != wantRange {
		t.Errorf("content range = %q, want %q", got, wantRange)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read range body: %v", err)
	}
	if !bytes.Equal(body, take[:100]) {
		t.Errorf("range body = %d bytes, want the first 100 bytes", len(body))
	}
}

// TestTakeAudioRefusesPathEscape proves a relative escape, an absolute path, a
// language escape, and a missing take all answer 404 and serve no file.
func TestTakeAudioRefusesPathEscape(t *testing.T) {
	storage, _ := takeAudioProject(t, "dub-escape")
	base := newRunTestServer(t, api.ServerOptions{StorageDir: storage})

	cases := []struct {
		name string
		path string
	}{
		{"relative escape", "/api/dubs/dub-escape/takes/ml-IN/..%2F..%2F..%2F..%2Fetc%2Fpasswd"},
		{"absolute path", "/api/dubs/dub-escape/takes/ml-IN/%2Fetc%2Fpasswd"},
		{"language escape", "/api/dubs/dub-escape/takes/..%2F..%2Fml-IN/seg_3_try1.wav"},
		{"missing take", "/api/dubs/dub-escape/takes/ml-IN/seg_9_try1.wav"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			response := getTake(t, base, test.path)
			defer response.Body.Close()
			if response.StatusCode != http.StatusNotFound {
				t.Errorf("status = %d, want 404", response.StatusCode)
			}
		})
	}
}

// TestTakeAudioRefusesSymlinkEscape proves a symlink inside the work directory
// cannot serve a file outside the storage root.
func TestTakeAudioRefusesSymlinkEscape(t *testing.T) {
	storage, _ := takeAudioProject(t, "dub-link")
	outsideDir := t.TempDir()
	secret := filepath.Join(outsideDir, "secret.wav")
	secretBytes := wavOfMs(250)
	if err := os.WriteFile(secret, secretBytes, 0o644); err != nil {
		t.Fatalf("write outside take: %v", err)
	}
	link := filepath.Join(storage, "dub-link", "work", "ml-IN", "escape.wav")
	if err := os.Symlink(secret, link); err != nil {
		t.Fatalf("create symlink: %v", err)
	}
	base := newRunTestServer(t, api.ServerOptions{StorageDir: storage})

	response := getTake(t, base, "/api/dubs/dub-link/takes/ml-IN/escape.wav")
	defer response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Errorf("symlink status = %d, want 404", response.StatusCode)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read symlink body: %v", err)
	}
	if bytes.Equal(body, secretBytes) {
		t.Errorf("symlink served the outside file, %d bytes", len(body))
	}
}

// TestTakeAudioUnavailableWithoutStorage proves an unwired server refuses the
// route instead of serving from an unknown root.
func TestTakeAudioUnavailableWithoutStorage(t *testing.T) {
	base := newRunTestServer(t, api.ServerOptions{})

	response := getTake(t, base, "/api/dubs/dub-none/takes/ml-IN/seg_3_try1.wav")
	defer response.Body.Close()
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", response.StatusCode)
	}
}
