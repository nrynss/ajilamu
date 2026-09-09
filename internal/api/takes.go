package api

import (
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// TakeAudioHandler serves GET /api/dubs/{id}/takes/{language}/{name}.
//
// A ledger payload stores an absolute take path, which the browser cannot use.
// This route streams the take from the project work directory instead, so the
// page names a project, a language, and a file name only.
//
// http.ServeContent answers a Range request with 206 and Content-Range, so a
// scrub seeks instead of downloading the whole take again. The file name is
// reduced to one path segment, and the handler opens the file under the
// storage root. A symlink that points outside the root fails, and a traversal
// by name cannot leave it.
func TakeAudioHandler(storageDir string, logger *slog.Logger) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	root := ""
	if storageDir != "" {
		absolute, err := filepath.Abs(storageDir)
		if err != nil {
			logger.Error("take storage root is unusable", "error", err)
		} else {
			root = absolute
		}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if root == "" {
			http.Error(w, "Take audio is unavailable because storage is not configured.", http.StatusServiceUnavailable)
			return
		}
		dubID := r.PathValue("id")
		language := r.PathValue("language")
		name := r.PathValue("name")
		if !safeDubID(dubID) || !safePathSegment(language) || !safePathSegment(name) {
			http.NotFound(w, r)
			return
		}
		target := filepath.Join(runWorkDirFor(root, dubID, language), name)
		relative, inside := pathInside(root, target)
		if !inside {
			http.NotFound(w, r)
			return
		}
		file, err := os.OpenInRoot(root, relative)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil || info.IsDir() {
			http.NotFound(w, r)
			return
		}
		http.ServeContent(w, r, name, info.ModTime(), file)
	})
}

// safePathSegment reports whether a value names exactly one path segment.
// A separator, a parent reference, or an empty value would escape the
// directory it is joined onto.
func safePathSegment(value string) bool {
	if value == "" || value == "." || value == ".." {
		return false
	}
	return !strings.ContainsAny(value, `/\`)
}

// pathInside returns the path of target relative to root.
// It reports false when target leaves root, so a traversal by name fails.
func pathInside(root, target string) (string, bool) {
	relative, err := filepath.Rel(root, target)
	if err != nil {
		return "", false
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", false
	}
	return relative, true
}
