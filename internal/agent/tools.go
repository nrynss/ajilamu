package agent

import (
	"errors"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/adk/v2/auth"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/mcptoolset"
)

// ReadTools names the mcp-clickhouse tools the editor agent may call.
// The list is a fence rather than a preference. run_query reaches the ledger
// through the mcp_readonly grant, so every accepted statement stays read-only.
var ReadTools = []string{
	"list_databases",
	"list_tables",
	"run_query",
}

// ToolsetConfig configures the mcp-clickhouse read toolset.
type ToolsetConfig struct {
	// Endpoint is the streamable HTTP URL of the mcp-clickhouse server.
	Endpoint string
	// AuthToken is the static bearer token the server expects.
	AuthToken string
	// Transport replaces the HTTP endpoint. Offline tests pass a fake server.
	// Auth needs the HTTP transport, so setting both is a configuration error.
	Transport mcp.Transport
}

// NewToolset builds the filtered read toolset.
// mcptoolset.Config.Endpoint builds a streamable HTTP transport, and Auth
// carries the static bearer token. tool.FilterToolset narrows the server's
// tools to ReadTools.
func NewToolset(cfg ToolsetConfig) (tool.Toolset, error) {
	if cfg.Transport != nil {
		if strings.TrimSpace(cfg.AuthToken) != "" {
			return nil, errors.New("mcp auth token needs a streamable HTTP transport")
		}
	} else {
		if strings.TrimSpace(cfg.Endpoint) == "" {
			return nil, errors.New("mcp endpoint is empty")
		}
		if strings.TrimSpace(cfg.AuthToken) == "" {
			return nil, errors.New("mcp auth token is empty")
		}
	}
	mcpCfg := mcptoolset.Config{Endpoint: cfg.Endpoint}
	if cfg.Transport != nil {
		mcpCfg.Transport = cfg.Transport
	} else {
		mcpCfg.Auth = auth.StaticToken(cfg.AuthToken)
	}
	set, err := mcptoolset.New(mcpCfg)
	if err != nil {
		return nil, fmt.Errorf("build mcp toolset: %w", err)
	}
	return tool.FilterToolset(set, tool.AllowedToolsPredicate(ReadTools)), nil
}
