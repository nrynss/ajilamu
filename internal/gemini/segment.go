// Package gemini implements Vertex AI Gemini clients for segmentation and
// Malayalam translation.
package gemini

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"google.golang.org/genai"

	"github.com/nrynss/ajilamu/internal/config"
	"github.com/nrynss/ajilamu/internal/cost"
	"github.com/nrynss/ajilamu/internal/types"
)

// segmentPrompt is the proven segmentation prompt from tools/validate_pipeline.py.
// It is byte verbatim and carries the Python leading and trailing newline.
const segmentPrompt = `
Analyze this speech audio clip carefully.
Identify all spoken sentences or distinct dialogue phrases.
For each segment, detect:
1. start_ms: Start time in milliseconds from beginning of clip
2. end_ms: End time in milliseconds
3. duration_ms: end_ms - start_ms
4. text: The exact English words spoken
5. speaker: Identified speaker name or role
6. emotion: Emotional register (e.g. Enthusiastic, Explanatory, Serious, Warm)

Return strictly valid JSON matching this schema:
[
  {
    "id": 1,
    "start_ms": 6000,
    "end_ms": 7800,
    "duration_ms": 1800,
    "text": "Hi, I'm Suni Williams",
    "speaker": "Suni Williams",
    "emotion": "Warm"
  }
]
`

// ChargeRecorder receives the itemized billing trail of API invocations.
type ChargeRecorder interface {
	// Add records one charge.
	Add(cost.Charge)
}

// Input carries one media buffer for a Gemini request.
// It holds bytes rather than a path so video frames can pass later.
type Input struct {
	// Data holds the raw media bytes.
	Data []byte
	// MIMEType names the media format such as audio/mp3.
	MIMEType string
}

// Segmenter turns one media buffer into timed dialogue segments.
type Segmenter interface {
	// Segment sends the media to Gemini and returns the parsed segments.
	Segment(ctx context.Context, in Input) ([]types.Segment, error)
}

// contentGenerator is the Models.GenerateContent surface the clients call.
type contentGenerator interface {
	GenerateContent(ctx context.Context, model string, contents []*genai.Content, config *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error)
}

// wireSegment mirrors one snake_case segment inside the model JSON reply.
type wireSegment struct {
	ID         int    `json:"id"`
	StartMs    int64  `json:"start_ms"`
	EndMs      int64  `json:"end_ms"`
	DurationMs int64  `json:"duration_ms"`
	Text       string `json:"text"`
	Speaker    string `json:"speaker"`
	Emotion    string `json:"emotion"`
}

// vertexSegmenter is the Vertex AI generateContent implementation of Segmenter.
type vertexSegmenter struct {
	cfg  *config.Config
	rec  ChargeRecorder
	card cost.RateCard
	gen  contentGenerator
}

// NewSegmenter builds the Vertex AI segmentation client.
// A nil client constructs the production ADC client.
func NewSegmenter(cfg *config.Config, rec ChargeRecorder, card cost.RateCard, client *genai.Client) (Segmenter, error) {
	if cfg == nil {
		return nil, errors.New("gemini segmenter needs a config")
	}
	if rec == nil {
		return nil, errors.New("gemini segmenter needs a charge recorder")
	}
	if client == nil {
		built, err := newVertexClient(cfg)
		if err != nil {
			return nil, err
		}
		client = built
	}
	if client.Models == nil {
		return nil, errors.New("gemini segmenter needs a models service")
	}
	return &vertexSegmenter{cfg: cfg, rec: rec, card: card, gen: client.Models}, nil
}

// Segment sends the media buffer with the proven prompt and parses the reply.
// The charge lands only after a successful round trip and a full parse.
// A recorded charge therefore always implies a completed segmentation pass.
func (v *vertexSegmenter) Segment(ctx context.Context, in Input) ([]types.Segment, error) {
	contents := []*genai.Content{
		genai.NewContentFromParts([]*genai.Part{
			genai.NewPartFromBytes(in.Data, in.MIMEType),
			genai.NewPartFromText(segmentPrompt),
		}, genai.RoleUser),
	}
	cfg := &genai.GenerateContentConfig{
		ResponseMIMEType: "application/json",
		ResponseSchema:   segmentResponseSchema(),
	}
	resp, err := v.gen.GenerateContent(ctx, v.cfg.GeminiModel, contents, cfg)
	if err != nil {
		return nil, err
	}
	text, err := replyText(resp)
	if err != nil {
		return nil, err
	}
	segments, err := decodeSegments(text)
	if err != nil {
		return nil, err
	}
	v.rec.Add(geminiCharge(cost.ChargeSegment, 0, resp.UsageMetadata, v.card))
	return segments, nil
}

// newVertexClient constructs the production ADC client.
// It passes BackendVertexAI, Project, and Location. It passes no Credentials,
// no APIKey, and no HTTPClient, so the SDK calls credentials.DetectDefault.
func newVertexClient(cfg *config.Config) (*genai.Client, error) {
	if cfg.GoogleCloudProject == "" {
		return nil, errors.New("config misses GOOGLE_CLOUD_PROJECT")
	}
	if cfg.GoogleCloudLocation == "" {
		return nil, errors.New("config misses GOOGLE_CLOUD_LOCATION")
	}
	if cfg.GeminiModel == "" {
		return nil, errors.New("config misses GEMINI_MODEL")
	}
	return genai.NewClient(context.Background(), &genai.ClientConfig{
		Backend:  genai.BackendVertexAI,
		Project:  cfg.GoogleCloudProject,
		Location: cfg.GoogleCloudLocation,
	})
}

// segmentResponseSchema is the strict JSON array schema for segmentation.
func segmentResponseSchema() *genai.Schema {
	return &genai.Schema{
		Type: genai.TypeArray,
		Items: &genai.Schema{
			Type:     genai.TypeObject,
			Required: []string{"id", "start_ms", "end_ms", "duration_ms", "text", "speaker", "emotion"},
			Properties: map[string]*genai.Schema{
				"id":          {Type: genai.TypeInteger},
				"start_ms":    {Type: genai.TypeInteger},
				"end_ms":      {Type: genai.TypeInteger},
				"duration_ms": {Type: genai.TypeInteger},
				"text":        {Type: genai.TypeString},
				"speaker":     {Type: genai.TypeString},
				"emotion":     {Type: genai.TypeString},
			},
		},
	}
}

// replyText extracts the first candidate text from a generateContent reply.
// It rejects replies with zero candidates and replies without text.
func replyText(resp *genai.GenerateContentResponse) (string, error) {
	if resp == nil {
		return "", errors.New("generateContent reply is missing")
	}
	if len(resp.Candidates) == 0 {
		return "", errors.New("generateContent reply carries no candidates")
	}
	content := resp.Candidates[0].Content
	if content == nil || len(content.Parts) == 0 {
		return "", errors.New("generateContent reply carries no parts")
	}
	text := strings.TrimSpace(resp.Text())
	if text == "" {
		return "", errors.New("generateContent reply carries an empty parts text")
	}
	return text, nil
}

// geminiCharge maps UsageMetadata token counts onto one Charge.
// A nil usage records zero tokens. Unit prices still come from the rate card.
func geminiCharge(kind cost.ChargeKind, takeID int, usage *genai.GenerateContentResponseUsageMetadata, card cost.RateCard) cost.Charge {
	c := cost.Charge{Kind: kind, TakeID: takeID}
	switch kind {
	case cost.ChargeSegment:
		c.PromptUnitPrice = card.SegmentPerPromptToken
		c.CandidateUnitPrice = card.SegmentPerCandidateToken
	case cost.ChargeTranslate:
		c.PromptUnitPrice = card.TranslatePerPromptToken
		c.CandidateUnitPrice = card.TranslatePerCandidateToken
	}
	if usage != nil {
		c.PromptTokens = int(usage.PromptTokenCount)
		c.CandidateTokens = int(usage.CandidatesTokenCount)
	}
	return c
}

// decodeSegments parses the model JSON and maps it onto typed segments.
// It rejects zero segments and any segment violating the pipeline invariants.
func decodeSegments(text string) ([]types.Segment, error) {
	var wire []wireSegment
	if err := json.Unmarshal([]byte(strings.TrimSpace(text)), &wire); err != nil {
		return nil, fmt.Errorf("parse model segment JSON: %w", err)
	}
	if len(wire) == 0 {
		return nil, errors.New("model returned zero segments")
	}
	out := make([]types.Segment, 0, len(wire))
	for i, ws := range wire {
		if err := ws.validate(); err != nil {
			return nil, fmt.Errorf("segment %d: %w", i+1, err)
		}
		if i > 0 && ws.ID <= wire[i-1].ID {
			return nil, fmt.Errorf("segment %d: id %d does not increase past %d", i+1, ws.ID, wire[i-1].ID)
		}
		out = append(out, types.Segment{
			ID:      ws.ID,
			StartMs: ws.StartMs,
			EndMs:   ws.EndMs,
			Text:    ws.Text,
			Speaker: types.Speaker{Name: ws.Speaker},
			Emotion: ws.Emotion,
		})
	}
	return out, nil
}

// validate enforces the segment invariants the downstream pipeline relies on.
func (w wireSegment) validate() error {
	if w.StartMs >= w.EndMs {
		return fmt.Errorf("start_ms %d does not precede end_ms %d", w.StartMs, w.EndMs)
	}
	if w.DurationMs != w.EndMs-w.StartMs {
		return fmt.Errorf("duration_ms %d does not equal end_ms minus start_ms %d", w.DurationMs, w.EndMs-w.StartMs)
	}
	if strings.TrimSpace(w.Text) == "" {
		return errors.New("text is empty")
	}
	if strings.TrimSpace(w.Speaker) == "" {
		return errors.New("speaker is empty")
	}
	if strings.TrimSpace(w.Emotion) == "" {
		return errors.New("emotion is empty")
	}
	return nil
}
