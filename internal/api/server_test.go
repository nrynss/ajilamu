package api_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nrynss/ajilamu/internal/api"
	"github.com/nrynss/ajilamu/internal/config"
	"github.com/nrynss/ajilamu/internal/ledger"
)

func TestStaticHandlerServesSPARoutesAndVideoRanges(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("<main>fixture workspace</main>"), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}
	copyFixture(t, filepath.Join("..", "..", "testdata", "clip.mp4"), filepath.Join(root, "clip.mp4"))

	handler, err := api.NewStaticHandler(root)
	if err != nil {
		t.Fatalf("NewStaticHandler: %v", err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	response, err := http.Get(server.URL + "/d/fixture")
	if err != nil {
		t.Fatalf("get workspace route: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("workspace status = %d, want 200", response.StatusCode)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read workspace: %v", err)
	}
	if !strings.Contains(string(body), "fixture workspace") {
		t.Fatalf("workspace body = %q", body)
	}

	request, err := http.NewRequest(http.MethodGet, server.URL+"/clip.mp4", nil)
	if err != nil {
		t.Fatalf("new range request: %v", err)
	}
	request.Header.Set("Range", "bytes=0-63")
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("get video range: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusPartialContent {
		t.Fatalf("video status = %d, want %d", response.StatusCode, http.StatusPartialContent)
	}
	if got := response.Header.Get("Content-Range"); !strings.HasPrefix(got, "bytes 0-63/") {
		t.Fatalf("Content-Range = %q", got)
	}
	bytes, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read video range: %v", err)
	}
	if len(bytes) != 64 {
		t.Fatalf("range length = %d, want 64", len(bytes))
	}
}

func TestServerDefersAbsentCredentialsWithoutStopping(t *testing.T) {
	cfg, err := config.LoadFromMap(map[string]string{})
	if err != nil {
		t.Fatalf("load no-environment config: %v", err)
	}
	server, err := api.NewServer(cfg, api.ServerOptions{FrontendRoot: testFrontend(t)})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)

	response, err := http.Get(httpServer.URL + "/api/ledger/ready")
	if err != nil {
		t.Fatalf("request deferred ledger feature: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("ledger readiness status = %d, want 503", response.StatusCode)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read ledger readiness: %v", err)
	}
	if !strings.Contains(string(body), "CLICKHOUSE_HOST") {
		t.Fatalf("credential error = %q", body)
	}

	response, err = http.Get(httpServer.URL + "/api/healthz")
	if err != nil {
		t.Fatalf("request health after credential failure: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("health status = %d, want 200", response.StatusCode)
	}
}

func TestServerMountsUploadHandlerOnPersistentStorage(t *testing.T) {
	storage := filepath.Join(t.TempDir(), "uploads")
	cfg, err := config.LoadFromMap(map[string]string{})
	if err != nil {
		t.Fatalf("load no-environment config: %v", err)
	}
	server, err := api.NewServer(cfg, api.ServerOptions{
		FrontendRoot: testFrontend(t),
		Upload:       api.NewUploadHandler(storage),
		Index: api.IndexHandlerFrom(func() []api.DubSummary {
			return api.ListUploadSummaries(storage)
		}),
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("language", "Malayalam"); err != nil {
		t.Fatalf("write language: %v", err)
	}
	video, err := writer.CreateFormFile("video", "source.mp4")
	if err != nil {
		t.Fatalf("create video part: %v", err)
	}
	if _, err := video.Write([]byte("fixture video")); err != nil {
		t.Fatalf("write video: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart request: %v", err)
	}

	response, err := http.Post(httpServer.URL+"/api/dubs/new", writer.FormDataContentType(), &body)
	if err != nil {
		t.Fatalf("post upload: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		payload, _ := io.ReadAll(response.Body)
		t.Fatalf("upload status = %d, want %d: %s", response.StatusCode, http.StatusCreated, payload)
	}
	var upload api.Upload
	if err := json.NewDecoder(response.Body).Decode(&upload); err != nil {
		t.Fatalf("decode upload response: %v", err)
	}
	if upload.ID == "" || filepath.IsAbs(upload.Video.Path) {
		t.Fatalf("upload response = %+v, want project-relative persisted video", upload)
	}
	stored, err := os.ReadFile(filepath.Join(storage, upload.Video.Path))
	if err != nil {
		t.Fatalf("read persisted video: %v", err)
	}
	if string(stored) != "fixture video" {
		t.Fatalf("persisted video = %q, want fixture content", stored)
	}

	indexResponse, err := http.Get(httpServer.URL + "/api/dubs")
	if err != nil {
		t.Fatalf("get index: %v", err)
	}
	defer indexResponse.Body.Close()
	if indexResponse.StatusCode != http.StatusOK {
		t.Fatalf("index status = %d, want %d", indexResponse.StatusCode, http.StatusOK)
	}
	var index api.DubIndex
	if err := json.NewDecoder(indexResponse.Body).Decode(&index); err != nil {
		t.Fatalf("decode index: %v", err)
	}
	if len(index.Dubs) != 1 || index.Dubs[0].ID != upload.ID {
		t.Fatalf("index dubs = %+v, want uploaded project %q", index.Dubs, upload.ID)
	}
}

func TestServerMountsSampleHandlerOnPersistentStorage(t *testing.T) {
	storage := filepath.Join(t.TempDir(), "uploads")
	cfg, err := config.LoadFromMap(map[string]string{})
	if err != nil {
		t.Fatalf("load no-environment config: %v", err)
	}
	server, err := api.NewServer(cfg, api.ServerOptions{
		FrontendRoot: testFrontend(t),
		Sample:       api.NewSampleHandler(storage),
		Index: api.IndexHandlerFrom(func() []api.DubSummary {
			return api.ListUploadSummaries(storage)
		}),
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)

	response, err := http.Post(httpServer.URL+"/api/dubs/sample", "", nil)
	if err != nil {
		t.Fatalf("post sample: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		payload, _ := io.ReadAll(response.Body)
		t.Fatalf("sample status = %d, want %d: %s", response.StatusCode, http.StatusCreated, payload)
	}
	var sample api.Upload
	if err := json.NewDecoder(response.Body).Decode(&sample); err != nil {
		t.Fatalf("decode sample response: %v", err)
	}
	if sample.ID == "" || sample.Video.Name != "NASA 75-second clip" {
		t.Fatalf("sample response = %+v, want named persisted project", sample)
	}
	stored, err := os.Stat(filepath.Join(storage, filepath.FromSlash(sample.Video.Path)))
	if err != nil {
		t.Fatalf("stat persisted sample: %v", err)
	}
	if stored.Size() != sample.Video.Bytes || stored.Size() == 0 {
		t.Fatalf("persisted bytes = %d, response bytes = %d", stored.Size(), sample.Video.Bytes)
	}

	indexResponse, err := http.Get(httpServer.URL + "/api/dubs")
	if err != nil {
		t.Fatalf("get sample index: %v", err)
	}
	defer indexResponse.Body.Close()
	var index api.DubIndex
	if err := json.NewDecoder(indexResponse.Body).Decode(&index); err != nil {
		t.Fatalf("decode sample index: %v", err)
	}
	if len(index.Dubs) != 1 || index.Dubs[0].ID != sample.ID || index.Dubs[0].Title != "NASA 75-second clip" {
		t.Fatalf("index dubs = %+v, want sample project %q", index.Dubs, sample.ID)
	}
}

func TestServerRejectsUnmatchedAPIRoutesBeforeSPA(t *testing.T) {
	cfg, err := config.LoadFromMap(map[string]string{})
	if err != nil {
		t.Fatalf("load no-environment config: %v", err)
	}
	server, err := api.NewServer(cfg, api.ServerOptions{
		FrontendRoot: testFrontend(t),
		Index: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
		Config: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}),
		Upload: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusCreated)
		}),
		Sample: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusCreated)
		}),
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)

	for _, test := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/dubs/new"},
		{http.MethodGet, "/api/dubs/sample"},
		{http.MethodPost, "/api/dubs"},
		{http.MethodGet, "/api/config"},
		{http.MethodPost, "/api/healthz"},
		{http.MethodGet, "/api/nonexistent"},
	} {
		request, err := http.NewRequest(test.method, httpServer.URL+test.path, nil)
		if err != nil {
			t.Fatalf("new %s %s request: %v", test.method, test.path, err)
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatalf("%s %s: %v", test.method, test.path, err)
		}
		body, readErr := io.ReadAll(response.Body)
		response.Body.Close()
		if readErr != nil {
			t.Fatalf("read %s %s response: %v", test.method, test.path, readErr)
		}
		if response.StatusCode != http.StatusNotFound && response.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("%s %s status = %d, want 404 or 405", test.method, test.path, response.StatusCode)
		}
		if strings.Contains(string(body), "fixture workspace") {
			t.Errorf("%s %s returned SPA body", test.method, test.path)
		}
	}

	for _, test := range []struct {
		method string
		path   string
		status int
	}{
		{http.MethodPost, "/api/dubs/new", http.StatusCreated},
		{http.MethodPost, "/api/dubs/sample", http.StatusCreated},
		{http.MethodGet, "/api/dubs", http.StatusOK},
		{http.MethodGet, "/api/healthz", http.StatusOK},
	} {
		request, err := http.NewRequest(test.method, httpServer.URL+test.path, nil)
		if err != nil {
			t.Fatalf("new %s %s request: %v", test.method, test.path, err)
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatalf("%s %s: %v", test.method, test.path, err)
		}
		response.Body.Close()
		if response.StatusCode != test.status {
			t.Errorf("%s %s status = %d, want %d", test.method, test.path, response.StatusCode, test.status)
		}
	}
}

func TestServerMountsHistoryRoutes(t *testing.T) {
	reader := &standInHistoryReader{
		commits: []api.Commit{{
			CommitID:      "commit-1",
			VersionNumber: 1,
			CreatedAt:     "2026-09-08T09:00:00Z",
		}},
		segments: []api.TimelineEntry{{
			SegmentIndex: 1,
			StartMs:      0,
			EndMs:        1200,
			VersionSeq:   2,
		}},
		comparison: api.BranchComparison{
			A: api.BranchSummary{CommitID: "commit-a", Branch: "main", AttributedCostUSD: "1.230000"},
			B: api.BranchSummary{CommitID: "commit-b", Branch: "shorter", AttributedCostUSD: "0.750000"},
		},
	}
	cfg, err := config.LoadFromMap(map[string]string{})
	if err != nil {
		t.Fatalf("load no-environment config: %v", err)
	}
	server, err := api.NewServer(cfg, api.ServerOptions{
		FrontendRoot: testFrontend(t),
		History:      reader,
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)

	for _, test := range []struct {
		path   string
		marker string
	}{
		{"/api/dubs/dub-1/history", `"commits":[{"commit_id":"commit-1"`},
		{"/api/dubs/dub-1/timeline?language=ml&commit=commit-1", `"segments":[{"segment_index":1`},
		{"/api/dubs/dub-1/branches?language=ml&a=commit-a&b=commit-b", `"a":{"commit_id":"commit-a"`},
	} {
		response, err := http.Get(httpServer.URL + test.path)
		if err != nil {
			t.Fatalf("get %s: %v", test.path, err)
		}
		body, readErr := io.ReadAll(response.Body)
		response.Body.Close()
		if readErr != nil {
			t.Fatalf("read %s: %v", test.path, readErr)
		}
		if response.StatusCode != http.StatusOK {
			t.Errorf("GET %s status = %d, want 200", test.path, response.StatusCode)
		}
		if got := response.Header.Get("Cache-Control"); got != "no-store" {
			t.Errorf("GET %s Cache-Control = %q, want no-store", test.path, got)
		}
		if !strings.Contains(string(body), test.marker) {
			t.Errorf("GET %s body = %q, want %q", test.path, body, test.marker)
		}
	}

	// Every route is mounted without a reader, so a clone with no credentials
	// answers 503 and the tab keeps its fixture commits.
	unavailable, err := api.NewServer(cfg, api.ServerOptions{FrontendRoot: testFrontend(t)})
	if err != nil {
		t.Fatalf("NewServer without history: %v", err)
	}
	unavailableServer := httptest.NewServer(unavailable.Handler())
	t.Cleanup(unavailableServer.Close)
	for _, path := range []string{
		"/api/dubs/dub-1/history",
		"/api/dubs/dub-1/timeline?language=ml&commit=commit-1",
		"/api/dubs/dub-1/branches?language=ml&a=commit-a&b=commit-b",
	} {
		response, err := http.Get(unavailableServer.URL + path)
		if err != nil {
			t.Fatalf("get %s: %v", path, err)
		}
		body, readErr := io.ReadAll(response.Body)
		response.Body.Close()
		if readErr != nil {
			t.Fatalf("read %s: %v", path, readErr)
		}
		if response.StatusCode != http.StatusServiceUnavailable {
			t.Errorf("GET %s without history status = %d, want 503", path, response.StatusCode)
		}
		if !strings.Contains(string(body), "Ledger history is unavailable.") {
			t.Errorf("GET %s without history body = %q", path, body)
		}
	}
}

func TestEntrypointExitsCleanlyWithoutLedger(t *testing.T) {
	projectRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve project root: %v", err)
	}
	portListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve entrypoint port: %v", err)
	}
	_, port, err := net.SplitHostPort(portListener.Addr().String())
	if err != nil {
		portListener.Close()
		t.Fatalf("split entrypoint port: %v", err)
	}
	if err := portListener.Close(); err != nil {
		t.Fatalf("free entrypoint port: %v", err)
	}
	binary := filepath.Join(t.TempDir(), "ajilamu")
	build := exec.Command("go", "build", "-o", binary, "./cmd/ajilamu")
	build.Dir = projectRoot
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build entrypoint: %v: %s", err, output)
	}

	command := exec.Command(binary)
	command.Dir = projectRoot
	command.Env = []string{
		"AJILAMU_DATA_DIR=" + filepath.Join(t.TempDir(), "data"),
		"AJILAMU_FRONTEND_DIR=" + testFrontend(t),
		"AJILAMU_SAMPLE_CLIP=" + filepath.Join(projectRoot, "testdata", "clip.mp4"),
		"ENV=development",
		"PORT=" + port,
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		t.Fatalf("capture entrypoint stderr: %v", err)
	}
	if err := command.Start(); err != nil {
		t.Fatalf("start entrypoint: %v", err)
	}
	done := make(chan struct{})
	var exitErr error
	go func() {
		exitErr = command.Wait()
		close(done)
	}()
	lines := make(chan string, 8)
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
		close(lines)
	}()
	t.Cleanup(func() {
		select {
		case <-done:
		default:
			_ = command.Process.Kill()
			<-done
		}
	})

	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case line, ok := <-lines:
			if ok && strings.Contains(line, "Ajilamu listening") {
				goto listening
			}
			if !ok {
				select {
				case <-done:
					t.Fatalf("entrypoint ended before listening: %v", exitErr)
				default:
					t.Fatal("entrypoint closed stderr before listening")
				}
			}
		case <-done:
			t.Fatalf("entrypoint ended before listening: %v", exitErr)
		case <-deadline.C:
			t.Fatal("entrypoint did not start within 10 seconds")
		}
	}

listening:

	// Observe the running process with no MCP settings, beyond startup logs.
	agentResponse, err := http.Post("http://127.0.0.1:"+port+"/api/dubs/fixture/agent", "application/json", strings.NewReader(`{"question":"Which line?"}`))
	if err != nil {
		t.Fatal(err)
	}
	agentBody, readErr := io.ReadAll(agentResponse.Body)
	agentResponse.Body.Close()
	if readErr != nil || agentResponse.StatusCode != 503 || !strings.Contains(string(agentBody), "The editor agent is unavailable.") {
		t.Fatalf("entrypoint agent response: %d %s %v", agentResponse.StatusCode, agentBody, readErr)
	}
	workspaceResponse, err := http.Get("http://127.0.0.1:" + port + "/d/fixture")
	if err != nil {
		t.Fatal(err)
	}
	workspaceResponse.Body.Close()
	if workspaceResponse.StatusCode != 200 {
		t.Fatalf("entrypoint workspace status %d", workspaceResponse.StatusCode)
	}
	response, err := http.Post("http://127.0.0.1:"+port+"/api/dubs/sample", "", nil)
	if err != nil {
		t.Fatalf("post sample through entrypoint: %v", err)
	}
	var sample api.Upload
	decodeErr := json.NewDecoder(response.Body).Decode(&sample)
	response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("sample through entrypoint status = %d, want %d", response.StatusCode, http.StatusCreated)
	}
	if decodeErr != nil || sample.ID == "" {
		t.Fatalf("sample through entrypoint = %+v, %v", sample, decodeErr)
	}

	if err := command.Process.Signal(os.Interrupt); err != nil {
		t.Fatalf("signal entrypoint: %v", err)
	}
	select {
	case <-done:
		if exitErr != nil {
			t.Fatalf("entrypoint exit after interrupt: %v", exitErr)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("entrypoint did not stop after interrupt")
	}
}

func TestShutdownFlushesDurableLedgerQueue(t *testing.T) {
	var accept atomic.Bool
	clickhouse := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if !accept.Load() {
			http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(clickhouse.Close)

	cfg := &config.Config{
		ClickHouseHost:     "fixture.invalid",
		ClickHousePort:     8443,
		ClickHouseUser:     "fixture",
		ClickHousePassword: "fixture",
		ClickHouseDatabase: "fixture",
	}
	eventLedger, err := ledger.New(cfg, filepath.Join(t.TempDir(), "queue"), ledger.WithEndpoint(clickhouse.URL))
	if err != nil {
		t.Fatalf("new ledger: %v", err)
	}
	t.Cleanup(func() { _ = eventLedger.Close() })
	insert := "INSERT INTO fixture.events FORMAT JSONEachRow"
	err = eventLedger.EnqueueJSON(context.Background(), insert, map[string]string{"event": "shutdown"})
	if !errors.Is(err, ledger.ErrPending) {
		t.Fatalf("enqueue error = %v, want durable pending event", err)
	}
	if pending, err := eventLedger.Pending(); err != nil || pending != 1 {
		t.Fatalf("pending before shutdown = %d, %v, want 1", pending, err)
	}

	accept.Store(true)
	server, err := api.NewServer(&config.Config{Port: "0"}, api.ServerOptions{
		FrontendRoot: testFrontend(t),
		Ledger:       eventLedger,
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	t.Cleanup(cancel)
	if err := server.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if err := server.FlushLedger(ctx); err != nil {
		t.Fatalf("FlushLedger: %v", err)
	}
	if pending, err := eventLedger.Pending(); err != nil || pending != 0 {
		t.Fatalf("pending after shutdown = %d, %v, want 0", pending, err)
	}
}

// TestShutdownFlushesLedgerWhenDrainExpires reproduces the round-three pin. An
// in-flight request outruns the drain deadline, and the flush must still run.
func TestShutdownFlushesLedgerWhenDrainExpires(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	flusher := &recordingFlusher{}
	server, err := api.NewServer(&config.Config{Port: "0"}, api.ServerOptions{
		FrontendRoot: testFrontend(t),
		Ledger:       flusher,
		Index: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			close(entered)
			<-release
			w.WriteHeader(http.StatusOK)
		}),
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(listener) }()
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })

	requestDone := make(chan struct{})
	go func() {
		defer close(requestDone)
		response, err := http.Get("http://" + listener.Addr().String() + "/api/dubs")
		if err == nil {
			response.Body.Close()
		}
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("in-flight request did not reach the handler")
	}

	drainCtx, cancelDrain := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancelDrain()
	if err := server.Shutdown(drainCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Shutdown error = %v, want context deadline exceeded", err)
	}

	flushCtx, cancelFlush := context.WithTimeout(context.Background(), time.Second)
	defer cancelFlush()
	if err := server.FlushLedger(flushCtx); err != nil {
		t.Fatalf("FlushLedger after drain deadline: %v", err)
	}
	if !flusher.flushed.Load() {
		t.Fatal("flush never ran after the drain deadline expired")
	}

	releaseOnce.Do(func() { close(release) })
	select {
	case <-requestDone:
	case <-time.After(5 * time.Second):
		t.Fatal("in-flight request did not finish after release")
	}
	if err := <-serveErr; !errors.Is(err, http.ErrServerClosed) {
		t.Fatalf("Serve error = %v, want http.ErrServerClosed", err)
	}
}

type recordingFlusher struct {
	flushed atomic.Bool
}

func (f *recordingFlusher) Flush(context.Context) error {
	f.flushed.Store(true)
	return nil
}

func (f *recordingFlusher) Pending() (int, error) {
	return 0, nil
}

// TestProductionServerRequiresFrontendBuild keeps a deployed host from booting
// with nothing to serve. Development still starts without a build.
func TestProductionServerRequiresFrontendBuild(t *testing.T) {
	_, err := api.NewServer(&config.Config{Env: "production"}, api.ServerOptions{})
	if err == nil || !strings.Contains(err.Error(), "frontend build") {
		t.Fatalf("NewServer in production without a frontend = %v, want frontend build error", err)
	}

	server, err := api.NewServer(&config.Config{Env: "development"}, api.ServerOptions{})
	if err != nil {
		t.Fatalf("NewServer in development without a frontend: %v", err)
	}
	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)

	response, err := http.Get(httpServer.URL + "/")
	if err != nil {
		t.Fatalf("get workspace without a build: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("workspace status = %d, want 503", response.StatusCode)
	}

	health, err := http.Get(httpServer.URL + "/api/healthz")
	if err != nil {
		t.Fatalf("get health without a build: %v", err)
	}
	defer health.Body.Close()
	if health.StatusCode != http.StatusOK {
		t.Fatalf("health status = %d, want 200", health.StatusCode)
	}
}

// TestLedgerReadyProbesClickHouse requires the readiness route to observe
// ClickHouse rather than the environment.
func TestLedgerReadyProbesClickHouse(t *testing.T) {
	ping := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ping" {
			_, _ = w.Write([]byte("Ok."))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(ping.Close)

	live := newLedgerReadyServer(t, clickHouseFixtureConfig(t, ping.URL))
	response := ledgerReadyRequest(t, live)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("ledger readiness = %d, want 200", response.StatusCode)
	}
	if status, _ := ledgerReadyPayload(t, response); status != "ready" {
		t.Fatalf("ledger readiness status = %q, want ready", status)
	}

	// Nothing listens on the reserved port, so the probe must fail on the dial
	// instead of waiting out the response budget.
	dead := newLedgerReadyServer(t, &config.Config{
		ClickHouseHost:     "127.0.0.1",
		ClickHousePort:     reserveClosedPort(t),
		ClickHouseUser:     "fixture",
		ClickHousePassword: "fixture",
	})
	response = ledgerReadyRequest(t, dead)
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("ledger readiness against dead ClickHouse = %d, want 503", response.StatusCode)
	}
	if status, _ := ledgerReadyPayload(t, response); status != "unreachable" {
		t.Fatalf("ledger readiness against dead ClickHouse status = %q, want unreachable", status)
	}
}

// TestLedgerReadyReportsUnreachableOnBadAnswer proves a non-2xx ping answer
// reports unreachable rather than ready or waking.
func TestLedgerReadyReportsUnreachableOnBadAnswer(t *testing.T) {
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ping" {
			http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(bad.Close)

	response := ledgerReadyRequest(t, newLedgerReadyServer(t, clickHouseFixtureConfig(t, bad.URL)))
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("ledger readiness = %d, want 503", response.StatusCode)
	}
	if status, _ := ledgerReadyPayload(t, response); status != "unreachable" {
		t.Fatalf("ledger readiness status = %q, want unreachable", status)
	}
}

// TestLedgerReadyReportsUnreachableOnRedirect proves a 3xx ping answer stays
// unreachable. The probe refuses redirects, so a redirect cannot become a 2xx.
func TestLedgerReadyReportsUnreachableOnRedirect(t *testing.T) {
	redirecting := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ping":
			http.Redirect(w, r, "/healthy", http.StatusFound)
		case "/healthy":
			_, _ = w.Write([]byte("Ok."))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(redirecting.Close)

	response := ledgerReadyRequest(t, newLedgerReadyServer(t, clickHouseFixtureConfig(t, redirecting.URL)))
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("ledger readiness = %d, want 503", response.StatusCode)
	}
	if status, _ := ledgerReadyPayload(t, response); status != "unreachable" {
		t.Fatalf("ledger readiness status = %q, want unreachable", status)
	}
}

// TestLedgerReadyReportsUnreachableOnSchemeMismatch proves a post-connect
// failure that is not a timeout reports unreachable. Waking must mean the
// response budget expired, so a TLS probe against a plain HTTP listener cannot
// report waking forever.
func TestLedgerReadyReportsUnreachableOnSchemeMismatch(t *testing.T) {
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ping" {
			_, _ = w.Write([]byte("Ok."))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(plain.Close)

	cfg := clickHouseFixtureConfig(t, plain.URL)
	cfg.ClickHouseSecure = true
	response := ledgerReadyRequest(t, newLedgerReadyServer(t, cfg))
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("ledger readiness = %d, want 503", response.StatusCode)
	}
	if status, _ := ledgerReadyPayload(t, response); status != "unreachable" {
		t.Fatalf("ledger readiness status = %q, want unreachable", status)
	}
}

// TestLedgerReadyReportsMisconfigurationBeforeProbing proves a missing
// credential fails the route without any network attempt.
func TestLedgerReadyReportsMisconfigurationBeforeProbing(t *testing.T) {
	var requests atomic.Int32
	standIn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(standIn.Close)

	cfg := clickHouseFixtureConfig(t, standIn.URL)
	cfg.ClickHousePassword = ""
	response := ledgerReadyRequest(t, newLedgerReadyServer(t, cfg))
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("ledger readiness = %d, want 503", response.StatusCode)
	}
	status, detail := ledgerReadyPayload(t, response)
	if status != "misconfigured" {
		t.Fatalf("ledger readiness status = %q, want misconfigured", status)
	}
	if !strings.Contains(detail, "CLICKHOUSE_PASSWORD") {
		t.Fatalf("ledger readiness detail = %q, want CLICKHOUSE_PASSWORD", detail)
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("ClickHouse stand-in requests = %d, want 0", got)
	}
}

func testHostPort(t *testing.T, rawURL string) (string, int) {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse test server URL: %v", err)
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		t.Fatalf("parse test server port: %v", err)
	}
	return parsed.Hostname(), port
}

func testFrontend(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("<main>fixture workspace</main>"), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}
	return root
}

func copyFixture(t *testing.T, source, destination string) {
	t.Helper()
	in, err := os.Open(source)
	if err != nil {
		t.Fatalf("open fixture %s: %v", source, err)
	}
	defer in.Close()
	out, err := os.Create(destination)
	if err != nil {
		t.Fatalf("create fixture copy %s: %v", destination, err)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		t.Fatalf("copy fixture: %v", err)
	}
	if err := out.Close(); err != nil {
		t.Fatalf("close fixture copy: %v", err)
	}
}

// ledgerReadyRequest returns the readiness response for a server URL.
func ledgerReadyRequest(t *testing.T, serverURL string) *http.Response {
	t.Helper()
	response, err := http.Get(serverURL + "/api/ledger/ready")
	if err != nil {
		t.Fatalf("get ledger readiness: %v", err)
	}
	return response
}

// ledgerReadyPayload reads a readiness response and returns status and detail.
func ledgerReadyPayload(t *testing.T, response *http.Response) (string, string) {
	t.Helper()
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read ledger readiness: %v", err)
	}
	if got := response.Header.Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Fatalf("ledger readiness content type = %q, want JSON", got)
	}
	var payload struct {
		Status string `json:"status"`
		Detail string `json:"detail"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode ledger readiness %q: %v", body, err)
	}
	return payload.Status, payload.Detail
}

// newLedgerReadyServer mounts the API handler over cfg and returns its URL.
func newLedgerReadyServer(t *testing.T, cfg *config.Config) string {
	t.Helper()
	server, err := api.NewServer(cfg, api.ServerOptions{FrontendRoot: testFrontend(t)})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)
	return httpServer.URL
}

// clickHouseFixtureConfig points a config at a stand-in ClickHouse URL.
func clickHouseFixtureConfig(t *testing.T, rawURL string) *config.Config {
	t.Helper()
	host, port := testHostPort(t, rawURL)
	return &config.Config{
		ClickHouseHost:     host,
		ClickHousePort:     port,
		ClickHouseUser:     "fixture",
		ClickHousePassword: "fixture",
	}
}

// reserveClosedPort returns a port that nothing listens on.
func reserveClosedPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve a dead port: %v", err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("close dead port: %v", err)
	}
	_, port, err := net.SplitHostPort(address)
	if err != nil {
		t.Fatalf("split dead address: %v", err)
	}
	number, err := strconv.Atoi(port)
	if err != nil {
		t.Fatalf("parse dead port: %v", err)
	}
	return number
}

// TestServerMountsRunRoutes proves the start, events, and cancel routes mount.
func TestServerMountsRunRoutes(t *testing.T) {
	storage := t.TempDir()
	writeProjectSource(t, storage, "dub-mount")
	started := make(chan struct{})
	runner := runFunc(func(ctx context.Context, _ api.RunRequest, _ func(api.ProgressEvent)) (api.RunResult, error) {
		close(started)
		<-ctx.Done()
		return api.RunResult{}, ctx.Err()
	})
	cfg, err := config.LoadFromMap(map[string]string{})
	if err != nil {
		t.Fatalf("load fixture config: %v", err)
	}
	server, err := api.NewServer(cfg, api.ServerOptions{
		FrontendRoot: testFrontend(t),
		Runner:       runner,
		Recorder:     recordFunc(func(context.Context, api.RunRequest, api.RunResult) error { return nil }),
		StorageDir:   storage,
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)

	start := postRun(t, httpServer.URL, "dub-mount", "language=ml")
	start.Body.Close()
	if start.StatusCode != http.StatusAccepted {
		t.Fatalf("start status = %d, want 202", start.StatusCode)
	}
	<-started

	events, err := http.Get(httpServer.URL + "/api/dubs/dub-mount/events")
	if err != nil {
		t.Fatalf("get events: %v", err)
	}
	if events.StatusCode != http.StatusOK {
		events.Body.Close()
		t.Fatalf("events status = %d, want 200", events.StatusCode)
	}
	if got := events.Header.Get("Content-Type"); got != "text/event-stream" {
		events.Body.Close()
		t.Fatalf("events content type = %q, want text/event-stream", got)
	}
	events.Body.Close()

	cancel := postCancel(t, httpServer.URL, "dub-mount")
	cancel.Body.Close()
	if cancel.StatusCode != http.StatusAccepted {
		t.Fatalf("cancel status = %d, want 202", cancel.StatusCode)
	}
}

// TestShutdownCancelsRunningPipeline proves shutdown cancels the pipeline
// context and waits for the run goroutine to stop.
func TestShutdownCancelsRunningPipeline(t *testing.T) {
	storage := t.TempDir()
	writeProjectSource(t, storage, "dub-shutdown")
	started := make(chan struct{})
	stopped := make(chan error, 1)
	runner := runFunc(func(ctx context.Context, _ api.RunRequest, _ func(api.ProgressEvent)) (api.RunResult, error) {
		close(started)
		<-ctx.Done()
		stopped <- ctx.Err()
		return api.RunResult{}, ctx.Err()
	})
	cfg, err := config.LoadFromMap(map[string]string{})
	if err != nil {
		t.Fatalf("load fixture config: %v", err)
	}
	server, err := api.NewServer(cfg, api.ServerOptions{
		FrontendRoot: testFrontend(t),
		Runner:       runner,
		Recorder:     recordFunc(func(context.Context, api.RunRequest, api.RunResult) error { return nil }),
		StorageDir:   storage,
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)

	start := postRun(t, httpServer.URL, "dub-shutdown", "language=ml")
	start.Body.Close()
	if start.StatusCode != http.StatusAccepted {
		t.Fatalf("start status = %d, want 202", start.StatusCode)
	}
	<-started

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	select {
	case err := <-stopped:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("pipeline context error = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown returned before the pipeline stopped")
	}
}

func TestServerWithoutMCPServesWorkspaceAndAgentUnavailable(t *testing.T) {
	cfg, err := config.LoadFromMap(map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	server, err := api.NewServer(cfg, api.ServerOptions{FrontendRoot: testFrontend(t)})
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)
	response, err := http.Post(httpServer.URL+"/api/dubs/fixture/agent", "application/json", strings.NewReader(`{"question":"Which line?"}`))
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 503 || string(body) != "{\"error\":\"The editor agent is unavailable.\"}\n" {
		t.Fatalf("unconfigured agent: %d %s", response.StatusCode, body)
	}
	for _, path := range []string{"/d/fixture", "/api/healthz"} {
		response, err := http.Get(httpServer.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != 200 {
			t.Errorf("%s status %d", path, response.StatusCode)
		}
	}
}

// TestServerRoutesProjectNaming proves the mux serves the rename route and
// that the index and the workspace report the stored name afterwards.
func TestServerRoutesProjectNaming(t *testing.T) {
	cfg, err := config.LoadFromMap(map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	storage := t.TempDir()
	server, err := api.NewServer(cfg, api.ServerOptions{
		FrontendRoot: testFrontend(t),
		StorageDir:   storage,
		Upload:       api.NewUploadHandler(storage),
		Rename:       api.NewRenameHandler(storage),
		Index: api.IndexHandlerFrom(func() []api.DubSummary {
			return api.ListUploadSummaries(storage)
		}),
		Workspace: &standInWorkspaceReader{},
		Project:   api.UploadProjectLookup(storage),
	})
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)

	id := createNamedProject(t, httpServer.URL, "Festival Cut")
	if got := indexTitle(t, httpServer.URL, id); got != "Festival Cut" {
		t.Fatalf("index title = %q, want Festival Cut", got)
	}
	if got := workspaceTitle(t, httpServer.URL, id); got != "Festival Cut" {
		t.Fatalf("workspace title = %q, want Festival Cut", got)
	}

	// A rename stores plain text, so markup stays literal in both reads.
	const renamed = `<b>Second</b> Cut`
	response, err := http.Post(httpServer.URL+"/api/dubs/"+id+"/title", "application/json", strings.NewReader(`{"title":"`+renamed+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("rename status = %d, want 200", response.StatusCode)
	}
	if got := indexTitle(t, httpServer.URL, id); got != renamed {
		t.Fatalf("index title = %q, want %q", got, renamed)
	}
	if got := workspaceTitle(t, httpServer.URL, id); got != renamed {
		t.Fatalf("workspace title = %q, want %q", got, renamed)
	}

	// A blank rename is refused and both reads keep the stored name.
	response, err = http.Post(httpServer.URL+"/api/dubs/"+id+"/title", "application/json", strings.NewReader(`{"title":"   "}`))
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusBadRequest || strings.TrimSpace(string(body)) != "A project name cannot be blank." {
		t.Fatalf("blank rename = %d %q, want 400 with a plain sentence", response.StatusCode, body)
	}
	if got := indexTitle(t, httpServer.URL, id); got != renamed {
		t.Fatalf("index title after blank rename = %q, want %q", got, renamed)
	}
}

// createNamedProject uploads one project through the mux and returns its id.
func createNamedProject(t *testing.T, baseURL, title string) string {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	video, err := form.CreateFormFile("video", "clip.mp4")
	if err != nil {
		t.Fatalf("create video part: %v", err)
	}
	if _, err := video.Write([]byte("video bytes")); err != nil {
		t.Fatalf("write video part: %v", err)
	}
	if err := form.WriteField("language", "ml"); err != nil {
		t.Fatalf("write language field: %v", err)
	}
	if err := form.WriteField("title", title); err != nil {
		t.Fatalf("write title field: %v", err)
	}
	if err := form.Close(); err != nil {
		t.Fatalf("close form: %v", err)
	}
	response, err := http.Post(baseURL+"/api/dubs/new", form.FormDataContentType(), &body)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		payload, _ := io.ReadAll(response.Body)
		t.Fatalf("create status = %d, want 201: %s", response.StatusCode, payload)
	}
	var created struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}
	if err := json.NewDecoder(response.Body).Decode(&created); err != nil {
		t.Fatalf("decode create answer: %v", err)
	}
	if created.Title != title {
		t.Fatalf("create title = %q, want %q", created.Title, title)
	}
	return created.ID
}

// indexTitle reads the title the index route reports for one project.
func indexTitle(t *testing.T, baseURL, id string) string {
	t.Helper()
	response, err := http.Get(baseURL + "/api/dubs")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var index api.DubIndex
	if err := json.NewDecoder(response.Body).Decode(&index); err != nil {
		t.Fatalf("decode index: %v", err)
	}
	for _, dub := range index.Dubs {
		if dub.ID == id {
			return dub.Title
		}
	}
	t.Fatalf("index holds no row for %s", id)
	return ""
}

// workspaceTitle reads the title the workspace route reports for one project.
func workspaceTitle(t *testing.T, baseURL, id string) string {
	t.Helper()
	response, err := http.Get(baseURL + "/api/dubs/" + id)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		payload, _ := io.ReadAll(response.Body)
		t.Fatalf("workspace status = %d, want 200: %s", response.StatusCode, payload)
	}
	var dub api.Dub
	if err := json.NewDecoder(response.Body).Decode(&dub); err != nil {
		t.Fatalf("decode workspace: %v", err)
	}
	return dub.Title
}
