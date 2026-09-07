// Package gemini implements Vertex AI Gemini clients for segmentation and
// Malayalam translation.
package gemini

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/nrynss/ajilamu/internal/config"
	"github.com/nrynss/ajilamu/internal/cost"
	"github.com/nrynss/ajilamu/internal/types"
	"golang.org/x/oauth2"
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

// defaultBaseURL serves requests when the config base URL is empty.
const defaultBaseURL = "https://aiplatform.googleapis.com"

// defaultHTTPTimeout bounds one generateContent round trip on the default client.
const defaultHTTPTimeout = 60 * time.Second

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

// generateRequest is the JSON body of one generateContent call.
type generateRequest struct {
	Contents         []generateContent        `json:"contents"`
	GenerationConfig generateGenerationConfig `json:"generationConfig"`
}

// generateContent holds the role and ordered parts of one conversation turn.
type generateContent struct {
	Role  string         `json:"role"`
	Parts []generatePart `json:"parts"`
}

// generatePart holds inline media or text. Go cannot express the union so
// exactly one field carries a value.
type generatePart struct {
	InlineData *inlineData `json:"inlineData,omitempty"`
	Text       string      `json:"text,omitempty"`
}

// inlineData carries base64 encoded media bytes.
type inlineData struct {
	MIMEType string `json:"mimeType"`
	Data     string `json:"data"`
}

// generateGenerationConfig tunes one generateContent call.
type generateGenerationConfig struct {
	ResponseMIMEType string  `json:"responseMimeType,omitempty"`
	Temperature      float64 `json:"temperature,omitempty"`
}

// generateResponse mirrors the Vertex AI envelope around the model output.
type generateResponse struct {
	Candidates []generateCandidate `json:"candidates"`
}

// generateCandidate holds one model reply.
type generateCandidate struct {
	Content generateContent `json:"content"`
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
	ts   oauth2.TokenSource
	rec  ChargeRecorder
	hc   *http.Client
	card cost.RateCard
}

// NewSegmenter builds the Vertex AI segmentation client.
// A nil hc selects a client with a 60 second timeout.
func NewSegmenter(cfg *config.Config, ts oauth2.TokenSource, rec ChargeRecorder, hc *http.Client, card cost.RateCard) (Segmenter, error) {
	if cfg == nil {
		return nil, errors.New("gemini segmenter needs a config")
	}
	if ts == nil {
		return nil, errors.New("gemini segmenter needs a token source")
	}
	if rec == nil {
		return nil, errors.New("gemini segmenter needs a charge recorder")
	}
	if hc == nil {
		hc = &http.Client{Timeout: defaultHTTPTimeout}
	}
	return &vertexSegmenter{cfg: cfg, ts: ts, rec: rec, hc: hc, card: card}, nil
}

// Segment sends the media buffer with the proven prompt and parses the reply.
// The charge lands only after a successful HTTP round trip and a full parse.
// A recorded charge therefore always implies a completed segmentation pass.
func (v *vertexSegmenter) Segment(ctx context.Context, in Input) ([]types.Segment, error) {
	tok, err := v.ts.Token()
	if err != nil {
		return nil, fmt.Errorf("fetch access token: %w", err)
	}
	endpoint, err := endpointURL(v.cfg)
	if err != nil {
		return nil, err
	}
	payload := generateRequest{
		Contents: []generateContent{{
			Role: "user",
			Parts: []generatePart{
				{InlineData: &inlineData{
					MIMEType: in.MIMEType,
					Data:     base64.StdEncoding.EncodeToString(in.Data),
				}},
				{Text: segmentPrompt},
			},
		}},
		GenerationConfig: generateGenerationConfig{ResponseMIMEType: "application/json"},
	}
	body, err := postGenerate(ctx, v.hc, tok.AccessToken, endpoint, payload)
	if err != nil {
		return nil, err
	}
	text, err := envelopeText(body)
	if err != nil {
		return nil, err
	}
	segments, err := decodeSegments(text)
	if err != nil {
		return nil, err
	}
	v.rec.Add(cost.Charge{
		Kind:      cost.ChargeSegment,
		TakeID:    0,
		Units:     len(segmentPrompt),
		UnitPrice: v.card.SegmentPerInputChar,
	})
	return segments, nil
}

// endpointURL builds the generateContent URL from the config base URL, project,
// location, and model. It never hardcodes the model or the location.
func endpointURL(cfg *config.Config) (string, error) {
	if cfg.GoogleCloudProject == "" {
		return "", errors.New("config misses GOOGLE_CLOUD_PROJECT")
	}
	if cfg.GoogleCloudLocation == "" {
		return "", errors.New("config misses GOOGLE_CLOUD_LOCATION")
	}
	if cfg.GeminiModel == "" {
		return "", errors.New("config misses GEMINI_MODEL")
	}
	base := cfg.VertexOpenAPIBaseURL
	if base == "" {
		base = defaultBaseURL
	}
	return fmt.Sprintf("%s/v1/projects/%s/locations/%s/publishers/google/models/%s:generateContent",
		strings.TrimSuffix(base, "/"), cfg.GoogleCloudProject, cfg.GoogleCloudLocation, cfg.GeminiModel), nil
}

// postGenerate sends one authenticated generateContent payload and returns the body.
// A non-200 reply becomes an error carrying the status and the trimmed body.
func postGenerate(ctx context.Context, hc *http.Client, token, endpoint string, payload generateRequest) ([]byte, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal generateContent payload: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("build generateContent request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("generateContent request: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read generateContent reply: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("generateContent failed: %d %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return body, nil
}

// envelopeText extracts candidates[0].content.parts[0].text from the reply.
// It rejects replies with zero candidates and replies without text.
func envelopeText(body []byte) (string, error) {
	var env generateResponse
	if err := json.Unmarshal(body, &env); err != nil {
		return "", fmt.Errorf("parse generateContent envelope: %w", err)
	}
	if len(env.Candidates) == 0 {
		return "", errors.New("generateContent reply carries no candidates")
	}
	parts := env.Candidates[0].Content.Parts
	if len(parts) == 0 {
		return "", errors.New("generateContent reply carries no parts")
	}
	if strings.TrimSpace(parts[0].Text) == "" {
		return "", errors.New("generateContent reply carries an empty parts text")
	}
	return parts[0].Text, nil
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
