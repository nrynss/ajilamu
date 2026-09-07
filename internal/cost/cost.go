package cost

import (
	"fmt"
	"strings"
)

// ChargeKind identifies the type of API operation billed.
type ChargeKind int

const (
	// ChargeSegment represents Gemini multimodal segmentation.
	ChargeSegment ChargeKind = iota
	// ChargeTranslate represents Gemini translation.
	ChargeTranslate
	// ChargeSynthesize represents Chirp 3 HD TTS.
	ChargeSynthesize
)

// String returns a textual representation of the charge kind.
func (k ChargeKind) String() string {
	switch k {
	case ChargeSegment:
		return "segment"
	case ChargeTranslate:
		return "translate"
	case ChargeSynthesize:
		return "synthesize"
	default:
		return "unknown"
	}
}

// Price represents a cost in nanodollars (billionths of a dollar).
type Price int64

const (
	// Nanodollar is one billionth of a dollar.
	Nanodollar Price = 1
	// Microdollar is one millionth of a dollar.
	Microdollar Price = 1_000
	// Millicent is one thousandth of a cent.
	Millicent Price = 10_000
	// Cent is one hundredth of a dollar.
	Cent Price = 10_000_000
	// Dollar is one dollar.
	Dollar Price = 1_000_000_000
)

// USD converts a dollar amount into a Price in nanodollars.
func USD(dollars float64) Price {
	return Price(dollars * float64(Dollar))
}

// Dollars converts a Price in nanodollars back into a dollar amount.
func (p Price) Dollars() float64 {
	return float64(p) / float64(Dollar)
}

// String formats the Price as a dollar amount. Non-zero costs never display as $0.00.
func (p Price) String() string {
	s := fmt.Sprintf("$%.9f", p.Dollars())
	s = strings.TrimRight(s, "0")
	if strings.HasSuffix(s, ".") {
		s += "00"
	} else if len(s)-strings.Index(s, ".") == 2 {
		s += "0"
	}
	return s
}

// Charge represents a single billed operation.
// Gemini kinds bill from UsageMetadata prompt and candidate token counts.
// Synthesize still bills per character through Units and UnitPrice.
// One Charge is one successful invocation. A take costs its own charges.
type Charge struct {
	// Kind specifies the API operation type.
	Kind ChargeKind
	// TakeID identifies the associated segment or take.
	TakeID int
	// Units counts billed characters for synthesize. Gemini kinds leave this at zero.
	Units int
	// UnitPrice specifies the price per character for synthesize.
	UnitPrice Price
	// PromptTokens is the UsageMetadata prompt token count for Gemini kinds.
	PromptTokens int
	// CandidateTokens is the UsageMetadata candidate token count for Gemini kinds.
	CandidateTokens int
	// PromptUnitPrice is the price per prompt token for Gemini kinds.
	PromptUnitPrice Price
	// CandidateUnitPrice is the price per candidate token for Gemini kinds.
	CandidateUnitPrice Price
}

// Total calculates the overall cost for this charge.
// It sums character units and both Gemini token kinds.
func (c Charge) Total() Price {
	return Price(c.Units)*c.UnitPrice +
		Price(c.PromptTokens)*c.PromptUnitPrice +
		Price(c.CandidateTokens)*c.CandidateUnitPrice
}

// RateCard holds unit pricing for various API operations.
// T2.2dev amends frozen T1.2. Gemini bills on tokens. Synthesize stays per character.
// SegmentPerInputChar and TranslatePerInputChar remain for fixture reconciliation.
type RateCard struct {
	// SegmentPerInputChar is the frozen T1.2 per-character proxy. Gemini no longer bills on it.
	SegmentPerInputChar Price
	// TranslatePerInputChar is the frozen T1.2 per-character proxy. Gemini no longer bills on it.
	TranslatePerInputChar Price
	// SynthesizePerChar sets the price per character for Chirp 3 HD TTS.
	SynthesizePerChar Price
	// SegmentPerPromptToken sets the price per prompt token for Gemini segmentation.
	SegmentPerPromptToken Price
	// SegmentPerCandidateToken sets the price per candidate token for Gemini segmentation.
	SegmentPerCandidateToken Price
	// TranslatePerPromptToken sets the price per prompt token for Gemini translation.
	TranslatePerPromptToken Price
	// TranslatePerCandidateToken sets the price per candidate token for Gemini translation.
	TranslatePerCandidateToken Price
}

// DefaultRateCard provides standard pricing based on current Google Cloud rates.
// Token prices are non-zero. Character Gemini prices stay for T1.5 fixture math.
func DefaultRateCard() RateCard {
	return RateCard{
		SegmentPerInputChar:        100,
		TranslatePerInputChar:      100,
		SynthesizePerChar:          30_000,
		SegmentPerPromptToken:      150,
		SegmentPerCandidateToken:   600,
		TranslatePerPromptToken:    150,
		TranslatePerCandidateToken: 600,
	}
}

// Ledger tracks a list of charges.
type Ledger struct {
	charges []Charge
}

// NewLedger creates and returns a new empty ledger.
func NewLedger() *Ledger {
	return &Ledger{}
}

// Add appends a new charge to the ledger.
func (l *Ledger) Add(c Charge) {
	l.charges = append(l.charges, c)
}

// Charges returns a copy of all recorded charges.
func (l *Ledger) Charges() []Charge {
	c := make([]Charge, len(l.charges))
	copy(c, l.charges)
	return c
}

// Total calculates the sum of all charges in the ledger.
func (l *Ledger) Total() Price {
	var total Price
	for _, c := range l.charges {
		total += c.Total()
	}
	return total
}

// TotalByKind calculates the total cost for a specific kind of charge.
func (l *Ledger) TotalByKind(k ChargeKind) Price {
	var total Price
	for _, c := range l.charges {
		if c.Kind == k {
			total += c.Total()
		}
	}
	return total
}

// TotalForTake calculates the total cost associated with a specific take identifier.
func (l *Ledger) TotalForTake(takeID int) Price {
	var total Price
	for _, c := range l.charges {
		if c.TakeID == takeID {
			total += c.Total()
		}
	}
	return total
}
