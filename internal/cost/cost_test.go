package cost

import (
	"math"
	"testing"
)

func TestPriceArithmetic(t *testing.T) {
	if USD(1.0) != Dollar {
		t.Errorf("USD(1.0) = %d, want %d", USD(1.0), Dollar)
	}

	want := 30 * Microdollar
	if got := USD(0.00003); got != want {
		t.Errorf("USD(0.00003) = %d, want %d", got, want)
	}

	dollars := USD(1.50).Dollars()
	if math.Abs(dollars-1.50) > 1e-9 {
		t.Errorf("USD(1.50).Dollars() = %v, want 1.50", dollars)
	}

	chirpCost := Price(1000) * USD(30.0) / 1_000_000
	if chirpCost != USD(0.03) {
		t.Errorf("Chirp 1000 chars cost = %d, want %d", chirpCost, USD(0.03))
	}

	geminiCost := Price(1000) * USD(0.10) / 1_000_000
	if geminiCost == 0 {
		t.Error("Gemini 1000 chars cost is zero")
	}
	if want := Price(100_000); geminiCost != want {
		t.Errorf("Gemini 1000 chars cost = %d, want %d", geminiCost, want)
	}
}

func TestPriceString(t *testing.T) {
	tests := []struct {
		name string
		p    Price
		want string
	}{
		{"zero", 0, "$0.00"},
		{"dollar", Dollar, "$1.00"},
		{"fifty cents", 50 * Cent, "$0.50"},
		{"micro-cost", 100_000, "$0.0001"},
		{"nano-cost", 100, "$0.0000001"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.p.String(); got != tt.want {
				t.Errorf("String() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestChargeTotal(t *testing.T) {
	rates := DefaultRateCard()
	c := Charge{
		Units:     1000,
		UnitPrice: rates.SynthesizePerChar,
	}
	got := c.Total()
	want := Price(1000) * rates.SynthesizePerChar
	if got != want {
		t.Errorf("Charge.Total() = %d, want %d", got, want)
	}
}

func TestChargeTotalSumsTokenKinds(t *testing.T) {
	rates := DefaultRateCard()
	c := Charge{
		Kind:               ChargeSegment,
		PromptTokens:       41,
		CandidateTokens:    17,
		PromptUnitPrice:    rates.SegmentPerPromptToken,
		CandidateUnitPrice: rates.SegmentPerCandidateToken,
	}
	got := c.Total()
	want := Price(41)*rates.SegmentPerPromptToken + Price(17)*rates.SegmentPerCandidateToken
	if got != want {
		t.Errorf("Charge.Total() = %d, want %d", got, want)
	}
	if c.Units != 0 {
		t.Errorf("Gemini charge Units = %d, want 0", c.Units)
	}
}

func TestRateCard(t *testing.T) {
	rates := DefaultRateCard()
	if rates.SegmentPerInputChar == 0 {
		t.Error("DefaultRateCard SegmentPerInputChar is zero")
	}
	if rates.TranslatePerInputChar == 0 {
		t.Error("DefaultRateCard TranslatePerInputChar is zero")
	}
	if rates.SynthesizePerChar == 0 {
		t.Error("DefaultRateCard SynthesizePerChar is zero")
	}
	if rates.SegmentPerPromptToken == 0 {
		t.Error("DefaultRateCard SegmentPerPromptToken is zero")
	}
	if rates.SegmentPerCandidateToken == 0 {
		t.Error("DefaultRateCard SegmentPerCandidateToken is zero")
	}
	if rates.TranslatePerPromptToken == 0 {
		t.Error("DefaultRateCard TranslatePerPromptToken is zero")
	}
	if rates.TranslatePerCandidateToken == 0 {
		t.Error("DefaultRateCard TranslatePerCandidateToken is zero")
	}

	customRates := RateCard{
		SegmentPerInputChar: 200,
	}
	c := Charge{Units: 1000, UnitPrice: customRates.SegmentPerInputChar}
	if got, want := c.Total(), Price(200_000); got != want {
		t.Errorf("Custom rate total = %d, want %d", got, want)
	}
}

func TestLedgerSimulateRun(t *testing.T) {
	l := NewLedger()
	rates := DefaultRateCard()

	l.Add(Charge{
		Kind:               ChargeSegment,
		TakeID:             0,
		PromptTokens:       5000,
		CandidateTokens:    800,
		PromptUnitPrice:    rates.SegmentPerPromptToken,
		CandidateUnitPrice: rates.SegmentPerCandidateToken,
	})

	var expectedTotal Price
	expectedTotal += Price(5000)*rates.SegmentPerPromptToken + Price(800)*rates.SegmentPerCandidateToken

	for i := 1; i <= 8; i++ {
		l.Add(Charge{
			Kind:               ChargeTranslate,
			TakeID:             i,
			PromptTokens:       100,
			CandidateTokens:    40,
			PromptUnitPrice:    rates.TranslatePerPromptToken,
			CandidateUnitPrice: rates.TranslatePerCandidateToken,
		})
		expectedTotal += Price(100)*rates.TranslatePerPromptToken + Price(40)*rates.TranslatePerCandidateToken

		l.Add(Charge{Kind: ChargeSynthesize, TakeID: i, Units: 100, UnitPrice: rates.SynthesizePerChar})
		expectedTotal += Price(100) * rates.SynthesizePerChar
	}

	if got := l.Total(); got != expectedTotal {
		t.Errorf("Ledger.Total() = %d, want %d", got, expectedTotal)
	}

	wantSynth := Price(8) * 100 * rates.SynthesizePerChar
	if got := l.TotalByKind(ChargeSynthesize); got != wantSynth {
		t.Errorf("TotalByKind(ChargeSynthesize) = %d, want %d", got, wantSynth)
	}

	wantSeg := Price(5000)*rates.SegmentPerPromptToken + Price(800)*rates.SegmentPerCandidateToken
	if got := l.TotalByKind(ChargeSegment); got != wantSeg {
		t.Errorf("TotalByKind(ChargeSegment) = %d, want %d", got, wantSeg)
	}

	wantTake3 := (Price(100)*rates.TranslatePerPromptToken + Price(40)*rates.TranslatePerCandidateToken) + (Price(100) * rates.SynthesizePerChar)
	if got := l.TotalForTake(3); got != wantTake3 {
		t.Errorf("TotalForTake(3) = %d, want %d", got, wantTake3)
	}
}

func TestLedgerNoDoubleCounting(t *testing.T) {
	l := NewLedger()
	rates := DefaultRateCard()

	c1 := Charge{Kind: ChargeSynthesize, TakeID: 10, Units: 50, UnitPrice: rates.SynthesizePerChar}
	c2 := Charge{Kind: ChargeSynthesize, TakeID: 11, Units: 55, UnitPrice: rates.SynthesizePerChar}

	l.Add(c1)
	l.Add(c2)

	if got := l.Total(); got != c1.Total()+c2.Total() {
		t.Errorf("Total() = %d, want %d", got, c1.Total()+c2.Total())
	}

	if got := l.TotalForTake(10); got != c1.Total() {
		t.Errorf("TotalForTake(10) = %d, want %d", got, c1.Total())
	}
}

// TestChargeAgentRates pins the T6.6 addition to the T1.2 contract.
// An agent turn bills the prompt and candidate tokens its response reported.
func TestChargeAgentRates(t *testing.T) {
	if got := ChargeAgent.String(); got != "agent" {
		t.Fatalf("ChargeAgent.String() = %q, want agent", got)
	}
	rates := DefaultRateCard()
	if rates.AgentPerPromptToken == 0 || rates.AgentPerCandidateToken == 0 {
		t.Fatal("DefaultRateCard agent token rates are zero")
	}
	c := Charge{
		Kind:               ChargeAgent,
		PromptTokens:       1200,
		CandidateTokens:    340,
		PromptUnitPrice:    rates.AgentPerPromptToken,
		CandidateUnitPrice: rates.AgentPerCandidateToken,
	}
	want := Price(1200)*rates.AgentPerPromptToken + Price(340)*rates.AgentPerCandidateToken
	if got := c.Total(); got != want {
		t.Fatalf("ChargeAgent.Total() = %d, want %d", got, want)
	}
	if c.Units != 0 {
		t.Fatalf("agent charge Units = %d, want 0", c.Units)
	}
}
