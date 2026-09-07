//go:build live

package gemini

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nrynss/ajilamu/internal/config"
	"github.com/nrynss/ajilamu/internal/cost"
)

// TestTranslateLive probes the real Vertex AI endpoint with golden segment 8.
// Run it with: go test -tags live ./internal/gemini/
// It never runs in CI and skips only when ADC is unusable.
func TestTranslateLive(t *testing.T) {
	loadDotEnv(t, filepath.Join("..", "..", ".env"))
	os.Unsetenv("GEMINI_API_KEY")

	project := os.Getenv("GOOGLE_CLOUD_PROJECT")
	if project == "" {
		t.Fatal("GOOGLE_CLOUD_PROJECT is unset")
	}

	cfg := &config.Config{
		GeminiModel:         config.DefaultGeminiModel,
		GoogleCloudProject:  project,
		GoogleCloudLocation: config.DefaultGoogleLocation,
	}
	ledger := cost.NewLedger()
	tr, err := NewTranslator(cfg, ledger, cost.DefaultRateCard(), nil)
	if err != nil {
		if isADCUnusable(err) {
			t.Skipf("application default credentials unusable: %v", err)
		}
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
		out, err := tr.Translate(context.Background(), req)
		if err != nil {
			t.Fatalf("live translation (%s) failed: %v", mode, err)
		}
		fmt.Printf("  %s (%s): %q\n", mode, time.Since(started).Round(time.Millisecond), out)
	}
	for _, c := range ledger.Charges() {
		fmt.Printf("  charge: kind=%v take=%d promptTokens=%d candidateTokens=%d total=%v\n",
			c.Kind, c.TakeID, c.PromptTokens, c.CandidateTokens, c.Total())
	}
}
