//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/agynio/agn-cli/internal/config"
	"github.com/agynio/agn-cli/internal/message"
)

const (
	summarizePrompt = "Tell me about the history of computing in detail"
	summarizeReply  = "Computing began with Charles Babbage who designed the Analytical Engine in the 1830s. Ada Lovelace wrote the first algorithm. Alan Turing formalized computation in 1936. ENIAC was built in 1945. The transistor was invented in 1947 at Bell Labs. Integrated circuits followed in the late 1950s."
	followupPrompt  = "What came next?"
	followupReply   = "After integrated circuits came microprocessors and personal computers."
)

func TestSummarization(t *testing.T) {
	env := newSummarizationTestEnv(t)
	binary := buildAgnBinary(t)
	threadID := "thread-summarize"

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	stdout, stderr := runAgnWithContext(t, ctx, binary, env.env, "exec", "--thread-id", threadID, summarizePrompt)
	require.Equal(t, summarizeReply, strings.TrimSpace(stdout))
	require.Equal(t, threadID, parseThreadID(t, stderr))

	stdout, stderr = runAgnWithContext(t, ctx, binary, env.env, "exec", "resume", threadID, followupPrompt)
	require.Equal(t, followupReply, strings.TrimSpace(stdout))
	require.Equal(t, threadID, parseThreadID(t, stderr))

	statePath := filepath.Join(env.home, ".agyn", "agn", "threads", threadID+".json")
	data, err := os.ReadFile(statePath)
	require.NoError(t, err)

	var persisted struct {
		Messages []struct {
			TokenCount int `json:"token_count"`
			Message    struct {
				Role string `json:"role"`
				Kind string `json:"kind"`
				Text string `json:"text"`
			} `json:"message"`
		} `json:"messages"`
	}
	require.NoError(t, json.Unmarshal(data, &persisted))
	require.Len(t, persisted.Messages, 3)

	summary := persisted.Messages[0]
	require.Equal(t, string(message.KindSummary), summary.Message.Kind)
	require.Equal(t, string(message.RoleSystem), summary.Message.Role)
	require.NotEmpty(t, strings.TrimSpace(summary.Message.Text))
	require.Greater(t, summary.TokenCount, 0)

	user := persisted.Messages[1]
	require.Equal(t, string(message.KindHuman), user.Message.Kind)
	require.Equal(t, string(message.RoleUser), user.Message.Role)
	require.Equal(t, followupPrompt, user.Message.Text)

	assistant := persisted.Messages[2]
	require.Equal(t, string(message.KindAI), assistant.Message.Kind)
	require.Equal(t, string(message.RoleAssistant), assistant.Message.Role)
}

func newSummarizationTestEnv(t *testing.T) testEnv {
	t.Helper()
	home := t.TempDir()
	configPath := filepath.Join(home, "config.yaml")
	configData := config.Config{
		LLM: config.LLMConfig{
			Endpoint: testLLMEndpoint,
			Auth:     config.AuthConfig{APIKey: "dummy"},
			Model:    "summarize-agent",
		},
		Summarization: config.SummarizationConfig{
			LLM: &config.LLMConfig{
				Endpoint: testLLMEndpoint,
				Auth:     config.AuthConfig{APIKey: "dummy"},
				Model:    "summarize-history",
			},
			KeepTokens: 4,
			MaxTokens:  90,
		},
	}
	payload, err := yaml.Marshal(configData)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(configPath, payload, 0o600))
	env := append(os.Environ(), "HOME="+home, "AGN_CONFIG_PATH="+configPath, "AGN_MCP_COMMAND=")
	return testEnv{home: home, env: env}
}
