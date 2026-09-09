package ledger

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/nrynss/ajilamu/internal/cost"
)

const chargeInsert = "INSERT INTO charges_raw (take_id, commit_id, project_id, dub_id, owner_id, language, segment_index, attempt, turn_id, call_index, kind, provider, unit, units, unit_price_usd) FORMAT JSONEachRow"

// AgentTurn contains the billed model calls from one editor agent request.
// It uses the whole-pass shape because an agent turn serves the dub, not a take.
type AgentTurn struct {
	TurnID    string
	CommitID  string
	ProjectID string
	DubID     string
	OwnerID   string
	Provider  string
	Charges   []cost.Charge
}

// RecordAgentTurn journals every measured call before flushing the durable queue.
// A retry keeps the turn id and call indexes, so ClickHouse collapses only delivery retries.
func (c *Client) RecordAgentTurn(ctx context.Context, turn AgentTurn) error {
	rows, err := turn.rows()
	if err != nil {
		return err
	}
	for _, row := range rows {
		if err := c.journalJSON(chargeInsert, row); err != nil {
			return fmt.Errorf("enqueue agent charge: %w", err)
		}
	}
	if len(rows) == 0 {
		return nil
	}
	return c.Flush(ctx)
}

func (t AgentTurn) rows() ([]chargeRow, error) {
	for _, field := range []struct {
		name  string
		value string
	}{
		{name: "turn_id", value: t.TurnID},
		{name: "project_id", value: t.ProjectID},
		{name: "dub_id", value: t.DubID},
		{name: "owner_id", value: t.OwnerID},
		{name: "provider", value: t.Provider},
	} {
		if strings.TrimSpace(field.value) == "" {
			return nil, errors.New("agent turn " + field.name + " is empty")
		}
	}
	if len(t.Charges) > math.MaxUint16+1 {
		return nil, fmt.Errorf("agent turn has %d calls, want at most %d", len(t.Charges), math.MaxUint16+1)
	}

	rows := make([]chargeRow, 0, len(t.Charges)*2)
	for i, charge := range t.Charges {
		if charge.Kind != cost.ChargeAgent {
			return nil, fmt.Errorf("agent turn charge kind %q is not agent", charge.Kind.String())
		}
		if charge.TakeID != 0 {
			return nil, fmt.Errorf("agent turn charge has take id %d", charge.TakeID)
		}
		if charge.Units != 0 || charge.UnitPrice != 0 {
			return nil, errors.New("agent charge includes synthesis character fields")
		}
		if charge.PromptTokens < 0 || charge.CandidateTokens < 0 ||
			charge.PromptUnitPrice < 0 || charge.CandidateUnitPrice < 0 {
			return nil, errors.New("agent charge has a negative count or price")
		}
		base := chargeRow{
			CommitID:     t.CommitID,
			ProjectID:    t.ProjectID,
			DubID:        t.DubID,
			OwnerID:      t.OwnerID,
			SegmentIndex: -1,
			TurnID:       t.TurnID,
			CallIndex:    uint16(i),
			Kind:         cost.ChargeAgent.String(),
			Provider:     t.Provider,
		}
		if charge.PromptTokens > 0 || charge.PromptUnitPrice != 0 {
			rows = append(rows, chargeRowWithCost(base, "prompt_tokens", int64(charge.PromptTokens), charge.PromptUnitPrice))
		}
		if charge.CandidateTokens > 0 || charge.CandidateUnitPrice != 0 {
			rows = append(rows, chargeRowWithCost(base, "candidate_tokens", int64(charge.CandidateTokens), charge.CandidateUnitPrice))
		}
	}
	return rows, nil
}
