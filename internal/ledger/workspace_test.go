package ledger

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nrynss/ajilamu/internal/api"
	"github.com/nrynss/ajilamu/internal/cost"
)

// workspaceStandIn serves one payload per ClickHouse statement. It records every
// request so a test can assert the bound parameters and the pinned settings.
func workspaceStandIn(t *testing.T, payloads map[string]string) (*Client, func() []capturedHistoryRequest) {
	t.Helper()
	var (
		mu       sync.Mutex
		requests []capturedHistoryRequest
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		params := r.URL.Query()
		mu.Lock()
		requests = append(requests, capturedHistoryRequest{query: params.Get("query"), params: params})
		mu.Unlock()
		payload, ok := payloads[params.Get("query")]
		if !ok {
			t.Errorf("unexpected query %q", params.Get("query"))
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if _, err := w.Write([]byte(payload)); err != nil {
			t.Errorf("write payload: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	client := newHistoryTestClient(t, server.URL)
	t.Cleanup(func() { client.Close() })
	return client, func() []capturedHistoryRequest {
		mu.Lock()
		defer mu.Unlock()
		return append([]capturedHistoryRequest(nil), requests...)
	}
}

// workspacePayloads is the plain JSONEachRow shape every workspace statement
// answers. The quoting stand-in rewrites the 64-bit fields into strings.
func workspacePayloads() map[string]string {
	return map[string]string{
		selectWorkspaceTakes:       `{"language":"ml","commit_id":"c1","segment_index":1,"attempt":1,"voice":"voice-a","text":"hello","audio_path":"a.wav","repair":"none","peaks":[],"slot_ms":1820,"measured_ms":1680,"delta_ms":-140,"created_ms":1000}` + "\n",
		selectWorkspaceTakeCharges: `{"language":"ml","commit_id":"c1","segment_index":1,"attempt":1,"kind":"translate","unit":"prompt_tokens","units":21,"unit_price_nanodollars":100}` + "\n",
		selectWholePassCharges: `{"commit_id":"","kind":"agent","unit":"turns","units":1,"unit_price_nanodollars":0,"total_nanodollars":2400}` + "\n" +
			`{"commit_id":"c1","kind":"segment","unit":"prompt_tokens","units":670,"unit_price_nanodollars":100,"total_nanodollars":67000}` + "\n",
		selectRunningTotal:       `{"total_nanodollars":995500,"segment_calls":1,"translate_calls":2,"synthesize_calls":3,"agent_calls":1}` + "\n",
		selectWorkspaceLanguages: `{"language":"ml","take_count":1}` + "\n",
		selectProjectMetadata:    `{"commit_count":2,"created_ms":1788897588404,"updated_ms":1788897589404,"languages":["ml"]}` + "\n",
	}
}

// quoteWorkspacePayload rewrites every 64-bit integer a statement returns as a
// JSON string, the shape a server with output_format_json_quote_64bit_integers
// set to 1 returns. It leaves Int32 and UInt8 columns as numbers.
func quoteWorkspacePayload(statement, payload string) string {
	var fields []string
	switch statement {
	case selectWorkspaceTakes:
		fields = []string{"slot_ms", "measured_ms", "delta_ms", "created_ms"}
	case selectWorkspaceTakeCharges:
		fields = []string{"units", "unit_price_nanodollars"}
	case selectWholePassCharges:
		fields = []string{"units", "unit_price_nanodollars", "total_nanodollars"}
	case selectRunningTotal:
		fields = []string{"total_nanodollars", "segment_calls", "translate_calls", "synthesize_calls", "agent_calls"}
	case selectWorkspaceLanguages:
		fields = []string{"take_count"}
	case selectProjectMetadata:
		fields = []string{"commit_count", "created_ms", "updated_ms"}
	default:
		return payload
	}
	var out strings.Builder
	for _, line := range strings.Split(strings.TrimRight(payload, "\n"), "\n") {
		decoder := json.NewDecoder(strings.NewReader(line))
		decoder.UseNumber()
		var row map[string]any
		if err := decoder.Decode(&row); err != nil {
			return payload
		}
		for _, field := range fields {
			number, ok := row[field].(json.Number)
			if !ok {
				continue
			}
			row[field] = number.String()
		}
		encoded, err := json.Marshal(row)
		if err != nil {
			return payload
		}
		out.Write(encoded)
		out.WriteByte('\n')
	}
	return out.String()
}

// TestWorkspaceReadsSurviveQuotedIntegers drives every workspace read through one
// stand-in. The server quotes 64-bit integers unless the request pins the quoting
// setting, so a read that builds its own unpinned request fails here. A bypass
// that copies both pins passes this test and fails TestOnlyClientPinsServerSettings.
func TestWorkspaceReadsSurviveQuotedIntegers(t *testing.T) {
	t.Parallel()

	payloads := workspacePayloads()
	var (
		mu       sync.Mutex
		requests []capturedHistoryRequest
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		params := r.URL.Query()
		mu.Lock()
		requests = append(requests, capturedHistoryRequest{query: params.Get("query"), params: params})
		mu.Unlock()
		statement := params.Get("query")
		payload, ok := payloads[statement]
		if !ok {
			t.Errorf("unexpected query %q", statement)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if params.Get("output_format_json_quote_64bit_integers") != "0" {
			payload = quoteWorkspacePayload(statement, payload)
		}
		if _, err := w.Write([]byte(payload)); err != nil {
			t.Errorf("write payload: %v", err)
		}
	}))
	defer server.Close()

	client := newHistoryTestClient(t, server.URL)
	defer client.Close()
	ctx := context.Background()

	tracks, err := client.WorkspaceTakes(ctx, fixtureDub)
	if err != nil {
		t.Fatalf("WorkspaceTakes: %v", err)
	}
	if len(tracks) != 1 || tracks[0].Language != fixtureLanguage || len(tracks[0].Lines) != 1 {
		t.Fatalf("WorkspaceTakes = %+v, want one %s track with one line", tracks, fixtureLanguage)
	}
	line := tracks[0].Lines[0]
	if line.SegmentID != 1 || line.Text != "hello" || line.Flagged {
		t.Errorf("line = %+v, want segment 1 with text hello and no flag", line)
	}
	if len(line.Takes) != 1 {
		t.Fatalf("line takes = %+v, want one take", line.Takes)
	}
	take := line.Takes[0]
	if take.Fit.SlotMs != 1820 || take.Fit.MeasuredMs != 1680 || take.Fit.DeltaMs != -140 || take.Fit.State != api.StateFits {
		t.Errorf("take fit = %+v, want slot 1820 measured 1680 delta -140 state fits", take.Fit)
	}
	if len(take.Charges) != 1 || take.Charges[0].Kind != "translate" ||
		take.Charges[0].Units != 21 || take.Charges[0].TotalNanodollars != cost.Price(2100) {
		t.Errorf("take charges = %+v, want one translate charge of 2100 nanodollars", take.Charges)
	}
	if take.Charges[0].SegmentID == nil || *take.Charges[0].SegmentID != 1 || take.Charges[0].TakeFile != "a.wav" {
		t.Errorf("charge binding = %+v, want segment 1 and take file a.wav", take.Charges[0])
	}

	wholePass, err := client.WholePassCharges(ctx, fixtureDub)
	if err != nil {
		t.Fatalf("WholePassCharges: %v", err)
	}
	if len(wholePass) != 2 || wholePass[0].Kind != "agent" || wholePass[1].Kind != "segment" ||
		wholePass[0].SegmentID != nil || wholePass[0].TakeFile != "" ||
		wholePass[0].TotalNanodollars != cost.Price(2400) {
		t.Errorf("whole-pass charges = %+v, want agent and segment charges", wholePass)
	}

	total, err := client.RunningTotal(ctx, fixtureDub)
	if err != nil {
		t.Fatalf("RunningTotal: %v", err)
	}
	if total.TotalNanodollars != cost.Price(995500) {
		t.Errorf("running total = %d, want 995500", total.TotalNanodollars)
	}
	if total.Covers != "1 segment call, 2 translation calls, 3 render calls, and 1 agent call." {
		t.Errorf("running total covers = %q", total.Covers)
	}

	languages, err := client.Languages(ctx, fixtureDub)
	if err != nil {
		t.Fatalf("Languages: %v", err)
	}
	if len(languages) != 1 || languages[0] != fixtureLanguage {
		t.Errorf("languages = %v, want [%s]", languages, fixtureLanguage)
	}

	meta, err := client.ProjectMetadata(ctx, fixtureDub)
	if err != nil {
		t.Fatalf("ProjectMetadata: %v", err)
	}
	if meta.ID != fixtureDub || len(meta.Languages) != 1 || meta.Languages[0] != fixtureLanguage {
		t.Errorf("project metadata = %+v, want id %s and language %s", meta, fixtureDub, fixtureLanguage)
	}
	wantCreated := time.UnixMilli(1788897588404).UTC().Format(time.RFC3339Nano)
	wantUpdated := time.UnixMilli(1788897589404).UTC().Format(time.RFC3339Nano)
	if meta.CreatedAt != wantCreated || meta.UpdatedAt != wantUpdated {
		t.Errorf("project timestamps = %q and %q, want %q and %q", meta.CreatedAt, meta.UpdatedAt, wantCreated, wantUpdated)
	}
	if meta.Title != "" || meta.Readiness != "" {
		t.Errorf("project metadata filled a field the ledger stores nowhere: %+v", meta)
	}

	mu.Lock()
	captured := append([]capturedHistoryRequest(nil), requests...)
	mu.Unlock()
	if len(captured) != 6 {
		t.Fatalf("request count = %d, want 6", len(captured))
	}
	seen := make(map[string]bool)
	for _, request := range captured {
		seen[request.query] = true
		assertPinnedQuerySettings(t, request)
		if request.params.Get("param_dub_id") != fixtureDub {
			t.Errorf("param_dub_id = %q, want %s", request.params.Get("param_dub_id"), fixtureDub)
		}
	}
	for _, statement := range []string{
		selectWorkspaceTakes, selectWorkspaceTakeCharges, selectWholePassCharges,
		selectRunningTotal, selectWorkspaceLanguages, selectProjectMetadata,
	} {
		if !seen[statement] {
			t.Errorf("no request carried statement %s", statement)
		}
	}

	// The quoted payloads are the failure the pin avoids, so pin the failures too.
	for statement, payload := range payloads {
		quoted := quoteWorkspacePayload(statement, payload)
		switch statement {
		case selectWorkspaceTakes:
			if _, err := decodeJSONEachRow[workspaceTakeRow](strings.NewReader(quoted)); err == nil {
				t.Errorf("workspaceTakeRow decoded quoted integers %q", quoted)
			}
		case selectWorkspaceTakeCharges:
			if _, err := decodeJSONEachRow[workspaceChargeRow](strings.NewReader(quoted)); err == nil {
				t.Errorf("workspaceChargeRow decoded quoted integers %q", quoted)
			}
		case selectWholePassCharges:
			if _, err := decodeJSONEachRow[wholePassChargeRow](strings.NewReader(quoted)); err == nil {
				t.Errorf("wholePassChargeRow decoded quoted integers %q", quoted)
			}
		case selectRunningTotal:
			if _, err := decodeJSONEachRow[runningTotalRow](strings.NewReader(quoted)); err == nil {
				t.Errorf("runningTotalRow decoded quoted integers %q", quoted)
			}
		case selectWorkspaceLanguages:
			if _, err := decodeJSONEachRow[languageRow](strings.NewReader(quoted)); err == nil {
				t.Errorf("languageRow decoded quoted integers %q", quoted)
			}
		case selectProjectMetadata:
			if _, err := decodeJSONEachRow[projectMetadataRow](strings.NewReader(quoted)); err == nil {
				t.Errorf("projectMetadataRow decoded quoted integers %q", quoted)
			}
		}
	}
}

// TestWorkspaceTakesGroupsAndOrders proves the reader rebuilds the wire shape
// from rows the server returns out of order. It also proves the flag rule: a line
// is flagged only when it holds a take and none of its takes fits.
func TestWorkspaceTakesGroupsAndOrders(t *testing.T) {
	t.Parallel()

	takes := strings.Join([]string{
		// Segment 2 arrives first and holds one short take.
		`{"language":"ml","commit_id":"c1","segment_index":2,"attempt":1,"voice":"voice-b","text":"second","audio_path":"b.wav","repair":"none","peaks":[],"slot_ms":5480,"measured_ms":4200,"delta_ms":-1280,"created_ms":3000}`,
		// Segment 1 holds two attempts. The stretch lands inside the slot.
		`{"language":"ml","commit_id":"c1","segment_index":1,"attempt":2,"voice":"voice-a","text":"first","audio_path":"a2.wav","repair":"atempo","peaks":[],"slot_ms":1820,"measured_ms":1830,"delta_ms":10,"created_ms":5000}`,
		`{"language":"ml","commit_id":"c1","segment_index":1,"attempt":1,"voice":"voice-a","text":"first","audio_path":"a1.wav","repair":"none","peaks":[],"slot_ms":1820,"measured_ms":1680,"delta_ms":-140,"created_ms":4000}`,
	}, "\n") + "\n"
	charges := strings.Join([]string{
		// The stretch attempt owns a synthesize charge.
		`{"language":"ml","commit_id":"c1","segment_index":1,"attempt":2,"kind":"synthesize","unit":"characters","units":29,"unit_price_nanodollars":30000}`,
		`{"language":"ml","commit_id":"c1","segment_index":1,"attempt":1,"kind":"translate","unit":"prompt_tokens","units":21,"unit_price_nanodollars":100}`,
	}, "\n") + "\n"

	client, captured := workspaceStandIn(t, map[string]string{
		selectWorkspaceTakes:       takes,
		selectWorkspaceTakeCharges: charges,
	})
	got, err := client.WorkspaceTakes(context.Background(), fixtureDub)
	if err != nil {
		t.Fatalf("WorkspaceTakes: %v", err)
	}
	if len(got) != 1 || len(got[0].Lines) != 2 {
		t.Fatalf("tracks = %+v, want one track with two lines", got)
	}
	first := got[0].Lines[0]
	if first.SegmentID != 1 || len(first.Takes) != 2 {
		t.Fatalf("first line = %+v, want segment 1 with two takes", first)
	}
	if first.Takes[0].Attempt != 1 || first.Takes[1].Attempt != 2 {
		t.Errorf("take order = %d then %d, want attempt 1 then 2", first.Takes[0].Attempt, first.Takes[1].Attempt)
	}
	if first.Takes[1].Fit.State != api.StateFits || first.Flagged {
		t.Errorf("first line = %+v, want the stretch to fit and clear the flag", first)
	}
	if len(first.Takes[0].Charges) != 1 || first.Takes[0].Charges[0].Kind != "translate" {
		t.Errorf("attempt 1 charges = %+v, want one translate charge", first.Takes[0].Charges)
	}
	if len(first.Takes[1].Charges) != 1 || first.Takes[1].Charges[0].Kind != "synthesize" ||
		first.Takes[1].Charges[0].TakeFile != "a2.wav" {
		t.Errorf("attempt 2 charges = %+v, want one synthesize charge on a2.wav", first.Takes[1].Charges)
	}
	second := got[0].Lines[1]
	if second.SegmentID != 2 || !second.Flagged {
		t.Errorf("second line = %+v, want segment 2 flagged because its only take is short", second)
	}
	if len(captured()) != 2 {
		t.Errorf("request count = %d, want the takes and charges statements", len(captured()))
	}
}

// TestWorkspaceTakesChargeLessTakeMarshalsEmptyCharges drives a take that owns
// no itemized charge. The wire contract declares an array, so the take must
// marshal as [] rather than null and survive a round trip with a non-nil slice.
func TestWorkspaceTakesChargeLessTakeMarshalsEmptyCharges(t *testing.T) {
	t.Parallel()

	takes := `{"language":"ml","commit_id":"c1","segment_index":1,"attempt":1,"voice":"voice-a","text":"hello","audio_path":"a.wav","repair":"none","peaks":[],"slot_ms":1820,"measured_ms":1680,"delta_ms":-140,"created_ms":1000}` + "\n"
	client, _ := workspaceStandIn(t, map[string]string{
		selectWorkspaceTakes:       takes,
		selectWorkspaceTakeCharges: "",
	})
	got, err := client.WorkspaceTakes(context.Background(), fixtureDub)
	if err != nil {
		t.Fatalf("WorkspaceTakes: %v", err)
	}
	if len(got) != 1 || len(got[0].Lines) != 1 || len(got[0].Lines[0].Takes) != 1 {
		t.Fatalf("tracks = %+v, want one track with one line and one take", got)
	}
	take := got[0].Lines[0].Takes[0]
	encoded, err := json.Marshal(take)
	if err != nil {
		t.Fatalf("marshal take: %v", err)
	}
	t.Logf("marshalled take = %s", encoded)
	if !strings.Contains(string(encoded), `"charges":[]`) {
		t.Errorf("marshalled take = %s, want charges as an empty array", encoded)
	}
	if strings.Contains(string(encoded), `"charges":null`) {
		t.Errorf("marshalled take = %s, want no null charges", encoded)
	}
	var decoded api.Take
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal take: %v", err)
	}
	if decoded.Charges == nil {
		t.Errorf("decoded charges = nil from %s, want a non-nil empty slice", encoded)
	}
}

// TestWorkspaceQueriesBindAndShape pins the statement shape. Every read binds
// dub_id as a parameter, reads a FINAL view, orders its rows, and splits
// whole-pass work from itemized work by kind.
func TestWorkspaceQueriesBindAndShape(t *testing.T) {
	t.Parallel()

	client, captured := workspaceStandIn(t, workspacePayloads())
	ctx := context.Background()
	if _, err := client.WorkspaceTakes(ctx, fixtureDub); err != nil {
		t.Fatalf("WorkspaceTakes: %v", err)
	}
	if _, err := client.WholePassCharges(ctx, fixtureDub); err != nil {
		t.Fatalf("WholePassCharges: %v", err)
	}
	if _, err := client.RunningTotal(ctx, fixtureDub); err != nil {
		t.Fatalf("RunningTotal: %v", err)
	}
	if _, err := client.Languages(ctx, fixtureDub); err != nil {
		t.Fatalf("Languages: %v", err)
	}
	if _, err := client.ProjectMetadata(ctx, fixtureDub); err != nil {
		t.Fatalf("ProjectMetadata: %v", err)
	}

	requests := captured()
	if len(requests) != 6 {
		t.Fatalf("request count = %d, want 6", len(requests))
	}
	for _, request := range requests {
		statement := request.query
		if !strings.Contains(statement, "{dub_id:String}") {
			t.Errorf("query omitted the bound dub_id: %s", statement)
		}
		if strings.Contains(statement, fixtureDub) {
			t.Errorf("query interpolated the dub id: %s", statement)
		}
		// A single aggregate row is ordered by construction, so only the
		// multi-row statements must carry an ORDER BY.
		if statement != selectRunningTotal && statement != selectProjectMetadata &&
			!strings.Contains(statement, "ORDER BY") {
			t.Errorf("query omitted ORDER BY: %s", statement)
		}
		if strings.Contains(statement, "_raw") {
			t.Errorf("query read a raw table instead of its FINAL view: %s", statement)
		}
		if strings.Contains(statement, "FROM takes") == false &&
			strings.Contains(statement, "FROM charges") == false &&
			strings.Contains(statement, "FROM commits") == false {
			t.Errorf("query named no ledger view: %s", statement)
		}
		if request.params.Get("param_dub_id") != fixtureDub {
			t.Errorf("param_dub_id = %q, want %s", request.params.Get("param_dub_id"), fixtureDub)
		}
		assertPinnedQuerySettings(t, request)
	}
	if !strings.Contains(selectWorkspaceTakeCharges, "kind IN ('translate', 'synthesize')") {
		t.Error("itemized charge statement does not exclude the whole-pass kind")
	}
	if !strings.Contains(selectWholePassCharges, "kind = 'segment'") ||
		!strings.Contains(selectWholePassCharges, "charges.kind = 'agent'") {
		t.Error("whole-pass statement does not select both whole-pass kinds by their real column")
	}
	if !strings.Contains(selectWholePassCharges, "uniqExact(turn_id)") ||
		!strings.Contains(selectWholePassCharges, "HAVING count() > 0") {
		t.Error("whole-pass statement does not fold agent turns into one display row")
	}
	if !strings.Contains(selectRunningTotal, "uniqExactIf(tuple(turn_id, call_index), kind = 'agent')") {
		t.Error("running total does not count distinct agent calls")
	}
	for _, kind := range []string{"segment", "translate", "synthesize"} {
		if !strings.Contains(selectRunningTotal, "uniqExactIf(tuple(language, commit_id, segment_index, attempt, kind, provider), kind = '"+kind+"')") {
			t.Errorf("running total does not count %s by call identity", kind)
		}
	}
}

// TestWholePassChargesFoldsIdenticalAgentTurns derives the fold from the
// itemized agent rows the writer records, then drives the client row mapping.
// The expected units and total come from the seed rather than a canned number.
func TestWholePassChargesFoldsIdenticalAgentTurns(t *testing.T) {
	t.Parallel()

	// The writer records one prompt row and one candidate row per agent turn,
	// so two identical turns seed four itemized rows.
	type agentUnit struct {
		turn    string
		units   int64
		priceNs int64
	}
	seed := []agentUnit{
		{turn: "turn-1", units: 1200, priceNs: 150},
		{turn: "turn-1", units: 340, priceNs: 600},
		{turn: "turn-2", units: 1200, priceNs: 150},
		{turn: "turn-2", units: 340, priceNs: 600},
	}
	turns := map[string]bool{}
	var totalNs int64
	for _, unit := range seed {
		turns[unit.turn] = true
		totalNs += unit.units * unit.priceNs
	}
	if len(turns) != 2 || totalNs != 768000 {
		t.Fatalf("seed folds to %d turns and %d nanodollars, want 2 and 768000", len(turns), totalNs)
	}

	folded, err := json.Marshal(wholePassChargeRow{
		Kind:             "agent",
		Unit:             "turns",
		Units:            int64(len(turns)),
		TotalNanodollars: totalNs,
	})
	if err != nil {
		t.Fatalf("marshal folded row: %v", err)
	}
	client, _ := workspaceStandIn(t, map[string]string{
		selectWholePassCharges: string(folded) + "\n",
	})
	charges, err := client.WholePassCharges(context.Background(), fixtureDub)
	if err != nil {
		t.Fatalf("WholePassCharges: %v", err)
	}
	if len(charges) != 1 {
		t.Fatalf("whole-pass charges = %+v, want one folded agent row", charges)
	}
	if charges[0].Kind != "agent" || charges[0].Units != int64(len(turns)) ||
		charges[0].UnitPriceNanodollars != 0 || charges[0].TotalNanodollars != cost.Price(totalNs) {
		t.Errorf("folded agent charge = %+v, want agent with %d turns and %d nanodollars", charges[0], len(turns), totalNs)
	}
	if charges[0].SegmentID != nil || charges[0].TakeFile != "" {
		t.Errorf("folded agent charge carries take identity: %+v", charges[0])
	}
}

// TestWorkspaceReadsRejectBlankDubID proves each read fails before transport.
func TestWorkspaceReadsRejectBlankDubID(t *testing.T) {
	t.Parallel()

	client, captured := workspaceStandIn(t, workspacePayloads())
	ctx := context.Background()
	if _, err := client.WorkspaceTakes(ctx, "  "); err == nil {
		t.Error("WorkspaceTakes accepted a blank dub id")
	}
	if _, err := client.WholePassCharges(ctx, ""); err == nil {
		t.Error("WholePassCharges accepted a blank dub id")
	}
	if _, err := client.RunningTotal(ctx, ""); err == nil {
		t.Error("RunningTotal accepted a blank dub id")
	}
	if _, err := client.Languages(ctx, ""); err == nil {
		t.Error("Languages accepted a blank dub id")
	}
	if _, err := client.ProjectMetadata(ctx, ""); err == nil {
		t.Error("ProjectMetadata accepted a blank dub id")
	}
	if len(captured()) != 0 {
		t.Errorf("blank dub id reached the server %d times", len(captured()))
	}
}

// TestRunningTotalWithNoCharges pins the empty-project answer. The stand-in
// serves the row a real server returns: every aggregate answers zero.
func TestRunningTotalWithNoCharges(t *testing.T) {
	t.Parallel()

	client, _ := workspaceStandIn(t, map[string]string{
		selectRunningTotal: `{"total_nanodollars":0,"segment_calls":0,"translate_calls":0,"synthesize_calls":0,"agent_calls":0}` + "\n",
	})
	total, err := client.RunningTotal(context.Background(), fixtureDub)
	if err != nil {
		t.Fatalf("RunningTotal: %v", err)
	}
	if total.TotalNanodollars != 0 {
		t.Errorf("running total = %d, want 0", total.TotalNanodollars)
	}
	if total.Covers != "0 segment calls, 0 translation calls, 0 render calls, and 0 agent calls." {
		t.Errorf("running total covers = %q", total.Covers)
	}
}

// TestProjectMetadataWithNoCommits pins the empty-project summary. A server
// answers one row with a zero count, so the reader must leave the timestamps
// empty rather than render the epoch.
func TestProjectMetadataWithNoCommits(t *testing.T) {
	t.Parallel()

	client, _ := workspaceStandIn(t, map[string]string{
		selectProjectMetadata: `{"commit_count":0,"created_ms":0,"updated_ms":0,"languages":[]}` + "\n",
	})
	meta, err := client.ProjectMetadata(context.Background(), fixtureDub)
	if err != nil {
		t.Fatalf("ProjectMetadata: %v", err)
	}
	if meta.ID != fixtureDub {
		t.Errorf("project metadata id = %q, want %s", meta.ID, fixtureDub)
	}
	if meta.CreatedAt != "" || meta.UpdatedAt != "" {
		t.Errorf("project timestamps = %q and %q, want empty", meta.CreatedAt, meta.UpdatedAt)
	}
	if len(meta.Languages) != 0 {
		t.Errorf("project languages = %v, want none", meta.Languages)
	}
}

// TestWorkspaceTakesRecoversAtempoStretch pins L3. The ledger stores the ratio
// in repair_detail, so the reader must parse it into thousandths. A take with
// no repair omits the wire field.
func TestWorkspaceTakesRecoversAtempoStretch(t *testing.T) {
	t.Parallel()

	takes := strings.Join([]string{
		`{"language":"ml","commit_id":"c1","segment_index":1,"attempt":1,"voice":"voice-a","text":"hello","audio_path":"a.wav","repair":"atempo","repair_detail":"atempo stretch applied at ratio 1.0500","peaks":[],"slot_ms":1820,"measured_ms":1830,"delta_ms":10,"created_ms":1000}`,
		`{"language":"ml","commit_id":"c1","segment_index":2,"attempt":1,"voice":"voice-a","text":"bye","audio_path":"b.wav","repair":"none","repair_detail":"","peaks":[],"slot_ms":1000,"measured_ms":1000,"delta_ms":0,"created_ms":2000}`,
	}, "\n") + "\n"
	client, _ := workspaceStandIn(t, map[string]string{
		selectWorkspaceTakes:       takes,
		selectWorkspaceTakeCharges: "",
	})
	got, err := client.WorkspaceTakes(context.Background(), fixtureDub)
	if err != nil {
		t.Fatalf("WorkspaceTakes: %v", err)
	}
	if len(got) != 1 || len(got[0].Lines) != 2 {
		t.Fatalf("tracks = %+v, want one track with two lines", got)
	}
	atempo := got[0].Lines[0].Takes[0]
	if atempo.StretchFactorMilli != 1050 {
		t.Errorf("atempo stretch factor = %d, want 1050", atempo.StretchFactorMilli)
	}
	plain := got[0].Lines[1].Takes[0]
	if plain.StretchFactorMilli != 0 {
		t.Errorf("none stretch factor = %d, want 0", plain.StretchFactorMilli)
	}
	atempoJSON, err := json.Marshal(atempo)
	if err != nil {
		t.Fatalf("marshal atempo take: %v", err)
	}
	plainJSON, err := json.Marshal(plain)
	if err != nil {
		t.Fatalf("marshal none take: %v", err)
	}
	t.Logf("atempo take: %s", atempoJSON)
	t.Logf("none take: %s", plainJSON)
	if !strings.Contains(string(atempoJSON), `"stretch_factor_milli":1050`) {
		t.Errorf("atempo take = %s, want stretch_factor_milli 1050", atempoJSON)
	}
	if strings.Contains(string(plainJSON), "stretch_factor_milli") {
		t.Errorf("none take = %s, want the field omitted", plainJSON)
	}
	if !strings.Contains(selectWorkspaceTakes, "repair_detail") {
		t.Error("takes statement does not select repair_detail")
	}
}

// indexPayloads is the grouped index answer for one project that holds takes
// and charges. A second project in the same request holds no row at all.
func indexPayloads() map[string]string {
	return map[string]string{
		selectIndexTakes: `{"dub_id":"d1","language":"ml","segment_index":1,"slot_ms":1000,"measured_ms":1000}` + "\n" +
			`{"dub_id":"d1","language":"ml","segment_index":2,"slot_ms":1000,"measured_ms":500}` + "\n",
		selectIndexSegmentCounts: `{"dub_id":"d1","language":"ml","segment_count":2}` + "\n",
		selectIndexTotals:        `{"dub_id":"d1","total_nanodollars":72309000}` + "\n",
	}
}

// TestIndexFactsGroupsAcrossProjects pins the grouped read. One project and
// four projects each cost three statements, because every id rides one bound
// array parameter. It also pins the readiness rule on the same rows the
// workspace reads: segment 2 holds only a short take, so the project reads
// review, and the charge sum rides the same row.
func TestIndexFactsGroupsAcrossProjects(t *testing.T) {
	t.Parallel()

	client, captured := workspaceStandIn(t, indexPayloads())
	ctx := context.Background()

	one, err := client.IndexFacts(ctx, []string{"d1"})
	if err != nil {
		t.Fatalf("IndexFacts one: %v", err)
	}
	if got := len(captured()); got != 3 {
		t.Fatalf("one-project request count = %d, want 3", got)
	}
	fact, ok := one["d1"]
	if !ok {
		t.Fatalf("IndexFacts one = %+v, want a fact for d1", one)
	}
	if fact.Readiness != api.ReadinessReview {
		t.Errorf("readiness = %q, want %s", fact.Readiness, api.ReadinessReview)
	}
	if fact.TotalNanodollars != cost.Price(72309000) {
		t.Errorf("total = %d nanodollars, want 72309000", fact.TotalNanodollars)
	}
	if _, ok := one["d2"]; ok {
		t.Errorf("IndexFacts invented a fact for a project the ledger holds no row for: %+v", one)
	}

	many, err := client.IndexFacts(ctx, []string{"d1", "d2", "d3", "d4"})
	if err != nil {
		t.Fatalf("IndexFacts many: %v", err)
	}
	if len(many) != 1 {
		t.Errorf("IndexFacts many = %+v, want only d1", many)
	}
	requests := captured()
	if len(requests) != 6 {
		t.Fatalf("two-call request count = %d, want 3 per call", len(requests))
	}
	for _, request := range requests[3:] {
		assertPinnedQuerySettings(t, request)
		if got := request.params.Get("param_dub_ids"); got != "['d1','d2','d3','d4']" {
			t.Errorf("param_dub_ids = %q, want the whole id list", got)
		}
	}
}

// TestIndexFactsLeavesAnEmptyProjectListUnqueried pins the short circuit. An
// index with no project row must not send a statement with an empty id list.
func TestIndexFactsLeavesAnEmptyProjectListUnqueried(t *testing.T) {
	t.Parallel()

	client, captured := workspaceStandIn(t, indexPayloads())
	facts, err := client.IndexFacts(context.Background(), []string{"", ""})
	if err != nil {
		t.Fatalf("IndexFacts: %v", err)
	}
	if len(facts) != 0 {
		t.Errorf("facts = %+v, want none", facts)
	}
	if got := len(captured()); got != 0 {
		t.Errorf("request count = %d, want 0", got)
	}
}
