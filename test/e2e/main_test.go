//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/agynio/agn-cli/internal/config"
)

const testLLMEndpoint = "https://testllm.dev/v1/org/agynio/suite/agn"

type testEnv struct {
	home string
	env  []string
}

var (
	buildOnce sync.Once
	buildErr  error
	agnBinary string
)

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}

func TestAgnExecHello(t *testing.T) {
	env := newTestEnv(t, "simple-hello", "")
	binary := buildAgnBinary(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	stdout, stderr := runAgnWithContext(t, ctx, binary, env.env, "exec", "hi")
	require.Equal(t, "Hi! How are you?", strings.TrimSpace(stdout))
	_ = parseThreadID(t, stderr)
}

func TestExecStatePersistence(t *testing.T) {
	env := newTestEnv(t, "simple-state", "")
	binary := buildAgnBinary(t)
	threadID := "thread-test"

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	stdout, stderr := runAgnWithContext(t, ctx, binary, env.env, "exec", "--thread-id", threadID, "hi")
	require.Equal(t, "Hi! How are you?", strings.TrimSpace(stdout))
	require.Equal(t, threadID, parseThreadID(t, stderr))

	stdout, stderr = runAgnWithContext(t, ctx, binary, env.env, "exec", "--thread-id", threadID, "fine")
	require.Equal(t, "How can I help you?", strings.TrimSpace(stdout))
	require.Equal(t, threadID, parseThreadID(t, stderr))

	statePath := filepath.Join(env.home, ".agyn", "agn", "threads", threadID+".json")
	data, err := os.ReadFile(statePath)
	require.NoError(t, err)
	var persisted struct {
		Messages []json.RawMessage `json:"messages"`
	}
	require.NoError(t, json.Unmarshal(data, &persisted))
	require.Len(t, persisted.Messages, 4)
}

func TestExecResume(t *testing.T) {
	env := newTestEnv(t, "simple-state", "")
	binary := buildAgnBinary(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	stdout, stderr := runAgnWithContext(t, ctx, binary, env.env, "exec", "hi")
	require.Equal(t, "Hi! How are you?", strings.TrimSpace(stdout))
	threadID := parseThreadID(t, stderr)

	stdout, stderr = runAgnWithContext(t, ctx, binary, env.env, "exec", "resume", threadID, "fine")
	require.Equal(t, "How can I help you?", strings.TrimSpace(stdout))
	require.Equal(t, threadID, parseThreadID(t, stderr))
}

func newTestEnv(t *testing.T, model string, systemPrompt string) testEnv {
	t.Helper()
	home := t.TempDir()
	configPath := filepath.Join(home, "config.yaml")
	configData := config.Config{
		LLM: config.LLMConfig{
			Endpoint: testLLMEndpoint,
			Auth:     config.AuthConfig{APIKey: "dummy"},
			Model:    model,
		},
		TokenCounting: config.TokenCountingConfig{
			Address: tokenCountingAddress(t),
		},
	}
	if strings.TrimSpace(systemPrompt) != "" {
		configData.SystemPrompt = systemPrompt
	}
	payload, err := yaml.Marshal(configData)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(configPath, payload, 0o600))
	env := append(os.Environ(), "HOME="+home, "AGN_CONFIG_PATH="+configPath)
	return testEnv{home: home, env: env}
}

func buildAgnBinary(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		repoRoot, err := repoRoot()
		if err != nil {
			buildErr = err
			return
		}
		dir, err := os.MkdirTemp("", "agn-e2e-")
		if err != nil {
			buildErr = err
			return
		}
		agnBinary = filepath.Join(dir, "agn")
		cmd := exec.Command("go", "build", "-o", agnBinary, "./cmd/agn")
		cmd.Dir = repoRoot
		output, err := cmd.CombinedOutput()
		if err != nil {
			buildErr = fmt.Errorf("build agn: %w: %s", err, strings.TrimSpace(string(output)))
		}
	})
	if buildErr != nil {
		t.Fatalf("build agn: %v", buildErr)
	}
	return agnBinary
}

func repoRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return filepath.Clean(filepath.Join(wd, "..", "..")), nil
}

func runAgnWithContext(t *testing.T, ctx context.Context, binary string, env []string, args ...string) (string, string) {
	t.Helper()
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Env = env
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("run agn %v: %v\nstdout: %s\nstderr: %s", args, err, stdout.String(), stderr.String())
	}
	return stdout.String(), stderr.String()
}

func parseThreadID(t *testing.T, stderr string) string {
	t.Helper()
	for _, line := range strings.Split(stderr, "\n") {
		if strings.HasPrefix(line, "thread_id:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "thread_id:"))
		}
	}
	t.Fatalf("thread_id not found in stderr: %q", stderr)
	return ""
}
