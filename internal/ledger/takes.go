package ledger

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/nrynss/ajilamu/internal/cost"
	"github.com/nrynss/ajilamu/internal/types"
)

const takeInsert = "INSERT INTO takes_raw (take_id, commit_id, project_id, dub_id, owner_id, language, segment_index, attempt, speaker, voice, text, slot_start_ms, slot_ms, measured_ms, delta_ms, repair, repair_detail, audio_path, peaks) FORMAT JSONEachRow"

// TakeAttempt contains the provenance of one rendered take and its own API charges.
// Charges never include a project running total.
type TakeAttempt struct {
	TakeID    string
	CommitID  string
	ProjectID string
	DubID     string
	OwnerID   string
	Language  string
	Voice     string
	// Text is the target-language line the take speaks.
	Text           string
	ChargeProvider string
	RepairDetail   string
	Repair         types.Repair
	Segment        types.Segment
	Take           types.Take
	Charges        []cost.Charge
	Peaks          []uint8
}

type takeRow struct {
	TakeID       string    `json:"take_id"`
	CommitID     string    `json:"commit_id"`
	ProjectID    string    `json:"project_id"`
	DubID        string    `json:"dub_id"`
	OwnerID      string    `json:"owner_id"`
	Language     string    `json:"language"`
	SegmentIndex int32     `json:"segment_index"`
	Attempt      uint8     `json:"attempt"`
	Speaker      string    `json:"speaker"`
	Voice        string    `json:"voice"`
	Text         string    `json:"text"`
	SlotStartMs  int64     `json:"slot_start_ms"`
	SlotMs       int32     `json:"slot_ms"`
	MeasuredMs   int32     `json:"measured_ms"`
	DeltaMs      int32     `json:"delta_ms"`
	Repair       string    `json:"repair"`
	RepairDetail string    `json:"repair_detail"`
	AudioPath    string    `json:"audio_path"`
	Peaks        takePeaks `json:"peaks"`
}

// takePeaks marshals unsigned waveform samples as a JSON number array.
// encoding/json would otherwise encode []uint8 as a base64 string.
type takePeaks []uint8

func (values takePeaks) MarshalJSON() ([]byte, error) {
	numbers := make([]int, len(values))
	for i, value := range values {
		numbers[i] = int(value)
	}
	return json.Marshal(numbers)
}

type chargeRow struct {
	TakeID       string `json:"take_id"`
	CommitID     string `json:"commit_id"`
	ProjectID    string `json:"project_id"`
	DubID        string `json:"dub_id"`
	OwnerID      string `json:"owner_id"`
	Language     string `json:"language"`
	SegmentIndex int32  `json:"segment_index"`
	Attempt      uint8  `json:"attempt"`
	TurnID       string `json:"turn_id"`
	CallIndex    uint16 `json:"call_index"`
	Kind         string `json:"kind"`
	Provider     string `json:"provider"`
	Unit         string `json:"unit"`
	Units        string `json:"units"`
	UnitPriceUSD string `json:"unit_price_usd"`
}

// RecordTake persists one take attempt and the individual calls that paid for it.
// It journals the take and every charge row before the first Flush.
// A failed send remains in the durable queue and returns ErrPending.
func (c *Client) RecordTake(ctx context.Context, attempt TakeAttempt) error {
	take, charges, err := attempt.rows()
	if err != nil {
		return err
	}
	if err := c.journalJSON(takeInsert, take); err != nil {
		return fmt.Errorf("enqueue take row: %w", err)
	}
	for _, charge := range charges {
		if err := c.journalJSON(chargeInsert, charge); err != nil {
			return fmt.Errorf("enqueue take charge: %w", err)
		}
	}
	return c.Flush(ctx)
}

// journalJSON stores one JSONEachRow payload without flushing.
func (c *Client) journalJSON(insert string, row any) error {
	if err := validateInsert(insert); err != nil {
		return err
	}
	body, err := json.Marshal(row)
	if err != nil {
		return fmt.Errorf("encode ledger row: %w", err)
	}
	body = append(body, '\n')
	return c.queue.Enqueue(Entry{Query: insert, Body: body})
}

func (a TakeAttempt) rows() (takeRow, []chargeRow, error) {
	if strings.TrimSpace(a.TakeID) == "" || strings.TrimSpace(a.CommitID) == "" ||
		strings.TrimSpace(a.ProjectID) == "" || strings.TrimSpace(a.DubID) == "" ||
		strings.TrimSpace(a.OwnerID) == "" || strings.TrimSpace(a.Language) == "" ||
		strings.TrimSpace(a.Voice) == "" || strings.TrimSpace(a.Text) == "" {
		return takeRow{}, nil, errors.New("take row has an empty identity, voice or text field")
	}
	if a.Segment.ID < 0 || a.Take.SegmentID != a.Segment.ID {
		return takeRow{}, nil, errors.New("take segment does not match its provenance segment")
	}
	if a.Take.Attempt < 1 || a.Take.Attempt > math.MaxUint8 {
		return takeRow{}, nil, fmt.Errorf("take attempt = %d, want 1 through %d", a.Take.Attempt, math.MaxUint8)
	}
	if strings.TrimSpace(a.Take.File) == "" {
		return takeRow{}, nil, errors.New("take audio path is empty")
	}
	if len(a.Peaks) != 0 && (len(a.Peaks) < 64 || len(a.Peaks) > 128) {
		return takeRow{}, nil, fmt.Errorf("take peaks have length %d, want empty or 64 through 128", len(a.Peaks))
	}
	if a.Take.Fit.Slot != a.Segment.SlotDuration() {
		return takeRow{}, nil, errors.New("take target slot does not match its provenance segment")
	}
	if a.Take.Fit.Delta != a.Take.Fit.Measured-a.Take.Fit.Slot {
		return takeRow{}, nil, errors.New("take delta is not measured duration minus target slot")
	}
	repair := a.Repair.String()
	if repair == "unknown" {
		return takeRow{}, nil, fmt.Errorf("take repair %d is unknown", a.Repair)
	}

	slotMs, err := durationMS(a.Take.Fit.Slot, "target slot")
	if err != nil || slotMs <= 0 {
		if err == nil {
			err = errors.New("target slot must be positive")
		}
		return takeRow{}, nil, err
	}
	measuredMs, err := durationMS(a.Take.Fit.Measured, "measured duration")
	if err != nil || measuredMs < 0 {
		if err == nil {
			err = errors.New("measured duration must not be negative")
		}
		return takeRow{}, nil, err
	}
	deltaMs, err := durationMS(a.Take.Fit.Delta, "signed delta")
	if err != nil {
		return takeRow{}, nil, err
	}
	charges, err := a.chargeRows()
	if err != nil {
		return takeRow{}, nil, err
	}
	return takeRow{
		TakeID:       a.TakeID,
		CommitID:     a.CommitID,
		ProjectID:    a.ProjectID,
		DubID:        a.DubID,
		OwnerID:      a.OwnerID,
		Language:     a.Language,
		SegmentIndex: int32(a.Segment.ID),
		Attempt:      uint8(a.Take.Attempt),
		Speaker:      a.Segment.Speaker.Name,
		Voice:        a.Voice,
		Text:         a.Text,
		SlotStartMs:  a.Segment.StartMs,
		SlotMs:       slotMs,
		MeasuredMs:   measuredMs,
		DeltaMs:      deltaMs,
		Repair:       repair,
		RepairDetail: a.RepairDetail,
		AudioPath:    a.Take.File,
		Peaks:        takePeaks(a.Peaks),
	}, charges, nil
}

func (a TakeAttempt) chargeRows() ([]chargeRow, error) {
	if len(a.Charges) > 0 && strings.TrimSpace(a.ChargeProvider) == "" {
		return nil, errors.New("take charge provider is empty")
	}
	rows := make([]chargeRow, 0, len(a.Charges)*2)
	for _, charge := range a.Charges {
		if charge.TakeID != a.Segment.ID {
			return nil, fmt.Errorf("charge take id %d does not match segment %d", charge.TakeID, a.Segment.ID)
		}
		base := chargeRow{
			TakeID:       a.TakeID,
			CommitID:     a.CommitID,
			ProjectID:    a.ProjectID,
			DubID:        a.DubID,
			OwnerID:      a.OwnerID,
			Language:     a.Language,
			SegmentIndex: int32(a.Segment.ID),
			Attempt:      uint8(a.Take.Attempt),
			Kind:         charge.Kind.String(),
			Provider:     a.ChargeProvider,
		}
		if base.Kind == "unknown" {
			return nil, fmt.Errorf("charge kind %d is unknown", charge.Kind)
		}
		switch charge.Kind {
		case cost.ChargeSynthesize:
			if charge.PromptTokens != 0 || charge.CandidateTokens != 0 ||
				charge.PromptUnitPrice != 0 || charge.CandidateUnitPrice != 0 {
				return nil, errors.New("synthesis charge includes Gemini token fields")
			}
			if charge.Units < 0 || charge.UnitPrice < 0 {
				return nil, errors.New("synthesis charge has a negative count or price")
			}
			rows = append(rows, chargeRowWithCost(base, "characters", int64(charge.Units), charge.UnitPrice))
		case cost.ChargeSegment, cost.ChargeTranslate:
			if charge.Units != 0 || charge.UnitPrice != 0 {
				return nil, errors.New("Gemini charge includes synthesis character fields")
			}
			if charge.PromptTokens < 0 || charge.CandidateTokens < 0 ||
				charge.PromptUnitPrice < 0 || charge.CandidateUnitPrice < 0 {
				return nil, errors.New("Gemini charge has a negative count or price")
			}
			if charge.PromptTokens > 0 || charge.PromptUnitPrice != 0 {
				rows = append(rows, chargeRowWithCost(base, "prompt_tokens", int64(charge.PromptTokens), charge.PromptUnitPrice))
			}
			if charge.CandidateTokens > 0 || charge.CandidateUnitPrice != 0 {
				rows = append(rows, chargeRowWithCost(base, "candidate_tokens", int64(charge.CandidateTokens), charge.CandidateUnitPrice))
			}
		default:
			return nil, fmt.Errorf("take charge kind %q does not belong to a take", base.Kind)
		}
	}
	return rows, nil
}

func chargeRowWithCost(row chargeRow, unit string, units int64, unitPrice cost.Price) chargeRow {
	row.Unit = unit
	row.Units = strconv.FormatInt(units, 10)
	row.UnitPriceUSD = priceUSD(unitPrice)
	return row
}

func durationMS(d time.Duration, name string) (int32, error) {
	if d%time.Millisecond != 0 {
		return 0, fmt.Errorf("%s %s is not whole milliseconds", name, d)
	}
	ms := d.Milliseconds()
	if ms < math.MinInt32 || ms > math.MaxInt32 {
		return 0, fmt.Errorf("%s %s overflows Int32 milliseconds", name, d)
	}
	return int32(ms), nil
}

func priceUSD(price cost.Price) string {
	negative := price < 0
	if negative {
		price = -price
	}
	whole := int64(price) / int64(cost.Dollar)
	fraction := int64(price) % int64(cost.Dollar)
	value := strconv.FormatInt(whole, 10) + "." + fmt.Sprintf("%09d", fraction)
	if negative {
		return "-" + value
	}
	return value
}
