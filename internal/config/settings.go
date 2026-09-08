package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

// SettingsDirectory is the application data subdirectory that holds local
// credentials. It reuses the T7.0 AJILAMU_DATA_DIR contract, so a deployment
// keeps the file outside the repository and outside every image layer.
const SettingsDirectory = "settings"

// settingsFileName names the credential file inside SettingsDirectory.
const settingsFileName = "credentials.json"

// Credential files and their directory stay owner-only. Group and world get no
// access, so another local user cannot read a stored key.
const (
	settingsDirMode  os.FileMode = 0o700
	settingsFileMode os.FileMode = 0o600
)

// SettingsPresence reports which local credentials exist. It never carries a
// credential value, so it is safe to serialize to a browser.
type SettingsPresence struct {
	VoiceKey       bool `json:"voice_key_set"`
	TranslationKey bool `json:"translation_key_set"`
}

// SettingsStore persists write-only credentials under the application data
// directory. No exported method returns a stored value.
type SettingsStore struct {
	dir  string
	file string
	mu   sync.Mutex
}

// storedSettings is the on-disk shape. It stays unexported, so no caller can
// marshal a credential out of the store.
type storedSettings struct {
	VoiceKey       string `json:"voice_key,omitempty"`
	TranslationKey string `json:"translation_key,omitempty"`
}

// OpenSettings opens the credential store under dataDir. An empty directory is
// an error, because the store never invents a second location and never
// defaults one.
func OpenSettings(dataDir string) (*SettingsStore, error) {
	if dataDir == "" {
		return nil, errors.New("settings data directory is empty")
	}
	dir := filepath.Join(dataDir, SettingsDirectory)
	if err := os.MkdirAll(dir, settingsDirMode); err != nil {
		return nil, fmt.Errorf("create settings directory: %w", err)
	}
	// MkdirAll leaves the mode of an existing directory untouched, so enforce
	// owner-only access on every open.
	if err := os.Chmod(dir, settingsDirMode); err != nil {
		return nil, fmt.Errorf("restrict settings directory: %w", err)
	}
	return &SettingsStore{dir: dir, file: filepath.Join(dir, settingsFileName)}, nil
}

// SaveCredentials stores the non-empty values. An empty value leaves the
// stored credential unchanged, so a form that omits a field preserves it.
// Errors name the path only and never carry a credential value.
func (s *SettingsStore) SaveCredentials(voiceKey, translationKey string) error {
	if s == nil {
		return errors.New("settings store is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	stored, err := s.loadCredentials()
	if err != nil {
		return err
	}
	if voiceKey != "" {
		stored.VoiceKey = voiceKey
	}
	if translationKey != "" {
		stored.TranslationKey = translationKey
	}
	body, err := json.Marshal(stored)
	if err != nil {
		return fmt.Errorf("encode settings: %w", err)
	}
	return s.writeCredentials(body)
}

// Presence reports which credentials are stored. It reads the file but never
// returns a credential value.
func (s *SettingsStore) Presence() (SettingsPresence, error) {
	if s == nil {
		return SettingsPresence{}, errors.New("settings store is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	stored, err := s.loadCredentials()
	if err != nil {
		return SettingsPresence{}, err
	}
	return SettingsPresence{
		VoiceKey:       stored.VoiceKey != "",
		TranslationKey: stored.TranslationKey != "",
	}, nil
}

// loadCredentials reads the stored shape. A missing file is an empty store,
// not an error, so a first save and a presence read before any save both work.
func (s *SettingsStore) loadCredentials() (storedSettings, error) {
	var stored storedSettings
	body, err := os.ReadFile(s.file)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return stored, nil
		}
		return stored, fmt.Errorf("read settings file: %w", err)
	}
	if err := json.Unmarshal(body, &stored); err != nil {
		return stored, fmt.Errorf("decode settings file: %w", err)
	}
	return stored, nil
}

// writeCredentials replaces the file atomically at mode 0600. The temp file is
// synced before the rename and the directory after it, so a restart sees either
// the old file or the new one.
func (s *SettingsStore) writeCredentials(body []byte) error {
	temp, err := os.CreateTemp(s.dir, ".credentials-*")
	if err != nil {
		return fmt.Errorf("create settings file: %w", err)
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if err := temp.Chmod(settingsFileMode); err != nil {
		temp.Close()
		return fmt.Errorf("restrict settings file: %w", err)
	}
	if _, err := temp.Write(body); err != nil {
		temp.Close()
		return fmt.Errorf("write settings file: %w", err)
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return fmt.Errorf("sync settings file: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close settings file: %w", err)
	}
	if err := os.Rename(tempName, s.file); err != nil {
		return fmt.Errorf("replace settings file: %w", err)
	}
	// The rename preserves the temp file mode, but enforce it in case a
	// pre-existing file carried a wider mode.
	if err := os.Chmod(s.file, settingsFileMode); err != nil {
		return fmt.Errorf("restrict settings file: %w", err)
	}
	dir, err := os.Open(s.dir)
	if err != nil {
		return fmt.Errorf("open settings directory: %w", err)
	}
	defer dir.Close()
	if err := dir.Sync(); err != nil && !errors.Is(err, fs.ErrInvalid) {
		return fmt.Errorf("sync settings directory: %w", err)
	}
	return nil
}
