package tts

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	texttospeech "cloud.google.com/go/texttospeech/apiv1"
	texttospeechpb "cloud.google.com/go/texttospeech/apiv1/texttospeechpb"
)

// VoiceLister wraps the Cloud Text-to-Speech voices.list call.
type VoiceLister interface {
	ListVoices(ctx context.Context, req *texttospeechpb.ListVoicesRequest) (*texttospeechpb.ListVoicesResponse, error)
}

// sdkVoiceLister adapts the official Cloud TTS client onto VoiceLister.
type sdkVoiceLister struct {
	inner *texttospeech.Client
}

func (s sdkVoiceLister) ListVoices(ctx context.Context, req *texttospeechpb.ListVoicesRequest) (*texttospeechpb.ListVoicesResponse, error) {
	return s.inner.ListVoices(ctx, req)
}

// committedChirp3HDLanguages lists the Cloud TTS Chirp 3 HD languages.
// The list matches docs.cloud.google.com/text-to-speech/docs/chirp3-hd
// as read on 2026-09-09. It is sorted, so GET /api/languages is stable.
var committedChirp3HDLanguages = []string{
	"ar-XA", "bg-BG", "bn-IN", "cmn-CN", "cs-CZ", "da-DK", "de-DE", "el-GR",
	"en-AU", "en-GB", "en-IN", "en-US", "es-ES", "es-US", "et-EE", "fi-FI",
	"fr-CA", "fr-FR", "gu-IN", "he-IL", "hi-IN", "hr-HR", "hu-HU", "id-ID",
	"it-IT", "ja-JP", "kn-IN", "ko-KR", "lt-LT", "lv-LV", "ml-IN", "mr-IN",
	"nb-NO", "nl-BE", "nl-NL", "pa-IN", "pl-PL", "pt-BR", "ro-RO", "ru-RU",
	"sk-SK", "sl-SI", "sr-RS", "sv-SE", "sw-KE", "ta-IN", "te-IN", "th-TH",
	"tr-TR", "uk-UA", "ur-IN", "vi-VN", "yue-HK",
}

// CommittedLanguages returns a copy of the committed fallback list.
// A clone with no credentials serves this list before any fetch runs.
func CommittedLanguages() []string {
	return append([]string(nil), committedChirp3HDLanguages...)
}

// CatalogState is one reading of the supported-language catalog.
type CatalogState struct {
	// Languages lists the supported BCP-47 codes, sorted.
	Languages []string
	// FromProvider reports whether a provider fetch produced the list.
	FromProvider bool
	// FetchedAt is the fetch time. It is zero before the first fetch.
	FetchedAt time.Time
}

// Catalog holds the supported-language list and where it came from.
// It serves the committed list until a provider fetch succeeds.
type Catalog struct {
	mu           sync.RWMutex
	lister       VoiceLister
	build        func(context.Context) (VoiceLister, error)
	languages    []string
	fromProvider bool
	fetchedAt    time.Time
	// buildMu serializes production client construction.
	buildMu sync.Mutex
}

// NewCatalog builds a catalog that serves the committed list.
// A nil lister opens the production ADC client on the first refresh.
func NewCatalog(lister VoiceLister) *Catalog {
	return &Catalog{
		lister:    lister,
		languages: CommittedLanguages(),
	}
}

// Current returns the catalog as it stands. The caller owns the copy.
func (c *Catalog) Current() CatalogState {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return CatalogState{
		Languages:    append([]string(nil), c.languages...),
		FromProvider: c.fromProvider,
		FetchedAt:    c.fetchedAt,
	}
}

// Refresh fetches the Chirp 3 HD languages through voices.list.
// A failure names the reason and leaves the cached list untouched.
func (c *Catalog) Refresh(ctx context.Context) error {
	lister, err := c.voiceLister(ctx)
	if err != nil {
		return err
	}
	resp, err := lister.ListVoices(ctx, &texttospeechpb.ListVoicesRequest{})
	if err != nil {
		return fmt.Errorf("fetch the supported languages from Cloud Text-to-Speech: %w", err)
	}
	languages, err := chirp3HDLanguages(resp)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.languages = languages
	c.fromProvider = true
	c.fetchedAt = time.Now().UTC()
	c.mu.Unlock()
	return nil
}

// voiceLister returns the injected lister or builds the production one once.
// The code serializes construction, so one client exists and none leaks. A
// failed build caches nothing, so the next refresh retries. The code does not
// coalesce concurrent refreshes. Each caller issues its own voices.list with
// its own context, so one caller's cancellation cannot fail another's refresh.
func (c *Catalog) voiceLister(ctx context.Context) (VoiceLister, error) {
	c.mu.RLock()
	lister, build := c.lister, c.build
	c.mu.RUnlock()
	if lister != nil {
		return lister, nil
	}
	c.buildMu.Lock()
	defer c.buildMu.Unlock()
	c.mu.RLock()
	lister = c.lister
	c.mu.RUnlock()
	if lister != nil {
		return lister, nil
	}
	if build == nil {
		build = newProductionVoiceLister
	}
	lister, err := build(ctx)
	if err != nil {
		return nil, nameMissingCredential(err)
	}
	c.mu.Lock()
	c.lister = lister
	c.mu.Unlock()
	return lister, nil
}

// newProductionVoiceLister builds the official Cloud TTS client on ADC.
// It passes no API key and no key file.
func newProductionVoiceLister(ctx context.Context) (VoiceLister, error) {
	client, err := texttospeech.NewClient(ctx)
	if err != nil {
		return nil, err
	}
	return sdkVoiceLister{inner: client}, nil
}

// nameMissingCredential names the credential when client construction failed
// because ADC is missing. Every other construction failure keeps its cause.
func nameMissingCredential(err error) error {
	if isADCMissing(err) {
		return fmt.Errorf("Cloud Text-to-Speech needs Application Default Credentials: set GOOGLE_APPLICATION_CREDENTIALS or run gcloud auth application-default login: %w", err)
	}
	return fmt.Errorf("construct Cloud Text-to-Speech client: %w", err)
}

// isADCMissing reports whether client construction failed for lack of ADC.
// The SDK reports the missing credential in prose, so match that prose.
func isADCMissing(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "default credentials")
}

// chirp3HDLanguages collects the language codes of the Chirp 3 HD voices.
// The codes come back sorted and deduplicated.
func chirp3HDLanguages(resp *texttospeechpb.ListVoicesResponse) ([]string, error) {
	if resp == nil {
		return nil, errors.New("Cloud Text-to-Speech returned no voices")
	}
	seen := map[string]struct{}{}
	for _, voice := range resp.GetVoices() {
		if !isChirp3HDVoice(voice.GetName()) {
			continue
		}
		for _, code := range voice.GetLanguageCodes() {
			code = strings.TrimSpace(code)
			if code == "" || ValidateLanguage(code) != nil {
				continue
			}
			seen[code] = struct{}{}
		}
	}
	if len(seen) == 0 {
		return nil, errors.New("Cloud Text-to-Speech returned no Chirp 3 HD languages")
	}
	languages := make([]string, 0, len(seen))
	for code := range seen {
		languages = append(languages, code)
	}
	sort.Strings(languages)
	return languages, nil
}

// isChirp3HDVoice reports whether a voice name belongs to the Chirp 3 HD set.
func isChirp3HDVoice(name string) bool {
	return strings.Contains(name, "-Chirp3-HD-")
}
