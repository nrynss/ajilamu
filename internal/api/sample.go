package api

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
)

const (
	sampleSourceLanguage = "en"
	sampleLanguage       = "ml"
	sampleTitle          = "NASA 75-second clip"
)

// SampleHandler creates a durable project from the committed NASA clip.
type SampleHandler struct {
	StorageDir string
}

// NewSampleHandler creates the unmounted handler for POST /api/dubs/sample.
func NewSampleHandler(storageDir string) *SampleHandler {
	return &SampleHandler{StorageDir: storageDir}
}

// ServeHTTP copies the sample clip into the same storage used by uploads.
func (h *SampleHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method must be POST", http.StatusMethodNotAllowed)
		return
	}
	if h.StorageDir == "" {
		slog.Error("Sample storage directory is not configured.")
		http.Error(w, "The sample is unavailable because upload storage is not configured.", http.StatusInternalServerError)
		return
	}

	source, err := openSampleVideo()
	if err != nil {
		slog.Error("The sample video could not be opened.", "error", err)
		http.Error(w, "We could not find the sample video. Upload a video instead.", http.StatusInternalServerError)
		return
	}
	defer source.Close()

	if err := os.MkdirAll(h.StorageDir, 0o750); err != nil {
		slog.Error("Could not prepare sample storage directory.", "dir", h.StorageDir, "error", err)
		http.Error(w, "We could not prepare storage for the sample. Try again.", http.StatusInternalServerError)
		return
	}
	id, err := uploadID()
	if err != nil {
		slog.Error("Could not generate sample upload id.", "error", err)
		http.Error(w, "We could not start the sample project. Try again.", http.StatusInternalServerError)
		return
	}
	staging, err := os.MkdirTemp(h.StorageDir, ".sample-"+id+"-")
	if err != nil {
		slog.Error("Could not create sample staging directory.", "dir", h.StorageDir, "error", err)
		http.Error(w, "We could not prepare the sample workspace. Try again.", http.StatusInternalServerError)
		return
	}
	defer func() {
		if staging != "" {
			_ = os.RemoveAll(staging)
		}
	}()

	name := sampleTitle
	videoPath := filepath.Join(staging, "source.mp4")
	bytes, err := copySampleVideo(videoPath, source)
	if err != nil {
		slog.Error("Could not save sample video.", "path", videoPath, "error", err)
		http.Error(w, "We could not save the sample video. Try again.", http.StatusInternalServerError)
		return
	}
	upload := Upload{
		ID:             id,
		SourceLanguage: sampleSourceLanguage,
		Language:       sampleLanguage,
		Video: UploadFile{
			Name:  name,
			Path:  videoPath,
			Bytes: bytes,
		},
	}
	if err := writeUploadRecord(staging, upload); err != nil {
		slog.Error("Could not write sample upload record.", "staging", staging, "error", err)
		http.Error(w, "We could not save the sample project. Try again.", http.StatusInternalServerError)
		return
	}

	if err := os.Rename(staging, filepath.Join(h.StorageDir, id)); err != nil {
		slog.Error("Could not move sample staging directory to final location.", "staging", staging, "destination", filepath.Join(h.StorageDir, id), "error", err)
		http.Error(w, "We could not finish the sample project. Try again.", http.StatusInternalServerError)
		return
	}
	staging = ""
	upload.Video.Path = filepath.ToSlash(filepath.Join(id, filepath.Base(videoPath)))

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(upload)
}

func openSampleVideo() (*os.File, error) {
	if override := os.Getenv("AJILAMU_SAMPLE_CLIP"); override != "" {
		file, err := os.Open(override)
		if err != nil {
			return nil, fmt.Errorf("open override AJILAMU_SAMPLE_CLIP=%s: %w", override, err)
		}
		return file, nil
	}
	candidates := []string{
		"clip.mp4",
		filepath.Join("testdata", "clip.mp4"),
		filepath.Join("..", "clip.mp4"),
		filepath.Join("..", "testdata", "clip.mp4"),
		filepath.Join("..", "..", "clip.mp4"),
		filepath.Join("..", "..", "testdata", "clip.mp4"),
	}
	for _, candidate := range candidates {
		file, err := os.Open(candidate)
		if err == nil {
			return file, nil
		}
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("open candidate %s: %w", candidate, err)
		}
	}
	return nil, fmt.Errorf("clip not found in searched locations %v (AJILAMU_SAMPLE_CLIP is unset): %w", candidates, os.ErrNotExist)
}

func copySampleVideo(destination string, source io.Reader) (int64, error) {
	file, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return 0, fmt.Errorf("create sample: %w", err)
	}
	bytes, copyErr := io.CopyBuffer(file, source, make([]byte, uploadBufferSize))
	closeErr := file.Close()
	if copyErr != nil {
		os.Remove(destination)
		return 0, fmt.Errorf("copy sample: %w", copyErr)
	}
	if closeErr != nil {
		os.Remove(destination)
		return 0, fmt.Errorf("close sample: %w", closeErr)
	}
	return bytes, nil
}
