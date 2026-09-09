package ledger

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/nrynss/ajilamu/internal/cost"
)

func TestRecordAgentTurnKeepsIdenticalTurnsDistinct(t *testing.T) {
	t.Parallel()

	var (
		mu   sync.Mutex
		rows []map[string]any
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if query := r.URL.Query().Get("query"); query != chargeInsert {
			t.Errorf("query = %q, want charge insert", query)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
			http.Error(w, "read body", http.StatusInternalServerError)
			return
		}
		var captured []map[string]any
		for _, line := range splitJSONRows(t, body) {
			var row map[string]any
			if err := json.Unmarshal(line, &row); err != nil {
				t.Errorf("decode charge row: %v", err)
				http.Error(w, "decode row", http.StatusBadRequest)
				return
			}
			captured = append(captured, row)
		}
		mu.Lock()
		rows = append(rows, captured...)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := fixtureLedgerClient(t, server.URL)
	defer client.Close()
	charge := cost.Charge{
		Kind:               cost.ChargeAgent,
		PromptTokens:       1200,
		CandidateTokens:    340,
		PromptUnitPrice:    150,
		CandidateUnitPrice: 600,
	}
	for _, turnID := range []string{"turn-a", "turn-b"} {
		err := client.RecordAgentTurn(context.Background(), AgentTurn{
			TurnID: turnID, CommitID: "commit-1", ProjectID: "dub-1",
			DubID: "dub-1", OwnerID: "local", Provider: "gemini-3.8-flash",
			Charges: []cost.Charge{charge},
		})
		if err != nil {
			t.Fatalf("RecordAgentTurn %s: %v", turnID, err)
		}
	}

	mu.Lock()
	got := append([]map[string]any(nil), rows...)
	mu.Unlock()
	if len(got) != 4 {
		t.Fatalf("charge rows = %d, want two itemized rows per turn", len(got))
	}
	byTurn := map[string]int{}
	for _, row := range got {
		turnID, _ := row["turn_id"].(string)
		byTurn[turnID]++
		if row["take_id"] != "" || row["segment_index"] != float64(-1) ||
			row["attempt"] != float64(0) || row["call_index"] != float64(0) {
			t.Errorf("agent row does not use whole-pass identity: %#v", row)
		}
		if row["kind"] != "agent" || row["commit_id"] != "commit-1" {
			t.Errorf("agent row lost kind or commit: %#v", row)
		}
	}
	if byTurn["turn-a"] != 2 || byTurn["turn-b"] != 2 {
		t.Errorf("rows by turn = %v, want two for each distinct turn", byTurn)
	}
}

func TestAgentTurnIndexesIdenticalCalls(t *testing.T) {
	t.Parallel()

	charge := cost.Charge{
		Kind: cost.ChargeAgent, PromptTokens: 10, CandidateTokens: 4,
		PromptUnitPrice: 150, CandidateUnitPrice: 600,
	}
	rows, err := (AgentTurn{
		TurnID: "turn-a", CommitID: "commit-1", ProjectID: "dub-1",
		DubID: "dub-1", OwnerID: "local", Provider: "gemini-3.8-flash",
		Charges: []cost.Charge{charge, charge},
	}).rows()
	if err != nil {
		t.Fatalf("rows: %v", err)
	}
	if len(rows) != 4 {
		t.Fatalf("rows = %d, want four itemized rows", len(rows))
	}
	for i, row := range rows {
		want := uint16(i / 2)
		if row.CallIndex != want {
			t.Errorf("row %d call_index = %d, want %d", i, row.CallIndex, want)
		}
	}
}

func TestAgentTurnRejectsInvalidCharges(t *testing.T) {
	t.Parallel()

	valid := AgentTurn{
		TurnID: "turn-a", ProjectID: "dub-1",
		DubID: "dub-1", OwnerID: "local", Provider: "gemini-3.8-flash",
		Charges: []cost.Charge{{Kind: cost.ChargeAgent, PromptTokens: 1, PromptUnitPrice: 150}},
	}
	cases := []struct {
		name string
		edit func(*AgentTurn)
	}{
		{name: "blank turn", edit: func(turn *AgentTurn) { turn.TurnID = "" }},
		{name: "wrong kind", edit: func(turn *AgentTurn) { turn.Charges[0].Kind = cost.ChargeTranslate }},
		{name: "take identity", edit: func(turn *AgentTurn) { turn.Charges[0].TakeID = 1 }},
		{name: "negative tokens", edit: func(turn *AgentTurn) { turn.Charges[0].PromptTokens = -1 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			turn := valid
			turn.Charges = append([]cost.Charge(nil), valid.Charges...)
			tc.edit(&turn)
			if _, err := turn.rows(); err == nil {
				t.Fatal("invalid agent turn produced rows")
			}
		})
	}
}
