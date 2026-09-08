package api_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nrynss/ajilamu/internal/api"
)

// fakeLanguageSource stands in for the TTS catalog behind the routes.
type fakeLanguageSource struct {
	catalog api.LanguageCatalog
	err     error
	calls   int
}

func (f *fakeLanguageSource) Catalog() api.LanguageCatalog {
	return f.catalog
}

func (f *fakeLanguageSource) Refresh(context.Context) (api.LanguageCatalog, error) {
	f.calls++
	if f.err != nil {
		return api.LanguageCatalog{}, f.err
	}
	return f.catalog, nil
}

// committedCatalog is the payload a clone with no credentials serves.
func committedCatalog() api.LanguageCatalog {
	return api.LanguageCatalog{
		Languages: []string{"en-US", "ml-IN"},
		Source:    api.CatalogSourceCommitted,
	}
}

func TestLanguagesHandlerServesCatalog(t *testing.T) {
	source := &fakeLanguageSource{catalog: committedCatalog()}
	handler := api.LanguagesHandler(source)

	request := httptest.NewRequest(http.MethodGet, "/api/languages", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	if got := response.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Fatalf("Content-Type = %q", got)
	}
	var catalog api.LanguageCatalog
	if err := jsonDecode(response, &catalog); err != nil {
		t.Fatalf("decode catalog: %v", err)
	}
	if got := strings.Join(catalog.Languages, ","); got != "en-US,ml-IN" {
		t.Errorf("languages = %q, want en-US,ml-IN", got)
	}
	if catalog.Source != api.CatalogSourceCommitted {
		t.Errorf("source = %q, want %q", catalog.Source, api.CatalogSourceCommitted)
	}
	if catalog.FetchedAt != "" {
		t.Errorf("fetched_at = %q, want empty for the committed list", catalog.FetchedAt)
	}
	if source.calls != 0 {
		t.Errorf("GET refreshed the provider %d times, want 0", source.calls)
	}
}

func TestLanguagesHandlerRejectsOtherMethods(t *testing.T) {
	handler := api.LanguagesHandler(&fakeLanguageSource{catalog: committedCatalog()})
	request := httptest.NewRequest(http.MethodPost, "/api/languages", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusMethodNotAllowed)
	}
	if got := response.Header().Get("Allow"); got != http.MethodGet {
		t.Errorf("Allow = %q, want GET", got)
	}
}

func TestLanguagesHandlerWithoutSourceAnswers503(t *testing.T) {
	handler := api.LanguagesHandler(nil)
	request := httptest.NewRequest(http.MethodGet, "/api/languages", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
}

func TestLanguagesRefreshHandlerReturnsProviderList(t *testing.T) {
	source := &fakeLanguageSource{catalog: api.LanguageCatalog{
		Languages: []string{"en-US", "ml-IN", "ta-IN"},
		Source:    api.CatalogSourceProvider,
		FetchedAt: "2026-09-09T12:00:00Z",
	}}
	handler := api.LanguagesRefreshHandler(source)

	request := httptest.NewRequest(http.MethodPost, "/api/languages/refresh", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if source.calls != 1 {
		t.Fatalf("refresh calls = %d, want 1", source.calls)
	}
	var catalog api.LanguageCatalog
	if err := jsonDecode(response, &catalog); err != nil {
		t.Fatalf("decode catalog: %v", err)
	}
	if got := strings.Join(catalog.Languages, ","); got != "en-US,ml-IN,ta-IN" {
		t.Errorf("languages = %q, want the provider list", got)
	}
	if catalog.Source != api.CatalogSourceProvider {
		t.Errorf("source = %q, want %q", catalog.Source, api.CatalogSourceProvider)
	}
	if catalog.FetchedAt != "2026-09-09T12:00:00Z" {
		t.Errorf("fetched_at = %q, want the provider fetch time", catalog.FetchedAt)
	}
}

func TestLanguagesRefreshFailureNamesReasonAndKeepsCache(t *testing.T) {
	source := &fakeLanguageSource{
		catalog: committedCatalog(),
		err:     errors.New("Cloud Text-to-Speech needs Application Default Credentials: set GOOGLE_APPLICATION_CREDENTIALS or run gcloud auth application-default login"),
	}
	refresh := api.LanguagesRefreshHandler(source)

	request := httptest.NewRequest(http.MethodPost, "/api/languages/refresh", nil)
	response := httptest.NewRecorder()
	refresh.ServeHTTP(response, request)

	if response.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadGateway)
	}
	body := response.Body.String()
	for _, want := range []string{"Refresh the language catalog.", "Application Default Credentials", "GOOGLE_APPLICATION_CREDENTIALS"} {
		if !strings.Contains(body, want) {
			t.Errorf("body = %q, want %q named", body, want)
		}
	}

	// GET still serves the cached list after the failed fetch.
	read := api.LanguagesHandler(source)
	after := httptest.NewRecorder()
	read.ServeHTTP(after, httptest.NewRequest(http.MethodGet, "/api/languages", nil))
	if after.Code != http.StatusOK {
		t.Fatalf("GET after a failed refresh = %d, want %d", after.Code, http.StatusOK)
	}
	var catalog api.LanguageCatalog
	if err := jsonDecode(after, &catalog); err != nil {
		t.Fatalf("decode catalog after failure: %v", err)
	}
	if got := strings.Join(catalog.Languages, ","); got != "en-US,ml-IN" {
		t.Errorf("cached languages after a failed refresh = %q, want en-US,ml-IN", got)
	}
	if catalog.Source != api.CatalogSourceCommitted {
		t.Errorf("cached source after a failed refresh = %q, want committed", catalog.Source)
	}
}

func TestLanguagesRefreshHandlerRejectsGet(t *testing.T) {
	handler := api.LanguagesRefreshHandler(&fakeLanguageSource{catalog: committedCatalog()})
	request := httptest.NewRequest(http.MethodGet, "/api/languages/refresh", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusMethodNotAllowed)
	}
	if got := response.Header().Get("Allow"); got != http.MethodPost {
		t.Errorf("Allow = %q, want POST", got)
	}
}
