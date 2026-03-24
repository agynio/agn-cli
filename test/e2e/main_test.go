//go:build e2e

package e2e

import (
	"bytes"
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
	testAPIKey       = "test-key"
	helloModel       = "simple-hello"
	stateModel       = "simple-state"
	helloResponse    = "Hi! How are you?"
	followUpResponse = "How can I help you?"
)

func TestAgnExecHello(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	tempDir := t.TempDir()
	configPath := writeConfig(t, tempDir, helloModel)
	repoRoot := resolveRepoRoot(t)

	binPath := buildBinary(t, ctx, repoRoot, tempDir)
	stdout, stderr, err := runAgnExec(ctx, binPath, tempDir, configPath, "hi", "")
	require.NoError(t, err, "agn exec failed: stdout=%q stderr=%q", strings.TrimSpace(stdout), strings.TrimSpace(stderr))
	require.Equal(t, helloResponse, strings.TrimSpace(stdout))
	_ = parseConversationID(t, stderr)
}

func TestExecStatePersistence(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	homeDir := t.TempDir()
	configPath := writeConfig(t, homeDir, stateModel)
	repoRoot := resolveRepoRoot(t)

	binPath := buildBinary(t, ctx, repoRoot, homeDir)
	stdout, stderr, err := runAgnExec(ctx, binPath, homeDir, configPath, "hi", "")
	require.NoError(t, err, "agn exec failed: stdout=%q stderr=%q", strings.TrimSpace(stdout), strings.TrimSpace(stderr))
	require.Equal(t, helloResponse, strings.TrimSpace(stdout))

	conversationID := parseConversationID(t, stderr)
	stdout, stderr, err = runAgnExec(ctx, binPath, homeDir, configPath, "fine", conversationID)
	require.NoError(t, err, "agn exec failed: stdout=%q stderr=%q", strings.TrimSpace(stdout), strings.TrimSpace(stderr))
	require.Equal(t, followUpResponse, strings.TrimSpace(stdout))
}

func writeConfig(t *testing.T, dir, model string) string {
	t.Helper()
	path := filepath.Join(dir, "config.yaml")
	config := fmt.Sprintf(`llm:
  endpoint: %s
  auth:
    api_key: %s
  model: %s
`, testEndpoint, testAPIKey, model)
	require.NoError(t, os.WriteFile(path, []byte(config), 0o600))
	return path
}

func resolveRepoRoot(t *testing.T) string {
	t.Helper()
	workDir, err := os.Getwd()
	require.NoError(t, err)
	root, err := filepath.Abs(filepath.Join(workDir, "../.."))
	require.NoError(t, err)
	return root
}

func buildBinary(t *testing.T, ctx context.Context, repoRoot, dir string) string {
	t.Helper()
	binPath := filepath.Join(dir, "agn")
	buildCmd := exec.CommandContext(ctx, "go", "build", "-o", binPath, "./cmd/agn")
	buildCmd.Dir = repoRoot
	buildOutput, err := buildCmd.CombinedOutput()
	require.NoError(t, err, "go build failed: %s", strings.TrimSpace(string(buildOutput)))
	return binPath
}

func runAgnExec(ctx context.Context, binPath, homeDir, configPath, prompt, conversationID string) (string, string, error) {
	args := []string{"exec", prompt}
	if strings.TrimSpace(conversationID) != "" {
		args = append(args, "--conversation-id", conversationID)
	}
	cmd := exec.CommandContext(ctx, binPath, args...)
	cmd.Env = append(filteredEnv(os.Environ(), "HOME", "AGN_CONFIG_PATH", "AGN_MCP_COMMAND"),
		"HOME="+homeDir,
		"AGN_CONFIG_PATH="+configPath,
		"AGN_MCP_COMMAND=",
	)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

func parseConversationID(t *testing.T, stderr string) string {
	t.Helper()
	lines := strings.Split(stderr, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "conversation_id:") {
			id := strings.TrimSpace(strings.TrimPrefix(line, "conversation_id:"))
			require.NotEmpty(t, id, "conversation id is empty")
			return id
		}
	}
	require.Fail(t, "conversation id not found", "stderr: %s", stderr)
	return ""
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
