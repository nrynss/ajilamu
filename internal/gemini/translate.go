package gemini

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"golang.org/x/oauth2"

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
	ts   oauth2.TokenSource
	rec  ChargeRecorder
	hc   *http.Client
	card cost.RateCard
}

// NewTranslator builds the Vertex AI translation client.
// A nil hc selects a client with a 60 second timeout.
func NewTranslator(cfg *config.Config, ts oauth2.TokenSource, rec ChargeRecorder, hc *http.Client, card cost.RateCard) (Translator, error) {
	if cfg == nil {
		return nil, errors.New("gemini translator needs a config")
	}
	if ts == nil {
		return nil, errors.New("gemini translator needs a token source")
	}
	if rec == nil {
		return nil, errors.New("gemini translator needs a charge recorder")
	}
	if hc == nil {
		hc = &http.Client{Timeout: defaultHTTPTimeout}
	}
	return &vertexTranslator{cfg: cfg, ts: ts, rec: rec, hc: hc, card: card}, nil
}

// Translate sends one line with its slot constraint and returns the Malayalam
// text. The charge lands only after a successful round trip and a non-empty
// reply. A recorded charge therefore always implies a completed translation.
func (v *vertexTranslator) Translate(ctx context.Context, req TranslateRequest) (string, error) {
	tok, err := v.ts.Token()
	if err != nil {
		return "", fmt.Errorf("fetch access token: %w", err)
	}
	endpoint, err := endpointURL(v.cfg)
	if err != nil {
		return "", err
	}
	prompt := translatePrompt(req)
	payload := generateRequest{
		Contents: []generateContent{{
			Role:  "user",
			Parts: []generatePart{{Text: prompt}},
		}},
		GenerationConfig: generateGenerationConfig{Temperature: 0.3},
	}
	body, err := postGenerate(ctx, v.hc, tok.AccessToken, endpoint, payload)
	if err != nil {
		return "", err
	}
	text, err := envelopeText(body)
	if err != nil {
		return "", err
	}
	out := strings.TrimSpace(text)
	if out == "" {
		return "", errors.New("translation reply is empty after trimming")
	}
	v.rec.Add(cost.Charge{
		Kind:      cost.ChargeTranslate,
		TakeID:    req.SegmentID,
		Units:     len(prompt),
		UnitPrice: v.card.TranslatePerInputChar,
	})
	return out, nil
}
