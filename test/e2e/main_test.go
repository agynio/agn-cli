//go:build e2e

package e2e

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/agynio/agn-cli/internal/llm"
	"github.com/agynio/agn-cli/internal/loop"
	"github.com/agynio/agn-cli/internal/message"
	"github.com/agynio/agn-cli/internal/state"
	"github.com/agynio/agn-cli/internal/summarize"
	"github.com/openai/openai-go/v3/responses"
)

const (
	testEndpoint = "https://testllm.dev/v1/org/agynio/suite/codex"
	testModel    = "simple-hello"
	testAPIKey   = "test-key"
)

func TestAgentHelloResponse(t *testing.T) {
	ctx, cancel := newTestContext(t)
	defer cancel()

	client := newTestClient(t)
	summarizer := newTestSummarizer(t, client)
	store := newTestStore(t)
	agent := newTestAgent(t, store, client, summarizer)

	result, err := agent.Run(ctx, loop.Input{
		Prompt: message.NewHumanMessage("hello"),
		Stream: false,
	})
	require.NoError(t, err)
	require.NotEmpty(t, result.ConversationID)
	require.Equal(t, "Hi! How are you?", result.Response)
}

func TestLLMClientDirect(t *testing.T) {
	ctx, cancel := newTestContext(t)
	defer cancel()

	client := newTestClient(t)
	inputs, err := llm.MessagesToInput([]message.Message{message.NewHumanMessage("hello")})
	require.NoError(t, err)

	response, err := client.CreateResponse(
		ctx,
		"",
		inputs,
		nil,
		responses.ResponseNewParamsToolChoiceUnion{},
		false,
		nil,
	)
	require.NoError(t, err)
	require.NotNil(t, response)
	require.NotEmpty(t, response.ID)
	require.Equal(t, "Hi! How are you?", strings.TrimSpace(response.OutputText()))
}

func TestStatePersistence(t *testing.T) {
	ctx, cancel := newTestContext(t)
	defer cancel()

	client := newTestClient(t)
	summarizer := newTestSummarizer(t, client)
	store := newTestStore(t)
	agent := newTestAgent(t, store, client, summarizer)

	result, err := agent.Run(ctx, loop.Input{
		Prompt: message.NewHumanMessage("hello"),
		Stream: false,
	})
	require.NoError(t, err)
	require.NotEmpty(t, result.ConversationID)

	conversation, err := store.Load(ctx, result.ConversationID)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(conversation.Messages), 2, "expected at least user prompt and assistant response")
}

func newTestContext(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(context.Background(), 30*time.Second)
}

func newTestClient(t *testing.T) *llm.Client {
	t.Helper()
	client, err := llm.NewClient(testEndpoint, testAPIKey, testModel)
	require.NoError(t, err)
	return client
}

func newTestSummarizer(t *testing.T, client *llm.Client) *summarize.Summarizer {
	t.Helper()
	summarizer, err := summarize.New(client, summarize.Config{})
	require.NoError(t, err)
	return summarizer
}

func newTestStore(t *testing.T) *state.LocalStore {
	t.Helper()
	store, err := state.NewLocalStore(t.TempDir())
	require.NoError(t, err)
	return store
}

func newTestAgent(t *testing.T, store state.Store, client *llm.Client, summarizer *summarize.Summarizer) *loop.Agent {
	t.Helper()
	agent, err := loop.NewAgent(loop.AgentConfig{
		Store:        store,
		LLM:          client,
		Summarizer:   summarizer,
		MCP:          nil,
		SystemPrompt: "",
	})
	require.NoError(t, err)
	return agent
}
