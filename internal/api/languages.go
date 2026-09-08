package api

import (
	"context"
	"encoding/json"
	"net/http"
)

// LanguageCatalogSource supplies the supported-language catalog.
// internal/api names wire types only, so cmd/ajilamu adapts tts.Catalog.
type LanguageCatalogSource interface {
	// Catalog returns the catalog as it stands. It never fails.
	Catalog() LanguageCatalog
	// Refresh fetches the provider list and returns the refreshed catalog.
	// A failure names the reason and leaves the cached catalog untouched.
	Refresh(ctx context.Context) (LanguageCatalog, error)
}

// LanguagesHandler serves GET /api/languages from the cached catalog.
// It serves the committed list before any provider fetch has run.
func LanguagesHandler(source LanguageCatalogSource) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "Method not allowed.", http.StatusMethodNotAllowed)
			return
		}
		if source == nil {
			http.Error(w, "The language catalog is unavailable.", http.StatusServiceUnavailable)
			return
		}
		writeLanguageCatalog(w, source.Catalog())
	})
}

// LanguagesRefreshHandler serves POST /api/languages/refresh.
// A failed fetch answers 502 with the reason and leaves the cache untouched.
func LanguagesRefreshHandler(source LanguageCatalogSource) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "Method not allowed.", http.StatusMethodNotAllowed)
			return
		}
		if source == nil {
			http.Error(w, "The language catalog is unavailable.", http.StatusServiceUnavailable)
			return
		}
		catalog, err := source.Refresh(r.Context())
		if err != nil {
			http.Error(w, "Refresh the language catalog. "+err.Error(), http.StatusBadGateway)
			return
		}
		writeLanguageCatalog(w, catalog)
	})
}

// writeLanguageCatalog answers one catalog payload as JSON.
// The list changes when a refresh lands, so the route stays uncached.
func writeLanguageCatalog(w http.ResponseWriter, catalog LanguageCatalog) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(catalog)
}
