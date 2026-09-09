package ledger

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nrynss/ajilamu/internal/config"
	"github.com/nrynss/ajilamu/internal/cost"
	"github.com/nrynss/ajilamu/internal/types"
)

type fixtureSegment struct {
	ID      int    `json:"id"`
	StartMS int64  `json:"start_ms"`
	EndMS   int64  `json:"end_ms"`
	Text    string `json:"text"`
	Speaker string `json:"speaker"`
}

type fixtureMetric struct {
	SegmentID          int   `json:"segment_id"`
	TakeMS             int64 `json:"take_ms"`
	RepairedDurationMS int64 `json:"repaired_duration_ms"`
}

type capturedInsert struct {
	query string
	rows  []map[string]any
}

func TestRecordTakeFixtureCapture(t *testing.T) {
	t.Parallel()

	segments := readFixtureSegments(t)
	metrics := readFixtureMetrics(t)
	var captured []capturedInsert
	var capturedMu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read insert body: %v", err)
			http.Error(w, "read body", http.StatusInternalServerError)
			return
		}
		var rows []map[string]any
		for _, line := range splitJSONRows(t, body) {
			var row map[string]any
			if err := json.Unmarshal(line, &row); err != nil {
				t.Errorf("decode JSONEachRow payload: %v", err)
				http.Error(w, "decode row", http.StatusBadRequest)
				return
			}
			rows = append(rows, row)
		}
		capturedMu.Lock()
		captured = append(captured, capturedInsert{query: r.URL.Query().Get("query"), rows: rows})
		capturedMu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := fixtureLedgerClient(t, server.URL)
	defer client.Close()

	attempts := fixtureTakeAttempts(segments, metrics)
	peaks := make([]uint8, 64)
	for i := range peaks {
		peaks[i] = uint8(i * 4)
	}
	attempts[0].Peaks = peaks
	for _, attempt := range attempts {
		if err := client.RecordTake(context.Background(), attempt); err != nil {
			t.Fatalf("RecordTake segment %d attempt %d: %v", attempt.Segment.ID, attempt.Take.Attempt, err)
		}
	}

	takes, charges := capturedRows(t, &captured, &capturedMu)
	if len(takes) != 10 {
		t.Fatalf("captured %d take rows, want 10", len(takes))
	}
	if len(charges) != 30 {
		t.Fatalf("captured %d charge rows, want 30", len(charges))
	}
	for _, row := range charges {
		if row["unit"] != "prompt_tokens" && row["unit"] != "candidate_tokens" && row["unit"] != "characters" {
			t.Errorf("charge unit = %v, want itemized prompt_tokens, candidate_tokens, or characters", row["unit"])
		}
		if row["take_id"] == "" || row["commit_id"] == "" || row["attempt"] == nil {
			t.Errorf("charge omitted attempt identity: %#v", row)
		}
		if _, hasRunningTotal := row["cost_usd"]; hasRunningTotal {
			t.Errorf("charge carried a derived or running cost total: %#v", row)
		}
	}
	assertThreeChargesPerAttempt(t, charges)

	segmentEight := takeBySegment(t, takes, 8)
	if got := segmentEight["slot_ms"]; got != float64(7110) {
		t.Errorf("segment 8 slot_ms = %v, want 7110", got)
	}
	if got := segmentEight["measured_ms"]; got != float64(4200) {
		t.Errorf("segment 8 measured_ms = %v, want 4200", got)
	}
	if got := segmentEight["delta_ms"]; got != float64(-2910) {
		t.Errorf("segment 8 delta_ms = %v, want -2910", got)
	}
	firstTake := takeBySegment(t, takes, 1)
	if got := firstTake["peaks"]; !equalJSONPeaks(got, peaks) {
		t.Errorf("segment 1 peaks = %v, want %v", got, peaks)
	}
	secondTake := takeBySegment(t, takes, 2)
	if got := secondTake["peaks"]; !equalJSONPeaks(got, nil) {
		t.Errorf("segment 2 empty peaks = %v, want []", got)
	}
	maximum := attempts[0]
	maximum.Peaks = make([]uint8, 128)
	for i := range maximum.Peaks {
		maximum.Peaks[i] = uint8(i)
	}
	beforeMaximum := captureCount(&captured, &capturedMu)
	if err := client.RecordTake(context.Background(), maximum); err != nil {
		t.Fatalf("RecordTake with 128 peaks: %v", err)
	}
	maximumTakes, maximumCharges := capturedRowsSince(t, &captured, &capturedMu, beforeMaximum)
	if len(maximumTakes) != 1 || len(maximumCharges) != 3 {
		t.Fatalf("128 peaks captured %d take rows and %d charge rows, want 1 and 3", len(maximumTakes), len(maximumCharges))
	}
	if got := maximumTakes[0]["peaks"]; !equalJSONPeaks(got, maximum.Peaks) {
		t.Errorf("128 peaks = %v, want %v", got, maximum.Peaks)
	}

	beforeInvalid := captureCount(&captured, &capturedMu)
	invalid := attempts[0]
	invalid.Take.Fit.Delta++
	if err := client.RecordTake(context.Background(), invalid); err == nil {
		t.Fatal("RecordTake accepted an invalid signed delta")
	}
	if got := captureCount(&captured, &capturedMu); got != beforeInvalid {
		t.Fatalf("invalid take made %d transport requests, want none", got-beforeInvalid)
	}

	beforeInvalid = captureCount(&captured, &capturedMu)
	invalid = attempts[0]
	invalid.Peaks = invalid.Peaks[:63]
	if err := client.RecordTake(context.Background(), invalid); err == nil {
		t.Fatal("RecordTake accepted an invalid peak vector")
	}
	if got := captureCount(&captured, &capturedMu); got != beforeInvalid {
		t.Fatalf("invalid peaks made %d transport requests, want none", got-beforeInvalid)
	}

	beforeInvalid = captureCount(&captured, &capturedMu)
	invalid = attempts[0]
	invalid.Peaks = make([]uint8, 129)
	if err := client.RecordTake(context.Background(), invalid); err == nil {
		t.Fatal("RecordTake accepted a 129-sample peak vector")
	}
	if got := captureCount(&captured, &capturedMu); got != beforeInvalid {
		t.Fatalf("129 peaks made %d transport requests, want none", got-beforeInvalid)
	}

	beforeInvalid = captureCount(&captured, &capturedMu)
	invalid = attempts[0]
	invalid.Charges = append([]cost.Charge(nil), invalid.Charges...)
	invalid.Charges[0].Kind = cost.ChargeAgent
	if err := client.RecordTake(context.Background(), invalid); err == nil {
		t.Fatal("RecordTake accepted an agent charge")
	}
	if got := captureCount(&captured, &capturedMu); got != beforeInvalid {
		t.Fatalf("agent charge on take made %d transport requests, want none", got-beforeInvalid)
	}

	beforeRetry := captureCount(&captured, &capturedMu)
	if err := client.RecordTake(context.Background(), attempts[9]); err != nil {
		t.Fatalf("retry RecordTake: %v", err)
	}
	retryTakes, retryCharges := capturedRowsSince(t, &captured, &capturedMu, beforeRetry)
	if len(retryTakes) != 1 || len(retryCharges) != 3 {
		t.Fatalf("retry captured %d take rows and %d charge rows, want 1 and 3", len(retryTakes), len(retryCharges))
	}
	assertSameIdentity(t, takes[9], retryTakes[0])
	for _, row := range retryCharges {
		if row["take_id"] != takes[9]["take_id"] || row["commit_id"] != takes[9]["commit_id"] || row["segment_index"] != takes[9]["segment_index"] || row["attempt"] != takes[9]["attempt"] {
			t.Errorf("retry charge identity changed: %#v", row)
		}
	}
}

func TestRecordTakeJournalsChargesBeforeFailedFlush(t *testing.T) {
	t.Parallel()

	type capturedDrop struct {
		status int
		query  string
		body   []byte
		rows   []map[string]any
	}
	var (
		mu       sync.Mutex
		requests []capturedDrop
		okOnce   sync.Once
	)
	recovery := make(chan struct{})
	delivered := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read insert body: %v", err)
			http.Error(w, "read body", http.StatusInternalServerError)
			return
		}
		var rows []map[string]any
		for _, line := range splitJSONRows(t, body) {
			var row map[string]any
			if err := json.Unmarshal(line, &row); err != nil {
				t.Errorf("decode JSONEachRow payload: %v", err)
				http.Error(w, "decode row", http.StatusBadRequest)
				return
			}
			rows = append(rows, row)
		}
		status := http.StatusServiceUnavailable
		select {
		case <-recovery:
			status = http.StatusOK
			okOnce.Do(func() { close(delivered) })
		default:
		}
		mu.Lock()
		requests = append(requests, capturedDrop{status: status, query: r.URL.Query().Get("query"), body: body, rows: rows})
		mu.Unlock()
		w.WriteHeader(status)
	}))
	defer server.Close()

	client := fixtureLedgerClient(t, server.URL)
	defer client.Close()

	segment := fixtureSegmentByID(t, 8)
	attempt := fixtureTakeAttempt(segment, 1, 4200, types.RepairNone)
	attempt.CommitID = "commit-drop"
	_, charges, err := attempt.rows()
	if err != nil {
		t.Fatalf("take rows: %v", err)
	}
	if len(charges) == 0 {
		t.Fatal("fixture take has no charge rows")
	}
	wantPending := 1 + len(charges)

	if err := client.RecordTake(context.Background(), attempt); !errors.Is(err, ErrPending) {
		t.Fatalf("RecordTake drop error = %v, want ErrPending", err)
	}
	if pending, err := client.Pending(); err != nil || pending != wantPending {
		t.Fatalf("Pending after failed delivery = %d, %v, want %d, nil", pending, err, wantPending)
	}

	journalTakes, journalCharges := decodeDurableTakeJournal(t, client.queue.dir)
	if len(journalTakes) != 1 {
		t.Fatalf("durable takes_raw files = %d, want 1", len(journalTakes))
	}
	if len(journalCharges) != len(charges) {
		t.Fatalf("durable charges_raw files = %d, want %d", len(journalCharges), len(charges))
	}
	if journalTakes[0]["take_id"] != "take-8-1" || journalTakes[0]["commit_id"] != "commit-drop" {
		t.Fatalf("durable take identity = take_id=%v commit_id=%v, want take-8-1 commit-drop", journalTakes[0]["take_id"], journalTakes[0]["commit_id"])
	}
	for i, row := range journalCharges {
		if row["take_id"] != "take-8-1" || row["commit_id"] != "commit-drop" {
			t.Errorf("durable charge %d identity = take_id=%v commit_id=%v, want take-8-1 commit-drop", i, row["take_id"], row["commit_id"])
		}
	}

	close(recovery)
	if err := client.Flush(context.Background()); err != nil {
		t.Fatalf("Flush replay: %v", err)
	}
	select {
	case <-delivered:
	case <-time.After(3 * time.Second):
		t.Fatal("replay did not deliver the recovered take")
	}
	if pending, err := client.Pending(); err != nil || pending != 0 {
		t.Fatalf("Pending after replay = %d, %v, want 0, nil", pending, err)
	}

	mu.Lock()
	got := append([]capturedDrop(nil), requests...)
	mu.Unlock()
	var recoveredTakes, recoveredCharges []map[string]any
	for _, insert := range got {
		if insert.status != http.StatusOK {
			continue
		}
		switch insert.query {
		case takeInsert:
			for _, row := range insert.rows {
				if row["commit_id"] == "commit-drop" && row["take_id"] == "take-8-1" {
					recoveredTakes = append(recoveredTakes, row)
				}
			}
		case chargeInsert:
			for _, row := range insert.rows {
				if row["commit_id"] == "commit-drop" && row["take_id"] == "take-8-1" {
					recoveredCharges = append(recoveredCharges, row)
				}
			}
		}
	}
	if len(recoveredTakes) == 0 {
		t.Fatal("recovered HTTP has no takes_raw body for take-8-1 commit-drop")
	}
	if len(recoveredCharges) != len(charges) {
		t.Fatalf("recovered HTTP charges_raw rows for commit-drop = %d, want %d", len(recoveredCharges), len(charges))
	}
}

func fixtureSegmentByID(t *testing.T, id int) types.Segment {
	t.Helper()
	for _, segment := range readFixtureSegments(t) {
		if segment.ID == id {
			return segment
		}
	}
	t.Fatalf("fixture missing segment %d", id)
	return types.Segment{}
}

func decodeDurableTakeJournal(t *testing.T, dir string) ([]map[string]any, []map[string]any) {
	t.Helper()
	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read durable queue: %v", err)
	}
	var takes, charges []map[string]any
	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".json") {
			continue
		}
		payload, err := os.ReadFile(filepath.Join(dir, file.Name()))
		if err != nil {
			t.Fatalf("read journal %s: %v", file.Name(), err)
		}
		var entry Entry
		if err := json.Unmarshal(payload, &entry); err != nil {
			t.Fatalf("decode journal %s: %v", file.Name(), err)
		}
		var row map[string]any
		if err := json.Unmarshal(entry.Body, &row); err != nil {
			t.Fatalf("decode journal body %s: %v", file.Name(), err)
		}
		switch entry.Query {
		case takeInsert:
			takes = append(takes, row)
		case chargeInsert:
			charges = append(charges, row)
		default:
			t.Errorf("unexpected journal query in %s: %q", file.Name(), entry.Query)
		}
	}
	return takes, charges
}

func equalJSONPeaks(got any, want []uint8) bool {
	values, ok := got.([]any)
	if !ok || len(values) != len(want) {
		return false
	}
	for i, value := range values {
		if value != float64(want[i]) {
			return false
		}
	}
	return true
}

func fixtureLedgerClient(t *testing.T, endpoint string) *Client {
	t.Helper()
	client, err := New(&config.Config{
		ClickHouseHost: "fixture.invalid", ClickHousePort: 8443, ClickHouseUser: "fixture",
		ClickHousePassword: "fixture", ClickHouseDatabase: "fixture",
	}, t.TempDir(), WithEndpoint(endpoint))
	if err != nil {
		t.Fatalf("new fixture ledger client: %v", err)
	}
	return client
}

func readFixtureSegments(t *testing.T) []types.Segment {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", "testdata", "segments.json"))
	if err != nil {
		t.Fatalf("read segments fixture: %v", err)
	}
	var fixture []fixtureSegment
	if err := json.Unmarshal(body, &fixture); err != nil {
		t.Fatalf("decode segments fixture: %v", err)
	}
	segments := make([]types.Segment, len(fixture))
	for i, segment := range fixture {
		segments[i] = types.Segment{ID: segment.ID, StartMs: segment.StartMS, EndMs: segment.EndMS, Text: segment.Text, Speaker: types.Speaker{Name: segment.Speaker}}
	}
	return segments
}

func readFixtureMetrics(t *testing.T) map[int]fixtureMetric {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", "testdata", "expected", "metrics.json"))
	if err != nil {
		t.Fatalf("read metrics fixture: %v", err)
	}
	var fixture []fixtureMetric
	if err := json.Unmarshal(body, &fixture); err != nil {
		t.Fatalf("decode metrics fixture: %v", err)
	}
	metrics := make(map[int]fixtureMetric, len(fixture))
	for _, metric := range fixture {
		metrics[metric.SegmentID] = metric
	}
	return metrics
}

func fixtureTakeAttempts(segments []types.Segment, metrics map[int]fixtureMetric) []TakeAttempt {
	attempts := make([]TakeAttempt, 0, 10)
	for _, segment := range segments {
		metric := metrics[segment.ID]
		attempts = append(attempts, fixtureTakeAttempt(segment, 1, metric.TakeMS, types.RepairNone))
		if segment.ID == 3 || segment.ID == 4 {
			attempts = append(attempts, fixtureTakeAttempt(segment, 2, metric.RepairedDurationMS, types.RepairAtempo))
		}
	}
	return attempts
}

func fixtureTakeAttempt(segment types.Segment, number int, measuredMS int64, repair types.Repair) TakeAttempt {
	slot := segment.SlotDuration()
	return TakeAttempt{
		TakeID: fmt.Sprintf("take-%d-%d", segment.ID, number), CommitID: "commit-fixture", ProjectID: "project-fixture",
		DubID: "dub-fixture", OwnerID: "owner-fixture", Language: "ml-IN", Voice: "ml-IN-Chirp3-HD", ChargeProvider: "fixture-provider", Repair: repair,
		Segment: segment,
		Take:    types.Take{SegmentID: segment.ID, Attempt: number, File: "testdata/takes/segment.wav", Fit: types.NewFit(slot, time.Duration(measuredMS)*time.Millisecond)},
		Charges: []cost.Charge{
			{Kind: cost.ChargeTranslate, TakeID: segment.ID, PromptTokens: 100 + segment.ID, CandidateTokens: 20 + segment.ID, PromptUnitPrice: 150, CandidateUnitPrice: 600},
			{Kind: cost.ChargeSynthesize, TakeID: segment.ID, Units: 1000 + segment.ID, UnitPrice: 30_000},
		},
	}
}

func splitJSONRows(t *testing.T, body []byte) [][]byte {
	t.Helper()
	var rows [][]byte
	for _, line := range bytesLines(body) {
		if len(line) > 0 {
			rows = append(rows, line)
		}
	}
	return rows
}

func bytesLines(body []byte) [][]byte {
	var lines [][]byte
	start := 0
	for i, b := range body {
		if b == '\n' {
			lines = append(lines, body[start:i])
			start = i + 1
		}
	}
	if start < len(body) {
		lines = append(lines, body[start:])
	}
	return lines
}

func capturedRows(t *testing.T, captured *[]capturedInsert, mu *sync.Mutex) ([]map[string]any, []map[string]any) {
	return capturedRowsSince(t, captured, mu, 0)
}

func capturedRowsSince(t *testing.T, captured *[]capturedInsert, mu *sync.Mutex, start int) ([]map[string]any, []map[string]any) {
	t.Helper()
	mu.Lock()
	defer mu.Unlock()
	var takes, charges []map[string]any
	for _, insert := range (*captured)[start:] {
		switch insert.query {
		case takeInsert:
			takes = append(takes, insert.rows...)
		case chargeInsert:
			charges = append(charges, insert.rows...)
		default:
			t.Errorf("unexpected insert query: %q", insert.query)
		}
	}
	return takes, charges
}

func captureCount(captured *[]capturedInsert, mu *sync.Mutex) int {
	mu.Lock()
	defer mu.Unlock()
	return len(*captured)
}

func takeBySegment(t *testing.T, takes []map[string]any, segment int) map[string]any {
	t.Helper()
	for _, row := range takes {
		if row["segment_index"] == float64(segment) && row["attempt"] == float64(1) {
			return row
		}
	}
	t.Fatalf("no attempt-one take for segment %d", segment)
	return nil
}

func assertSameIdentity(t *testing.T, first, retry map[string]any) {
	t.Helper()
	for _, field := range []string{"take_id", "commit_id", "project_id", "dub_id", "language", "segment_index", "attempt"} {
		if retry[field] != first[field] {
			t.Errorf("retry %s = %v, want %v", field, retry[field], first[field])
		}
	}
}

func assertThreeChargesPerAttempt(t *testing.T, charges []map[string]any) {
	t.Helper()
	counts := make(map[string]int)
	for _, row := range charges {
		identity := fmt.Sprintf("%v/%v/%v/%v", row["commit_id"], row["segment_index"], row["attempt"], row["take_id"])
		counts[identity]++
	}
	for identity, count := range counts {
		if count != 3 {
			t.Errorf("attempt %s captured %d itemized charges, want 3", identity, count)
		}
	}
}
