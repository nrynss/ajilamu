package tts

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	texttospeechpb "cloud.google.com/go/texttospeech/apiv1/texttospeechpb"

	"github.com/nrynss/ajilamu/internal/config"
	"github.com/nrynss/ajilamu/internal/cost"
	"github.com/nrynss/ajilamu/internal/types"
)

const cannedLINEAR16 = "RIFF-canned-linear16-wav-bytes"

const malayalamLine = "ചലനം"

func testConfig() *config.Config {
	return &config.Config{
		GeminiModel:         "gemini-3.8-flash",
		GoogleCloudProject:  "test-project",
		GoogleCloudLocation: "global",
	}
}

type fakeTTS struct {
	err    error
	audio  []byte
	gotReq *texttospeechpb.SynthesizeSpeechRequest
	calls  int
}

func (f *fakeTTS) SynthesizeSpeech(ctx context.Context, req *texttospeechpb.SynthesizeSpeechRequest) (*texttospeechpb.SynthesizeSpeechResponse, error) {
	f.gotReq = req
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return &texttospeechpb.SynthesizeSpeechResponse{AudioContent: f.audio}, nil
}

func newTestSynthesizer(t *testing.T, rec ChargeRecorder, client TTSClient) Synthesizer {
	t.Helper()
	syn, err := NewSynthesizer(testConfig(), rec, cost.DefaultRateCard(), client)
	if err != nil {
		t.Fatalf("NewSynthesizer: %v", err)
	}
	return syn
}

func TestSynthesizePinsChirpRequestAndWritesLINEAR16(t *testing.T) {
	client := &fakeTTS{audio: []byte(cannedLINEAR16)}
	ledger := cost.NewLedger()
	syn := newTestSynthesizer(t, ledger, client)
	out := filepath.Join(t.TempDir(), "nested", "seg_1_try1.wav")

	req := SynthesizeRequest{
		SegmentID: 1,
		Text:      malayalamLine,
		Speaker:   types.Speaker{Name: "Suni Williams"},
		OutPath:   out,
	}
	if err := syn.Synthesize(context.Background(), req); err != nil {
		t.Fatalf("Synthesize: %v", err)
	}

	if client.calls != 1 {
		t.Fatalf("SynthesizeSpeech calls = %d, want 1", client.calls)
	}
	if client.gotReq == nil || client.gotReq.Voice == nil || client.gotReq.AudioConfig == nil || client.gotReq.Input == nil {
		t.Fatalf("request is incomplete: %#v", client.gotReq)
	}
	if got := client.gotReq.Voice.LanguageCode; got != Malayalam {
		t.Errorf("languageCode = %q, want %s", got, Malayalam)
	}
	if got := client.gotReq.Voice.Name; got != "ml-IN-Chirp3-HD-Achernar" {
		t.Errorf("voice name = %q, want ml-IN-Chirp3-HD-Achernar", got)
	}
	if got := client.gotReq.AudioConfig.AudioEncoding; got != texttospeechpb.AudioEncoding_LINEAR16 {
		t.Errorf("audioEncoding = %v, want LINEAR16", got)
	}
	if got := client.gotReq.AudioConfig.SampleRateHertz; got != 0 {
		t.Errorf("sampleRateHertz = %d, want 0 so Chirp keeps its native rate", got)
	}
	if got := client.gotReq.Input.GetText(); got != malayalamLine {
		t.Errorf("input text = %q, want %q", got, malayalamLine)
	}

	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read take: %v", err)
	}
	if !bytes.Equal(got, []byte(cannedLINEAR16)) {
		t.Errorf("wrote %q, want the LINEAR16 bytes unchanged", got)
	}
}

func TestSynthesizeAssignsAchirdToMark(t *testing.T) {
	client := &fakeTTS{audio: []byte(cannedLINEAR16)}
	syn := newTestSynthesizer(t, cost.NewLedger(), client)
	out := filepath.Join(t.TempDir(), "seg_7_try1.wav")

	err := syn.Synthesize(context.Background(), SynthesizeRequest{
		SegmentID: 7,
		Text:      malayalamLine,
		Speaker:   types.Speaker{Name: "Mark Vande Hei"},
		OutPath:   out,
	})
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if client.gotReq == nil || client.gotReq.Voice == nil {
		t.Fatal("request missed voice params")
	}
	if got := client.gotReq.Voice.Name; got != "ml-IN-Chirp3-HD-Achird" {
		t.Errorf("Mark Vande Hei voice = %q, want ml-IN-Chirp3-HD-Achird", got)
	}
}

func TestSynthesizeChargeUsesRuneCount(t *testing.T) {
	client := &fakeTTS{audio: []byte(cannedLINEAR16)}
	ledger := cost.NewLedger()
	card := cost.DefaultRateCard()
	syn, err := NewSynthesizer(testConfig(), ledger, card, client)
	if err != nil {
		t.Fatalf("NewSynthesizer: %v", err)
	}

	req := SynthesizeRequest{
		SegmentID: 8,
		Text:      malayalamLine,
		Speaker:   types.Speaker{Name: "Mark Vande Hei"},
		OutPath:   filepath.Join(t.TempDir(), "seg_8_try1.wav"),
	}
	if err := syn.Synthesize(context.Background(), req); err != nil {
		t.Fatalf("Synthesize: %v", err)
	}

	runes := utf8.RuneCountInString(malayalamLine)
	if runes == len(malayalamLine) {
		t.Fatalf("malayalamLine rune count %d equals byte length, pick a better pin", runes)
	}
	charges := ledger.Charges()
	if len(charges) != 1 {
		t.Fatalf("charges = %d, want 1", len(charges))
	}
	want := cost.Charge{
		Kind:      cost.ChargeSynthesize,
		TakeID:    8,
		Units:     runes,
		UnitPrice: card.SynthesizePerChar,
	}
	if charges[0] != want {
		t.Errorf("charge = %#v, want %#v", charges[0], want)
	}
	if charges[0].PromptTokens != 0 || charges[0].CandidateTokens != 0 {
		t.Errorf("Gemini token fields = %d/%d, want 0", charges[0].PromptTokens, charges[0].CandidateTokens)
	}
	if charges[0].Units == len(malayalamLine) {
		t.Errorf("charge billed byte length %d, want rune count %d", len(malayalamLine), runes)
	}
	if got, wantTotal := charges[0].Total(), cost.Price(runes)*card.SynthesizePerChar; got != wantTotal {
		t.Errorf("charge total = %d, want %d", got, wantTotal)
	}
}

func TestSynthesizeErrorRecordsNoChargeAndWritesNoFile(t *testing.T) {
	client := &fakeTTS{err: errors.New("upstream exploded")}
	ledger := cost.NewLedger()
	syn := newTestSynthesizer(t, ledger, client)
	out := filepath.Join(t.TempDir(), "seg_1_try1.wav")

	err := syn.Synthesize(context.Background(), SynthesizeRequest{
		SegmentID: 1,
		Text:      malayalamLine,
		Speaker:   types.Speaker{Name: "Suni Williams"},
		OutPath:   out,
	})
	if err == nil || !strings.Contains(err.Error(), "upstream exploded") {
		t.Errorf("error = %v, want upstream exploded", err)
	}
	if charges := ledger.Charges(); len(charges) != 0 {
		t.Errorf("failed pass recorded %d charges, want zero", len(charges))
	}
	if _, err := os.Stat(out); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("failed pass left %s, want no file", out)
	}
}

func TestSynthesizeEmptyAudioRecordsNoCharge(t *testing.T) {
	client := &fakeTTS{audio: nil}
	ledger := cost.NewLedger()
	syn := newTestSynthesizer(t, ledger, client)
	out := filepath.Join(t.TempDir(), "seg_1_try1.wav")

	err := syn.Synthesize(context.Background(), SynthesizeRequest{
		SegmentID: 1,
		Text:      malayalamLine,
		Speaker:   types.Speaker{Name: "Suni Williams"},
		OutPath:   out,
	})
	if err == nil || !strings.Contains(err.Error(), "empty audio") {
		t.Errorf("error = %v, want empty audio", err)
	}
	if charges := ledger.Charges(); len(charges) != 0 {
		t.Errorf("empty audio recorded %d charges, want zero", len(charges))
	}
	if _, err := os.Stat(out); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("empty audio left %s, want no file", out)
	}
}

func TestSynthesizeUnknownSpeakerSkipsAPI(t *testing.T) {
	client := &fakeTTS{audio: []byte(cannedLINEAR16)}
	ledger := cost.NewLedger()
	syn := newTestSynthesizer(t, ledger, client)
	out := filepath.Join(t.TempDir(), "seg_1_try1.wav")

	err := syn.Synthesize(context.Background(), SynthesizeRequest{
		SegmentID: 1,
		Text:      malayalamLine,
		Speaker:   types.Speaker{Name: "Mark Van der High"},
		OutPath:   out,
	})
	if err == nil || !strings.Contains(err.Error(), "unknown speaker") {
		t.Errorf("error = %v, want unknown speaker", err)
	}
	if client.calls != 0 {
		t.Errorf("unknown speaker called SynthesizeSpeech %d times", client.calls)
	}
	if charges := ledger.Charges(); len(charges) != 0 {
		t.Errorf("unknown speaker recorded %d charges, want zero", len(charges))
	}
}

func TestSynthesizeRefusesOverwriteWithoutAPICall(t *testing.T) {
	client := &fakeTTS{audio: []byte(cannedLINEAR16)}
	ledger := cost.NewLedger()
	syn := newTestSynthesizer(t, ledger, client)
	out := filepath.Join(t.TempDir(), "seg_1_try1.wav")
	if err := os.WriteFile(out, []byte("prior-take"), 0o644); err != nil {
		t.Fatalf("seed dest: %v", err)
	}

	err := syn.Synthesize(context.Background(), SynthesizeRequest{
		SegmentID: 1,
		Text:      malayalamLine,
		Speaker:   types.Speaker{Name: "Suni Williams"},
		OutPath:   out,
	})
	if err == nil || !strings.Contains(err.Error(), "overwrite") {
		t.Errorf("error = %v, want overwrite refusal", err)
	}
	if client.calls != 0 {
		t.Errorf("overwrite path called SynthesizeSpeech %d times", client.calls)
	}
	if charges := ledger.Charges(); len(charges) != 0 {
		t.Errorf("overwrite path recorded %d charges, want zero", len(charges))
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	if !bytes.Equal(got, []byte("prior-take")) {
		t.Errorf("dest became %q, want the prior take untouched", got)
	}
}

func TestSynthesizeFailedWriteRecordsNoCharge(t *testing.T) {
	client := &fakeTTS{audio: []byte(cannedLINEAR16)}
	ledger := cost.NewLedger()
	syn := newTestSynthesizer(t, ledger, client)
	dir := t.TempDir()
	blocker := filepath.Join(dir, "not-a-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("seed blocker: %v", err)
	}
	out := filepath.Join(blocker, "seg_1_try1.wav")

	err := syn.Synthesize(context.Background(), SynthesizeRequest{
		SegmentID: 1,
		Text:      malayalamLine,
		Speaker:   types.Speaker{Name: "Suni Williams"},
		OutPath:   out,
	})
	if err == nil {
		t.Fatal("Synthesize succeeded, want write failure")
	}
	if charges := ledger.Charges(); len(charges) != 0 {
		t.Errorf("failed write recorded %d charges, want zero", len(charges))
	}
}

func TestNewSynthesizerNilArguments(t *testing.T) {
	cfg := testConfig()
	var rec ChargeRecorder = cost.NewLedger()
	client := &fakeTTS{audio: []byte(cannedLINEAR16)}

	if _, err := NewSynthesizer(nil, rec, cost.DefaultRateCard(), client); err == nil {
		t.Error("nil config accepted, want error")
	}
	if _, err := NewSynthesizer(cfg, nil, cost.DefaultRateCard(), client); err == nil {
		t.Error("nil charge recorder accepted, want error")
	}
	broken := testConfig()
	broken.GoogleCloudProject = ""
	if _, err := NewSynthesizer(broken, rec, cost.DefaultRateCard(), client); err == nil {
		t.Error("missing project accepted, want error")
	}
	if _, err := NewSynthesizer(cfg, rec, cost.DefaultRateCard(), client); err != nil {
		t.Errorf("valid arguments rejected: %v", err)
	}
}

func TestFixtureSynthesizerCopiesTakeAndCharges(t *testing.T) {
	src := testdataFile(t, filepath.Join("takes", "seg_1_try1.wav"))
	want, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read fixture take: %v", err)
	}
	ledger := cost.NewLedger()
	card := cost.DefaultRateCard()
	syn := NewFixtureSynthesizer(filepath.Dir(src), ledger, card)
	out := filepath.Join(t.TempDir(), "copied", "seg_1_try1.wav")

	req := SynthesizeRequest{
		SegmentID: 1,
		Text:      malayalamLine,
		Speaker:   types.Speaker{Name: "Suni Williams"},
		OutPath:   out,
	}
	if err := syn.Synthesize(context.Background(), req); err != nil {
		t.Fatalf("fixture Synthesize: %v", err)
	}

	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read copied take: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("copied %d bytes, want %d fixture bytes", len(got), len(want))
	}
	charges := ledger.Charges()
	if len(charges) != 1 {
		t.Fatalf("fixture charges = %d, want 1", len(charges))
	}
	wantCharge := cost.Charge{
		Kind:      cost.ChargeSynthesize,
		TakeID:    1,
		Units:     utf8.RuneCountInString(malayalamLine),
		UnitPrice: card.SynthesizePerChar,
	}
	if charges[0] != wantCharge {
		t.Errorf("fixture charge = %#v, want %#v", charges[0], wantCharge)
	}
}

func TestFixtureSynthesizerMissingTakeRecordsNoCharge(t *testing.T) {
	src := testdataFile(t, filepath.Join("takes", "seg_1_try1.wav"))
	ledger := cost.NewLedger()
	syn := NewFixtureSynthesizer(filepath.Dir(src), ledger, cost.DefaultRateCard())
	out := filepath.Join(t.TempDir(), "seg_99_try1.wav")

	err := syn.Synthesize(context.Background(), SynthesizeRequest{
		SegmentID: 99,
		Text:      malayalamLine,
		Speaker:   types.Speaker{Name: "Suni Williams"},
		OutPath:   out,
	})
	if err == nil {
		t.Fatal("missing fixture succeeded, want error")
	}
	if charges := ledger.Charges(); len(charges) != 0 {
		t.Errorf("missing fixture recorded %d charges, want zero", len(charges))
	}
	if _, err := os.Stat(out); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("missing fixture left %s, want no file", out)
	}
}

func TestPackageAvoidsEnvReaders(t *testing.T) {
	matches, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("list package files: %v", err)
	}
	if len(matches) == 0 {
		t.Fatal("found no Go files in the package")
	}
	needles := []string{"Get" + "env", "Lookup" + "Env", "go" + "dotenv"}
	for _, path := range matches {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for _, needle := range needles {
			if bytes.Contains(body, []byte(needle)) {
				t.Errorf("%s mentions %s", path, needle)
			}
		}
	}
}
