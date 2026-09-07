package tts

import (
	"errors"
	"fmt"
	"strings"

	"github.com/nrynss/ajilamu/internal/types"
)

// Malayalam is the BCP-47 language code this pipeline synthesizes.
const Malayalam = "ml-IN"

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
	if language == "" {
		return Voice{}, errors.New("tts assign needs a language code")
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
