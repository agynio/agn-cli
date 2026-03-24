//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

type testEnv struct {
	home string
	env  []string
}

var (
	buildOnce sync.Once
	buildErr  error
	agnBinary string
)

func TestAgnExecHello(t *testing.T) {
	server := newStubServer(t)
	defer server.Close()

	env := newTestEnv(t, server.URL)
	binary := buildAgnBinary(t)

	stdout, stderr := runAgn(t, binary, env.env, "exec", "hi")
	if strings.TrimSpace(stdout) != "echo: hi" {
		t.Fatalf("expected hello response, got %q", stdout)
	}
	threadID := parseThreadID(t, stderr)
	if threadID == "" {
		t.Fatalf("expected thread_id in stderr, got %q", stderr)
	}
}

func TestExecStatePersistence(t *testing.T) {
	server := newStubServer(t)
	defer server.Close()

	env := newTestEnv(t, server.URL)
	binary := buildAgnBinary(t)
	threadID := "thread-test"

	stdout, stderr := runAgn(t, binary, env.env, "exec", "--thread-id", threadID, "hello")
	if strings.TrimSpace(stdout) != "echo: hello" {
		t.Fatalf("expected hello response, got %q", stdout)
	}
	if parseThreadID(t, stderr) != threadID {
		t.Fatalf("expected thread_id %q in stderr, got %q", threadID, stderr)
	}
	statePath := filepath.Join(env.home, ".agyn", "agn", "threads", threadID+".json")
	if _, err := os.Stat(statePath); err != nil {
		t.Fatalf("expected thread file to exist: %v", err)
	}

	stdout, stderr = runAgn(t, binary, env.env, "exec", "--thread-id", threadID, "again")
	if strings.TrimSpace(stdout) != "echo: again" {
		t.Fatalf("expected again response, got %q", stdout)
	}
	if parseThreadID(t, stderr) != threadID {
		t.Fatalf("expected thread_id %q in stderr, got %q", threadID, stderr)
	}
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read thread file: %v", err)
	}
	var persisted struct {
		Messages []json.RawMessage `json:"messages"`
	}
	if err := json.Unmarshal(data, &persisted); err != nil {
		t.Fatalf("parse thread file: %v", err)
	}
	if len(persisted.Messages) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(persisted.Messages))
	}
}

func TestExecResume(t *testing.T) {
	server := newStubServer(t)
	defer server.Close()

	env := newTestEnv(t, server.URL)
	binary := buildAgnBinary(t)

	_, stderr := runAgn(t, binary, env.env, "exec", "hi")
	threadID := parseThreadID(t, stderr)

	stdout, stderr := runAgn(t, binary, env.env, "exec", "resume", threadID, "fine")
	if strings.TrimSpace(stdout) != "echo: fine" {
		t.Fatalf("expected fine response, got %q", stdout)
	}
	if parseThreadID(t, stderr) != threadID {
		t.Fatalf("expected thread_id %q in stderr, got %q", threadID, stderr)
	}
}

func newStubServer(t *testing.T) *httptest.Server {
	t.Helper()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req responseRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		prompt := lastUserPrompt(req.Input)
		response := map[string]any{
			"id": "resp-id",
			"output": []any{
				map[string]any{
					"type": "message",
					"role": "assistant",
					"content": []any{
						map[string]any{"type": "output_text", "text": fmt.Sprintf("echo: %s", prompt)},
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
		}
	})
	return httptest.NewServer(handler)
}

type responseRequest struct {
	Input json.RawMessage `json:"input"`
}

func lastUserPrompt(input json.RawMessage) string {
	var messages []map[string]any
	if err := json.Unmarshal(input, &messages); err != nil {
		return ""
	}
	for i := len(messages) - 1; i >= 0; i-- {
		role, _ := messages[i]["role"].(string)
		if role != "user" {
			continue
		}
		if text := extractInputText(messages[i]["content"]); text != "" {
			return text
		}
	}
	return ""
}

func extractInputText(content any) string {
	switch typed := content.(type) {
	case string:
		return strings.TrimSpace(typed)
	case []any:
		for _, entry := range typed {
			if text := extractInputText(entry); text != "" {
				return text
			}
		}
	case map[string]any:
		if text, ok := typed["text"].(string); ok {
			return strings.TrimSpace(text)
		}
		if text, ok := typed["input_text"].(string); ok {
			return strings.TrimSpace(text)
		}
		if nested, ok := typed["content"]; ok {
			return extractInputText(nested)
		}
	}
	return ""
}

func newTestEnv(t *testing.T, endpoint string) testEnv {
	t.Helper()
	home := t.TempDir()
	configPath := filepath.Join(home, "config.yaml")
	config := fmt.Sprintf("llm:\n  endpoint: %s\n  model: test-model\n  auth:\n    api_key: test-key\n", endpoint)
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	env := append(os.Environ(), "HOME="+home, "AGN_CONFIG_PATH="+configPath, "AGN_MCP_COMMAND=")
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

func runAgn(t *testing.T, binary string, env []string, args ...string) (string, string) {
	t.Helper()
	cmd := exec.Command(binary, args...)
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
