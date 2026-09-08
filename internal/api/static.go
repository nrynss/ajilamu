package api

import (
	"errors"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// NewStaticHandler serves a static Svelte build and falls back to index.html
// for client-side workspace routes. http.ServeFile preserves byte ranges for
// video and audio assets.
func NewStaticHandler(root string) (http.Handler, error) {
	root = filepath.Clean(strings.TrimSpace(root))
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, errors.New("frontend root is not a directory")
	}
	index := filepath.Join(root, "index.html")
	if info, err := os.Stat(index); err != nil || info.IsDir() {
		return nil, errors.New("frontend root has no index.html")
	}

	files := http.FileServer(http.Dir(root))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cleanPath := path.Clean("/" + r.URL.Path)
		name := strings.TrimPrefix(cleanPath, "/")
		if name == "" {
			http.ServeFile(w, r, index)
			return
		}
		candidate := filepath.Join(root, filepath.FromSlash(name))
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			files.ServeHTTP(w, r)
			return
		}
		if path.Ext(name) == "" {
			http.ServeFile(w, r, index)
			return
		}
		http.NotFound(w, r)
	}), nil
}

// FrontendUnavailable reports a missing static build without preventing the
// API and fixture diagnostics from starting in a fresh clone.
func FrontendUnavailable() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "Frontend build is unavailable. Run npm --prefix web run build.", http.StatusServiceUnavailable)
	})
}
