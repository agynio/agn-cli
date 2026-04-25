//go:build e2e

package e2e

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/agynio/agn-cli/internal/config"
)

const (
	summarizePrompt = "Tell me about the history of computing in detail"
	summarizeReply  = "Computing began with Charles Babbage who designed the Analytical Engine in the 1830s. Ada Lovelace wrote the first algorithm. Alan Turing formalized computation in 1936. ENIAC was built in 1945. The transistor was invented in 1947 at Bell Labs. Integrated circuits followed in the late 1950s."
	followupPrompt  = "What came next?"
	followupReply   = "After integrated circuits came microprocessors and personal computers."
)

type summarizationTestEnv struct {
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

	base := append(os.Environ(), "HOME="+home)
	return summarizationTestEnv{
		turn1: append(append([]string{}, base...), "AGN_CONFIG_PATH="+turn1Path),
		turn2: append(append([]string{}, base...), "AGN_CONFIG_PATH="+turn2Path),
	}
}
