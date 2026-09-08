package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// MaxUploadBytes limits one multipart request while leaving room for long creator videos.
	MaxUploadBytes   int64 = 20 << 30
	uploadBufferSize       = 128 << 10
)

// UploadFile describes one stored creator asset without exposing its disk path.
type UploadFile struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	Bytes int64  `json:"bytes"`
}

// Upload is the durable project input created by an upload request.
type Upload struct {
	ID string `json:"id"`
	// SourceLanguage is the film language code. Empty means unknown, which
	// an upload record written before T7.5b carries.
	SourceLanguage string      `json:"source_language"`
	Language       string      `json:"language"`
	Video          UploadFile  `json:"video"`
	Music          *UploadFile `json:"music,omitempty"`
}

// UploadHandler streams multipart project inputs into StorageDir.
// StorageDir must be a persistent volume supplied by the server entrypoint.
type UploadHandler struct {
	StorageDir string
	MaxBytes   int64
}

// NewUploadHandler creates the unmounted handler for POST /api/dubs/new.
func NewUploadHandler(storageDir string) *UploadHandler {
	return &UploadHandler{StorageDir: storageDir, MaxBytes: MaxUploadBytes}
}

// ServeHTTP accepts a required video, optional music, a target language, and
// an optional source language.
func (h *UploadHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method must be POST", http.StatusMethodNotAllowed)
		return
	}
	if h.StorageDir == "" {
		http.Error(w, "upload storage is not configured", http.StatusInternalServerError)
		return
	}
	if !isMultipart(r.Header.Get("Content-Type")) {
		http.Error(w, "request must use multipart/form-data", http.StatusUnsupportedMediaType)
		return
	}

	limit := h.MaxBytes
	if limit <= 0 {
		limit = MaxUploadBytes
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	reader, err := r.MultipartReader()
	if err != nil {
		http.Error(w, "invalid multipart request", http.StatusBadRequest)
		return
	}
	if err := os.MkdirAll(h.StorageDir, 0o750); err != nil {
		http.Error(w, "create upload storage", http.StatusInternalServerError)
		return
	}

	id, err := uploadID()
	if err != nil {
		http.Error(w, "create project id", http.StatusInternalServerError)
		return
	}
	staging, err := os.MkdirTemp(h.StorageDir, ".upload-"+id+"-")
	if err != nil {
		http.Error(w, "create upload workspace", http.StatusInternalServerError)
		return
	}
	defer os.RemoveAll(staging)

	var upload Upload
	upload.ID = id
	for {
		part, nextErr := reader.NextPart()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			h.writeUploadError(w, nextErr)
			return
		}

		name := part.FormName()
		if part.FileName() == "" {
			value, readErr := readUploadField(part)
			part.Close()
			if readErr != nil {
				h.writeUploadError(w, readErr)
				return
			}
			switch name {
			case "language", "languages":
				upload.Language = value
			case "source_language", "source_languages":
				upload.SourceLanguage = value
			}
			continue
		}

		switch name {
		case "video":
			if upload.Video.Path != "" {
				part.Close()
				http.Error(w, "one video file is required", http.StatusBadRequest)
				return
			}
			file, writeErr := streamUploadPart(staging, "source", part)
			part.Close()
			if writeErr != nil {
				h.writeUploadError(w, writeErr)
				return
			}
			upload.Video = file
		case "music":
			if upload.Music != nil {
				part.Close()
				http.Error(w, "only one music track is allowed", http.StatusBadRequest)
				return
			}
			file, writeErr := streamUploadPart(staging, "music", part)
			part.Close()
			if writeErr != nil {
				h.writeUploadError(w, writeErr)
				return
			}
			upload.Music = &file
		default:
			part.Close()
			http.Error(w, "unknown upload field", http.StatusBadRequest)
			return
		}
	}
	if upload.Video.Path == "" {
		http.Error(w, "a video file is required", http.StatusBadRequest)
		return
	}
	if upload.Language == "" {
		http.Error(w, "a target language is required", http.StatusBadRequest)
		return
	}
	if err := writeUploadRecord(staging, upload); err != nil {
		http.Error(w, "finalize upload", http.StatusInternalServerError)
		return
	}

	projectDir := filepath.Join(h.StorageDir, id)
	if err := os.Rename(staging, projectDir); err != nil {
		http.Error(w, "finalize upload", http.StatusInternalServerError)
		return
	}
	staging = ""
	upload.Video.Path = filepath.ToSlash(filepath.Join(id, filepath.Base(upload.Video.Path)))
	if upload.Music != nil {
		upload.Music.Path = filepath.ToSlash(filepath.Join(id, filepath.Base(upload.Music.Path)))
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(upload); err != nil {
		return
	}
}

func (h *UploadHandler) writeUploadError(w http.ResponseWriter, err error) {
	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) {
		http.Error(w, "upload exceeds the size limit", http.StatusRequestEntityTooLarge)
		return
	}
	http.Error(w, "store upload", http.StatusBadRequest)
}

func isMultipart(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	return err == nil && mediaType == "multipart/form-data"
}

func readUploadField(part io.Reader) (string, error) {
	const maxFieldBytes = 8 << 10
	data, err := io.ReadAll(io.LimitReader(part, maxFieldBytes+1))
	if err != nil {
		return "", fmt.Errorf("read field: %w", err)
	}
	if len(data) > maxFieldBytes {
		return "", errors.New("multipart field is too large")
	}
	return strings.TrimSpace(string(data)), nil
}

func streamUploadPart(dir, stem string, part interface {
	io.Reader
	FileName() string
}) (UploadFile, error) {
	extension := safeExtension(part.FileName())
	path := filepath.Join(dir, stem+extension)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return UploadFile{}, fmt.Errorf("create upload: %w", err)
	}
	bytes, copyErr := io.CopyBuffer(file, part, make([]byte, uploadBufferSize))
	closeErr := file.Close()
	if copyErr != nil {
		os.Remove(path)
		return UploadFile{}, fmt.Errorf("stream upload: %w", copyErr)
	}
	if closeErr != nil {
		os.Remove(path)
		return UploadFile{}, fmt.Errorf("close upload: %w", closeErr)
	}
	return UploadFile{Name: filepath.Base(part.FileName()), Path: path, Bytes: bytes}, nil
}

func safeExtension(filename string) string {
	extension := strings.ToLower(filepath.Ext(filepath.Base(filename)))
	if len(extension) < 2 || len(extension) > 12 {
		return ".bin"
	}
	for _, char := range extension[1:] {
		if (char < 'a' || char > 'z') && (char < '0' || char > '9') {
			return ".bin"
		}
	}
	return extension
}

const uploadRecordName = "project.json"

type uploadRecord struct {
	ID             string `json:"id"`
	Title          string `json:"title"`
	SourceLanguage string `json:"source_language"`
	Language       string `json:"language"`
	CreatedAt      string `json:"created_at"`
}

func writeUploadRecord(dir string, upload Upload) error {
	record := uploadRecord{
		ID:             upload.ID,
		Title:          upload.Video.Name,
		SourceLanguage: upload.SourceLanguage,
		Language:       upload.Language,
		CreatedAt:      time.Now().UTC().Format(time.RFC3339),
	}
	payload, err := json.Marshal(record)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, uploadRecordName), payload, 0o600)
}

// ListUploadSummaries reads persisted upload records from StorageDir.
// It skips staging directories and unreadable entries so a partial write
// cannot hide the rest of the index.
func ListUploadSummaries(storageDir string) []DubSummary {
	if storageDir == "" {
		return nil
	}
	entries, err := os.ReadDir(storageDir)
	if err != nil {
		return nil
	}
	summaries := make([]DubSummary, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		payload, err := os.ReadFile(filepath.Join(storageDir, entry.Name(), uploadRecordName))
		if err != nil {
			continue
		}
		var record uploadRecord
		if err := json.Unmarshal(payload, &record); err != nil || record.ID == "" {
			continue
		}
		summaries = append(summaries, DubSummary{
			ID:        record.ID,
			Title:     record.Title,
			Languages: []string{record.Language},
			Readiness: ReadinessPending,
			CreatedAt: record.CreatedAt,
			UpdatedAt: record.CreatedAt,
		})
	}
	return summaries
}

func uploadID() (string, error) {
	data := make([]byte, 16)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return hex.EncodeToString(data), nil
}
