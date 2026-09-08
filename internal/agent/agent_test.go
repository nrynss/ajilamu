package agent

import (
	"context"
	"fmt"
	"iter"
	"regexp"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/tool/toolconfirmation"
	"google.golang.org/genai"

	"github.com/nrynss/ajilamu/internal/config"
	"github.com/nrynss/ajilamu/internal/cost"
)

// fakeServerTools names the tools the fake mcp-clickhouse server exposes.
// It carries two tools the filter must drop.
var fakeServerTools = []string{
	"list_databases",
	"list_tables",
	"run_query",
	"run_chdb_select_query",
	"insert_rows",
}

// newFakeServer starts an in-memory MCP server and returns the client transport.
// Nothing here opens a socket, so the offline suite makes no network call.
func newFakeServer(t *testing.T) mcp.Transport {
	t.Helper()
	server := mcp.NewServer(&mcp.Implementation{Name: "fake-mcp-clickhouse", Version: "test"}, nil)
	for _, name := range fakeServerTools {
		mcp.AddTool(server, &mcp.Tool{Name: name, Description: "fake " + name},
			func(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, struct{}, error) {
				return nil, struct{}{}, nil
			})
	}
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	if _, err := server.Connect(context.Background(), serverTransport, nil); err != nil {
		t.Fatalf("connect fake MCP server: %v", err)
	}
	return clientTransport
}

// toolContext is the read-only ADK context a tool listing needs.
type toolContext struct {
	*adkagent.StrictContextMock
}

// ToolConfirmation reports no pending confirmation.
func (c *toolContext) ToolConfirmation() *toolconfirmation.ToolConfirmation { return nil }

// newToolContext returns a context for one tool listing.
func newToolContext(ctx context.Context) *toolContext {
	mock := adkagent.NewStrictContextMock(ctx)
	return &toolContext{StrictContextMock: &mock}
}

// TestToolsetNarrowsToReadTools builds the toolset against a fake MCP server.
// The named read list survives and every other tool is dropped.
func TestToolsetNarrowsToReadTools(t *testing.T) {
	ctx := context.Background()
	set, err := NewToolset(ToolsetConfig{Transport: newFakeServer(t)})
	if err != nil {
		t.Fatalf("NewToolset: %v", err)
	}
	tools, err := set.Tools(newToolContext(ctx))
	if err != nil {
		t.Fatalf("Tools: %v", err)
	}
	got := map[string]bool{}
	for _, candidate := range tools {
		got[candidate.Name()] = true
	}
	for _, name := range ReadTools {
		if !got[name] {
			t.Errorf("read tool %q is absent", name)
		}
	}
	for _, name := range []string{"run_chdb_select_query", "insert_rows"} {
		if got[name] {
			t.Errorf("tool %q reached the agent", name)
		}
	}
	if len(tools) != len(ReadTools) {
		t.Errorf("toolset has %d tools, want %d", len(tools), len(ReadTools))
	}
}

// TestNewToolsetRejectsBadConfig keeps the transport rules honest.
// Auth needs the streamable HTTP transport, so a fake transport rejects a token.
func TestNewToolsetRejectsBadConfig(t *testing.T) {
	if _, err := NewToolset(ToolsetConfig{}); err == nil {
		t.Error("NewToolset without an endpoint returned no error")
	}
	if _, err := NewToolset(ToolsetConfig{Endpoint: "http://127.0.0.1:8000/mcp"}); err == nil {
		t.Error("NewToolset without a token returned no error")
	}
	if _, err := NewToolset(ToolsetConfig{Transport: newFakeServer(t), AuthToken: "token"}); err == nil {
		t.Error("NewToolset with a fake transport and a token returned no error")
	}
}

// TestNewToolsetAcceptsHTTPAuth builds the production shape without a call.
// mcptoolset builds the streamable HTTP transport and wraps it with the token.
func TestNewToolsetAcceptsHTTPAuth(t *testing.T) {
	set, err := NewToolset(ToolsetConfig{Endpoint: "http://127.0.0.1:8000/mcp", AuthToken: "token"})
	if err != nil {
		t.Fatalf("NewToolset: %v", err)
	}
	if set == nil {
		t.Fatal("NewToolset returned a nil toolset")
	}
}

// fakeModel answers one turn with fixed token counts and no tool call.
type fakeModel struct {
	promptTokens    int32
	candidateTokens int32
}

// Name names the fake model.
func (m *fakeModel) Name() string { return "fake-gemini" }

// GenerateContent yields one text response carrying usage metadata.
func (m *fakeModel) GenerateContent(_ context.Context, _ *model.LLMRequest, _ bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		yield(&model.LLMResponse{
			Content: genai.NewContentFromText("Line 3 costs $0.0023182.", genai.RoleModel),
			UsageMetadata: &genai.GenerateContentResponseUsageMetadata{
				PromptTokenCount:     m.promptTokens,
				CandidatesTokenCount: m.candidateTokens,
			},
			FinishReason: genai.FinishReasonStop,
			TurnComplete: true,
		}, nil)
	}
}

// TestAgentTurnEmitsChargeAgent runs one turn against a fake model.
// The turn reports the token counts the response carried, priced by the card.
func TestAgentTurnEmitsChargeAgent(t *testing.T) {
	card := cost.DefaultRateCard()
	editor, err := New(context.Background(), Config{
		ModelName: "fake-model",
		Model:     &fakeModel{promptTokens: 1200, candidateTokens: 340},
		Transport: newFakeServer(t),
		RateCard:  card,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	reply, err := editor.Ask(context.Background(), "session-1", "What did line 3 cost?")
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if !strings.Contains(reply.Text, "$0.0023182") {
		t.Errorf("reply text = %q", reply.Text)
	}
	if len(reply.Charges) == 0 {
		t.Fatal("turn emitted no charge")
	}
	charge := reply.Charges[0]
	if charge.Kind != cost.ChargeAgent {
		t.Errorf("charge kind = %v, want ChargeAgent", charge.Kind)
	}
	if charge.PromptTokens != 1200 || charge.CandidateTokens != 340 {
		t.Errorf("token counts = %d/%d, want 1200/340", charge.PromptTokens, charge.CandidateTokens)
	}
	want := cost.Price(1200)*card.AgentPerPromptToken + cost.Price(340)*card.AgentPerCandidateToken
	if got := charge.Total(); got != want {
		t.Errorf("charge total = %d, want %d", got, want)
	}
	again, err := editor.Ask(context.Background(), "session-1", "And line 4?")
	if err != nil {
		t.Fatalf("second Ask: %v", err)
	}
	if len(again.Charges) != 1 {
		t.Errorf("second turn charges = %d, want 1", len(again.Charges))
	}
}

// TestAskRejectsEmptyInput keeps an empty question off the model path.
func TestAskRejectsEmptyInput(t *testing.T) {
	editor, err := New(context.Background(), Config{
		ModelName: "fake-model",
		Model:     &fakeModel{promptTokens: 1, candidateTokens: 1},
		Transport: newFakeServer(t),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := editor.Ask(context.Background(), "", "hello"); err == nil {
		t.Error("Ask with an empty session id returned no error")
	}
	if _, err := editor.Ask(context.Background(), "session-1", "   "); err == nil {
		t.Error("Ask with an empty question returned no error")
	}
}

// TestNewFromConfigDisablesWithoutMCP pins the optional MCP contract.
// A missing setting disables the agent and never stops the server.
func TestNewFromConfigDisablesWithoutMCP(t *testing.T) {
	editor, err := NewFromConfig(context.Background(), &config.Config{GeminiModel: "fake-model"})
	if err != nil {
		t.Fatalf("NewFromConfig: %v", err)
	}
	if editor != nil {
		t.Fatal("NewFromConfig returned an agent without MCP settings")
	}
}

// grantPrivileges captures the privilege list between GRANT and ON.
// ClickHouse writes one GRANT statement per row, so every row parses.
var grantPrivileges = regexp.MustCompile(`(?is)\bGRANT\s+(.+?)\s+ON\b`)

// requireSelectOnlyGrants fails unless every privilege in the SHOW GRANTS
// text is exactly SELECT. It parses the list between GRANT and ON, splits
// on commas, and compares each entry with SELECT. The whitelist rejects
// every other privilege, known or unknown, so a future ClickHouse
// privilege cannot slip through. ClickHouse expands ALL into a list that
// carries a privilege other than SELECT.
func requireSelectOnlyGrants(grants string) error {
	matches := grantPrivileges.FindAllStringSubmatch(grants, -1)
	if len(matches) == 0 {
		return fmt.Errorf("grant set names no SELECT: %q", grants)
	}
	for _, match := range matches {
		for _, privilege := range strings.Split(match[1], ",") {
			entry := strings.ToUpper(strings.TrimSpace(privilege))
			if entry != "SELECT" {
				return fmt.Errorf("grant set names %s: %q", entry, grants)
			}
		}
	}
	return nil
}

// TestSelectOnlyGrants drives the parser over measured grant sets.
// The SELECT-only sets pass and every other privilege fails.
func TestSelectOnlyGrants(t *testing.T) {
	tests := []struct {
		name   string
		grants string
		want   bool
	}{
		{"select only", "GRANT SELECT ON default.* TO mcp_readonly", true},
		{"tool envelope", `{"columns": ["GRANTS FOR mcp_readonly FORMAT Native"], "rows": [["GRANT SELECT ON default.* TO mcp_readonly"]]}`, true},
		{"two select rows", `{"columns": ["GRANTS FOR mcp_readonly FORMAT Native"], "rows": [["GRANT SELECT ON default.* TO mcp_readonly"], ["GRANT SELECT ON default.commits_raw TO mcp_readonly"]]}`, true},
		{"insert grant", "GRANT SELECT, INSERT ON default.* TO mcp_writer", false},
		{"all grant", "GRANT ALL ON *.* TO mcp_all", false},
		{"insert only", "GRANT INSERT ON default.* TO mcp_writer", false},
		{"alter grant", "GRANT SELECT, ALTER ON default.* TO mcp_readonly", false},
		{"alter update grant", "GRANT SELECT, ALTER UPDATE ON default.commits_raw TO mcp_readonly", false},
		{"drop grant", "GRANT SELECT, DROP TABLE ON default.* TO mcp_readonly", false},
		{"create grant", "GRANT SELECT, CREATE ON default.* TO mcp_readonly", false},
		{"truncate grant", "GRANT SELECT, TRUNCATE ON default.* TO mcp_readonly", false},
		{"delete grant", "GRANT SELECT, DELETE ON default.* TO mcp_readonly", false},
		{"optimize grant", "GRANT SELECT, OPTIMIZE ON default.* TO mcp_readonly", false},
		{"unknown grant", "GRANT SELECT, FUTUREPRIV ON default.* TO mcp_readonly", false},
		{"measured two row envelope", `{"columns": ["GRANTS FOR mcp_readonly FORMAT Native"], "rows": [["GRANT SELECT ON default.* TO mcp_readonly"], ["GRANT INSERT ON default.commits_raw TO mcp_readonly"]]}`, false},
		{"empty", "", false},
	}
	for _, test := range tests {
		err := requireSelectOnlyGrants(test.grants)
		if test.want && err != nil {
			t.Errorf("%s: rejected %q: %v", test.name, test.grants, err)
		}
		if !test.want && err == nil {
			t.Errorf("%s: accepted %q, want a refusal", test.name, test.grants)
		}
	}
}
