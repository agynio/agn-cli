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
