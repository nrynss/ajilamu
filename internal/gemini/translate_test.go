package gemini

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"google.golang.org/genai"

	"github.com/nrynss/ajilamu/internal/config"
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

	ledger := cost.NewLedger()
	gen := &fakeGenerator{text: "ചലനം", promptTokens: 23, candTokens: 5}
	tr, err := newTranslatorForTest(testConfig(), ledger, cost.DefaultRateCard(), gen)
	if err != nil {
		t.Fatalf("newTranslatorForTest: %v", err)
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

			if gen.gotModel != "gemini-3.8-flash" {
				t.Errorf("model = %q, want gemini-3.8-flash", gen.gotModel)
			}
			if len(gen.gotContents) != 1 || len(gen.gotContents[0].Parts) != 1 {
				t.Fatalf("contents shape = %#v, want one content with one text part", gen.gotContents)
			}
			if gen.gotContents[0].Role != string(genai.RoleUser) && gen.gotContents[0].Role != "user" {
				t.Errorf("role = %q, want user", gen.gotContents[0].Role)
			}

			gotPrompt := gen.gotContents[0].Parts[0].Text
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
			if gen.gotConfig == nil || gen.gotConfig.Temperature == nil || *gen.gotConfig.Temperature != 0.3 {
				t.Errorf("temperature = %v, want 0.3", gen.gotConfig)
			}
			if gen.gotConfig.ResponseMIMEType != "" {
				t.Errorf("ResponseMIMEType = %q, want it omitted", gen.gotConfig.ResponseMIMEType)
			}
			if gen.gotConfig.ResponseSchema != nil {
				t.Errorf("ResponseSchema is set, want it omitted")
			}
		})
	}
}

func TestTranslateChargeItemization(t *testing.T) {
	var prompts []string
	gen := &fakeGenerator{
		text:         "ഗതിനിയമം",
		promptTokens: 29,
		candTokens:   11,
	}
	ledger := cost.NewLedger()
	card := cost.DefaultRateCard()
	tr, err := newTranslatorForTest(testConfig(), ledger, card, gen)
	if err != nil {
		t.Fatalf("newTranslatorForTest: %v", err)
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
		prompts = append(prompts, gen.gotContents[0].Parts[0].Text)
	}

	charges := ledger.Charges()
	if len(charges) != len(reqs) {
		t.Fatalf("charges = %d entries, want %d", len(charges), len(reqs))
	}
	for i, req := range reqs {
		want := cost.Charge{
			Kind:               cost.ChargeTranslate,
			TakeID:             req.SegmentID,
			PromptTokens:       29,
			CandidateTokens:    11,
			PromptUnitPrice:    card.TranslatePerPromptToken,
			CandidateUnitPrice: card.TranslatePerCandidateToken,
		}
		if charges[i] != want {
			t.Errorf("charge %d = %#v, want %#v", i, charges[i], want)
		}
		wantTotal := cost.Price(29)*card.TranslatePerPromptToken + cost.Price(11)*card.TranslatePerCandidateToken
		if charges[i].Total() != wantTotal {
			t.Errorf("charge %d total = %v, want %v", i, charges[i].Total(), wantTotal)
		}
		if charges[i].Units == len(prompts[i]) {
			t.Errorf("charge %d billed on prompt character length %d, want UsageMetadata tokens", i, charges[i].Units)
		}
	}
}

func TestTranslateTrimsReply(t *testing.T) {
	gen := &fakeGenerator{text: "\n\t  ഒരു വസ്തു ചലനാവസ്ഥയിൽ തുടരുന്നു  \n", promptTokens: 12, candTokens: 8}
	ledger := cost.NewLedger()
	tr, err := newTranslatorForTest(testConfig(), ledger, cost.DefaultRateCard(), gen)
	if err != nil {
		t.Fatalf("newTranslatorForTest: %v", err)
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
		name string
		gen  *fakeGenerator
	}{
		{"generate error", &fakeGenerator{err: errors.New("upstream exploded")}},
		{"zero candidates", &fakeGenerator{candidates: []*genai.Candidate{}}},
		{"empty text", &fakeGenerator{text: "   "}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ledger := cost.NewLedger()
			tr, err := newTranslatorForTest(testConfig(), ledger, cost.DefaultRateCard(), tc.gen)
			if err != nil {
				t.Fatalf("newTranslatorForTest: %v", err)
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
	cfg := testConfig()
	var rec ChargeRecorder = cost.NewLedger()
	client := testSDKClient(t)

	if _, err := NewTranslator(nil, rec, cost.DefaultRateCard(), client); err == nil {
		t.Error("nil config accepted, want error")
	}
	if _, err := NewTranslator(cfg, nil, cost.DefaultRateCard(), client); err == nil {
		t.Error("nil charge recorder accepted, want error")
	}
	if _, err := NewTranslator(cfg, rec, cost.DefaultRateCard(), client); err != nil {
		t.Errorf("valid arguments rejected: %v", err)
	}
}

func newTranslatorForTest(cfg *config.Config, rec ChargeRecorder, card cost.RateCard, gen contentGenerator) (Translator, error) {
	if cfg == nil {
		return nil, errors.New("gemini translator needs a config")
	}
	if rec == nil {
		return nil, errors.New("gemini translator needs a charge recorder")
	}
	if gen == nil {
		return nil, errors.New("gemini translator needs a generateContent client")
	}
	return &vertexTranslator{cfg: cfg, rec: rec, card: card, gen: gen}, nil
}
