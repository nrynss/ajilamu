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

// provenNormalConstraint is the proven normal slot sentence for fixture
// segment 8, from tools/validate_pipeline.py line 189.
const provenNormalConstraint = "The translated line will be spoken in Malayalam in a video slot that lasts approximately 7.1 seconds (7110 ms). Keep the translation natural, spoken, and fit the rhythm."

// provenMalayalamPrompt is the byte exact prompt the proven body in
// tools/validate_pipeline.py builds for fixture segment 8 in normal mode. The
// opening slot names the Malayalam script and the closing slot the plain name.
// Transcribed from translate_to_malayalam, lines 193 to 200.
const provenMalayalamPrompt = "\n" +
	"Translate this English dialogue line into natural spoken Malayalam script (മലയാളം):\n" +
	"Original English: \"" + goldenText + "\"\n" +
	"Speaker emotion: Explanatory\n" +
	"Constraint: " + provenNormalConstraint + "\n" +
	"\n" +
	"Respond with strictly the translated Malayalam text. No markdown, no quotes, no explanation.\n"

// spanishConstraint is the normal slot sentence for a Spanish target.
const spanishConstraint = "The translated line will be spoken in Spanish in a video slot that lasts approximately 7.1 seconds (7110 ms). Keep the translation natural, spoken, and fit the rhythm."

// spanishTargetPrompt is the byte exact prompt for an English source and a
// Spanish target. Spanish has no proven script hint, so both target slots
// carry the plain name.
const spanishTargetPrompt = "\n" +
	"Translate this English dialogue line into natural spoken Spanish:\n" +
	"Original English: \"" + goldenText + "\"\n" +
	"Speaker emotion: Explanatory\n" +
	"Constraint: " + spanishConstraint + "\n" +
	"\n" +
	"Respond with strictly the translated Spanish text. No markdown, no quotes, no explanation.\n"

// spanishSourcePrompt is the byte exact prompt for a Spanish source and a
// Malayalam target. It carries the Spanish name in both known-source slots.
const spanishSourcePrompt = "\n" +
	"Translate this Spanish dialogue line into natural spoken Malayalam script (മലയാളം):\n" +
	"Original Spanish: \"" + goldenText + "\"\n" +
	"Speaker emotion: Explanatory\n" +
	"Constraint: " + provenNormalConstraint + "\n" +
	"\n" +
	"Respond with strictly the translated Malayalam text. No markdown, no quotes, no explanation.\n"

// unknownSourceSpanishPrompt is the byte exact prompt for a Spanish target
// with no source language. It names no source language at all.
const unknownSourceSpanishPrompt = "\n" +
	"Translate this dialogue line into natural spoken Spanish:\n" +
	"Original text: \"" + goldenText + "\"\n" +
	"Speaker emotion: Explanatory\n" +
	"Constraint: " + spanishConstraint + "\n" +
	"\n" +
	"Respond with strictly the translated Spanish text. No markdown, no quotes, no explanation.\n"

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
		SegmentID:          8,
		Text:               goldenText,
		TargetSlot:         7110 * time.Millisecond,
		Emotion:            "Explanatory",
		TargetLanguageName: "Malayalam",
		SourceLanguageName: "English",
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
			want := provenMalayalamPrompt
			if tc.mode != ModeNormal {
				want = strings.Replace(provenMalayalamPrompt, provenNormalConstraint, tc.wantConstraint, 1)
			}
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
		{SegmentID: 8, Text: goldenText, TargetSlot: 7110 * time.Millisecond, Emotion: "Explanatory", Mode: ModeNormal, TargetLanguageName: "Malayalam", SourceLanguageName: "English"},
		{SegmentID: 3, Text: "Hi, I'm Suni Williams", TargetSlot: 1800 * time.Millisecond, Emotion: "Warm", Mode: ModeShorter, TargetLanguageName: "Malayalam", SourceLanguageName: "English"},
		{SegmentID: 5, Text: "That is our six months in space", TargetSlot: 3000 * time.Millisecond, Emotion: "Serious", Mode: ModeFuller, TargetLanguageName: "Malayalam", SourceLanguageName: "English"},
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

	req := TranslateRequest{SegmentID: 8, Text: goldenText, TargetSlot: 7110 * time.Millisecond, Emotion: "Explanatory", TargetLanguageName: "Malayalam", SourceLanguageName: "English"}
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

			req := TranslateRequest{SegmentID: 8, Text: goldenText, TargetSlot: 7110 * time.Millisecond, Emotion: "Explanatory", TargetLanguageName: "Malayalam", SourceLanguageName: "English"}
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

// TestTranslatePromptNamesTargetLanguage pins the Spanish target prompt.
// The prompt names Spanish and never names Malayalam.
func TestTranslatePromptNamesTargetLanguage(t *testing.T) {
	gen := &fakeGenerator{text: "El movimiento", promptTokens: 20, candTokens: 6}
	ledger := cost.NewLedger()
	tr, err := newTranslatorForTest(testConfig(), ledger, cost.DefaultRateCard(), gen)
	if err != nil {
		t.Fatalf("newTranslatorForTest: %v", err)
	}

	req := TranslateRequest{
		SegmentID:          8,
		Text:               goldenText,
		TargetSlot:         7110 * time.Millisecond,
		Emotion:            "Explanatory",
		TargetLanguageName: "Spanish",
		SourceLanguageName: "English",
	}
	if _, err := tr.Translate(context.Background(), req); err != nil {
		t.Fatalf("Translate: %v", err)
	}

	got := gen.gotContents[0].Parts[0].Text
	want := spanishTargetPrompt
	if got != want {
		t.Errorf("prompt mismatch\n got: %q\nwant: %q", got, want)
	}
	if strings.Contains(got, "Malayalam") {
		t.Errorf("Spanish prompt names Malayalam: %q", got)
	}
	if !strings.Contains(got, "Spanish") {
		t.Errorf("prompt misses Spanish: %q", got)
	}
}

// TestTranslatePromptOmitsUnknownSourceLanguage proves the prompt never claims
// English when the request carries no source language.
func TestTranslatePromptOmitsUnknownSourceLanguage(t *testing.T) {
	gen := &fakeGenerator{text: "El movimiento", promptTokens: 20, candTokens: 6}
	ledger := cost.NewLedger()
	tr, err := newTranslatorForTest(testConfig(), ledger, cost.DefaultRateCard(), gen)
	if err != nil {
		t.Fatalf("newTranslatorForTest: %v", err)
	}

	req := TranslateRequest{
		SegmentID:          8,
		Text:               goldenText,
		TargetSlot:         7110 * time.Millisecond,
		Emotion:            "Explanatory",
		TargetLanguageName: "Spanish",
	}
	if _, err := tr.Translate(context.Background(), req); err != nil {
		t.Fatalf("Translate: %v", err)
	}

	got := gen.gotContents[0].Parts[0].Text
	want := unknownSourceSpanishPrompt
	if got != want {
		t.Errorf("prompt mismatch\n got: %q\nwant: %q", got, want)
	}
	if strings.Contains(got, "English") {
		t.Errorf("prompt claims English with no source language: %q", got)
	}
}

// TestTranslateRejectsEmptyTargetLanguage proves a missing target name fails
// before the generateContent call and records no charge.
func TestTranslateRejectsEmptyTargetLanguage(t *testing.T) {
	gen := &fakeGenerator{text: "hola", promptTokens: 20, candTokens: 6}
	ledger := cost.NewLedger()
	tr, err := newTranslatorForTest(testConfig(), ledger, cost.DefaultRateCard(), gen)
	if err != nil {
		t.Fatalf("newTranslatorForTest: %v", err)
	}

	req := TranslateRequest{SegmentID: 8, Text: goldenText, TargetSlot: 7110 * time.Millisecond, Emotion: "Explanatory"}
	if _, err := tr.Translate(context.Background(), req); err == nil {
		t.Fatal("Translate accepted an empty target language, want error")
	}
	if gen.gotContents != nil {
		t.Errorf("empty target language reached the generator: %#v", gen.gotContents)
	}
	if charges := ledger.Charges(); len(charges) != 0 {
		t.Errorf("empty target language recorded %d charges, want zero", len(charges))
	}
}

// TestTranslatePromptNamesNonEnglishSource proves the prompt carries a
// non-English source name in both known-source slots. Every other test in the
// package passes "English", the word the old hardcoded prompt carried.
func TestTranslatePromptNamesNonEnglishSource(t *testing.T) {
	gen := &fakeGenerator{text: "ചലനം", promptTokens: 20, candTokens: 6}
	ledger := cost.NewLedger()
	tr, err := newTranslatorForTest(testConfig(), ledger, cost.DefaultRateCard(), gen)
	if err != nil {
		t.Fatalf("newTranslatorForTest: %v", err)
	}

	req := TranslateRequest{
		SegmentID:          8,
		Text:               goldenText,
		TargetSlot:         7110 * time.Millisecond,
		Emotion:            "Explanatory",
		TargetLanguageName: "Malayalam",
		SourceLanguageName: "Spanish",
	}
	if _, err := tr.Translate(context.Background(), req); err != nil {
		t.Fatalf("Translate: %v", err)
	}

	got := gen.gotContents[0].Parts[0].Text
	if got != spanishSourcePrompt {
		t.Errorf("prompt mismatch\n got: %q\nwant: %q", got, spanishSourcePrompt)
	}
	if !strings.Contains(got, "Translate this Spanish dialogue line") {
		t.Errorf("prompt misses the Spanish opening slot: %q", got)
	}
	if !strings.Contains(got, "Original Spanish:") {
		t.Errorf("prompt misses the Spanish original slot: %q", got)
	}
}
