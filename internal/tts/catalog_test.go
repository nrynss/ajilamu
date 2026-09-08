package tts

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	texttospeechpb "cloud.google.com/go/texttospeech/apiv1/texttospeechpb"
)

// fakeVoiceLister stands in for the Cloud TTS voices.list call.
type fakeVoiceLister struct {
	resp   *texttospeechpb.ListVoicesResponse
	err    error
	gotReq *texttospeechpb.ListVoicesRequest
	calls  int
}

func (f *fakeVoiceLister) ListVoices(_ context.Context, req *texttospeechpb.ListVoicesRequest) (*texttospeechpb.ListVoicesResponse, error) {
	f.calls++
	f.gotReq = req
	return f.resp, f.err
}

// chirpVoice builds one Chirp 3 HD voice entry for a language code.
func chirpVoice(code string) *texttospeechpb.Voice {
	return &texttospeechpb.Voice{
		Name:          code + "-Chirp3-HD-Achernar",
		LanguageCodes: []string{code},
	}
}

func TestCommittedLanguagesAreRealChirp3HDCodes(t *testing.T) {
	languages := CommittedLanguages()
	if len(languages) != 53 {
		t.Fatalf("committed languages = %d, want the 53 documented Chirp 3 HD locales", len(languages))
	}
	if !sort.StringsAreSorted(languages) {
		t.Errorf("committed languages are not sorted: %v", languages)
	}
	seen := map[string]bool{}
	for _, code := range languages {
		if err := ValidateLanguage(code); err != nil {
			t.Errorf("committed code %q is malformed: %v", code, err)
		}
		if seen[code] {
			t.Errorf("committed code %q appears twice", code)
		}
		seen[code] = true
	}
	for _, code := range []string{"ar-XA", "cmn-CN", "en-US", "es-ES", "ml-IN", "nb-NO", "vi-VN", "yue-HK"} {
		if !seen[code] {
			t.Errorf("committed list lacks documented locale %q", code)
		}
	}
}

func TestCommittedLanguagesReturnsACopy(t *testing.T) {
	languages := CommittedLanguages()
	languages[0] = "invented"
	if again := CommittedLanguages(); again[0] == "invented" {
		t.Fatal("CommittedLanguages returned the package slice, so a caller mutated the fallback")
	}
}

func TestNewCatalogServesCommittedList(t *testing.T) {
	catalog := NewCatalog(&fakeVoiceLister{})
	state := catalog.Current()
	if state.FromProvider {
		t.Error("a new catalog claims a provider fetch")
	}
	if !state.FetchedAt.IsZero() {
		t.Errorf("new catalog fetched at %v, want zero", state.FetchedAt)
	}
	if got, want := strings.Join(state.Languages, ","), strings.Join(CommittedLanguages(), ","); got != want {
		t.Errorf("new catalog languages differ from the committed list")
	}
}

func TestRefreshCollectsChirp3HDLanguages(t *testing.T) {
	lister := &fakeVoiceLister{resp: &texttospeechpb.ListVoicesResponse{
		Voices: []*texttospeechpb.Voice{
			chirpVoice("ml-IN"),
			chirpVoice("en-US"),
			chirpVoice("ml-IN"),
			{Name: "en-US-Wavenet-A", LanguageCodes: []string{"en-US"}},
			{Name: "es-ES-Chirp3-HD-Charon", LanguageCodes: []string{"es-ES", "es-US"}},
			{Name: "xx-XX-Chirp3-HD-Orus", LanguageCodes: []string{"not a language", ""}},
		},
	}}
	catalog := NewCatalog(lister)

	if err := catalog.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if lister.calls != 1 {
		t.Fatalf("ListVoices calls = %d, want 1", lister.calls)
	}
	if got := lister.gotReq.GetLanguageCode(); got != "" {
		t.Errorf("ListVoices languageCode = %q, want empty so every voice returns", got)
	}
	state := catalog.Current()
	if !state.FromProvider {
		t.Error("a successful refresh did not mark the provider as the source")
	}
	if state.FetchedAt.IsZero() {
		t.Error("a successful refresh left FetchedAt zero")
	}
	want := "en-US,es-ES,es-US,ml-IN"
	if got := strings.Join(state.Languages, ","); got != want {
		t.Errorf("refreshed languages = %q, want %q", got, want)
	}
}

func TestRefreshFailureKeepsCachedList(t *testing.T) {
	catalog := NewCatalog(&fakeVoiceLister{err: errors.New("provider is down")})
	before := catalog.Current()

	err := catalog.Refresh(context.Background())
	if err == nil {
		t.Fatal("Refresh with a failing provider returned nil")
	}
	if !strings.Contains(err.Error(), "provider is down") {
		t.Errorf("Refresh error = %v, want the provider reason", err)
	}
	if !strings.Contains(err.Error(), "Cloud Text-to-Speech") {
		t.Errorf("Refresh error = %v, want the failing service named", err)
	}
	after := catalog.Current()
	if after.FromProvider || !after.FetchedAt.IsZero() {
		t.Error("a failed refresh claimed a provider fetch")
	}
	if strings.Join(after.Languages, ",") != strings.Join(before.Languages, ",") {
		t.Errorf("a failed refresh changed the cached list")
	}
}

func TestRefreshFailureKeepsFetchedList(t *testing.T) {
	lister := &fakeVoiceLister{resp: &texttospeechpb.ListVoicesResponse{
		Voices: []*texttospeechpb.Voice{chirpVoice("ta-IN")},
	}}
	catalog := NewCatalog(lister)
	if err := catalog.Refresh(context.Background()); err != nil {
		t.Fatalf("first Refresh: %v", err)
	}

	lister.err = errors.New("provider is down")
	if err := catalog.Refresh(context.Background()); err == nil {
		t.Fatal("second Refresh with a failing provider returned nil")
	}
	state := catalog.Current()
	if got := strings.Join(state.Languages, ","); got != "ta-IN" {
		t.Errorf("cached languages after a failed refresh = %q, want ta-IN", got)
	}
	if !state.FromProvider {
		t.Error("a failed refresh dropped the provider source")
	}
}

func TestRefreshWithoutChirp3HDVoicesKeepsList(t *testing.T) {
	catalog := NewCatalog(&fakeVoiceLister{resp: &texttospeechpb.ListVoicesResponse{
		Voices: []*texttospeechpb.Voice{
			{Name: "en-US-Wavenet-A", LanguageCodes: []string{"en-US"}},
		},
	}})

	err := catalog.Refresh(context.Background())
	if err == nil {
		t.Fatal("Refresh with no Chirp 3 HD voices returned nil")
	}
	if !strings.Contains(err.Error(), "Chirp 3 HD") {
		t.Errorf("Refresh error = %v, want the missing Chirp 3 HD voices named", err)
	}
	if state := catalog.Current(); state.FromProvider {
		t.Error("an empty provider answer replaced the committed list")
	}
}

func TestRefreshNamesMissingADCCredential(t *testing.T) {
	catalog := &Catalog{
		build: func(context.Context) (VoiceLister, error) {
			return nil, errors.New("google: could not find default credentials. See https://cloud.google.com/docs/authentication/external/set-up-adc for more information")
		},
		languages: CommittedLanguages(),
	}

	err := catalog.Refresh(context.Background())
	if err == nil {
		t.Fatal("Refresh without ADC returned nil")
	}
	for _, want := range []string{"Application Default Credentials", "GOOGLE_APPLICATION_CREDENTIALS"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Refresh error = %v, want %q named", err, want)
		}
	}
	if state := catalog.Current(); state.FromProvider {
		t.Error("a missing credential replaced the committed list")
	}
}

func TestIsADCMissing(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"adc prose", errors.New("google: could not find default credentials. See https://example.test"), true},
		{"mixed case", errors.New("Could not find Default Credentials"), true},
		{"unrelated", errors.New("connection refused"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isADCMissing(tc.err); got != tc.want {
				t.Errorf("isADCMissing(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// countingVoiceLister counts voices.list calls and is safe under the race detector.
type countingVoiceLister struct {
	resp  *texttospeechpb.ListVoicesResponse
	calls atomic.Int64
}

func (c *countingVoiceLister) ListVoices(_ context.Context, _ *texttospeechpb.ListVoicesRequest) (*texttospeechpb.ListVoicesResponse, error) {
	c.calls.Add(1)
	return c.resp, nil
}

// TestConcurrentRefreshBuildsOneClient pins the serialized build.
// Eight refreshes race the first build and must share one client.
func TestConcurrentRefreshBuildsOneClient(t *testing.T) {
	lister := &countingVoiceLister{resp: &texttospeechpb.ListVoicesResponse{
		Voices: []*texttospeechpb.Voice{chirpVoice("ml-IN")},
	}}
	var builds atomic.Int64
	catalog := &Catalog{
		build: func(context.Context) (VoiceLister, error) {
			builds.Add(1)
			time.Sleep(time.Millisecond)
			return lister, nil
		},
		languages: CommittedLanguages(),
	}

	const refreshers = 8
	start := make(chan struct{})
	errs := make([]error, refreshers)
	var wg sync.WaitGroup
	for i := range refreshers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs[i] = catalog.Refresh(context.Background())
		}()
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("Refresh %d: %v", i, err)
		}
	}
	if got := builds.Load(); got != 1 {
		t.Errorf("client builds = %d, want 1", got)
	}
	if got := lister.calls.Load(); got != refreshers {
		t.Errorf("ListVoices calls = %d, want %d, one per refresh and none coalesced", got, refreshers)
	}
	t.Logf("client builds = %d, ListVoices calls = %d", builds.Load(), lister.calls.Load())
}
