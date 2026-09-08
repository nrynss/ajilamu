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

func TestEntrypointExitsCleanlyWithoutLedger(t *testing.T) {
	projectRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve project root: %v", err)
	}
	binary := filepath.Join(t.TempDir(), "ajilamu")
	build := exec.Command("go", "build", "-o", binary, "./cmd/ajilamu")
	build.Dir = projectRoot
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build entrypoint: %v: %s", err, output)
	}

	command := exec.Command(binary)
	command.Dir = projectRoot
	command.Env = []string{"ENV=development", "PORT=0"}
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
	host, port := testHostPort(t, ping.URL)

	liveCfg := &config.Config{
		ClickHouseHost:     host,
		ClickHousePort:     port,
		ClickHouseUser:     "fixture",
		ClickHousePassword: "fixture",
	}
	server, err := api.NewServer(liveCfg, api.ServerOptions{FrontendRoot: testFrontend(t)})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)

	response, err := http.Get(httpServer.URL + "/api/ledger/ready")
	if err != nil {
		t.Fatalf("get ledger readiness: %v", err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatalf("read ledger readiness: %v", err)
	}
	if response.StatusCode != http.StatusOK || !strings.Contains(string(body), `"ready"`) {
		t.Fatalf("ledger readiness = %d %q, want 200 ready", response.StatusCode, body)
	}

	// Nothing listens on the dead port, so the probe must fail the route.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve a dead port: %v", err)
	}
	deadAddress := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("close dead port: %v", err)
	}
	_, deadPort, err := net.SplitHostPort(deadAddress)
	if err != nil {
		t.Fatalf("split dead address: %v", err)
	}
	deadPortNumber, err := strconv.Atoi(deadPort)
	if err != nil {
		t.Fatalf("parse dead port: %v", err)
	}

	deadCfg := &config.Config{
		ClickHouseHost:     "127.0.0.1",
		ClickHousePort:     deadPortNumber,
		ClickHouseUser:     "fixture",
		ClickHousePassword: "fixture",
	}
	deadServer, err := api.NewServer(deadCfg, api.ServerOptions{FrontendRoot: testFrontend(t)})
	if err != nil {
		t.Fatalf("NewServer with dead ClickHouse: %v", err)
	}
	deadHTTP := httptest.NewServer(deadServer.Handler())
	t.Cleanup(deadHTTP.Close)

	response, err = http.Get(deadHTTP.URL + "/api/ledger/ready")
	if err != nil {
		t.Fatalf("get ledger readiness against dead ClickHouse: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("ledger readiness against dead ClickHouse = %d, want 503", response.StatusCode)
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
