//go:build live

package gemini

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/oauth2/google"

	"github.com/nrynss/ajilamu/internal/config"
	"github.com/nrynss/ajilamu/internal/cost"
)

// TestTranslateLive probes the real Vertex AI endpoint with golden segment 8.
// Run it with: go test -tags live ./internal/gemini/
// It never runs in CI and skips without Google Cloud credentials.
func TestTranslateLive(t *testing.T) {
	loadDotEnv(t, filepath.Join("..", "..", ".env"))
	project := os.Getenv("GOOGLE_CLOUD_PROJECT")
	if project == "" {
		t.Skip("GOOGLE_CLOUD_PROJECT is unset")
	}

	ctx := context.Background()
	creds, err := google.FindDefaultCredentials(ctx, "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		t.Skipf("no usable application default credentials: %v", err)
	}

	cfg := &config.Config{
		GeminiModel:         envDefault(t, "GEMINI_MODEL", config.DefaultGeminiModel),
		GoogleCloudProject:  project,
		GoogleCloudLocation: envDefault(t, "GOOGLE_CLOUD_LOCATION", config.DefaultGoogleLocation),
	}
	ledger := cost.NewLedger()
	tr, err := NewTranslator(cfg, creds.TokenSource, ledger, nil, cost.DefaultRateCard())
	if err != nil {
		t.Fatalf("build translator: %v", err)
	}

	req := TranslateRequest{
		SegmentID:  8,
		Text:       "Also, an object in motion tends to stay in motion unless acted on by an outside force.",
		TargetSlot: 7110 * time.Millisecond,
		Emotion:    "Explanatory",
	}

	fmt.Printf("\n--- live translation (model %s, project %s, location %s) ---\n", cfg.GeminiModel, cfg.GoogleCloudProject, cfg.GoogleCloudLocation)
	fmt.Printf("--- golden segment 8: Mark Vande Hei, slot 7110 ms ---\n")
	for _, mode := range []TranslateMode{ModeNormal, ModeShorter, ModeFuller} {
		req.Mode = mode
		started := time.Now()
		out, err := tr.Translate(ctx, req)
		if err != nil {
			// A live failure is evidence. Report it instead of masking it.
			t.Fatalf("live translation (%s) failed: %v", mode, err)
		}
		fmt.Printf("  %s (%s): %q\n", mode, time.Since(started).Round(time.Millisecond), out)
	}
	for _, c := range ledger.Charges() {
		fmt.Printf("  charge: kind=%v take=%d units=%d unitPrice=%v total=%v\n",
			c.Kind, c.TakeID, c.Units, c.UnitPrice, c.Total())
	}
}
