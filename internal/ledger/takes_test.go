package ledger

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

	beforeInvalid := captureCount(&captured, &capturedMu)
	invalid := attempts[0]
	invalid.Take.Fit.Delta++
	if err := client.RecordTake(context.Background(), invalid); err == nil {
		t.Fatal("RecordTake accepted an invalid signed delta")
	}
	if got := captureCount(&captured, &capturedMu); got != beforeInvalid {
		t.Fatalf("invalid take made %d transport requests, want none", got-beforeInvalid)
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
