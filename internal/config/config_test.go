package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadConfigWithAPIKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := []byte(`llm:
  endpoint: https://api.openai.com/v1
  auth:
    api_key: sk-test
  model: gpt-4.1
system_prompt: |
  You are a test agent.
`)
	require.NoError(t, os.WriteFile(path, content, 0o600))

	cfg, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, "https://api.openai.com/v1", cfg.LLM.Endpoint)
	require.Equal(t, "gpt-4.1", cfg.LLM.Model)

	key, err := cfg.LLM.Auth.ResolveAPIKey()
	require.NoError(t, err)
	require.Equal(t, "sk-test", key)
}

func TestLoadConfigWithAPIKeyEnv(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "env-key")
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := []byte(`llm:
  endpoint: https://api.openai.com/v1
  auth:
    api_key_env: OPENAI_API_KEY
  model: gpt-4.1
`)
	require.NoError(t, os.WriteFile(path, content, 0o600))

	cfg, err := Load(path)
	require.NoError(t, err)

	key, err := cfg.LLM.Auth.ResolveAPIKey()
	require.NoError(t, err)
	require.Equal(t, "env-key", key)
}

func TestLoadConfigWithSummarizationLLM(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := []byte(`llm:
  endpoint: https://api.openai.com/v1
  auth:
    api_key: sk-test
  model: gpt-4.1
summarization:
  llm:
    endpoint: https://api.openai.com/v1
    auth:
      api_key: sum-key
    model: gpt-4.1-mini
  keep_tokens: 42
  max_tokens: 100
`)
	require.NoError(t, os.WriteFile(path, content, 0o600))

	cfg, err := Load(path)
	require.NoError(t, err)
	require.NotNil(t, cfg.Summarization.LLM)
	require.Equal(t, 42, cfg.Summarization.KeepTokens)
	require.Equal(t, 100, cfg.Summarization.MaxTokens)
	require.Equal(t, "gpt-4.1-mini", cfg.Summarization.LLM.Model)
}

func TestLoadConfigWithoutSummarization(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := []byte(`llm:
  endpoint: https://api.openai.com/v1
  auth:
    api_key: sk-test
  model: gpt-4.1
`)
	require.NoError(t, os.WriteFile(path, content, 0o600))

	cfg, err := Load(path)
	require.NoError(t, err)
	require.Nil(t, cfg.Summarization.LLM)
	require.Equal(t, 0, cfg.Summarization.KeepTokens)
	require.Equal(t, 0, cfg.Summarization.MaxTokens)
}

func TestLoadConfigWithSummarizationThresholds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := []byte(`llm:
  endpoint: https://api.openai.com/v1
  auth:
    api_key: sk-test
  model: gpt-4.1
summarization:
  keep_tokens: 10
  max_tokens: 20
`)
	require.NoError(t, os.WriteFile(path, content, 0o600))

	cfg, err := Load(path)
	require.NoError(t, err)
	require.Nil(t, cfg.Summarization.LLM)
	require.Equal(t, 10, cfg.Summarization.KeepTokens)
	require.Equal(t, 20, cfg.Summarization.MaxTokens)
}

func TestLoadConfigInvalidSummarizationLLM(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := []byte(`llm:
  endpoint: https://api.openai.com/v1
  auth:
    api_key: sk-test
  model: gpt-4.1
summarization:
  llm:
    endpoint: not a url
    auth:
      api_key: sum-key
    model: gpt-4.1-mini
`)
	require.NoError(t, os.WriteFile(path, content, 0o600))

	_, err := Load(path)
	require.Error(t, err)
	require.ErrorContains(t, err, "summarization.llm.endpoint")
}

func TestLoadConfigWithMCPCommandServer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := []byte(`llm:
  endpoint: https://api.openai.com/v1
  auth:
    api_key: sk-test
  model: gpt-4.1
mcp:
  servers:
    weather:
      command: /bin/echo
      args:
        - hello
      env:
        SAMPLE_KEY: sample-value
`)
	require.NoError(t, os.WriteFile(path, content, 0o600))

	cfg, err := Load(path)
	require.NoError(t, err)
	require.Contains(t, cfg.MCP.Servers, "weather")
}

func TestLoadConfigWithMCPURLServer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := []byte(`llm:
  endpoint: https://api.openai.com/v1
  auth:
    api_key: sk-test
  model: gpt-4.1
mcp:
  servers:
    tools:
      url: https://mcp.local
`)
	require.NoError(t, os.WriteFile(path, content, 0o600))

	cfg, err := Load(path)
	require.NoError(t, err)
	require.Contains(t, cfg.MCP.Servers, "tools")
}

func TestLoadConfigWithInvalidMCPServerName(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := []byte(`llm:
  endpoint: https://api.openai.com/v1
  auth:
    api_key: sk-test
  model: gpt-4.1
mcp:
  servers:
    BadName:
      command: /bin/echo
`)
	require.NoError(t, os.WriteFile(path, content, 0o600))

	_, err := Load(path)
	require.Error(t, err)
	require.ErrorContains(t, err, "mcp.servers.BadName name is invalid")
}

func TestLoadConfigWithMCPCommandAndURL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := []byte(`llm:
  endpoint: https://api.openai.com/v1
  auth:
    api_key: sk-test
  model: gpt-4.1
mcp:
  servers:
    weather:
      command: /bin/echo
      url: https://mcp.local
`)
	require.NoError(t, os.WriteFile(path, content, 0o600))

	_, err := Load(path)
	require.Error(t, err)
	require.ErrorContains(t, err, "exactly one of command or url is required")
}

func TestLoadConfigWithMCPURLAndArgs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := []byte(`llm:
  endpoint: https://api.openai.com/v1
  auth:
    api_key: sk-test
  model: gpt-4.1
mcp:
  servers:
    tools:
      url: https://mcp.local
      args:
        - hello
`)
	require.NoError(t, os.WriteFile(path, content, 0o600))

	_, err := Load(path)
	require.Error(t, err)
	require.ErrorContains(t, err, "args are only valid with command")
}
