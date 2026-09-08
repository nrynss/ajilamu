// Package agent builds the editor agent on google.golang.org/adk/v2.
// The agent reads the ledger through a self-hosted mcp-clickhouse server.
// It answers questions and proposes mutations. It writes nothing.
package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/model/gemini"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/genai"

	"github.com/nrynss/ajilamu/internal/config"
	"github.com/nrynss/ajilamu/internal/cost"
)

// DefaultName names the editor agent inside the ADK tree.
const DefaultName = "editor"

// AppName names the ADK application.
const AppName = "ajilamu"

// UserID names the single local creator.
const UserID = "creator"

// Instruction tells the agent what it may do.
// It reads the ledger and proposes mutations. It never writes.
const Instruction = `You answer questions about an Ajilamu dubbing ledger.
Use the read tools to inspect the ledger. Every tool is read-only.
Never claim a change landed, because you cannot apply one.
Propose a change as a command-bar instruction instead.
The command bar parses and validates it. Deterministic Go code applies it.
Keep answers short. Cite the line ids and the costs you read.`

// Config carries the settings one editor agent needs.
type Config struct {
	// Name is the agent name inside the ADK tree.
	Name string
	// ModelName names the Gemini model for agent turns.
	ModelName string
	// Project and Location reach Vertex AI.
	Project  string
	Location string
	// MCPEndpoint and MCPAuthToken reach the read-only MCP server.
	MCPEndpoint  string
	MCPAuthToken string
	// Transport replaces the MCP endpoint for offline tests.
	Transport mcp.Transport
	// Model replaces the Gemini client for offline tests.
	Model model.LLM
	// RateCard prices the token counts each turn reports.
	RateCard cost.RateCard
}

// Agent answers questions about the ledger. It reads. It never writes.
type Agent struct {
	name   string
	runner *runner.Runner
	card   cost.RateCard

	// turn serializes agent turns, so a turn owns the charges it produced.
	turn sync.Mutex
	// mu guards charges, which an ADK callback appends during a turn.
	mu      sync.Mutex
	charges []cost.Charge
}

// New builds the editor agent.
// It uses cfg.Model when set, and otherwise builds a Vertex AI Gemini model.
func New(ctx context.Context, cfg Config) (*Agent, error) {
	if strings.TrimSpace(cfg.Name) == "" {
		cfg.Name = DefaultName
	}
	if cfg.RateCard == (cost.RateCard{}) {
		cfg.RateCard = cost.DefaultRateCard()
	}
	llm, err := buildModel(ctx, cfg)
	if err != nil {
		return nil, err
	}
	set, err := NewToolset(ToolsetConfig{
		Endpoint:  cfg.MCPEndpoint,
		AuthToken: cfg.MCPAuthToken,
		Transport: cfg.Transport,
	})
	if err != nil {
		return nil, err
	}
	editor := &Agent{
		name: cfg.Name,
		card: cfg.RateCard,
	}
	root, err := llmagent.New(llmagent.Config{
		Name:                cfg.Name,
		Description:         "Answers questions about the dubbing ledger.",
		Instruction:         Instruction,
		Model:               llm,
		Toolsets:            []tool.Toolset{set},
		AfterModelCallbacks: []llmagent.AfterModelCallback{editor.recordCharge},
	})
	if err != nil {
		return nil, fmt.Errorf("build llm agent: %w", err)
	}
	editor.runner, err = runner.New(runner.Config{
		AppName:           AppName,
		Agent:             root,
		SessionService:    session.InMemoryService(),
		AutoCreateSession: true,
	})
	if err != nil {
		return nil, fmt.Errorf("build runner: %w", err)
	}
	return editor, nil
}

// NewFromConfig builds the editor agent from process settings.
// It returns a nil agent and no error when the MCP settings are absent.
// A missing setting disables the agent and never stops the server.
func NewFromConfig(ctx context.Context, cfg *config.Config) (*Agent, error) {
	if !cfg.MCPConfigured() {
		return nil, nil
	}
	return New(ctx, Config{
		ModelName:    cfg.GeminiModel,
		Project:      cfg.GoogleCloudProject,
		Location:     cfg.GoogleCloudLocation,
		MCPEndpoint:  cfg.ClickHouseMCPURL,
		MCPAuthToken: cfg.ClickHouseMCPAuthToken,
	})
}

// buildModel returns the model for one agent.
// A test model wins. Otherwise the caller must name a model and a project.
func buildModel(ctx context.Context, cfg Config) (model.LLM, error) {
	if cfg.Model != nil {
		return cfg.Model, nil
	}
	if strings.TrimSpace(cfg.ModelName) == "" {
		return nil, errors.New("agent config misses a model name")
	}
	if strings.TrimSpace(cfg.Project) == "" {
		return nil, errors.New("agent config misses a Google Cloud project")
	}
	llm, err := gemini.NewModel(ctx, cfg.ModelName, &genai.ClientConfig{
		Backend:  genai.BackendVertexAI,
		Project:  cfg.Project,
		Location: cfg.Location,
	})
	if err != nil {
		return nil, fmt.Errorf("build gemini model: %w", err)
	}
	return llm, nil
}

// Reply is one agent answer and what it cost.
type Reply struct {
	// Text is the answer the model produced.
	Text string
	// Charges itemizes every model call the turn made.
	Charges []cost.Charge
}

// Ask runs one turn and returns its answer and charges.
// The agent reads the ledger. It applies no mutation.
// Ask runs one turn at a time, so a turn owns the charges it produced.
func (a *Agent) Ask(ctx context.Context, sessionID, question string) (Reply, error) {
	if strings.TrimSpace(sessionID) == "" {
		return Reply{}, errors.New("session id is empty")
	}
	if strings.TrimSpace(question) == "" {
		return Reply{}, errors.New("question is empty")
	}
	a.turn.Lock()
	defer a.turn.Unlock()
	a.mu.Lock()
	a.charges = nil
	a.mu.Unlock()

	var text strings.Builder
	message := genai.NewContentFromText(question, genai.RoleUser)
	for event, err := range a.runner.Run(ctx, UserID, sessionID, message, agent.RunConfig{}) {
		if err != nil {
			return Reply{}, fmt.Errorf("agent turn: %w", err)
		}
		if event == nil || event.Author != a.name || event.Content == nil {
			continue
		}
		for _, part := range event.Content.Parts {
			if part != nil && part.Text != "" {
				text.WriteString(part.Text)
			}
		}
	}
	return Reply{
		Text:    strings.TrimSpace(text.String()),
		Charges: a.takeCharges(),
	}, nil
}

// recordCharge turns one model response into a ChargeAgent.
// It keeps the token counts the response reported and prices them.
func (a *Agent) recordCharge(ctx agent.Context, resp *model.LLMResponse, respErr error) (*model.LLMResponse, error) {
	if respErr != nil || resp == nil || resp.UsageMetadata == nil {
		return resp, respErr
	}
	usage := resp.UsageMetadata
	charge := cost.Charge{
		Kind:               cost.ChargeAgent,
		PromptTokens:       int(usage.PromptTokenCount),
		CandidateTokens:    int(usage.CandidatesTokenCount),
		PromptUnitPrice:    a.card.AgentPerPromptToken,
		CandidateUnitPrice: a.card.AgentPerCandidateToken,
	}
	a.mu.Lock()
	a.charges = append(a.charges, charge)
	a.mu.Unlock()
	return resp, nil
}

// takeCharges returns and clears the charges of the last turn.
func (a *Agent) takeCharges() []cost.Charge {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.charges) == 0 {
		return nil
	}
	out := make([]cost.Charge, len(a.charges))
	copy(out, a.charges)
	a.charges = nil
	return out
}
