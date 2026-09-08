package tts

import (
	"errors"
	"fmt"
	"strings"

	"github.com/nrynss/ajilamu/internal/types"
)

// Malayalam is the BCP-47 code the fixture run synthesizes. It is a test
// constant, not a production default. Every caller passes its own language.
const Malayalam = "ml-IN"

// ValidateLanguage reports whether code is a well formed BCP-47 language tag.
// An empty code fails. The check accepts a 2 or 3 letter primary subtag, an
// optional 4 letter script, an optional 2 letter or 3 digit region, and
// optional 5 to 8 character variants. It is stricter than BCP-47 on purpose,
// so a typo such as "spanish" fails before any billable call.
func ValidateLanguage(code string) error {
	code = strings.TrimSpace(code)
	if code == "" {
		return errors.New("tts needs a target language code")
	}
	parts := strings.Split(code, "-")
	if !isPrimarySubtag(parts[0]) {
		return fmt.Errorf("malformed language code %q", code)
	}
	for _, part := range parts[1:] {
		if !isLanguageSubtag(part) {
			return fmt.Errorf("malformed language code %q", code)
		}
	}
	return nil
}

// isPrimarySubtag reports whether part is a 2 or 3 letter language subtag.
func isPrimarySubtag(part string) bool {
	return (len(part) == 2 || len(part) == 3) && isASCIIAlpha(part)
}

// isLanguageSubtag reports whether part is a script, region, or variant.
func isLanguageSubtag(part string) bool {
	switch {
	case len(part) == 4 && isASCIIAlpha(part):
		return true
	case len(part) == 2 && isASCIIAlpha(part):
		return true
	case len(part) == 3 && isASCIIDigit(part):
		return true
	case len(part) == 4 && part[0] >= '0' && part[0] <= '9' && isASCIIAlnum(part):
		return true
	case len(part) >= 5 && len(part) <= 8 && isASCIIAlnum(part):
		return true
	default:
		return false
	}
}

func isASCIIAlpha(part string) bool {
	for i := 0; i < len(part); i++ {
		c := part[i]
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') {
			return false
		}
	}
	return len(part) > 0
}

func isASCIIDigit(part string) bool {
	for i := 0; i < len(part); i++ {
		if part[i] < '0' || part[i] > '9' {
			return false
		}
	}
	return len(part) > 0
}

func isASCIIAlnum(part string) bool {
	for i := 0; i < len(part); i++ {
		c := part[i]
		alpha := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
		digit := c >= '0' && c <= '9'
		if !alpha && !digit {
			return false
		}
	}
	return len(part) > 0
}

// Voice names one Chirp 3 HD speaker for a language.
type Voice struct {
	// Name is the Cloud TTS voice id, such as ml-IN-Chirp3-HD-Achernar.
	Name string
	// LanguageCode is the BCP-47 code sent with the request.
	LanguageCode string
	// Gender is the documented Chirp 3 HD gender of Name.
	Gender string
}

// chirp3HDStar is one documented Chirp 3 HD voice star and its gender.
// Google's table lists Achernar as Female and Achird as Male.
type chirp3HDStar struct {
	star   string
	gender string
}

// knownSpeakers maps fixture speakers onto distinct Chirp 3 HD stars.
// Suni Williams is female, so she keeps Achernar from the Python run.
// Mark Vande Hei is male, so he gets Achird rather than sharing Achernar.
var knownSpeakers = map[string]chirp3HDStar{
	"Suni Williams":  {star: "Achernar", gender: "Female"},
	"Mark Vande Hei": {star: "Achird", gender: "Male"},
}

// Assign maps a speaker onto a distinct Chirp 3 HD voice for language.
// Unknown speakers return an error instead of reusing another voice.
func Assign(speaker types.Speaker, language string) (Voice, error) {
	language = strings.TrimSpace(language)
	if err := ValidateLanguage(language); err != nil {
		return Voice{}, err
	}
	name := strings.TrimSpace(speaker.Name)
	if name == "" {
		return Voice{}, errors.New("tts assign needs a speaker name")
	}
	profile, ok := knownSpeakers[name]
	if !ok {
		return Voice{}, fmt.Errorf("unknown speaker %q", name)
	}
	return Voice{
		Name:         language + "-Chirp3-HD-" + profile.star,
		LanguageCode: language,
		Gender:       profile.gender,
	}, nil
}
