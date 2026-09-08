package gemini

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"google.golang.org/genai"

	"github.com/nrynss/ajilamu/internal/config"
	"github.com/nrynss/ajilamu/internal/cost"
)

// TranslateMode selects the length constraint one translation prompt enforces.
type TranslateMode int

const (
	// ModeNormal asks for a natural spoken line inside the slot.
	ModeNormal TranslateMode = iota
	// ModeShorter asks for an extremely concise line with minimal syllables.
	ModeShorter
	// ModeFuller asks for complete phrasing that fills a slot left too empty.
	ModeFuller
)

// String returns the pipeline name of the mode. Unknown values report normal.
func (m TranslateMode) String() string {
	switch m {
	case ModeShorter:
		return "shorter"
	case ModeFuller:
		return "fuller"
	default:
		return "normal"
	}
}

// TranslateRequest carries one dialogue line into the translation client.
type TranslateRequest struct {
	// SegmentID identifies the take that owns this line. It lands on the charge.
	SegmentID int
	// Text holds the source line in the source language.
	Text string
	// TargetSlot is the video duration budget the target line must fit.
	TargetSlot time.Duration
	// Emotion names the emotional register the translation should preserve.
	Emotion string
	// Mode selects the length constraint inside the prompt.
	Mode TranslateMode
	// TargetLanguageName is the target language in words, such as Spanish.
	// It must be set, because the prompt names the target language.
	TargetLanguageName string
	// SourceLanguageName is the source language in words, such as English.
	// Empty means the source language is unknown, so the prompt names none.
	SourceLanguageName string
}

// Translator turns one source line into spoken text in the target language.
type Translator interface {
	// Translate sends the line with its slot constraint and returns the target text.
	Translate(ctx context.Context, req TranslateRequest) (string, error)
}

// translatePromptBody composes the prompt for a known source language. The
// wording follows the proven body in tools/validate_pipeline.py. The source
// name, the opening target slot, the closing target slot and the line body
// are slots. The template carries the Python leading and trailing newline.
const translatePromptBody = `
Translate this %s dialogue line into natural spoken %s:
Original %s: "%s"
Speaker emotion: %s
Constraint: %s

Respond with strictly the translated %s text. No markdown, no quotes, no explanation.
`

// translatePromptBodyUnknownSource composes the prompt when the source
// language is unknown. It never claims English.
const translatePromptBodyUnknownSource = `
Translate this dialogue line into natural spoken %s:
Original text: "%s"
Speaker emotion: %s
Constraint: %s

Respond with strictly the translated %s text. No markdown, no quotes, no explanation.
`

// targetOpeningSlot names the target language in the opening slot of the
// prompt. tools/validate_pipeline.py wrote the Malayalam script name there, so
// Malayalam keeps its proven suffix. Every other language names its plain
// name, which is the generic case.
func targetOpeningSlot(target string) string {
	if target == "Malayalam" {
		return "Malayalam script (മലയാളം)"
	}
	return target
}

// normalConstraint ports the proven normal slot sentence from
// tools/validate_pipeline.py. The seconds value carries one decimal place.
func normalConstraint(slot time.Duration, target string) string {
	ms := slot.Milliseconds()
	return fmt.Sprintf("The translated line will be spoken in %s in a video slot that lasts approximately %.1f seconds (%d ms). Keep the translation natural, spoken, and fit the rhythm.", target, float64(ms)/1000, ms)
}

// shorterConstraint ports the proven shorter slot sentence from
// tools/validate_pipeline.py.
func shorterConstraint(slot time.Duration) string {
	return fmt.Sprintf("This translation MUST be extremely concise and fast to speak. The previous attempt was too long for the %dms slot. Use minimal syllables.", slot.Milliseconds())
}

// fullerConstraint is the fuller analog of the shorter sentence. It reports the
// slot with the normal format and demands complete phrasing without padding.
func fullerConstraint(slot time.Duration) string {
	return fmt.Sprintf("This translation MUST fill its video slot naturally. The previous attempt left the %.1f seconds (%d ms) slot too empty. Use complete natural phrasing that fills it, no meaningless padding.", float64(slot.Milliseconds())/1000, slot.Milliseconds())
}

// translatePrompt composes the prompt for one request. It names the target
// language, and the source language when the request carries one. An empty
// target name fails, so the prompt never names an unnamed language.
func translatePrompt(req TranslateRequest) (string, error) {
	target := strings.TrimSpace(req.TargetLanguageName)
	if target == "" {
		return "", errors.New("gemini translate needs a target language name")
	}
	constraint := normalConstraint(req.TargetSlot, target)
	switch req.Mode {
	case ModeShorter:
		constraint = shorterConstraint(req.TargetSlot)
	case ModeFuller:
		constraint = fullerConstraint(req.TargetSlot)
	}
	source := strings.TrimSpace(req.SourceLanguageName)
	opening := targetOpeningSlot(target)
	if source == "" {
		return fmt.Sprintf(translatePromptBodyUnknownSource, opening, req.Text, req.Emotion, constraint, target), nil
	}
	return fmt.Sprintf(translatePromptBody, source, opening, source, req.Text, req.Emotion, constraint, target), nil
}

// vertexTranslator is the Vertex AI generateContent implementation of Translator.
type vertexTranslator struct {
	cfg  *config.Config
	rec  ChargeRecorder
	card cost.RateCard
	gen  contentGenerator
}

// NewTranslator builds the Vertex AI translation client.
// A nil client constructs the production ADC client.
func NewTranslator(cfg *config.Config, rec ChargeRecorder, card cost.RateCard, client *genai.Client) (Translator, error) {
	if cfg == nil {
		return nil, errors.New("gemini translator needs a config")
	}
	if rec == nil {
		return nil, errors.New("gemini translator needs a charge recorder")
	}
	if client == nil {
		built, err := newVertexClient(cfg)
		if err != nil {
			return nil, err
		}
		client = built
	}
	if client.Models == nil {
		return nil, errors.New("gemini translator needs a models service")
	}
	return &vertexTranslator{cfg: cfg, rec: rec, card: card, gen: client.Models}, nil
}

// Translate sends one line with its slot constraint and returns the target
// text. It builds the prompt first, so a missing target language fails before
// the call. The charge lands only after a successful round trip and a
// non-empty reply. A recorded charge therefore implies a completed translation.
func (v *vertexTranslator) Translate(ctx context.Context, req TranslateRequest) (string, error) {
	prompt, err := translatePrompt(req)
	if err != nil {
		return "", err
	}
	contents := []*genai.Content{
		genai.NewContentFromParts([]*genai.Part{
			genai.NewPartFromText(prompt),
		}, genai.RoleUser),
	}
	cfg := &genai.GenerateContentConfig{
		Temperature: genai.Ptr(float32(0.3)),
	}
	resp, err := v.gen.GenerateContent(ctx, v.cfg.GeminiModel, contents, cfg)
	if err != nil {
		return "", err
	}
	out, err := replyText(resp)
	if err != nil {
		return "", err
	}
	v.rec.Add(geminiCharge(cost.ChargeTranslate, req.SegmentID, resp.UsageMetadata, v.card))
	return out, nil
}
