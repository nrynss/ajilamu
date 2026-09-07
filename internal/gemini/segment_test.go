package gemini

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/nrynss/ajilamu/internal/config"
	"github.com/nrynss/ajilamu/internal/cost"
	"golang.org/x/oauth2"
)

// fixturePath locates the golden segment fixture from this package.
const fixturePath = "../../testdata/segments.json"

// testConfig builds a config whose Vertex base URL points at the fake server.
func testConfig(base string) *config.Config {
	return &config.Config{
		GeminiModel:          "gemini-3.8-flash",
		GoogleCloudProject:   "test-project",
		GoogleCloudLocation:  "us-central1",
		VertexOpenAPIBaseURL: base,
	}
}

// envelopeJSON wraps one parts text into the Vertex reply shape.
func envelopeJSON(t *testing.T, text string) []byte {
	t.Helper()
	raw, err := json.Marshal(generateResponse{
		Candidates: []generateCandidate{{
			Content: generateContent{Parts: []generatePart{{Text: text}}},
		}},
	})
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	return raw
}

func readFixture(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return raw
}

func TestSegmentSuccess(t *testing.T) {
	fixture := readFixture(t)

	var gotAuth string
	var gotPath string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		gotBody = body
		w.Write(envelopeJSON(t, string(fixture)))
	}))
	defer srv.Close()

	ledger := cost.NewLedger()
	seg, err := NewSegmenter(testConfig(srv.URL), oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "token-123"}), ledger, nil, cost.DefaultRateCard())
	if err != nil {
		t.Fatalf("NewSegmenter: %v", err)
	}

	in := Input{Data: []byte("fake audio bytes"), MIMEType: "audio/mp3"}
	got, err := seg.Segment(context.Background(), in)
	if err != nil {
		t.Fatalf("Segment: %v", err)
	}

	want, err := decodeSegments(string(fixture))
	if err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("segments mismatch\n got: %#v\nwant: %#v", got, want)
	}

	if gotAuth != "Bearer token-123" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer token-123")
	}
	wantPath := "/v1/projects/test-project/locations/us-central1/publishers/google/models/gemini-3.8-flash:generateContent"
	if gotPath != wantPath {
		t.Errorf("request path = %q, want %q", gotPath, wantPath)
	}

	var req generateRequest
	if err := json.Unmarshal(gotBody, &req); err != nil {
		t.Fatalf("decode request payload: %v", err)
	}
	if len(req.Contents) != 1 || len(req.Contents[0].Parts) != 2 {
		t.Fatalf("payload shape = %#v, want one content with two parts", req)
	}
	media := req.Contents[0].Parts[0].InlineData
	if media == nil {
		t.Fatalf("payload carries no inlineData: %s", gotBody)
	}
	if media.MIMEType != "audio/mp3" {
		t.Errorf("inlineData.mimeType = %q, want audio/mp3", media.MIMEType)
	}
	if media.Data != base64.StdEncoding.EncodeToString([]byte("fake audio bytes")) {
		t.Errorf("inlineData.data is not the base64 of the input buffer")
	}
	if req.Contents[0].Parts[1].Text != segmentPrompt {
		t.Errorf("text part does not carry the verbatim prompt")
	}
	if req.GenerationConfig.ResponseMIMEType != "application/json" {
		t.Errorf("responseMimeType = %q, want application/json", req.GenerationConfig.ResponseMIMEType)
	}

	charges := ledger.Charges()
	if len(charges) != 1 {
		t.Fatalf("charges = %d entries, want exactly one", len(charges))
	}
	wantCharge := cost.Charge{
		Kind:      cost.ChargeSegment,
		TakeID:    0,
		Units:     len(segmentPrompt),
		UnitPrice: cost.DefaultRateCard().SegmentPerInputChar,
	}
	if charges[0] != wantCharge {
		t.Errorf("charge = %#v, want %#v", charges[0], wantCharge)
	}
	if charges[0].Total() != wantCharge.Total() {
		t.Errorf("charge total = %v, want %v", charges[0].Total(), wantCharge.Total())
	}
}

func TestSegmentHTTP500(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "upstream exploded", http.StatusInternalServerError)
	}))
	defer srv.Close()

	ledger := cost.NewLedger()
	seg, err := NewSegmenter(testConfig(srv.URL), oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "t"}), ledger, nil, cost.DefaultRateCard())
	if err != nil {
		t.Fatalf("NewSegmenter: %v", err)
	}

	_, err = seg.Segment(context.Background(), Input{Data: []byte("x"), MIMEType: "audio/mp3"})
	if err == nil {
		t.Fatal("Segment succeeded, want HTTP 500 error")
	}
	if !strings.Contains(err.Error(), "500") || !strings.Contains(err.Error(), "upstream exploded") {
		t.Errorf("error = %v, want status 500 and trimmed body", err)
	}
	if charges := ledger.Charges(); len(charges) != 0 {
		t.Errorf("failed pass recorded %d charges, want zero", len(charges))
	}
}

func TestSegmentZeroCandidates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"candidates":[]}`))
	}))
	defer srv.Close()

	ledger := cost.NewLedger()
	seg, err := NewSegmenter(testConfig(srv.URL), oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "t"}), ledger, nil, cost.DefaultRateCard())
	if err != nil {
		t.Fatalf("NewSegmenter: %v", err)
	}

	_, err = seg.Segment(context.Background(), Input{Data: []byte("x"), MIMEType: "audio/mp3"})
	if err == nil || !strings.Contains(err.Error(), "no candidates") {
		t.Errorf("error = %v, want zero candidates failure", err)
	}
	if charges := ledger.Charges(); len(charges) != 0 {
		t.Errorf("failed pass recorded %d charges, want zero", len(charges))
	}
}

func TestSegmentEmptyPartsText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"  "}]}}]}`))
	}))
	defer srv.Close()

	ledger := cost.NewLedger()
	seg, err := NewSegmenter(testConfig(srv.URL), oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "t"}), ledger, nil, cost.DefaultRateCard())
	if err != nil {
		t.Fatalf("NewSegmenter: %v", err)
	}

	_, err = seg.Segment(context.Background(), Input{Data: []byte("x"), MIMEType: "audio/mp3"})
	if err == nil || !strings.Contains(err.Error(), "empty parts text") {
		t.Errorf("error = %v, want empty parts text failure", err)
	}
	if charges := ledger.Charges(); len(charges) != 0 {
		t.Errorf("failed pass recorded %d charges, want zero", len(charges))
	}
}

func TestSegmentMalformedInnerJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(envelopeJSON(t, "{not json"))
	}))
	defer srv.Close()

	ledger := cost.NewLedger()
	seg, err := NewSegmenter(testConfig(srv.URL), oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "t"}), ledger, nil, cost.DefaultRateCard())
	if err != nil {
		t.Fatalf("NewSegmenter: %v", err)
	}

	_, err = seg.Segment(context.Background(), Input{Data: []byte("x"), MIMEType: "audio/mp3"})
	if err == nil || !strings.Contains(err.Error(), "parse model segment JSON") {
		t.Errorf("error = %v, want malformed inner JSON failure", err)
	}
	if charges := ledger.Charges(); len(charges) != 0 {
		t.Errorf("failed pass recorded %d charges, want zero", len(charges))
	}
}

func TestSegmentDurationMismatch(t *testing.T) {
	inner := `[{"id":1,"start_ms":1000,"end_ms":2000,"duration_ms":999,"text":"Hi","speaker":"Suni Williams","emotion":"Warm"}]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(envelopeJSON(t, inner))
	}))
	defer srv.Close()

	ledger := cost.NewLedger()
	seg, err := NewSegmenter(testConfig(srv.URL), oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "t"}), ledger, nil, cost.DefaultRateCard())
	if err != nil {
		t.Fatalf("NewSegmenter: %v", err)
	}

	_, err = seg.Segment(context.Background(), Input{Data: []byte("x"), MIMEType: "audio/mp3"})
	if err == nil || !strings.Contains(err.Error(), "duration_ms") {
		t.Errorf("error = %v, want duration mismatch failure", err)
	}
	if charges := ledger.Charges(); len(charges) != 0 {
		t.Errorf("failed pass recorded %d charges, want zero", len(charges))
	}
}

func TestDecodeSegmentsValidation(t *testing.T) {
	valid := wireSegment{ID: 1, StartMs: 1000, EndMs: 2000, DurationMs: 1000, Text: "Hi", Speaker: "Suni Williams", Emotion: "Warm"}

	cases := []struct {
		name    string
		mutate  func(w *wireSegment)
		wantErr string
	}{
		{"start not before end", func(w *wireSegment) { w.EndMs = 1000 }, "does not precede"},
		{"duration mismatch", func(w *wireSegment) { w.DurationMs = 999 }, "duration_ms"},
		{"empty text", func(w *wireSegment) { w.Text = " " }, "text is empty"},
		{"empty speaker", func(w *wireSegment) { w.Speaker = "" }, "speaker is empty"},
		{"empty emotion", func(w *wireSegment) { w.Emotion = "" }, "emotion is empty"},
	}

	for _, tc := range cases {
		seg := valid
		tc.mutate(&seg)
		raw, err := json.Marshal([]wireSegment{seg})
		if err != nil {
			t.Fatalf("%s: marshal: %v", tc.name, err)
		}
		if _, err := decodeSegments(string(raw)); err == nil || !strings.Contains(err.Error(), tc.wantErr) {
			t.Errorf("%s: error = %v, want %q", tc.name, err, tc.wantErr)
		}
	}

	decreasing := []wireSegment{valid, {ID: 1, StartMs: 3000, EndMs: 4000, DurationMs: 1000, Text: "More", Speaker: "Mark Vande Hei", Emotion: "Warm"}}
	raw, err := json.Marshal(decreasing)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if _, err := decodeSegments(string(raw)); err == nil || !strings.Contains(err.Error(), "does not increase") {
		t.Errorf("decreasing ids: error = %v, want non-increasing id failure", err)
	}

	if _, err := decodeSegments("[]"); err == nil || !strings.Contains(err.Error(), "zero segments") {
		t.Errorf("empty list: error = %v, want zero segments failure", err)
	}
}

func TestDecodeSegmentsGoldenFixture(t *testing.T) {
	segments, err := decodeSegments(string(readFixture(t)))
	if err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	if len(segments) != 8 {
		t.Fatalf("segments = %d, want 8", len(segments))
	}
	first := segments[0]
	if first.ID != 1 || first.StartMs != 5908 || first.EndMs != 7728 {
		t.Errorf("first = %#v, want id 1 over 5908 to 7728", first)
	}
	if first.Speaker.Name != "Suni Williams" || first.Emotion != "Warm" {
		t.Errorf("first speaker or emotion = %#v, %q", first.Speaker, first.Emotion)
	}
	last := segments[7]
	if last.Speaker.Name != "Mark Vande Hei" || last.EndMs != 54491 {
		t.Errorf("last = %#v, want Mark Vande Hei ending at 54491", last)
	}
}

func TestEndpointURL(t *testing.T) {
	cfg := testConfig("")
	got, err := endpointURL(cfg)
	if err != nil {
		t.Fatalf("endpointURL: %v", err)
	}
	want := "https://aiplatform.googleapis.com/v1/projects/test-project/locations/us-central1/publishers/google/models/gemini-3.8-flash:generateContent"
	if got != want {
		t.Errorf("endpointURL = %q, want %q", got, want)
	}

	cfg.VertexOpenAPIBaseURL = "http://fake.local/"
	got, err = endpointURL(cfg)
	if err != nil {
		t.Fatalf("endpointURL override: %v", err)
	}
	want = "http://fake.local/v1/projects/test-project/locations/us-central1/publishers/google/models/gemini-3.8-flash:generateContent"
	if got != want {
		t.Errorf("endpointURL override = %q, want %q", got, want)
	}

	custom := testConfig("")
	custom.GoogleCloudProject = "ajilamu-prod"
	custom.GoogleCloudLocation = "europe-west4"
	custom.GeminiModel = "gemini-2.5-flash"
	got, err = endpointURL(custom)
	if err != nil {
		t.Fatalf("endpointURL non-default: %v", err)
	}
	want = "https://aiplatform.googleapis.com/v1/projects/ajilamu-prod/locations/europe-west4/publishers/google/models/gemini-2.5-flash:generateContent"
	if got != want {
		t.Errorf("endpointURL non-default = %q, want %q", got, want)
	}

	for name, mutate := range map[string]func(*config.Config){
		"project":  func(c *config.Config) { c.GoogleCloudProject = "" },
		"location": func(c *config.Config) { c.GoogleCloudLocation = "" },
		"model":    func(c *config.Config) { c.GeminiModel = "" },
	} {
		broken := testConfig("")
		mutate(broken)
		if _, err := endpointURL(broken); err == nil {
			t.Errorf("missing %s: endpointURL succeeded, want error", name)
		}
	}
}

func TestNewSegmenterNilArguments(t *testing.T) {
	cfg := testConfig("")
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "t"})
	var rec ChargeRecorder = cost.NewLedger()

	if _, err := NewSegmenter(nil, ts, rec, nil, cost.DefaultRateCard()); err == nil {
		t.Error("nil config accepted, want error")
	}
	if _, err := NewSegmenter(cfg, nil, rec, nil, cost.DefaultRateCard()); err == nil {
		t.Error("nil token source accepted, want error")
	}
	if _, err := NewSegmenter(cfg, ts, nil, nil, cost.DefaultRateCard()); err == nil {
		t.Error("nil charge recorder accepted, want error")
	}
	if _, err := NewSegmenter(cfg, ts, rec, nil, cost.DefaultRateCard()); err != nil {
		t.Errorf("valid arguments rejected: %v", err)
	}
}
