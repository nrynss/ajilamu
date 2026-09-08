package api

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nrynss/ajilamu/internal/config"
	"github.com/nrynss/ajilamu/internal/cost"
)

// runnerFunc adapts a function to PipelineRunner for these tests.
type runnerFunc func(ctx context.Context, req RunRequest, emit func(ProgressEvent)) (RunResult, error)

// Run invokes the wrapped function.
func (f runnerFunc) Run(ctx context.Context, req RunRequest, emit func(ProgressEvent)) (RunResult, error) {
	return f(ctx, req, emit)
}

// noopRecorder accepts every persisted run.
type noopRecorder struct{}

// Persist does nothing.
func (noopRecorder) Persist(context.Context, RunRequest, RunResult) error { return nil }

// TestEventsStreamFramesWithIDsAndCost proves the stream numbers every frame
// from one, sends the resume cost first, and ends with the terminal event.
func TestEventsStreamFramesWithIDsAndCost(t *testing.T) {
	storage := t.TempDir()
	writeEventSource(t, storage, "dub-stream")
	started := make(chan struct{})
	gate := make(chan struct{})
	runner := runnerFunc(func(_ context.Context, _ RunRequest, emit func(ProgressEvent)) (RunResult, error) {
		close(started)
		<-gate
		emit(progressEvent(100, 1))
		emit(progressEvent(200, 2))
		emit(ProgressEvent{
			Type:             EventDone,
			Stage:            StageMeasuring,
			Sentence:         "The dubbing pipeline finished.",
			TotalNanodollars: 300,
			Language:         "ml",
		})
		return RunResult{TotalCost: 300}, nil
	})
	base := newEventServer(t, runner, noopRecorder{}, storage)

	startEventRun(t, base, "dub-stream")
	<-started
	response := openEvents(t, base, "dub-stream")
	defer response.Body.Close()
	if got := response.Header.Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("event content type = %q, want text/event-stream", got)
	}
	reader := newFrameReader(response.Body)

	id, event, err := reader.next()
	if err != nil {
		t.Fatalf("read resume frame: %v", err)
	}
	if id != 1 || event.TotalNanodollars != 0 {
		t.Fatalf("resume frame = id %d cost %d, want id 1 cost 0", id, event.TotalNanodollars)
	}
	close(gate)

	want := []struct {
		id   uint64
		cost cost.Price
	}{
		{id: 2, cost: 100},
		{id: 3, cost: 200},
		{id: 4, cost: 300},
	}
	for _, step := range want {
		id, event, err := reader.next()
		if err != nil {
			t.Fatalf("read frame %d: %v", step.id, err)
		}
		if id != step.id {
			t.Fatalf("frame id = %d, want %d", id, step.id)
		}
		if event.TotalNanodollars != step.cost {
			t.Fatalf("frame %d cost = %d, want %d", id, event.TotalNanodollars, step.cost)
		}
	}
	if _, _, err := reader.next(); err != io.EOF {
		t.Fatalf("stream error after terminal = %v, want EOF", err)
	}
}

// TestEventsDropSlowSubscriberThroughRoutes proves the mounted stream drops
// events for a client that stops reading, rather than stalling the run.
func TestEventsDropSlowSubscriberThroughRoutes(t *testing.T) {
	storage := t.TempDir()
	writeEventSource(t, storage, "dub-slow-route")
	runGate := make(chan struct{})
	burst := make(chan struct{})
	runner := runnerFunc(func(ctx context.Context, _ RunRequest, emit func(ProgressEvent)) (RunResult, error) {
		<-runGate
		for i := 0; i < 200; i++ {
			emit(progressEvent(cost.Price(i), i))
		}
		close(burst)
		<-ctx.Done()
		return RunResult{}, ctx.Err()
	})
	cfg, err := config.LoadFromMap(map[string]string{})
	if err != nil {
		t.Fatalf("load fixture config: %v", err)
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("<main>fixture</main>"), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}
	server, err := NewServer(cfg, ServerOptions{
		FrontendRoot: root,
		Runner:       runner,
		Recorder:     noopRecorder{},
		StorageDir:   storage,
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)
	startEventRun(t, httpServer.URL, "dub-slow-route")

	run, ok := server.runs.lookup("dub-slow-route")
	if !ok {
		t.Fatal("the started run is missing from the registry")
	}
	writer := &blockingWriter{header: make(http.Header), first: make(chan struct{}), gate: make(chan struct{})}
	request := httptest.NewRequest(http.MethodGet, "/api/dubs/dub-slow-route/events", nil)
	handlerDone := make(chan struct{})
	go func() {
		server.Handler().ServeHTTP(writer, request)
		close(handlerDone)
	}()
	select {
	case <-writer.first:
	case <-time.After(5 * time.Second):
		t.Fatal("the stream never wrote its first frame")
	}

	close(runGate)
	select {
	case <-burst:
	case <-time.After(5 * time.Second):
		t.Fatal("the run stalled behind a slow subscriber")
	}
	run.mu.Lock()
	var dropped uint64
	for sub := range run.subs {
		dropped = sub.dropped
	}
	run.mu.Unlock()
	if want := uint64(200 - subscriberBuffer); dropped != want {
		t.Fatalf("dropped events = %d, want %d", dropped, want)
	}

	close(writer.gate)
	server.runs.cancelRun("dub-slow-route")
	select {
	case <-handlerDone:
	case <-time.After(5 * time.Second):
		t.Fatal("the stream did not close after the run ended")
	}
}

// blockingWriter stops every write until the test opens the gate.
type blockingWriter struct {
	header http.Header
	first  chan struct{}
	gate   chan struct{}
	once   sync.Once
}

// Header returns the response headers.
func (w *blockingWriter) Header() http.Header { return w.header }

// WriteHeader records the status without writing it.
func (w *blockingWriter) WriteHeader(int) {}

// Write blocks until the gate opens, and signals the first write.
func (w *blockingWriter) Write(body []byte) (int, error) {
	w.once.Do(func() { close(w.first) })
	<-w.gate
	return len(body), nil
}

// Flush does nothing, because this writer never buffers.
func (w *blockingWriter) Flush() {}

// TestEventsReconnectKeepsCumulativeCost proves a reconnect receives the
// current cumulative cost before it streams live events.
func TestEventsReconnectKeepsCumulativeCost(t *testing.T) {
	storage := t.TempDir()
	writeEventSource(t, storage, "dub-reconnect")
	started := make(chan struct{})
	first := make(chan struct{})
	second := make(chan struct{})
	runner := runnerFunc(func(_ context.Context, _ RunRequest, emit func(ProgressEvent)) (RunResult, error) {
		close(started)
		<-first
		emit(progressEvent(100, 1))
		<-second
		emit(progressEvent(250, 2))
		return RunResult{TotalCost: 250}, nil
	})
	base := newEventServer(t, runner, noopRecorder{}, storage)

	startEventRun(t, base, "dub-reconnect")
	<-started

	firstStream := openEvents(t, base, "dub-reconnect")
	firstReader := newFrameReader(firstStream.Body)
	if _, event, err := firstReader.next(); err != nil || event.TotalNanodollars != 0 {
		t.Fatalf("first resume frame = %+v, err %v", event, err)
	}
	close(first)
	if _, event, err := firstReader.next(); err != nil || event.TotalNanodollars != 100 {
		t.Fatalf("first live frame = %+v, err %v", event, err)
	}
	firstStream.Body.Close()

	secondStream := openEvents(t, base, "dub-reconnect")
	defer secondStream.Body.Close()
	secondReader := newFrameReader(secondStream.Body)
	_, event, err := secondReader.next()
	if err != nil {
		t.Fatalf("read reconnected resume frame: %v", err)
	}
	if event.TotalNanodollars != 100 {
		t.Fatalf("reconnected resume cost = %d, want 100", event.TotalNanodollars)
	}
	close(second)
	if _, event, err := secondReader.next(); err != nil || event.TotalNanodollars != 250 {
		t.Fatalf("second live frame = %+v, err %v", event, err)
	}
	_, event, err = secondReader.next()
	if err != nil {
		t.Fatalf("read terminal frame: %v", err)
	}
	if event.Type != EventDone || event.TotalNanodollars != 250 {
		t.Fatalf("terminal frame = %+v, want done at 250", event)
	}
}

// TestEventsServeTerminalAfterFinish proves a subscriber that arrives after the
// run finished receives the terminal event and nothing else.
func TestEventsServeTerminalAfterFinish(t *testing.T) {
	storage := t.TempDir()
	writeEventSource(t, storage, "dub-late")
	runner := runnerFunc(func(_ context.Context, _ RunRequest, _ func(ProgressEvent)) (RunResult, error) {
		return RunResult{TotalCost: 7}, nil
	})
	base := newEventServer(t, runner, noopRecorder{}, storage)

	startEventRun(t, base, "dub-late")
	drainEventStream(t, base, "dub-late")

	response := openEvents(t, base, "dub-late")
	defer response.Body.Close()
	reader := newFrameReader(response.Body)
	_, event, err := reader.next()
	if err != nil {
		t.Fatalf("read late terminal frame: %v", err)
	}
	if event.Type != EventDone || event.TotalNanodollars != 7 {
		t.Fatalf("late terminal frame = %+v, want done at 7", event)
	}
	if _, _, err := reader.next(); err != io.EOF {
		t.Fatalf("stream error after the late terminal = %v, want EOF", err)
	}
}

// TestEventsDropSlowSubscriber proves a full subscriber queue drops events and
// counts the drops instead of stalling the pipeline.
func TestEventsDropSlowSubscriber(t *testing.T) {
	registry := newRunRegistry(
		runnerFunc(func(ctx context.Context, _ RunRequest, _ func(ProgressEvent)) (RunResult, error) {
			<-ctx.Done()
			return RunResult{}, ctx.Err()
		}),
		noopRecorder{},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	run, err := registry.start(RunRequest{DubID: "dub-slow", Language: "ml"})
	if err != nil {
		t.Fatalf("start run: %v", err)
	}
	sub, _, finished := run.subscribe()
	if finished {
		t.Fatal("a fresh run reported itself finished")
	}

	const extra = 200
	emitted := make(chan struct{})
	go func() {
		for i := 0; i < extra; i++ {
			run.emit(ProgressEvent{
				Type:             EventProgress,
				Stage:            StageTranslating,
				Sentence:         "Translating a line.",
				TotalNanodollars: cost.Price(i),
				SegmentID:        i,
			})
		}
		close(emitted)
	}()
	select {
	case <-emitted:
	case <-time.After(5 * time.Second):
		t.Fatal("emit stalled behind a slow subscriber")
	}

	wantDropped := uint64(extra - (subscriberBuffer - 1))
	if sub.dropped != wantDropped {
		t.Fatalf("dropped events = %d, want %d", sub.dropped, wantDropped)
	}
	if frame := <-sub.frames; frame.id != 1 {
		t.Fatalf("first buffered frame id = %d, want the resume frame", frame.id)
	}

	registry.cancelRun("dub-slow")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	registry.wait(ctx)
}

// TestEventsAnswerNotFoundWithoutRun proves the stream route answers 404 when
// the dub has no run.
func TestEventsAnswerNotFoundWithoutRun(t *testing.T) {
	base := newEventServer(t, runnerFunc(func(context.Context, RunRequest, func(ProgressEvent)) (RunResult, error) {
		return RunResult{}, nil
	}), noopRecorder{}, t.TempDir())

	response, err := http.Get(base + "/api/dubs/dub-none/events")
	if err != nil {
		t.Fatalf("get events: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("events status = %d, want 404", response.StatusCode)
	}
	var payload struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode events failure: %v", err)
	}
	if payload.Error != runMissing {
		t.Fatalf("events error = %q, want %q", payload.Error, runMissing)
	}
}

// frameReader parses one SSE frame at a time.
type frameReader struct {
	reader *bufio.Reader
}

// newFrameReader wraps a response body.
func newFrameReader(body io.Reader) *frameReader {
	return &frameReader{reader: bufio.NewReader(body)}
}

// next returns the next numbered event.
func (f *frameReader) next() (uint64, ProgressEvent, error) {
	var id uint64
	var data string
	for {
		line, err := f.reader.ReadString('\n')
		if err != nil {
			return 0, ProgressEvent{}, err
		}
		line = strings.TrimRight(line, "\r\n")
		switch {
		case line == "":
			if data == "" {
				continue
			}
			var event ProgressEvent
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				return 0, ProgressEvent{}, fmt.Errorf("decode event data: %w", err)
			}
			return id, event, nil
		case strings.HasPrefix(line, "id: "):
			parsed, err := strconv.ParseUint(strings.TrimPrefix(line, "id: "), 10, 64)
			if err != nil {
				return 0, ProgressEvent{}, fmt.Errorf("parse frame id: %w", err)
			}
			id = parsed
		case strings.HasPrefix(line, "data: "):
			data = strings.TrimPrefix(line, "data: ")
		}
	}
}

// progressEvent builds one progress event with a running cost.
func progressEvent(total cost.Price, segment int) ProgressEvent {
	return ProgressEvent{
		Type:             EventProgress,
		Stage:            StageTranslating,
		Sentence:         fmt.Sprintf("Translating line %d.", segment),
		TotalNanodollars: total,
		SegmentID:        segment,
		Language:         "ml",
	}
}

// newEventServer mounts the API and returns its URL.
func newEventServer(t *testing.T, runner PipelineRunner, recorder RunRecorder, storage string) string {
	t.Helper()
	cfg, err := config.LoadFromMap(map[string]string{})
	if err != nil {
		t.Fatalf("load fixture config: %v", err)
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("<main>fixture</main>"), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}
	server, err := NewServer(cfg, ServerOptions{
		FrontendRoot: root,
		Runner:       runner,
		Recorder:     recorder,
		StorageDir:   storage,
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)
	return httpServer.URL
}

// writeEventSource writes one upload project with a source video.
func writeEventSource(t *testing.T, storage, dubID string) {
	t.Helper()
	dir := filepath.Join(storage, dubID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create project directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "source.mp4"), []byte("source"), 0o644); err != nil {
		t.Fatalf("write source video: %v", err)
	}
}

// startEventRun starts one run and requires the 202 answer.
func startEventRun(t *testing.T, base, dubID string) {
	t.Helper()
	response, err := http.Post(base+"/api/dubs/"+dubID+"/run?language=ml", "", nil)
	if err != nil {
		t.Fatalf("start run: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("start status = %d, want 202", response.StatusCode)
	}
}

// openEvents opens the event stream.
func openEvents(t *testing.T, base, dubID string) *http.Response {
	t.Helper()
	response, err := http.Get(base + "/api/dubs/" + dubID + "/events")
	if err != nil {
		t.Fatalf("open event stream: %v", err)
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		t.Fatalf("event status = %d, want 200", response.StatusCode)
	}
	return response
}

// drainEventStream reads the stream to its end.
func drainEventStream(t *testing.T, base, dubID string) {
	t.Helper()
	response := openEvents(t, base, dubID)
	defer response.Body.Close()
	if _, err := io.Copy(io.Discard, response.Body); err != nil {
		t.Fatalf("drain event stream: %v", err)
	}
}
