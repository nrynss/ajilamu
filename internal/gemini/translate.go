package gemini

import (
	"context"
	"errors"
	"fmt"
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
	// Text holds the English source line.
	Text string
	// TargetSlot is the video duration budget the Malayalam line must fit.
	TargetSlot time.Duration
	// Emotion names the emotional register the translation should preserve.
	Emotion string
	// Mode selects the length constraint inside the prompt.
	Mode TranslateMode
}

// Translator turns one English line into spoken Malayalam text.
type Translator interface {
	// Translate sends the line with its slot constraint and returns the Malayalam text.
	Translate(ctx context.Context, req TranslateRequest) (string, error)
}

// translatePromptBody is the proven translation prompt body from
// tools/validate_pipeline.py. It is byte verbatim and carries the Python
// leading and trailing newline. The %s slots replace the Python f-string fields.
const translatePromptBody = `
Translate this English dialogue line into natural spoken Malayalam script (മലയാളം):
Original English: "%s"
Speaker emotion: %s
Constraint: %s

Respond with strictly the translated Malayalam text. No markdown, no quotes, no explanation.
`

// normalConstraint ports the proven normal slot sentence from
// tools/validate_pipeline.py. The seconds value carries one decimal place.
func normalConstraint(slot time.Duration) string {
	ms := slot.Milliseconds()
	return fmt.Sprintf("The translated line will be spoken in Malayalam in a video slot that lasts approximately %.1f seconds (%d ms). Keep the translation natural, spoken, and fit the rhythm.", float64(ms)/1000, ms)
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

// translatePrompt composes the prompt for one request from the proven body and
// the constraint sentence of the requested mode.
func translatePrompt(req TranslateRequest) string {
	constraint := normalConstraint(req.TargetSlot)
	switch req.Mode {
	case ModeShorter:
		constraint = shorterConstraint(req.TargetSlot)
	case ModeFuller:
		constraint = fullerConstraint(req.TargetSlot)
	}
	return fmt.Sprintf(translatePromptBody, req.Text, req.Emotion, constraint)
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

// Translate sends one line with its slot constraint and returns the Malayalam
// text. The charge lands only after a successful round trip and a non-empty
// reply. A recorded charge therefore always implies a completed translation.
func (v *vertexTranslator) Translate(ctx context.Context, req TranslateRequest) (string, error) {
	prompt := translatePrompt(req)
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
