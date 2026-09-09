package agent_test

import (
	"context"
	"encoding/json"
	"iter"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"

	"github.com/nrynss/ajilamu/internal/agent"
	"github.com/nrynss/ajilamu/internal/api"
	"github.com/nrynss/ajilamu/internal/config"
	"github.com/nrynss/ajilamu/internal/cost"
)

// routeModel reads once through MCP, then answers with independently set usage.
type routeModel struct{ calls int }

func (*routeModel) Name() string { return "offline-route-model" }

func (m *routeModel) GenerateContent(_ context.Context, _ *model.LLMRequest, _ bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		m.calls++
		response := &model.LLMResponse{
			Content:       genai.NewContentFromText("Line 3 costs the most.", genai.RoleModel),
			UsageMetadata: &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: 1200, CandidatesTokenCount: 340},
			FinishReason:  genai.FinishReasonStop,
			TurnComplete:  true,
		}
		if m.calls == 1 {
			response.Content = &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{Name: "run_query", Args: map[string]any{"query": "SELECT 3 AS line"}}}}}
			response.TurnComplete = false
		}
		yield(response, nil)
	}
}

func TestAgentHTTPRouteWithOfflineTransport(t *testing.T) {
	ctx := t.Context()
	mcpServer := mcp.NewServer(&mcp.Implementation{Name: "offline-ledger", Version: "test"}, nil)
	reads := 0
	mcp.AddTool(mcpServer, &mcp.Tool{Name: "run_query", Description: "Read the ledger"},
		func(_ context.Context, _ *mcp.CallToolRequest, args struct {
			Query string `json:"query"`
		}) (*mcp.CallToolResult, struct{}, error) {
			if args.Query != "SELECT 3 AS line" {
				t.Errorf("unexpected query %q", args.Query)
			}
			reads++
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "line 3"}}}, struct{}{}, nil
		})
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	session, err := mcpServer.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Close() })
	model := &routeModel{}
	editor, err := agent.New(ctx, agent.Config{Transport: clientTransport, Model: model})
	if err != nil {
		t.Fatal(err)
	}
	server, err := api.NewServer(&config.Config{}, api.ServerOptions{Agent: editor})
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)
	response, err := http.Post(httpServer.URL+"/api/dubs/offline-dub/agent", "application/json", strings.NewReader(`{"question":"Which line costs the most?"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var got api.AgentResponse
	if err := json.NewDecoder(response.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 200 || got.Answer != "Line 3 costs the most." || reads != 1 || model.calls != 2 {
		t.Fatalf("status %d, answer %q, reads %d, model calls %d", response.StatusCode, got.Answer, reads, model.calls)
	}
	// Independent arithmetic pins both billed calls, beyond the program's total.
	if got.TotalNanodollars != 768000 || len(got.Charges) != 2 {
		t.Fatalf("charges %+v, total %d", got.Charges, got.TotalNanodollars)
	}
	for _, charge := range got.Charges {
		if charge.Kind != cost.ChargeAgent || charge.PromptTokens != 1200 || charge.CandidateTokens != 340 || charge.PromptUnitPrice != 150 || charge.CandidateUnitPrice != 600 {
			t.Fatalf("measured charge differs from usage and rate card: %+v", charge)
		}
	}
}
