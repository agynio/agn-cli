//go:build e2e

package e2e

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/agynio/agn-cli/internal/llm"
	"github.com/agynio/agn-cli/internal/loop"
	"github.com/agynio/agn-cli/internal/message"
	"github.com/agynio/agn-cli/internal/state"
	"github.com/agynio/agn-cli/internal/summarize"
)

func TestAgentSystemPrompt(t *testing.T) {
	cfg := loadTestConfig(t)

	llmClient, err := llm.NewClient(cfg.Endpoint, cfg.APIKey, cfg.Model)
	require.NoError(t, err)

	store, err := state.NewLocalStore(t.TempDir())
	require.NoError(t, err)

	summarizer, err := summarize.New(llmClient, summarize.Config{})
	require.NoError(t, err)

	agent, err := loop.NewAgent(loop.AgentConfig{
		Store:        store,
		LLM:          llmClient,
		Summarizer:   summarizer,
		MCP:          nil,
		SystemPrompt: "You are personal assistant",
	})
	require.NoError(t, err)

	ctx := context.Background()
	result, err := agent.Run(ctx, loop.Input{Prompt: message.NewHumanMessage("hi")})
	require.NoError(t, err)
	require.Equal(t, "Hello! I am here to help!", result.Response)
}
