//go:build e2e

package e2e

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/agynio/agn-cli/internal/config"
)

const (
	toolPairPrompt       = "What is the weather in Paris right now please?"
	toolPairTurn1Reply   = "The weather in Paris is currently 18\u00b0C and partly cloudy."
	toolPairFollowup     = "thanks"
	toolPairTurn2Reply   = "You're welcome!"
	toolPairKeepTokens   = 30
	toolPairMaxTokens    = 50
	toolPairThreadID     = "thread-tool-pair"
	toolPairTurn1Model   = "summarize-tool-pair-turn1"
	toolPairTurn2Model   = "summarize-tool-pair-turn2"
	toolPairHistoryModel = "summarize-tool-pair-history"
)

type toolPairTestEnv struct {
	turn1 []string
	turn2 []string
}

func TestSummarizationToolPair(t *testing.T) {
	mcpBinary := buildMCPWeatherServer(t)
	env := newToolPairTestEnv(t, mcpBinary)
	binary := buildAgnBinary(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	stdout, stderr := runAgnWithContext(t, ctx, binary, env.turn1, "exec", "--thread-id", toolPairThreadID, toolPairPrompt)
	require.Equal(t, toolPairTurn1Reply, strings.TrimSpace(stdout))
	require.Equal(t, toolPairThreadID, parseThreadID(t, stderr))

	stdout, stderr = runAgnWithContext(t, ctx, binary, env.turn2, "exec", "resume", toolPairThreadID, toolPairFollowup)
	require.Equal(t, toolPairTurn2Reply, strings.TrimSpace(stdout))
	require.Equal(t, toolPairThreadID, parseThreadID(t, stderr))
}

func buildMCPWeatherServer(t *testing.T) string {
	t.Helper()
	repoRoot, err := repoRoot()
	require.NoError(t, err)

	buildDir := t.TempDir()
	binary := filepath.Join(buildDir, "mcp-weather-server")
	cmd := exec.Command("go", "build", "-o", binary, "./test/e2e/testdata/mcp_weather_server.go")
	cmd.Dir = repoRoot
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "build mcp weather server: %s", strings.TrimSpace(string(output)))
	return binary
}

func newToolPairTestEnv(t *testing.T, mcpCommand string) toolPairTestEnv {
	t.Helper()
	home := t.TempDir()

	summarizationLLM := &config.LLMConfig{
		Endpoint: testLLMEndpoint,
		Auth:     config.AuthConfig{APIKey: "dummy"},
		Model:    toolPairHistoryModel,
	}
	summarizationCfg := config.SummarizationConfig{
		LLM:        summarizationLLM,
		KeepTokens: toolPairKeepTokens,
		MaxTokens:  toolPairMaxTokens,
	}

	turn1Path := filepath.Join(home, "config-tool-pair-turn1.yaml")
	turn1Config := config.Config{
		LLM: config.LLMConfig{
			Endpoint: testLLMEndpoint,
			Auth:     config.AuthConfig{APIKey: "dummy"},
			Model:    toolPairTurn1Model,
		},
		Summarization: summarizationCfg,
	}
	turn1Payload, err := yaml.Marshal(turn1Config)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(turn1Path, turn1Payload, 0o600))

	turn2Path := filepath.Join(home, "config-tool-pair-turn2.yaml")
	turn2Config := config.Config{
		LLM: config.LLMConfig{
			Endpoint: testLLMEndpoint,
			Auth:     config.AuthConfig{APIKey: "dummy"},
			Model:    toolPairTurn2Model,
		},
		Summarization: summarizationCfg,
	}
	turn2Payload, err := yaml.Marshal(turn2Config)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(turn2Path, turn2Payload, 0o600))

	base := append(os.Environ(), "HOME="+home, "AGN_MCP_COMMAND="+mcpCommand)
	return toolPairTestEnv{
		turn1: append(append([]string{}, base...), "AGN_CONFIG_PATH="+turn1Path),
		turn2: append(append([]string{}, base...), "AGN_CONFIG_PATH="+turn2Path),
	}
}
