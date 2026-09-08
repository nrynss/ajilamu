//go:build live

package agent

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/toolconfirmation"
)

// liveDubTitle names the fixture dub. The ledger stores no title column, so
// the probe reports this label beside the dub id it queries.
const liveDubTitle = "NASA 75-second clip"

// liveGrantUser names the ClickHouse user the deploy pins for the MCP server.
// The SELECT grant on this user is the read-only boundary.
const liveGrantUser = "mcp_readonly"

// liveContext is the minimal ADK context one direct tool call needs.
type liveContext struct {
	*adkagent.StrictContextMock
}

// ToolConfirmation reports no pending confirmation.
func (c *liveContext) ToolConfirmation() *toolconfirmation.ToolConfirmation { return nil }

// newLiveContext returns a context for one direct tool call.
func newLiveContext() *liveContext {
	mock := adkagent.NewStrictContextMock(context.Background())
	return &liveContext{StrictContextMock: &mock}
}

// runnable is the Run method every mcptoolset tool exposes.
type runnable interface {
	Run(ctx adkagent.Context, args any) (map[string]any, error)
}

// TestLiveMCPClickHouseReadsAndRefusesInsert probes a running container.
// It needs CLICKHOUSE_MCP_URL and CLICKHOUSE_MCP_AUTH_TOKEN, and skips without them.
func TestLiveMCPClickHouseReadsAndRefusesInsert(t *testing.T) {
	endpoint := strings.TrimSpace(os.Getenv("CLICKHOUSE_MCP_URL"))
	token := strings.TrimSpace(os.Getenv("CLICKHOUSE_MCP_AUTH_TOKEN"))
	if endpoint == "" || token == "" {
		t.Skip("CLICKHOUSE_MCP_URL and CLICKHOUSE_MCP_AUTH_TOKEN are unset")
	}
	ctx := context.Background()
	set, err := NewToolset(ToolsetConfig{Endpoint: endpoint, AuthToken: token})
	if err != nil {
		t.Fatalf("NewToolset: %v", err)
	}
	tools, err := set.Tools(newLiveContext())
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	byName := map[string]tool.Tool{}
	for _, candidate := range tools {
		byName[candidate.Name()] = candidate
	}
	for _, name := range ReadTools {
		if byName[name] == nil {
			t.Fatalf("read tool %q is absent from the container", name)
		}
	}

	// The grant set is the boundary. The INSERT refusal below is a second check.
	grants, err := callTool(ctx, byName["run_query"], map[string]any{"query": "SHOW GRANTS FOR " + liveGrantUser})
	if err != nil {
		t.Fatalf("SHOW GRANTS FOR %s: %v", liveGrantUser, err)
	}
	t.Logf("grants for %s: %s", liveGrantUser, grants)
	if err := requireSelectOnlyGrants(grants); err != nil {
		t.Fatalf("grant set is not SELECT-only: %v", err)
	}
	user, err := callTool(ctx, byName["run_query"], map[string]any{"query": "SELECT currentUser()"})
	if err != nil {
		t.Fatalf("currentUser: %v", err)
	}
	if !strings.Contains(user, liveGrantUser) {
		t.Fatalf("the MCP server connects as %s, want %s", user, liveGrantUser)
	}

	database := envOr("CLICKHOUSE_DATABASE", "default")
	dubID := envOr("AJILAMU_LIVE_DUB_ID", "6d9c2f1a-3b4e-4a8d-9c1e-7f2b5a3d8c40")

	tables, err := callTool(ctx, byName["list_tables"], map[string]any{"database": database})
	if err != nil {
		t.Fatalf("list_tables: %v", err)
	}
	t.Logf("tables in %s: %s", database, tables)
	if !strings.Contains(tables, "commits") {
		t.Fatalf("list_tables does not name commits: %s", tables)
	}

	query := fmt.Sprintf("SELECT commit_id, version_seq, message FROM commits WHERE dub_id = '%s' ORDER BY version_seq LIMIT 5", dubID)
	commits, err := callTool(ctx, byName["run_query"], map[string]any{"query": query})
	if err != nil {
		t.Fatalf("read commits: %v", err)
	}
	t.Logf("commits for %s (%s): %s", dubID, liveDubTitle, commits)

	// The refusal is a second check. The grant read above is the proof.
	insert := fmt.Sprintf("INSERT INTO commits_raw (commit_id, project_id, dub_id, owner_id, version_seq) VALUES ('probe', 'probe', '%s', 'probe', 999999)", dubID)
	if _, err := callTool(ctx, byName["run_query"], map[string]any{"query": insert}); err == nil {
		t.Fatal("INSERT through the agent path succeeded, want a read-only refusal")
	} else {
		t.Logf("INSERT refused as a second check: %v", err)
	}
}

// callTool runs one MCP tool and returns its text output.
func callTool(ctx context.Context, candidate tool.Tool, args map[string]any) (string, error) {
	runner, ok := candidate.(runnable)
	if !ok {
		return "", fmt.Errorf("tool %s has no Run method", candidate.Name())
	}
	out, err := runner.Run(newLiveContext(), args)
	if err != nil {
		return "", err
	}
	return toolText(out["output"]), nil
}

// toolText returns the text an MCP tool result carries.
// ADK returns the MCP structured content when the server sets it, and the
// text content otherwise. mcp-clickhouse nests its text under result.
func toolText(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case map[string]any:
		if text, ok := typed["result"].(string); ok {
			return text
		}
	}
	return fmt.Sprintf("%v", value)
}

// envOr reads one environment variable and falls back when it is empty.
func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
