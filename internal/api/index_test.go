package api_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/nrynss/ajilamu/internal/api"
)

func TestIndexHandlerOrdersAndCopiesDubs(t *testing.T) {
	dubs := []api.DubSummary{
		{ID: "older", CreatedAt: "2026-09-08T08:00:00Z", Languages: []string{"ml"}},
		{ID: "newer", CreatedAt: "2026-09-08T09:00:00Z", Languages: []string{"ta"}},
	}
	handler := api.IndexHandler(dubs)
	dubs[1].ID = "mutated"
	dubs[1].Languages[0] = "mutated"

	request := httptest.NewRequest(http.MethodGet, "/api/dubs", nil)
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
	var index api.DubIndex
	if err := jsonDecode(response, &index); err != nil {
		t.Fatalf("decode index: %v", err)
	}
	if got := []string{index.Dubs[0].ID, index.Dubs[1].ID}; strings.Join(got, ",") != "newer,older" {
		t.Fatalf("ids = %v, want [newer older]", got)
	}
	if got := index.Dubs[0].Languages[0]; got != "ta" {
		t.Fatalf("copied language = %q, want ta", got)
	}
}

func TestIndexHandlerFromQueriesProviderPerRequest(t *testing.T) {
	store := []api.DubSummary{
		{ID: "first", CreatedAt: "2026-09-08T08:00:00Z", Languages: []string{"ml"}},
	}
	handler := api.IndexHandlerFrom(func() []api.DubSummary {
		return store
	})

	// First fetch returns 1 project.
	req1 := httptest.NewRequest(http.MethodGet, "/api/dubs", nil)
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)
	var idx1 api.DubIndex
	if err := jsonDecode(rec1, &idx1); err != nil {
		t.Fatalf("decode 1: %v", err)
	}
	if len(idx1.Dubs) != 1 || idx1.Dubs[0].ID != "first" {
		t.Fatalf("dubs 1 = %v, want [first]", idx1.Dubs)
	}

	// Add second project to store.
	store = append(store, api.DubSummary{ID: "second", CreatedAt: "2026-09-08T09:00:00Z", Languages: []string{"ta"}})

	// Second fetch returns 2 projects, newest first.
	req2 := httptest.NewRequest(http.MethodGet, "/api/dubs", nil)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	var idx2 api.DubIndex
	if err := jsonDecode(rec2, &idx2); err != nil {
		t.Fatalf("decode 2: %v", err)
	}
	if len(idx2.Dubs) != 2 || idx2.Dubs[0].ID != "second" || idx2.Dubs[1].ID != "first" {
		t.Fatalf("dubs 2 = %v, want [second first]", idx2.Dubs)
	}
}

func TestIndexHandlerRejectsOtherMethods(t *testing.T) {
	response := httptest.NewRecorder()
	api.IndexHandler(nil).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/dubs", nil))

	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusMethodNotAllowed)
	}
	if got := response.Header().Get("Allow"); got != http.MethodGet {
		t.Fatalf("Allow = %q, want GET", got)
	}
}

func TestIndexHandlerUnparseableCreatedAtSortsAsOldest(t *testing.T) {
	dubs := []api.DubSummary{
		{ID: "invalid-date", CreatedAt: "not-a-timestamp"},
		{ID: "valid-date", CreatedAt: "2026-09-08T09:00:00Z"},
	}
	handler := api.IndexHandler(dubs)

	request := httptest.NewRequest(http.MethodGet, "/api/dubs", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	var index api.DubIndex
	if err := jsonDecode(response, &index); err != nil {
		t.Fatalf("decode index: %v", err)
	}
	if len(index.Dubs) != 2 {
		t.Fatalf("len = %d, want 2", len(index.Dubs))
	}
	if got := []string{index.Dubs[0].ID, index.Dubs[1].ID}; strings.Join(got, ",") != "valid-date,invalid-date" {
		t.Fatalf("ids = %v, want [valid-date invalid-date]", got)
	}
}

func TestConfigHandlerAcceptsOnlyExpectedFormAndKeepsSecretsOutOfResponses(t *testing.T) {
	const voiceKey = "voice-secret"
	const translationKey = "translation-secret"
	called := false
	handler := api.ConfigHandler(func(update api.ConfigUpdate) error {
		called = true
		if got, ok := update.VoiceKey(); !ok || got != voiceKey {
			t.Fatalf("voice key = %q, %t", got, ok)
		}
		if got, ok := update.TranslationKey(); !ok || got != translationKey {
			t.Fatalf("translation key = %q, %t", got, ok)
		}
		return nil
	})

	response := serveConfigForm(handler, url.Values{
		"voice_key":       {voiceKey},
		"translation_key": {translationKey},
	})
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
	}
	if !called {
		t.Fatal("save callback was not called")
	}
	if body := response.Body.String(); strings.Contains(body, voiceKey) || strings.Contains(body, translationKey) {
		t.Fatalf("response exposes credential: %q", body)
	}
}

func TestConfigUpdateRedactsCredentialsInLogsAndStringer(t *testing.T) {
	const voiceKey = "SUPER-SECRET-VOICE-KEY-99"
	const translationKey = "SUPER-SECRET-TRANS-KEY-88"

	var capturedUpdate api.ConfigUpdate
	handler := api.ConfigHandler(func(update api.ConfigUpdate) error {
		capturedUpdate = update
		return nil
	})

	response := serveConfigForm(handler, url.Values{
		"voice_key":       {voiceKey},
		"translation_key": {translationKey},
	})
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
	}

	// 1. Check slog output.
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))
	logger.Info("testing update logging", "update", capturedUpdate)
	logOutput := logBuf.String()

	if strings.Contains(logOutput, voiceKey) || strings.Contains(logOutput, translationKey) {
		t.Fatalf("slog output leaked credential: %q", logOutput)
	}
	if !strings.Contains(logOutput, "voice_key_set=true") || !strings.Contains(logOutput, "translation_key_set=true") {
		t.Fatalf("slog output missing expected redacted fields: %q", logOutput)
	}

	// 2. Check fmt.Stringer output.
	fmtOutput := fmt.Sprintf("%v", capturedUpdate)
	if strings.Contains(fmtOutput, voiceKey) || strings.Contains(fmtOutput, translationKey) {
		t.Fatalf("fmt output leaked credential: %q", fmtOutput)
	}
	if capturedStr := capturedUpdate.String(); strings.Contains(capturedStr, voiceKey) || strings.Contains(capturedStr, translationKey) {
		t.Fatalf("String() leaked credential: %q", capturedStr)
	}
}

func TestConfigHandlerAllowsOmittedFields(t *testing.T) {
	const voiceKey = "ONLY-VOICE-KEY"
	const transKey = "ONLY-TRANS-KEY"

	t.Run("omitted translation_key", func(t *testing.T) {
		called := false
		handler := api.ConfigHandler(func(update api.ConfigUpdate) error {
			called = true
			if got, ok := update.VoiceKey(); !ok || got != voiceKey {
				t.Fatalf("voice key = %q, %t", got, ok)
			}
			if got, ok := update.TranslationKey(); ok || got != "" {
				t.Fatalf("translation key = %q, %t", got, ok)
			}
			return nil
		})

		response := serveConfigForm(handler, url.Values{
			"voice_key": {voiceKey},
		})
		if response.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
		}
		if !called {
			t.Fatal("save callback was not called")
		}
	})

	t.Run("omitted voice_key", func(t *testing.T) {
		called := false
		handler := api.ConfigHandler(func(update api.ConfigUpdate) error {
			called = true
			if got, ok := update.VoiceKey(); ok || got != "" {
				t.Fatalf("voice key = %q, %t", got, ok)
			}
			if got, ok := update.TranslationKey(); !ok || got != transKey {
				t.Fatalf("translation key = %q, %t", got, ok)
			}
			return nil
		})

		response := serveConfigForm(handler, url.Values{
			"translation_key": {transKey},
		})
		if response.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
		}
		if !called {
			t.Fatal("save callback was not called")
		}
	})
}

func TestConfigHandlerRejectsBothEmptyFields(t *testing.T) {
	called := false
	handler := api.ConfigHandler(func(api.ConfigUpdate) error {
		called = true
		return nil
	})

	response := serveConfigForm(handler, url.Values{
		"voice_key":       {""},
		"translation_key": {""},
	})
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
	if called {
		t.Fatal("save callback was called")
	}
	if body := response.Body.String(); !strings.Contains(body, "Enter a key before saving.") {
		t.Fatalf("body = %q, want the empty-save sentence", body)
	}
}

func TestConfigHandlerRejectsInvalidFormsBeforeCallback(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		body        string
		want        int
	}{
		{name: "unknown field", contentType: "application/x-www-form-urlencoded", body: "voice_key=voice&translation_key=translation&unexpected=value", want: http.StatusBadRequest},
		{name: "duplicate field", contentType: "application/x-www-form-urlencoded", body: "voice_key=voice&voice_key=again&translation_key=translation", want: http.StatusBadRequest},
		{name: "wrong content type", contentType: "application/json", body: `{}`, want: http.StatusUnsupportedMediaType},
		{name: "too large", contentType: "application/x-www-form-urlencoded", body: "voice_key=" + strings.Repeat("a", 16<<10), want: http.StatusRequestEntityTooLarge},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			called := false
			handler := api.ConfigHandler(func(api.ConfigUpdate) error {
				called = true
				return nil
			})
			request := httptest.NewRequest(http.MethodPost, "/api/config", strings.NewReader(test.body))
			request.Header.Set("Content-Type", test.contentType)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			if response.Code != test.want {
				t.Fatalf("status = %d, want %d", response.Code, test.want)
			}
			if called {
				t.Fatal("save callback was called")
			}
		})
	}
}

func TestConfigHandlerReturns413ForChunkedBodyOverLimit(t *testing.T) {
	handler := api.ConfigHandler(func(api.ConfigUpdate) error {
		return nil
	})

	oversizedBody := "voice_key=" + strings.Repeat("a", 20<<10)
	request := httptest.NewRequest(http.MethodPost, "/api/config", strings.NewReader(oversizedBody))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Transfer-Encoding", "chunked")
	request.ContentLength = -1 // Chunked, no declared length.

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d (413)", response.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestConfigHandlerReturns503WhenCallbackIsNil(t *testing.T) {
	handler := api.ConfigHandler(nil)
	response := serveConfigForm(handler, url.Values{
		"voice_key": {"voice"},
	})
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
}

func TestConfigHandlerUsesSecretFreeCallbackFailure(t *testing.T) {
	const secret = "callback-secret"
	handler := api.ConfigHandler(func(api.ConfigUpdate) error {
		return errors.New(secret)
	})

	response := serveConfigForm(handler, url.Values{
		"voice_key":       {"voice"},
		"translation_key": {"translation"},
	})
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
	if body := response.Body.String(); strings.Contains(body, secret) {
		t.Fatalf("response exposes callback error: %q", body)
	}
}

func serveConfigForm(handler http.Handler, values url.Values) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/api/config", strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func jsonDecode(response *httptest.ResponseRecorder, target any) error {
	return json.NewDecoder(response.Body).Decode(target)
}
