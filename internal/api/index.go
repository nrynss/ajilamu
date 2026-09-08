package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"mime"
	"net/http"
	"sort"
	"time"
)

const maxCredentialRequestBytes int64 = 16 << 10

// IndexHandlerFrom queries provider on each GET request, sorts the returned
// summaries newest project first, and serves DubIndex JSON without caching.
func IndexHandlerFrom(provider func() []DubSummary) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "Method not allowed.", http.StatusMethodNotAllowed)
			return
		}

		var dubs []DubSummary
		if provider != nil {
			dubs = provider()
		}
		index := DubIndex{Dubs: cloneDubs(dubs)}
		sort.SliceStable(index.Dubs, func(i, j int) bool {
			return creationTime(index.Dubs[i]).After(creationTime(index.Dubs[j]))
		})

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if err := json.NewEncoder(w).Encode(index); err != nil {
			return
		}
	})
}

// IndexHandler returns the project index as JSON, newest project first.
// It snapshots its input at construction time for backward compatibility.
func IndexHandler(dubs []DubSummary) http.Handler {
	snapshot := cloneDubs(dubs)
	return IndexHandlerFrom(func() []DubSummary {
		return snapshot
	})
}

// ConfigUpdate holds credential values received from the settings form.
// The credential fields remain unexported so encoding this value cannot expose
// them to a browser or a log that marshals its arguments as JSON.
type ConfigUpdate struct {
	voiceKey       string
	translationKey string
}

// VoiceKey returns a submitted voice-service key. The second result is false
// when the creator left that field empty, which means to preserve its value.
func (u ConfigUpdate) VoiceKey() (string, bool) {
	return u.voiceKey, u.voiceKey != ""
}

// TranslationKey returns a submitted translation-service key. The second
// result is false when the creator left that field empty.
func (u ConfigUpdate) TranslationKey() (string, bool) {
	return u.translationKey, u.translationKey != ""
}

// LogValue implements slog.LogValuer so structured logs do not print credential values.
func (u ConfigUpdate) LogValue() slog.Value {
	return slog.GroupValue(
		slog.Bool("voice_key_set", u.voiceKey != ""),
		slog.Bool("translation_key_set", u.translationKey != ""),
	)
}

// String implements fmt.Stringer so string formatting hides credential values.
func (u ConfigUpdate) String() string {
	return "ConfigUpdate{redacted}"
}

// ConfigHandler accepts the write-only settings form. It never serializes or
// logs a credential, and every response omits submitted values.
//
// Save receives only non-empty fields. It must persist them without returning
// credential text in its error, because this handler deliberately returns a
// generic failure to the browser.
func ConfigHandler(save func(ConfigUpdate) error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "Method not allowed.", http.StatusMethodNotAllowed)
			return
		}
		if save == nil {
			http.Error(w, "Settings are unavailable.", http.StatusServiceUnavailable)
			return
		}

		contentType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || contentType != "application/x-www-form-urlencoded" {
			http.Error(w, "Settings must be submitted by the form.", http.StatusUnsupportedMediaType)
			return
		}
		if r.ContentLength > maxCredentialRequestBytes {
			http.Error(w, "Settings request is too large.", http.StatusRequestEntityTooLarge)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxCredentialRequestBytes)
		if err := r.ParseForm(); err != nil {
			var maxBytesErr *http.MaxBytesError
			if errors.As(err, &maxBytesErr) {
				http.Error(w, "Settings request is too large.", http.StatusRequestEntityTooLarge)
				return
			}
			http.Error(w, "Settings could not be read.", http.StatusBadRequest)
			return
		}
		for name := range r.PostForm {
			if name != "voice_key" && name != "translation_key" {
				http.Error(w, "Settings could not be read.", http.StatusBadRequest)
				return
			}
		}

		voiceKey, ok := singleFormValue(r.PostForm, "voice_key")
		if !ok {
			http.Error(w, "Settings could not be read.", http.StatusBadRequest)
			return
		}
		translationKey, ok := singleFormValue(r.PostForm, "translation_key")
		if !ok {
			http.Error(w, "Settings could not be read.", http.StatusBadRequest)
			return
		}

		update := ConfigUpdate{voiceKey: voiceKey, translationKey: translationKey}
		if _, set := update.VoiceKey(); !set {
			if _, set := update.TranslationKey(); !set {
				http.Error(w, "Enter a key before saving.", http.StatusBadRequest)
				return
			}
		}
		if err := save(update); err != nil {
			http.Error(w, "Settings could not be saved.", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	})
}

func cloneDubs(dubs []DubSummary) []DubSummary {
	cloned := make([]DubSummary, len(dubs))
	for i, dub := range dubs {
		cloned[i] = dub
		cloned[i].Languages = append([]string(nil), dub.Languages...)
	}
	return cloned
}

func creationTime(dub DubSummary) time.Time {
	createdAt, err := time.Parse(time.RFC3339, dub.CreatedAt)
	if err != nil {
		return time.Time{}
	}
	return createdAt
}

func singleFormValue(values map[string][]string, name string) (string, bool) {
	entries, ok := values[name]
	if !ok {
		return "", true
	}
	if len(entries) != 1 {
		return "", false
	}
	return entries[0], true
}
