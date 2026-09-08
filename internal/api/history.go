package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
)

// HistoryHandlerFrom serves a dub's commit DAG.
// The mux owns the method guard, so this handler answers GET only.
func HistoryHandlerFrom(reader HistoryReader, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if reader == nil {
			writeHistoryFailure(w, http.StatusServiceUnavailable, historyUnavailable)
			return
		}
		commits, err := reader.ListCommits(r.Context(), r.PathValue("id"))
		if err != nil {
			logHistoryFailure(logger, "list commits", err)
			writeHistoryFailure(w, http.StatusInternalServerError, historyReadFailed)
			return
		}
		if commits == nil {
			commits = []Commit{}
		}
		writeHistoryJSON(w, http.StatusOK, DubHistory{Commits: commits})
	})
}

// TimelineHandlerFrom serves one commit's timeline for one language.
// It rejects a blank language or commit before it reads the ledger.
func TimelineHandlerFrom(reader HistoryReader, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if reader == nil {
			writeHistoryFailure(w, http.StatusServiceUnavailable, historyUnavailable)
			return
		}
		language, problem := requiredHistoryQuery(r, "language")
		if problem != "" {
			writeHistoryFailure(w, http.StatusBadRequest, problem)
			return
		}
		commit, problem := requiredHistoryQuery(r, "commit")
		if problem != "" {
			writeHistoryFailure(w, http.StatusBadRequest, problem)
			return
		}
		segments, err := reader.TimelineAt(r.Context(), r.PathValue("id"), language, commit)
		if err != nil {
			logHistoryFailure(logger, "read timeline", err)
			writeHistoryFailure(w, http.StatusInternalServerError, historyReadFailed)
			return
		}
		if segments == nil {
			segments = []TimelineEntry{}
		}
		writeHistoryJSON(w, http.StatusOK, TimelineView{CommitID: commit, Language: language, Segments: segments})
	})
}

// BranchCompareHandlerFrom serves metrics for two heads of one language track.
// It rejects a blank language, a, or b before it reads the ledger.
func BranchCompareHandlerFrom(reader HistoryReader, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if reader == nil {
			writeHistoryFailure(w, http.StatusServiceUnavailable, historyUnavailable)
			return
		}
		language, problem := requiredHistoryQuery(r, "language")
		if problem != "" {
			writeHistoryFailure(w, http.StatusBadRequest, problem)
			return
		}
		a, problem := requiredHistoryQuery(r, "a")
		if problem != "" {
			writeHistoryFailure(w, http.StatusBadRequest, problem)
			return
		}
		b, problem := requiredHistoryQuery(r, "b")
		if problem != "" {
			writeHistoryFailure(w, http.StatusBadRequest, problem)
			return
		}
		comparison, err := reader.CompareBranches(r.Context(), r.PathValue("id"), language, a, b)
		if err != nil {
			logHistoryFailure(logger, "compare branches", err)
			writeHistoryFailure(w, http.StatusInternalServerError, historyReadFailed)
			return
		}
		writeHistoryJSON(w, http.StatusOK, comparison)
	})
}

// History failure sentences. A read failure never carries the internal text.
const (
	historyUnavailable = "Ledger history is unavailable."
	historyReadFailed  = "Could not read the ledger history."
)

// historyError is the JSON body every failed history route returns.
type historyError struct {
	// Error is a sentence the browser can show.
	Error string `json:"error"`
}

// writeHistoryJSON writes body as JSON with the headers every route shares.
func writeHistoryJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// writeHistoryFailure answers one route with a sentence and no internal detail.
func writeHistoryFailure(w http.ResponseWriter, status int, sentence string) {
	writeHistoryJSON(w, status, historyError{Error: sentence})
}

// logHistoryFailure records the detail the response hides.
func logHistoryFailure(logger *slog.Logger, operation string, err error) {
	if logger == nil {
		logger = slog.Default()
	}
	logger.Error("ledger history read failed", "operation", operation, "error", err)
}

// requiredHistoryQuery reads one query parameter and rejects a blank value.
// The second result is the 400 sentence naming the parameter when it is blank.
func requiredHistoryQuery(r *http.Request, name string) (string, string) {
	value := strings.TrimSpace(r.URL.Query().Get(name))
	if value == "" {
		return "", "The " + name + " query parameter is required."
	}
	return value, ""
}
