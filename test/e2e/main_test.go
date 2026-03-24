//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	testEndpoint     = "https://testllm.dev/v1/org/agynio/suite/agn"
	testModel        = "simple-hello"
	testAPIKey       = "test-key"
	expectedResponse = "Hi! How are you?"
)

func TestAgnExecHello(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")
	config := fmt.Sprintf(`llm:
  endpoint: %s
  auth:
    api_key: %s
  model: %s
`, testEndpoint, testAPIKey, testModel)
	require.NoError(t, os.WriteFile(configPath, []byte(config), 0o600))

	repoRoot, err := resolveRepoRoot()
	require.NoError(t, err)

	binPath := filepath.Join(tempDir, "agn")
	buildCmd := exec.CommandContext(ctx, "go", "build", "-o", binPath, "./cmd/agn")
	buildCmd.Dir = repoRoot
	buildOutput, err := buildCmd.CombinedOutput()
	require.NoError(t, err, "go build failed: %s", strings.TrimSpace(string(buildOutput)))

	execCmd := exec.CommandContext(ctx, binPath, "exec", "hi")
	execCmd.Env = append(filteredEnv(os.Environ(), "HOME", "AGN_CONFIG_PATH", "AGN_MCP_COMMAND"),
		"HOME="+tempDir,
		"AGN_CONFIG_PATH="+configPath,
		"AGN_MCP_COMMAND=",
	)
	output, err := execCmd.CombinedOutput()
	require.NoError(t, err, "agn exec failed: %s", strings.TrimSpace(string(output)))

	require.Equal(t, expectedResponse, strings.TrimSpace(string(output)))
}

func resolveRepoRoot() (string, error) {
	workDir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return filepath.Abs(filepath.Join(workDir, "../.."))
}

func filteredEnv(env []string, keys ...string) []string {
	filtered := make([]string, 0, len(env))
	for _, entry := range env {
		if shouldSkipEnv(entry, keys) {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
}

func shouldSkipEnv(entry string, keys []string) bool {
	for _, key := range keys {
		if strings.HasPrefix(entry, key+"=") {
			return true
		}
	}
	return false
}
