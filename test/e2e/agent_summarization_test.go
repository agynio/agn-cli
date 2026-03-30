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

type summarizationTestEnv struct {
	home  string
	turn1 []string
	turn2 []string
}

func TestSummarization(t *testing.T) {
	env := newSummarizationTestEnv(t)
	binary := buildAgnBinary(t)
	threadID := "thread-summarize"

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	stdout, stderr := runAgnWithContext(t, ctx, binary, env.turn1, "exec", "--thread-id", threadID, summarizePrompt)
	require.Equal(t, summarizeReply, strings.TrimSpace(stdout))
	require.Equal(t, threadID, parseThreadID(t, stderr))

	stdout, stderr = runAgnWithContext(t, ctx, binary, env.turn2, "exec", "resume", threadID, followupPrompt)
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

func newSummarizationTestEnv(t *testing.T) summarizationTestEnv {
	t.Helper()
	home := t.TempDir()

	summarizationLLM := &config.LLMConfig{
		Endpoint: testLLMEndpoint,
		Auth:     config.AuthConfig{APIKey: "dummy"},
		Model:    "summarize-history",
	}
	summarizationCfg := config.SummarizationConfig{
		LLM:        summarizationLLM,
		KeepTokens: 4,
		MaxTokens:  90,
	}

	turn1Path := filepath.Join(home, "config-turn1.yaml")
	turn1Config := config.Config{
		LLM: config.LLMConfig{
			Endpoint: testLLMEndpoint,
			Auth:     config.AuthConfig{APIKey: "dummy"},
			Model:    "summarize-agent-turn1",
		},
		Summarization: summarizationCfg,
	}
	turn1Payload, err := yaml.Marshal(turn1Config)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(turn1Path, turn1Payload, 0o600))

	turn2Path := filepath.Join(home, "config-turn2.yaml")
	turn2Config := config.Config{
		LLM: config.LLMConfig{
			Endpoint: testLLMEndpoint,
			Auth:     config.AuthConfig{APIKey: "dummy"},
			Model:    "summarize-agent-turn2",
		},
		Summarization: summarizationCfg,
	}
	turn2Payload, err := yaml.Marshal(turn2Config)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(turn2Path, turn2Payload, 0o600))

	base := append(os.Environ(), "HOME="+home, "AGN_MCP_COMMAND=")
	return summarizationTestEnv{
		home:  home,
		turn1: append(append([]string{}, base...), "AGN_CONFIG_PATH="+turn1Path),
		turn2: append(append([]string{}, base...), "AGN_CONFIG_PATH="+turn2Path),
	}
}
