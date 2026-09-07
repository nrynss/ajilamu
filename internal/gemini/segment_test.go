package gemini

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"google.golang.org/genai"

	"github.com/nrynss/ajilamu/internal/config"
	"github.com/nrynss/ajilamu/internal/cost"
)

// fixturePath locates the golden segment fixture from this package.
const fixturePath = "../../testdata/segments.json"

// testConfig builds a config for offline tests. It never reads process env.
func testConfig() *config.Config {
	return &config.Config{
		GeminiModel:         "gemini-3.8-flash",
		GoogleCloudProject:  "test-project",
		GoogleCloudLocation: "global",
	}
}

// fakeGenerator records GenerateContent arguments and returns a scripted reply.
type fakeGenerator struct {
	err          error
	text         string
	promptTokens int32
	candTokens   int32
	candidates   []*genai.Candidate
	gotModel     string
	gotContents  []*genai.Content
	gotConfig    *genai.GenerateContentConfig
}

func (f *fakeGenerator) GenerateContent(ctx context.Context, model string, contents []*genai.Content, cfg *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error) {
	f.gotModel = model
	f.gotContents = contents
	f.gotConfig = cfg
	if f.err != nil {
		return nil, f.err
	}
	resp := &genai.GenerateContentResponse{
		UsageMetadata: &genai.GenerateContentResponseUsageMetadata{
			PromptTokenCount:     f.promptTokens,
			CandidatesTokenCount: f.candTokens,
		},
	}
	if f.candidates != nil {
		resp.Candidates = f.candidates
		return resp, nil
	}
	resp.Candidates = []*genai.Candidate{{
		Content: &genai.Content{Parts: []*genai.Part{{Text: f.text}}},
	}}
	return resp, nil
}

func readFixture(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return raw
}

func TestSegmentPromptByteLength(t *testing.T) {
	if got := len(segmentPrompt); got != 658 {
		t.Fatalf("segmentPrompt is %d bytes, want 658", got)
	}
}

func TestSegmentSuccess(t *testing.T) {
	fixture := readFixture(t)
	gen := &fakeGenerator{
		text:         string(fixture),
		promptTokens: 41,
		candTokens:   17,
	}
	ledger := cost.NewLedger()
	card := cost.DefaultRateCard()
	seg, err := newSegmenterForTest(testConfig(), ledger, card, gen)
	if err != nil {
		t.Fatalf("newSegmenterForTest: %v", err)
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

	if gen.gotModel != "gemini-3.8-flash" {
		t.Errorf("model = %q, want gemini-3.8-flash", gen.gotModel)
	}
	if len(gen.gotContents) != 1 || len(gen.gotContents[0].Parts) != 2 {
		t.Fatalf("contents shape = %#v, want one content with two parts", gen.gotContents)
	}
	media := gen.gotContents[0].Parts[0].InlineData
	if media == nil {
		t.Fatal("first part carries no InlineData")
	}
	if media.MIMEType != "audio/mp3" {
		t.Errorf("inlineData.MIMEType = %q, want audio/mp3", media.MIMEType)
	}
	if !bytes.Equal(media.Data, []byte("fake audio bytes")) {
		t.Errorf("inlineData.Data is not the input buffer")
	}
	if gen.gotContents[0].Parts[1].Text != segmentPrompt {
		t.Errorf("text part does not carry the verbatim prompt")
	}
	if gen.gotConfig == nil {
		t.Fatal("GenerateContentConfig is nil")
	}
	if gen.gotConfig.ResponseMIMEType != "application/json" {
		t.Errorf("ResponseMIMEType = %q, want application/json", gen.gotConfig.ResponseMIMEType)
	}
	if gen.gotConfig.ResponseSchema == nil {
		t.Fatal("ResponseSchema is nil")
	}
	if gen.gotConfig.ResponseSchema.Type != genai.TypeArray {
		t.Errorf("ResponseSchema.Type = %q, want ARRAY", gen.gotConfig.ResponseSchema.Type)
	}
	if gen.gotConfig.ResponseSchema.Items == nil {
		t.Fatal("ResponseSchema.Items is nil")
	}
	wantRequired := []string{"id", "start_ms", "end_ms", "duration_ms", "text", "speaker", "emotion"}
	if !reflect.DeepEqual(gen.gotConfig.ResponseSchema.Items.Required, wantRequired) {
		t.Errorf("schema required = %v, want %v", gen.gotConfig.ResponseSchema.Items.Required, wantRequired)
	}

	charges := ledger.Charges()
	if len(charges) != 1 {
		t.Fatalf("charges = %d entries, want exactly one", len(charges))
	}
	wantCharge := cost.Charge{
		Kind:               cost.ChargeSegment,
		TakeID:             0,
		PromptTokens:       41,
		CandidateTokens:    17,
		PromptUnitPrice:    card.SegmentPerPromptToken,
		CandidateUnitPrice: card.SegmentPerCandidateToken,
	}
	if charges[0] != wantCharge {
		t.Errorf("charge = %#v, want %#v", charges[0], wantCharge)
	}
	wantTotal := cost.Price(41)*card.SegmentPerPromptToken + cost.Price(17)*card.SegmentPerCandidateToken
	if charges[0].Total() != wantTotal {
		t.Errorf("charge total = %v, want %v", charges[0].Total(), wantTotal)
	}
	if charges[0].Units == len(segmentPrompt) {
		t.Errorf("charge billed on prompt character length %d, want UsageMetadata tokens", charges[0].Units)
	}
}

func TestSegmentGenerateErrorRecordsNoCharge(t *testing.T) {
	gen := &fakeGenerator{err: errors.New("upstream exploded")}
	ledger := cost.NewLedger()
	seg, err := newSegmenterForTest(testConfig(), ledger, cost.DefaultRateCard(), gen)
	if err != nil {
		t.Fatalf("newSegmenterForTest: %v", err)
	}

	_, err = seg.Segment(context.Background(), Input{Data: []byte("x"), MIMEType: "audio/mp3"})
	if err == nil {
		t.Fatal("Segment succeeded, want generateContent error")
	}
	if !strings.Contains(err.Error(), "upstream exploded") {
		t.Errorf("error = %v, want upstream exploded", err)
	}
	if charges := ledger.Charges(); len(charges) != 0 {
		t.Errorf("failed pass recorded %d charges, want zero", len(charges))
	}
}

func TestSegmentZeroCandidates(t *testing.T) {
	gen := &fakeGenerator{candidates: []*genai.Candidate{}}
	ledger := cost.NewLedger()
	seg, err := newSegmenterForTest(testConfig(), ledger, cost.DefaultRateCard(), gen)
	if err != nil {
		t.Fatalf("newSegmenterForTest: %v", err)
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
	gen := &fakeGenerator{text: "  "}
	ledger := cost.NewLedger()
	seg, err := newSegmenterForTest(testConfig(), ledger, cost.DefaultRateCard(), gen)
	if err != nil {
		t.Fatalf("newSegmenterForTest: %v", err)
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
	gen := &fakeGenerator{text: "{not json"}
	ledger := cost.NewLedger()
	seg, err := newSegmenterForTest(testConfig(), ledger, cost.DefaultRateCard(), gen)
	if err != nil {
		t.Fatalf("newSegmenterForTest: %v", err)
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
	gen := &fakeGenerator{text: inner}
	ledger := cost.NewLedger()
	seg, err := newSegmenterForTest(testConfig(), ledger, cost.DefaultRateCard(), gen)
	if err != nil {
		t.Fatalf("newSegmenterForTest: %v", err)
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

func TestNewSegmenterNilArguments(t *testing.T) {
	cfg := testConfig()
	var rec ChargeRecorder = cost.NewLedger()
	client := testSDKClient(t)

	if _, err := NewSegmenter(nil, rec, cost.DefaultRateCard(), client); err == nil {
		t.Error("nil config accepted, want error")
	}
	if _, err := NewSegmenter(cfg, nil, cost.DefaultRateCard(), client); err == nil {
		t.Error("nil charge recorder accepted, want error")
	}
	if _, err := NewSegmenter(cfg, rec, cost.DefaultRateCard(), client); err != nil {
		t.Errorf("valid arguments rejected: %v", err)
	}
}

func TestNewVertexClientRejectsMissingFields(t *testing.T) {
	for name, mutate := range map[string]func(*config.Config){
		"project":  func(c *config.Config) { c.GoogleCloudProject = "" },
		"location": func(c *config.Config) { c.GoogleCloudLocation = "" },
		"model":    func(c *config.Config) { c.GeminiModel = "" },
	} {
		broken := testConfig()
		mutate(broken)
		if _, err := newVertexClient(broken); err == nil {
			t.Errorf("missing %s: newVertexClient succeeded, want error", name)
		}
	}
}

func TestNewSegmenterUsesSDKClient(t *testing.T) {
	fixture := readFixture(t)
	var gotBody []byte
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		gotBody = body
		resp := map[string]any{
			"candidates": []any{
				map[string]any{
					"content": map[string]any{
						"parts": []any{map[string]any{"text": string(fixture)}},
					},
				},
			},
			"usageMetadata": map[string]any{
				"promptTokenCount":     41,
				"candidatesTokenCount": 17,
			},
		}
		raw, err := json.Marshal(resp)
		if err != nil {
			t.Errorf("marshal fake reply: %v", err)
			http.Error(w, "marshal", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(raw)
	}))
	t.Cleanup(srv.Close)

	client, err := genai.NewClient(context.Background(), &genai.ClientConfig{
		Backend:    genai.BackendVertexAI,
		Project:    "test-project",
		Location:   "us-central1",
		HTTPClient: srv.Client(),
		HTTPOptions: genai.HTTPOptions{
			BaseURL: srv.URL,
		},
	})
	if err != nil {
		t.Fatalf("genai.NewClient: %v", err)
	}

	ledger := cost.NewLedger()
	card := cost.DefaultRateCard()
	seg, err := NewSegmenter(testConfig(), ledger, card, client)
	if err != nil {
		t.Fatalf("NewSegmenter: %v", err)
	}
	got, err := seg.Segment(context.Background(), Input{Data: []byte("fake audio bytes"), MIMEType: "audio/mp3"})
	if err != nil {
		t.Fatalf("Segment through SDK: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("Segment returned zero segments")
	}
	if !strings.Contains(gotPath, "gemini-3.8-flash") {
		t.Errorf("request path = %q, want the configured model id", gotPath)
	}
	if !strings.Contains(gotPath, "test-project") {
		t.Errorf("request path = %q, want the configured project", gotPath)
	}
	var payload map[string]any
	if err := json.Unmarshal(gotBody, &payload); err != nil {
		t.Fatalf("decode SDK request body: %v\n%s", err, gotBody)
	}
	contents, _ := payload["contents"].([]any)
	if len(contents) != 1 {
		t.Fatalf("SDK contents = %#v, want one turn", payload["contents"])
	}
	turn, _ := contents[0].(map[string]any)
	parts, _ := turn["parts"].([]any)
	if len(parts) != 2 {
		t.Fatalf("SDK parts = %#v, want inline data and prompt", turn["parts"])
	}
	textPart, _ := parts[1].(map[string]any)
	if text, _ := textPart["text"].(string); text != segmentPrompt {
		t.Errorf("SDK prompt text mismatch\n got: %q", text)
	}
	genCfg, _ := payload["generationConfig"].(map[string]any)
	if mime, _ := genCfg["responseMimeType"].(string); mime != "application/json" {
		t.Errorf("SDK responseMimeType = %q, want application/json", mime)
	}
	if _, ok := genCfg["responseSchema"]; !ok {
		t.Errorf("SDK request misses responseSchema: %s", gotBody)
	}

	charges := ledger.Charges()
	if len(charges) != 1 {
		t.Fatalf("charges = %d entries, want 1", len(charges))
	}
	if charges[0].PromptTokens != 41 || charges[0].CandidateTokens != 17 {
		t.Errorf("tokens = %d/%d, want 41/17 from UsageMetadata", charges[0].PromptTokens, charges[0].CandidateTokens)
	}
}

func testSDKClient(t *testing.T) *genai.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"candidates":[]}`))
	}))
	t.Cleanup(srv.Close)
	client, err := genai.NewClient(context.Background(), &genai.ClientConfig{
		Backend:    genai.BackendVertexAI,
		Project:    "test-project",
		Location:   "us-central1",
		HTTPClient: srv.Client(),
		HTTPOptions: genai.HTTPOptions{
			BaseURL: srv.URL,
		},
	})
	if err != nil {
		t.Fatalf("test SDK client: %v", err)
	}
	return client
}

func newSegmenterForTest(cfg *config.Config, rec ChargeRecorder, card cost.RateCard, gen contentGenerator) (Segmenter, error) {
	if cfg == nil {
		return nil, errors.New("gemini segmenter needs a config")
	}
	if rec == nil {
		return nil, errors.New("gemini segmenter needs a charge recorder")
	}
	if gen == nil {
		return nil, errors.New("gemini segmenter needs a generateContent client")
	}
	return &vertexSegmenter{cfg: cfg, rec: rec, card: card, gen: gen}, nil
}
