package tts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nrynss/ajilamu/internal/types"
)

func testdataFile(t *testing.T, rel string) string {
	t.Helper()
	candidates := []string{
		filepath.Join("testdata", rel),
		filepath.Join("..", "..", "testdata", rel),
		filepath.Join("..", "testdata", rel),
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	t.Fatalf("missing testdata/%s", rel)
	return ""
}

func TestAssignFixtureSpeakersGetDistinctDocumentedVoices(t *testing.T) {
	raw, err := os.ReadFile(testdataFile(t, "segments.json"))
	if err != nil {
		t.Fatalf("read segments fixture: %v", err)
	}
	var segments []struct {
		Speaker string `json:"speaker"`
	}
	if err := json.Unmarshal(raw, &segments); err != nil {
		t.Fatalf("decode segments fixture: %v", err)
	}

	unique := make([]types.Speaker, 0, 2)
	seen := map[string]struct{}{}
	for _, seg := range segments {
		if _, ok := seen[seg.Speaker]; ok {
			continue
		}
		seen[seg.Speaker] = struct{}{}
		unique = append(unique, types.Speaker{Name: seg.Speaker})
	}
	if len(unique) != 2 {
		t.Fatalf("unique speakers = %d, want 2", len(unique))
	}

	voices := make(map[string]Voice, len(unique))
	names := make(map[string]string, len(unique))
	for _, speaker := range unique {
		voice, err := Assign(speaker, Malayalam)
		if err != nil {
			t.Fatalf("Assign(%q): %v", speaker.Name, err)
		}
		if voice.LanguageCode != Malayalam {
			t.Errorf("%s language = %q, want %s", speaker.Name, voice.LanguageCode, Malayalam)
		}
		if !strings.HasPrefix(voice.Name, Malayalam+"-Chirp3-HD-") {
			t.Errorf("%s voice %q is not a Chirp 3 HD Malayalam name", speaker.Name, voice.Name)
		}
		if other, clash := names[voice.Name]; clash {
			t.Errorf("speakers %q and %q share voice %q", speaker.Name, other, voice.Name)
		}
		names[voice.Name] = speaker.Name
		voices[speaker.Name] = voice
	}

	suni, ok := voices["Suni Williams"]
	if !ok {
		t.Fatalf("fixture speakers = %v, missing Suni Williams", voices)
	}
	if suni.Name != "ml-IN-Chirp3-HD-Achernar" {
		t.Errorf("Suni Williams voice = %q, want ml-IN-Chirp3-HD-Achernar", suni.Name)
	}
	if suni.Gender != "Female" {
		t.Errorf("Suni Williams gender = %q, want Female", suni.Gender)
	}

	mark, ok := voices["Mark Vande Hei"]
	if !ok {
		t.Fatalf("fixture speakers = %v, missing Mark Vande Hei", voices)
	}
	if mark.Name != "ml-IN-Chirp3-HD-Achird" {
		t.Errorf("Mark Vande Hei voice = %q, want ml-IN-Chirp3-HD-Achird", mark.Name)
	}
	if mark.Gender != "Male" {
		t.Errorf("Mark Vande Hei gender = %q, want Male", mark.Gender)
	}
	if suni.Name == mark.Name {
		t.Fatal("Suni Williams and Mark Vande Hei share a voice")
	}
}

func TestAssignUnknownSpeakerErrors(t *testing.T) {
	_, err := Assign(types.Speaker{Name: "Mark Van der High"}, Malayalam)
	if err == nil {
		t.Fatal("unknown speaker accepted, want error")
	}
	if !strings.Contains(err.Error(), "Mark Van der High") {
		t.Errorf("error = %v, want the unknown speaker name", err)
	}

	suni, err := Assign(types.Speaker{Name: "Suni Williams"}, Malayalam)
	if err != nil {
		t.Fatalf("Suni Williams: %v", err)
	}
	if suni.Name != "ml-IN-Chirp3-HD-Achernar" {
		t.Errorf("unknown speaker path reused or changed Suni's voice: %q", suni.Name)
	}
}

func TestAssignRejectsEmptyInputs(t *testing.T) {
	if _, err := Assign(types.Speaker{Name: "Suni Williams"}, ""); err == nil {
		t.Error("empty language accepted, want error")
	}
	if _, err := Assign(types.Speaker{}, Malayalam); err == nil {
		t.Error("empty speaker accepted, want error")
	}
	if _, err := Assign(types.Speaker{Name: "  "}, Malayalam); err == nil {
		t.Error("blank speaker accepted, want error")
	}
}

func TestValidateLanguageAcceptsWellFormedCodes(t *testing.T) {
	cases := []string{"ml-IN", "es-ES", "en", "de", "pt-BR", "zh-Hans-CN", "es-419", "sr-Latn-RS"}
	for _, code := range cases {
		if err := ValidateLanguage(code); err != nil {
			t.Errorf("ValidateLanguage(%q) = %v, want nil", code, err)
		}
	}
}

func TestValidateLanguageRejectsBadCodes(t *testing.T) {
	cases := []string{"", "   ", "spanish", "es_ES", "e", "es-", "-ES", "es-ESP", "es ES", "123", "es-!!"}
	for _, code := range cases {
		if err := ValidateLanguage(code); err == nil {
			t.Errorf("ValidateLanguage(%q) = nil, want error", code)
		}
	}
}

func TestAssignRejectsMalformedLanguage(t *testing.T) {
	for _, code := range []string{"", "spanish", "es_ES"} {
		if _, err := Assign(types.Speaker{Name: "Suni Williams"}, code); err == nil {
			t.Errorf("Assign language %q accepted, want error", code)
		}
	}
}
