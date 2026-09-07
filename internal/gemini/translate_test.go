package gemini

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"

	"github.com/nrynss/ajilamu/internal/cost"
)

// goldenText is the English source line of fixture segment 8.
const goldenText = "Also, an object in motion tends to stay in motion unless acted on by an outside force."

// wantTranslatePrompt builds the byte exact prompt the client must send.
// It pins the Python proven body including the leading and trailing newline.
func wantTranslatePrompt(text, emotion, constraint string) string {
	return "\nTranslate this English dialogue line into natural spoken Malayalam script (മലയാളം):\n" +
		"Original English: \"" + text + "\"\n" +
		"Speaker emotion: " + emotion + "\n" +
		"Constraint: " + constraint + "\n\n" +
		"Respond with strictly the translated Malayalam text. No markdown, no quotes, no explanation.\n"
}

func TestTranslatePromptPerMode(t *testing.T) {
	cases := []struct {
		mode           TranslateMode
		wantConstraint string
	}{
		{ModeNormal, "The translated line will be spoken in Malayalam in a video slot that lasts approximately 7.1 seconds (7110 ms). Keep the translation natural, spoken, and fit the rhythm."},
		{ModeShorter, "This translation MUST be extremely concise and fast to speak. The previous attempt was too long for the 7110ms slot. Use minimal syllables."},
		{ModeFuller, "This translation MUST fill its video slot naturally. The previous attempt left the 7.1 seconds (7110 ms) slot too empty. Use complete natural phrasing that fills it, no meaningless padding."},
	}

	var gotAuth string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		gotBody = body
		w.Write(envelopeJSON(t, "ചലനം"))
	}))
	defer srv.Close()

	ledger := cost.NewLedger()
	tr, err := NewTranslator(testConfig(srv.URL), oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "token-123"}), ledger, nil, cost.DefaultRateCard())
	if err != nil {
		t.Fatalf("NewTranslator: %v", err)
	}

	req := TranslateRequest{
		SegmentID:  8,
		Text:       goldenText,
		TargetSlot: 7110 * time.Millisecond,
		Emotion:    "Explanatory",
	}
	for _, tc := range cases {
		t.Run(tc.mode.String(), func(t *testing.T) {
			req.Mode = tc.mode
			if _, err := tr.Translate(context.Background(), req); err != nil {
				t.Fatalf("Translate: %v", err)
			}

			if gotAuth != "Bearer token-123" {
				t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer token-123")
			}

			var payload generateRequest
			if err := json.Unmarshal(gotBody, &payload); err != nil {
				t.Fatalf("decode request payload: %v", err)
			}
			if len(payload.Contents) != 1 || len(payload.Contents[0].Parts) != 1 {
				t.Fatalf("payload shape = %#v, want one content with one text part", payload)
			}
			if payload.Contents[0].Role != "user" {
				t.Errorf("role = %q, want user", payload.Contents[0].Role)
			}

			gotPrompt := payload.Contents[0].Parts[0].Text
			want := wantTranslatePrompt(goldenText, "Explanatory", tc.wantConstraint)
			if gotPrompt != want {
				t.Errorf("prompt mismatch\n got: %q\nwant: %q", gotPrompt, want)
			}
			if !strings.Contains(gotPrompt, tc.wantConstraint) {
				t.Errorf("prompt misses the %s constraint sentence: %q", tc.mode, gotPrompt)
			}
			if !strings.Contains(gotPrompt, "Speaker emotion: Explanatory") {
				t.Errorf("prompt misses the emotion line: %q", gotPrompt)
			}
			if payload.GenerationConfig.Temperature != 0.3 {
				t.Errorf("temperature = %v, want 0.3", payload.GenerationConfig.Temperature)
			}
			if strings.Contains(string(gotBody), "responseMimeType") {
				t.Errorf("payload carries responseMimeType, want it omitted: %s", gotBody)
			}
		})
	}
}

func TestTranslateChargeItemization(t *testing.T) {
	var prompts []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
			return
		}
		var payload generateRequest
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Errorf("decode request payload: %v", err)
			return
		}
		prompts = append(prompts, payload.Contents[0].Parts[0].Text)
		w.Write(envelopeJSON(t, "ഗതിനിയമം"))
	}))
	defer srv.Close()

	ledger := cost.NewLedger()
	card := cost.DefaultRateCard()
	tr, err := NewTranslator(testConfig(srv.URL), oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "t"}), ledger, nil, card)
	if err != nil {
		t.Fatalf("NewTranslator: %v", err)
	}

	reqs := []TranslateRequest{
		{SegmentID: 8, Text: goldenText, TargetSlot: 7110 * time.Millisecond, Emotion: "Explanatory", Mode: ModeNormal},
		{SegmentID: 3, Text: "Hi, I'm Suni Williams", TargetSlot: 1800 * time.Millisecond, Emotion: "Warm", Mode: ModeShorter},
		{SegmentID: 5, Text: "That is our six months in space", TargetSlot: 3000 * time.Millisecond, Emotion: "Serious", Mode: ModeFuller},
	}
	for _, req := range reqs {
		if _, err := tr.Translate(context.Background(), req); err != nil {
			t.Fatalf("Translate(%s): %v", req.Mode, err)
		}
	}

	charges := ledger.Charges()
	if len(charges) != len(reqs) {
		t.Fatalf("charges = %d entries, want %d", len(charges), len(reqs))
	}
	for i, req := range reqs {
		want := cost.Charge{
			Kind:      cost.ChargeTranslate,
			TakeID:    req.SegmentID,
			Units:     len(translatePrompt(req)),
			UnitPrice: card.TranslatePerInputChar,
		}
		if charges[i] != want {
			t.Errorf("charge %d = %#v, want %#v", i, charges[i], want)
		}
		if charges[i].Total() != want.Total() {
			t.Errorf("charge %d total = %v, want %v", i, charges[i].Total(), want.Total())
		}
		if charges[i].Units != len(prompts[i]) {
			t.Errorf("charge %d units = %d, want the %d bytes actually sent", i, charges[i].Units, len(prompts[i]))
		}
	}
}

func TestTranslateTrimsReply(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(envelopeJSON(t, "\n\t  ഒരു വസ്തു ചലനാവസ്ഥയിൽ തുടരുന്നു  \n"))
	}))
	defer srv.Close()

	ledger := cost.NewLedger()
	tr, err := NewTranslator(testConfig(srv.URL), oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "t"}), ledger, nil, cost.DefaultRateCard())
	if err != nil {
		t.Fatalf("NewTranslator: %v", err)
	}

	req := TranslateRequest{SegmentID: 8, Text: goldenText, TargetSlot: 7110 * time.Millisecond, Emotion: "Explanatory"}
	got, err := tr.Translate(context.Background(), req)
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}
	want := "ഒരു വസ്തു ചലനാവസ്ഥയിൽ തുടരുന്നു"
	if got != want {
		t.Errorf("Translate = %q, want %q", got, want)
	}
}

func TestTranslateErrorsRecordNoCharges(t *testing.T) {
	cases := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{"non-200", func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "upstream exploded", http.StatusInternalServerError)
		}},
		{"zero candidates", func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"candidates": []}`))
		}},
		{"empty text", func(w http.ResponseWriter, r *http.Request) {
			w.Write(envelopeJSON(t, "   "))
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handler)
			defer srv.Close()

			ledger := cost.NewLedger()
			tr, err := NewTranslator(testConfig(srv.URL), oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "t"}), ledger, nil, cost.DefaultRateCard())
			if err != nil {
				t.Fatalf("NewTranslator: %v", err)
			}

			req := TranslateRequest{SegmentID: 8, Text: goldenText, TargetSlot: 7110 * time.Millisecond, Emotion: "Explanatory"}
			if _, err := tr.Translate(context.Background(), req); err == nil {
				t.Fatal("Translate succeeded, want an error")
			}
			if charges := ledger.Charges(); len(charges) != 0 {
				t.Errorf("failed pass recorded %d charges, want zero", len(charges))
			}
		})
	}
}

func TestNewTranslatorNilArguments(t *testing.T) {
	cfg := testConfig("")
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "t"})
	var rec ChargeRecorder = cost.NewLedger()

	if _, err := NewTranslator(nil, ts, rec, nil, cost.DefaultRateCard()); err == nil {
		t.Error("nil config accepted, want error")
	}
	if _, err := NewTranslator(cfg, nil, rec, nil, cost.DefaultRateCard()); err == nil {
		t.Error("nil token source accepted, want error")
	}
	if _, err := NewTranslator(cfg, ts, nil, nil, cost.DefaultRateCard()); err == nil {
		t.Error("nil charge recorder accepted, want error")
	}
	built, err := NewTranslator(cfg, ts, rec, nil, cost.DefaultRateCard())
	if err != nil {
		t.Fatalf("valid arguments rejected: %v", err)
	}
	vt, ok := built.(*vertexTranslator)
	if !ok {
		t.Fatalf("NewTranslator returned %T, want *vertexTranslator", built)
	}
	if vt.hc.Timeout != 60*time.Second {
		t.Errorf("default client timeout = %v, want 60s", vt.hc.Timeout)
	}
}
