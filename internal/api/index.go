package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"mime"
	"net/http"
	"sort"
	"time"

	"github.com/nrynss/ajilamu/internal/cost"
)

const maxCredentialRequestBytes int64 = 16 << 10

// Index failure sentences. A read failure never carries internal text.
const indexReadFailed = "Could not read the project ledger."

// IndexFact is the ledger-derived part of one index row. The upload record
// supplies the title, the languages and the timestamps.
type IndexFact struct {
	// Readiness reports the project state, named as the workspace names it.
	Readiness string
	// TotalNanodollars is the exact running sum of every charge.
	TotalNanodollars cost.Price
}

// IndexLedger reads the ledger facts for many projects in one grouped read.
// One call must serve the whole index, so the query count never grows with
// the number of projects.
type IndexLedger interface {
	IndexFacts(ctx context.Context, dubIDs []string) (map[string]IndexFact, error)
}

// ReadinessFacts counts every input the readiness rule reads.
//
// The workspace derives these from the assembled tracks. The index derives
// them from a grouped ledger read. workspaceReadiness and DeriveReadiness
// apply one rule to them, so both routes name a project with one word.
type ReadinessFacts struct {
	// Takes counts every recorded attempt.
	Takes int
	// Rendered counts the lines that hold at least one take.
	Rendered int
	// Flagged reports a line that holds a take and none of them fits.
	Flagged bool
	// SegmentCount counts the first language track's head timeline entries.
	// Only the coverage rule reads it.
	SegmentCount int
	// TrackCount counts the target language tracks.
	TrackCount int
	// Running reports an in-flight run for the project.
	Running bool
}

// DeriveReadiness turns counted facts into the wire readiness word.
//
// It applies the precedence workspaceReadiness implements for the workspace
// route. An in-flight run wins. No take at all means the project waits for its
// first run. A flagged line needs the creator. A segment with no rendered line
// is not finished. Every line fitting and every segment covered means ready.
// TestIndexAndWorkspaceReadinessAgree pins the two rules against each other.
func DeriveReadiness(facts ReadinessFacts) string {
	if facts.Running {
		return ReadinessRunning
	}
	switch {
	case facts.Takes == 0:
		return ReadinessPending
	case facts.Flagged:
		return ReadinessReview
	case facts.SegmentCount > 0 && facts.Rendered < facts.SegmentCount*facts.TrackCount:
		return ReadinessReview
	default:
		return ReadinessReady
	}
}

// IndexHandlerWithLedger serves the dub index and overlays every row with the
// ledger's readiness and running total.
//
// base supplies the rows the upload records describe. ledger answers for every
// row in one grouped call. A nil ledger leaves each row as base built it, so a
// clone with no ClickHouse credentials still serves the fixture. A read
// failure answers 500 rather than reporting a started project as pending.
func IndexHandlerWithLedger(base func() []DubSummary, ledger IndexLedger, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "Method not allowed.", http.StatusMethodNotAllowed)
			return
		}

		var dubs []DubSummary
		if base != nil {
			dubs = base()
		}
		dubs = cloneDubs(dubs)
		if ledger != nil {
			if ids := dubIDs(dubs); len(ids) > 0 {
				facts, err := ledger.IndexFacts(r.Context(), ids)
				if err != nil {
					logIndexFailure(logger, err)
					http.Error(w, indexReadFailed, http.StatusInternalServerError)
					return
				}
				dubs = overlayIndexFacts(dubs, facts)
			}
		}

		index := DubIndex{Dubs: dubs}
		sort.SliceStable(index.Dubs, func(i, j int) bool {
			return creationTime(index.Dubs[i]).After(creationTime(index.Dubs[j]))
		})

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if err := json.NewEncoder(w).Encode(index); err != nil {
			return
		}
	})
}

// IndexHandlerFrom queries provider on each GET request, sorts the returned
// summaries newest project first, and serves DubIndex JSON without caching.
func IndexHandlerFrom(provider func() []DubSummary) http.Handler {
	return IndexHandlerWithLedger(provider, nil, nil)
}

// IndexHandler returns the project index as JSON, newest project first.
// It snapshots its input at construction time for backward compatibility.
func IndexHandler(dubs []DubSummary) http.Handler {
	snapshot := cloneDubs(dubs)
	return IndexHandlerFrom(func() []DubSummary {
		return snapshot
	})
}

// dubIDs lists the distinct project ids one row set names, in row order.
// An empty id names no project, so it never reaches the ledger read.
func dubIDs(dubs []DubSummary) []string {
	ids := make([]string, 0, len(dubs))
	seen := make(map[string]bool, len(dubs))
	for _, dub := range dubs {
		if dub.ID == "" || seen[dub.ID] {
			continue
		}
		seen[dub.ID] = true
		ids = append(ids, dub.ID)
	}
	return ids
}

// overlayIndexFacts replaces the readiness and the running total of every row
// the ledger answered for. A row with no ledger fact keeps its base values, so
// an upload with no ledger rows stays pending and free.
func overlayIndexFacts(dubs []DubSummary, facts map[string]IndexFact) []DubSummary {
	for i := range dubs {
		fact, ok := facts[dubs[i].ID]
		if !ok {
			continue
		}
		dubs[i].Readiness = fact.Readiness
		dubs[i].TotalNanodollars = fact.TotalNanodollars
	}
	return dubs
}

// logIndexFailure records the detail the response hides.
func logIndexFailure(logger *slog.Logger, err error) {
	if logger == nil {
		logger = slog.Default()
	}
	logger.Error("ledger index read failed", "error", err)
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
