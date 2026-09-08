// Package tts synthesizes speech with Cloud Chirp 3 HD voices.
package tts

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	texttospeech "cloud.google.com/go/texttospeech/apiv1"
	texttospeechpb "cloud.google.com/go/texttospeech/apiv1/texttospeechpb"

	"github.com/nrynss/ajilamu/internal/config"
	"github.com/nrynss/ajilamu/internal/cost"
	"github.com/nrynss/ajilamu/internal/types"
)

// SynthesizeRequest is one spoken line to render as a WAV take.
type SynthesizeRequest struct {
	// SegmentID identifies the take. It lands on the charge as TakeID.
	SegmentID int
	// Text is the spoken line in the synthesizer's target language.
	Text string
	// Speaker selects the Chirp 3 HD voice through Assign.
	Speaker types.Speaker
	// OutPath is the WAV path to write once. The caller chooses the name.
	OutPath string
}

// Synthesizer renders one spoken line to an independent WAV file.
type Synthesizer interface {
	// Synthesize writes LINEAR16 audio to OutPath and records one charge.
	Synthesize(ctx context.Context, req SynthesizeRequest) error
}

// ChargeRecorder receives the itemized billing trail of API invocations.
type ChargeRecorder interface {
	// Add records one charge.
	Add(cost.Charge)
}

// TTSClient wraps Cloud Text-to-Speech SynthesizeSpeech.
type TTSClient interface {
	SynthesizeSpeech(ctx context.Context, req *texttospeechpb.SynthesizeSpeechRequest) (*texttospeechpb.SynthesizeSpeechResponse, error)
}

// sdkTTSClient adapts the official Cloud TTS client onto TTSClient.
type sdkTTSClient struct {
	inner *texttospeech.Client
}

func (s sdkTTSClient) SynthesizeSpeech(ctx context.Context, req *texttospeechpb.SynthesizeSpeechRequest) (*texttospeechpb.SynthesizeSpeechResponse, error) {
	return s.inner.SynthesizeSpeech(ctx, req)
}

// chirpSynthesizer is the Cloud TTS implementation of Synthesizer.
type chirpSynthesizer struct {
	rec      ChargeRecorder
	card     cost.RateCard
	client   TTSClient
	language string
}

// NewSynthesizer builds the Chirp 3 HD synthesis client for one target language.
// The language must be a well formed BCP-47 code. A nil client constructs the
// production ADC client. The language reaches Cloud TTS as the LanguageCode.
func NewSynthesizer(cfg *config.Config, language string, rec ChargeRecorder, card cost.RateCard, client TTSClient) (Synthesizer, error) {
	if cfg == nil {
		return nil, errors.New("tts synthesizer needs a config")
	}
	language = strings.TrimSpace(language)
	if err := ValidateLanguage(language); err != nil {
		return nil, err
	}
	if rec == nil {
		return nil, errors.New("tts synthesizer needs a charge recorder")
	}
	if cfg.GoogleCloudProject == "" {
		return nil, errors.New("config misses GOOGLE_CLOUD_PROJECT")
	}
	if client == nil {
		built, err := newProductionClient()
		if err != nil {
			return nil, err
		}
		client = built
	}
	return &chirpSynthesizer{
		rec:      rec,
		card:     card,
		client:   client,
		language: language,
	}, nil
}

// newProductionClient builds the official Cloud TTS client on ADC.
// It passes no API key and no key file.
func newProductionClient() (TTSClient, error) {
	c, err := texttospeech.NewClient(context.Background())
	if err != nil {
		return nil, fmt.Errorf("construct Cloud Text-to-Speech client: %w", err)
	}
	return sdkTTSClient{inner: c}, nil
}

// Synthesize requests native-rate LINEAR16 audio and writes it to OutPath.
// The charge lands only after the WAV write succeeds.
func (s *chirpSynthesizer) Synthesize(ctx context.Context, req SynthesizeRequest) error {
	if err := validateRequest(req); err != nil {
		return err
	}
	voice, err := Assign(req.Speaker, s.language)
	if err != nil {
		return err
	}
	if err := refuseOverwrite(req.OutPath); err != nil {
		return err
	}
	// Omit SampleRateHertz so Chirp returns its native rate.
	resp, err := s.client.SynthesizeSpeech(ctx, &texttospeechpb.SynthesizeSpeechRequest{
		Input: &texttospeechpb.SynthesisInput{
			InputSource: &texttospeechpb.SynthesisInput_Text{Text: req.Text},
		},
		Voice: &texttospeechpb.VoiceSelectionParams{
			LanguageCode: voice.LanguageCode,
			Name:         voice.Name,
		},
		AudioConfig: &texttospeechpb.AudioConfig{
			AudioEncoding: texttospeechpb.AudioEncoding_LINEAR16,
		},
	})
	if err != nil {
		return err
	}
	if resp == nil || len(resp.AudioContent) == 0 {
		return errors.New("tts synthesize received empty audio")
	}
	if err := writeTake(req.OutPath, resp.AudioContent); err != nil {
		return err
	}
	s.rec.Add(synthesizeCharge(req, s.card))
	return nil
}

// fixtureSynthesizer copies golden takes and still records the synthesize charge.
type fixtureSynthesizer struct {
	takesDir string
	rec      ChargeRecorder
	card     cost.RateCard
}

// NewFixtureSynthesizer copies golden takes from testdata and still bills.
func NewFixtureSynthesizer(testdataTakesDir string, rec ChargeRecorder, card cost.RateCard) Synthesizer {
	return &fixtureSynthesizer{takesDir: testdataTakesDir, rec: rec, card: card}
}

// Synthesize copies testdata/takes/seg_{id}_try1.wav onto OutPath.
func (f *fixtureSynthesizer) Synthesize(ctx context.Context, req SynthesizeRequest) error {
	if f.rec == nil {
		return errors.New("tts synthesizer needs a charge recorder")
	}
	if err := validateRequest(req); err != nil {
		return err
	}
	src := filepath.Join(f.takesDir, fmt.Sprintf("seg_%d_try1.wav", req.SegmentID))
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := writeTake(req.OutPath, data); err != nil {
		return err
	}
	f.rec.Add(synthesizeCharge(req, f.card))
	return nil
}

func synthesizeCharge(req SynthesizeRequest, card cost.RateCard) cost.Charge {
	return cost.Charge{
		Kind:      cost.ChargeSynthesize,
		TakeID:    req.SegmentID,
		Units:     utf8.RuneCountInString(req.Text),
		UnitPrice: card.SynthesizePerChar,
	}
}

func validateRequest(req SynthesizeRequest) error {
	if req.OutPath == "" {
		return errors.New("tts synthesize needs an output path")
	}
	if req.Text == "" {
		return errors.New("tts synthesize needs spoken text")
	}
	return nil
}

func refuseOverwrite(path string) error {
	_, err := os.Stat(path)
	if err == nil {
		return fmt.Errorf("tts refuses to overwrite %s", path)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// writeTake stores LINEAR16 bytes as a WAV file.
// The API payload already includes a WAV header. Do not wrap it again.
func writeTake(path string, audio []byte) error {
	if len(audio) == 0 {
		return errors.New("tts synthesize received empty audio")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	_, writeErr := f.Write(audio)
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}
