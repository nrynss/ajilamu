package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	testVoiceKey       = "voice-secret-9f3a"
	testTranslationKey = "translation-secret-2b7c"
)

func TestOpenSettingsRejectsEmptyDataDir(t *testing.T) {
	if _, err := OpenSettings(""); err == nil {
		t.Fatal("OpenSettings(\"\") succeeded, want an error")
	}
}

func TestSettingsStorePersistsCredentialsUnderDataDirWithOwnerOnlyModes(t *testing.T) {
	dataDir := t.TempDir()
	store, err := OpenSettings(dataDir)
	if err != nil {
		t.Fatalf("OpenSettings: %v", err)
	}
	if err := store.SaveCredentials(testVoiceKey, testTranslationKey); err != nil {
		t.Fatalf("SaveCredentials: %v", err)
	}

	file := filepath.Join(dataDir, SettingsDirectory, settingsFileName)
	info, err := os.Stat(file)
	if err != nil {
		t.Fatalf("stat stored credential file: %v", err)
	}
	if got := info.Mode().Perm(); got != settingsFileMode {
		t.Fatalf("file mode = %#o, want %#o", got, settingsFileMode)
	}
	if got := info.Mode().Perm() & 0o077; got != 0 {
		t.Fatalf("file mode grants group or world access: %#o", got)
	}
	dirInfo, err := os.Stat(filepath.Dir(file))
	if err != nil {
		t.Fatalf("stat settings directory: %v", err)
	}
	if got := dirInfo.Mode().Perm() & 0o077; got != 0 {
		t.Fatalf("directory mode grants group or world access: %#o", got)
	}
}

func TestSettingsStoreSurvivesReopenAndStillReadsTheValue(t *testing.T) {
	dataDir := t.TempDir()
	first, err := OpenSettings(dataDir)
	if err != nil {
		t.Fatalf("OpenSettings: %v", err)
	}
	if err := first.SaveCredentials(testVoiceKey, testTranslationKey); err != nil {
		t.Fatalf("SaveCredentials: %v", err)
	}

	// A second store stands in for a restarted process. Presence must report
	// both keys, and the store must still recover the exact values.
	reopened, err := OpenSettings(dataDir)
	if err != nil {
		t.Fatalf("reopen settings store: %v", err)
	}
	presence, err := reopened.Presence()
	if err != nil {
		t.Fatalf("Presence: %v", err)
	}
	if !presence.VoiceKey || !presence.TranslationKey {
		t.Fatalf("presence = %+v, want both keys present", presence)
	}
	stored, err := reopened.loadCredentials()
	if err != nil {
		t.Fatalf("loadCredentials after reopen: %v", err)
	}
	if stored.VoiceKey != testVoiceKey {
		t.Fatalf("stored voice key = %q, want the submitted value", stored.VoiceKey)
	}
	if stored.TranslationKey != testTranslationKey {
		t.Fatalf("stored translation key = %q, want the submitted value", stored.TranslationKey)
	}
}

func TestSettingsStorePreservesUnsetCredential(t *testing.T) {
	store, err := OpenSettings(t.TempDir())
	if err != nil {
		t.Fatalf("OpenSettings: %v", err)
	}
	if err := store.SaveCredentials(testVoiceKey, testTranslationKey); err != nil {
		t.Fatalf("SaveCredentials: %v", err)
	}
	const replacement = "translation-secret-replaced"
	if err := store.SaveCredentials("", replacement); err != nil {
		t.Fatalf("SaveCredentials with empty voice key: %v", err)
	}
	stored, err := store.loadCredentials()
	if err != nil {
		t.Fatalf("loadCredentials: %v", err)
	}
	if stored.VoiceKey != testVoiceKey {
		t.Fatalf("voice key = %q, want the preserved value", stored.VoiceKey)
	}
	if stored.TranslationKey != replacement {
		t.Fatalf("translation key = %q, want %q", stored.TranslationKey, replacement)
	}
}

func TestSettingsStorePresenceBeforeAnySave(t *testing.T) {
	dataDir := t.TempDir()
	store, err := OpenSettings(dataDir)
	if err != nil {
		t.Fatalf("OpenSettings: %v", err)
	}
	presence, err := store.Presence()
	if err != nil {
		t.Fatalf("Presence: %v", err)
	}
	if presence.VoiceKey || presence.TranslationKey {
		t.Fatalf("presence = %+v, want both keys absent", presence)
	}
	if _, err := os.Stat(filepath.Join(dataDir, SettingsDirectory, settingsFileName)); !os.IsNotExist(err) {
		t.Fatalf("credential file exists before any save: %v", err)
	}
}

func TestSettingsStoreTightensExistingFilePermissions(t *testing.T) {
	dataDir := t.TempDir()
	dir := filepath.Join(dataDir, SettingsDirectory)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("prepare settings directory: %v", err)
	}
	file := filepath.Join(dir, settingsFileName)
	if err := os.WriteFile(file, []byte(`{"voice_key":"old"}`), 0o644); err != nil {
		t.Fatalf("prepare credential file: %v", err)
	}

	store, err := OpenSettings(dataDir)
	if err != nil {
		t.Fatalf("OpenSettings: %v", err)
	}
	if err := store.SaveCredentials(testVoiceKey, ""); err != nil {
		t.Fatalf("SaveCredentials: %v", err)
	}
	info, err := os.Stat(file)
	if err != nil {
		t.Fatalf("stat credential file: %v", err)
	}
	if got := info.Mode().Perm(); got != settingsFileMode {
		t.Fatalf("file mode = %#o, want %#o", got, settingsFileMode)
	}
	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat settings directory: %v", err)
	}
	if got := dirInfo.Mode().Perm(); got != settingsDirMode {
		t.Fatalf("directory mode = %#o, want %#o", got, settingsDirMode)
	}
}

func TestSettingsPresenceSerializesWithoutCredentialValues(t *testing.T) {
	store, err := OpenSettings(t.TempDir())
	if err != nil {
		t.Fatalf("OpenSettings: %v", err)
	}
	if err := store.SaveCredentials(testVoiceKey, testTranslationKey); err != nil {
		t.Fatalf("SaveCredentials: %v", err)
	}
	presence, err := store.Presence()
	if err != nil {
		t.Fatalf("Presence: %v", err)
	}
	body, err := json.Marshal(presence)
	if err != nil {
		t.Fatalf("marshal presence: %v", err)
	}
	encoded := string(body)
	if strings.Contains(encoded, testVoiceKey) || strings.Contains(encoded, testTranslationKey) {
		t.Fatalf("presence JSON exposes a credential: %s", encoded)
	}
	if !strings.Contains(encoded, `"voice_key_set":true`) || !strings.Contains(encoded, `"translation_key_set":true`) {
		t.Fatalf("presence JSON = %s, want both set flags", encoded)
	}
}

func TestSettingsStorePresenceMapsOneStoredKey(t *testing.T) {
	store, err := OpenSettings(t.TempDir())
	if err != nil {
		t.Fatalf("OpenSettings: %v", err)
	}
	if err := store.SaveCredentials(testVoiceKey, ""); err != nil {
		t.Fatalf("SaveCredentials: %v", err)
	}
	presence, err := store.Presence()
	if err != nil {
		t.Fatalf("Presence: %v", err)
	}
	if !presence.VoiceKey || presence.TranslationKey {
		t.Fatalf("presence = %+v, want the voice key only", presence)
	}
}
