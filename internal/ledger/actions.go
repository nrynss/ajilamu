package ledger

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/nrynss/ajilamu/internal/api"
)

const insertAction = "INSERT INTO actions_raw (action_id, commit_id, project_id, dub_id, owner_id, language, segment_index, take_id, action_type, author, prompt, before_value, after_value) FORMAT JSONEachRow"

// Action is one append-only provenance row for a commit.
//
// CreatedAt, IngestedAt, and EventKey are omitted deliberately.
// actions_raw supplies those timestamps and the natural-identity hash.
// BeforeValue and AfterValue hold JSON text that explains the edit.
// They stay strings so encoding/json cannot flatten them into the row.
// A non-empty value that fails json.Valid is rejected before enqueue.
type Action struct {
	ActionID     string `json:"action_id"`
	CommitID     string `json:"commit_id"`
	ProjectID    string `json:"project_id"`
	DubID        string `json:"dub_id"`
	OwnerID      string `json:"owner_id"`
	Language     string `json:"language"`
	SegmentIndex int32  `json:"segment_index"`
	TakeID       string `json:"take_id"`
	ActionType   string `json:"action_type"`
	Author       string `json:"author"`
	Prompt       string `json:"prompt"`
	BeforeValue  string `json:"before_value"`
	AfterValue   string `json:"after_value"`
}

var recognizedActionTypes = map[string]struct{}{
	api.ActionSegmentCreated:    {},
	api.ActionBoundaryNudged:    {},
	api.ActionSpeakerReassigned: {},
	api.ActionTextCorrected:     {},
	api.ActionTakeRendered:      {},
	api.ActionAtempoStretched:   {},
	api.ActionLineRewritten:     {},
	api.ActionUserCommand:       {},
}

var recognizedAuthors = map[string]struct{}{
	api.AuthorAgent:      {},
	api.AuthorCommandBar: {},
	api.AuthorManualUI:   {},
}

// RecordAction durably records one action for a commit.
// Validation runs before the queue or transport sees a row.
func (c *Client) RecordAction(ctx context.Context, action Action) error {
	if err := validateAction(action); err != nil {
		return err
	}
	if action.ActionID == "" {
		id, err := newActionID()
		if err != nil {
			return err
		}
		action.ActionID = id
	}
	return c.EnqueueJSON(ctx, insertAction, action)
}

func validateAction(action Action) error {
	for _, field := range []struct {
		name  string
		value string
	}{
		{name: "commit_id", value: action.CommitID},
		{name: "project_id", value: action.ProjectID},
		{name: "dub_id", value: action.DubID},
		{name: "owner_id", value: action.OwnerID},
		{name: "language", value: action.Language},
	} {
		if strings.TrimSpace(field.value) == "" {
			return errors.New("ledger action " + field.name + " is empty")
		}
	}
	if action.SegmentIndex < -1 {
		return fmt.Errorf("ledger action segment_index %d is below -1", action.SegmentIndex)
	}
	// PHASE-4 named this event stretched. The frozen P1 wire stores atempo_stretched.
	if action.ActionType == "stretched" {
		return fmt.Errorf("ledger action action_type %q is unknown, store %q", action.ActionType, api.ActionAtempoStretched)
	}
	if _, ok := recognizedActionTypes[action.ActionType]; !ok {
		return fmt.Errorf("ledger action action_type %q is unknown", action.ActionType)
	}
	if _, ok := recognizedAuthors[action.Author]; !ok {
		return fmt.Errorf("ledger action author %q is unknown", action.Author)
	}
	if action.Author == api.AuthorCommandBar && action.Prompt == "" {
		return errors.New("ledger command_bar action prompt is empty")
	}
	for _, field := range []struct {
		name  string
		value string
	}{
		{name: "before_value", value: action.BeforeValue},
		{name: "after_value", value: action.AfterValue},
	} {
		if field.value != "" && !json.Valid([]byte(field.value)) {
			return fmt.Errorf("ledger action %s is not JSON text", field.name)
		}
	}
	return nil
}

func newActionID() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("generate ledger action_id: %w", err)
	}
	return hex.EncodeToString(buf[:]), nil
}
