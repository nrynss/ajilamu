package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nrynss/ajilamu/internal/agent"
	"github.com/nrynss/ajilamu/internal/config"
	"github.com/nrynss/ajilamu/internal/cost"
)

type agentFunc func(context.Context, string, string) (agent.Reply, error)

func (f agentFunc) Ask(ctx context.Context, id, question string) (agent.Reply, error) {
	return f(ctx, id, question)
}

func TestAgentRoutePreservesChargesAndDubContext(t *testing.T) {
	charges := []cost.Charge{
		{Kind: cost.ChargeAgent, PromptTokens: 10, CandidateTokens: 4, PromptUnitPrice: 150, CandidateUnitPrice: 600},
		{Kind: cost.ChargeAgent, PromptTokens: 20, CandidateTokens: 2, PromptUnitPrice: 150, CandidateUnitPrice: 600},
	}
	calls := 0
	editor := agentFunc(func(ctx context.Context, id, question string) (agent.Reply, error) {
		calls++
		if id != "dub-1" || question != "For dub dub-1:\nWhich line?" {
			t.Fatalf("wrong context: %q %q", id, question)
		}
		return agent.Reply{Text: "Line 3.", Charges: charges}, nil
	})
	server, err := NewServer(&config.Config{}, ServerOptions{Agent: editor})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/dubs/dub-1/agent", strings.NewReader(`{"question":"Which line?"}`)))
	var got AgentResponse
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if response.Code != 200 || calls != 1 || got.Answer != "Line 3." || got.TotalNanodollars != 8100 || len(got.Charges) != 2 {
		t.Fatalf("response %d: %s, calls %d", response.Code, response.Body.String(), calls)
	}
	for i := range charges {
		if got.Charges[i] != charges[i] {
			t.Fatalf("charge %d changed: %+v", i, got.Charges[i])
		}
	}
}

func TestAgentRouteRejectsMalformedQuestionsBeforeCalling(t *testing.T) {
	editor := agentFunc(func(context.Context, string, string) (agent.Reply, error) {
		t.Fatal("invalid request reached agent")
		return agent.Reply{}, nil
	})
	for _, body := range []string{"", "null", `{}`, `{"question":" "}`, `{"question":12}`, `{"question":"x","extra":true}`, `{"question":"x"}{}`, `{"question":"` + strings.Repeat("x", 65536) + `"}`} {
		t.Run(body[:min(len(body), 32)], func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
			req.SetPathValue("id", "dub-1")
			rec := httptest.NewRecorder()
			AgentHandler(editor, nil).ServeHTTP(rec, req)
			if rec.Code != 400 {
				t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestAgentRouteHidesProviderFailureAndNormalizesEmptyCharges(t *testing.T) {
	for _, fail := range []bool{false, true} {
		editor := agentFunc(func(context.Context, string, string) (agent.Reply, error) {
			if fail {
				return agent.Reply{}, errors.New("private provider credentials")
			}
			return agent.Reply{Text: "No charge reported."}, nil
		})
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"question":"x"}`))
		req.SetPathValue("id", "dub-1")
		rec := httptest.NewRecorder()
		AgentHandler(editor, nil).ServeHTTP(rec, req)
		if fail {
			if rec.Code != 502 || strings.Contains(rec.Body.String(), "private") {
				t.Fatalf("failure: %d %s", rec.Code, rec.Body.String())
			}
		} else if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"charges":[]`) || !strings.Contains(rec.Body.String(), `"total_nanodollars":0`) {
			t.Fatalf("empty charges: %s", rec.Body.String())
		}
	}
}
