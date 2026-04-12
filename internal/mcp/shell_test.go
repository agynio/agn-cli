package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type shellOutput struct {
	ExitCode        int    `json:"exit_code"`
	Stdout          string `json:"stdout"`
	Stderr          string `json:"stderr"`
	OutputTruncated bool   `json:"output_truncated"`
	OutputBytes     int    `json:"output_bytes"`
	OutputFile      string `json:"output_file"`
}

func TestShellToolSuccess(t *testing.T) {
	provider := NewShellToolProvider(ShellToolConfig{})
	result, err := provider.CallTool(context.Background(), ToolCall{
		Name:      ShellToolName,
		Arguments: json.RawMessage(`{"command":"echo hello"}`),
	})
	require.NoError(t, err)

	output := decodeShellOutput(t, result)
	require.Equal(t, 0, output.ExitCode)
	require.Equal(t, "hello\n", output.Stdout)
	require.Equal(t, "", output.Stderr)
}

func TestShellToolNonZeroExit(t *testing.T) {
	provider := NewShellToolProvider(ShellToolConfig{})
	result, err := provider.CallTool(context.Background(), ToolCall{
		Name:      ShellToolName,
		Arguments: json.RawMessage(`{"command":"echo fail 1>&2; exit 3"}`),
	})
	require.NoError(t, err)

	output := decodeShellOutput(t, result)
	require.Equal(t, 3, output.ExitCode)
	require.Equal(t, "", output.Stdout)
	require.Equal(t, "fail\n", output.Stderr)
}

func TestShellToolCwd(t *testing.T) {
	provider := NewShellToolProvider(ShellToolConfig{})
	workdir := t.TempDir()
	result, err := provider.CallTool(context.Background(), ToolCall{
		Name:      ShellToolName,
		Arguments: json.RawMessage(`{"command":"pwd","cwd":"` + workdir + `"}`),
	})
	require.NoError(t, err)

	output := decodeShellOutput(t, result)
	require.Equal(t, strings.TrimSpace(workdir), strings.TrimSpace(output.Stdout))
}

func TestShellToolTimeout(t *testing.T) {
	provider := NewShellToolProvider(ShellToolConfig{Timeout: 1})
	_, err := provider.CallTool(context.Background(), ToolCall{
		Name:      ShellToolName,
		Arguments: json.RawMessage(`{"command":"sleep 2"}`),
	})
	require.Error(t, err)
	require.ErrorContains(t, err, "timeout")
}

func TestShellToolIdleTimeout(t *testing.T) {
	provider := NewShellToolProvider(ShellToolConfig{IdleTimeout: 1})
	_, err := provider.CallTool(context.Background(), ToolCall{
		Name:      ShellToolName,
		Arguments: json.RawMessage(`{"command":"sleep 2","timeout":5}`),
	})
	require.Error(t, err)
	require.ErrorContains(t, err, "idle timeout")
}

func TestShellToolTruncation(t *testing.T) {
	provider := NewShellToolProvider(ShellToolConfig{MaxOutput: 10})
	result, err := provider.CallTool(context.Background(), ToolCall{
		Name:      ShellToolName,
		Arguments: json.RawMessage(`{"command":"printf '1234567890abc'"}`),
	})
	require.NoError(t, err)

	output := decodeShellOutput(t, result)
	require.True(t, output.OutputTruncated)
	require.Equal(t, 13, output.OutputBytes)
	require.Equal(t, "1234567890", output.Stdout)
	require.NotEmpty(t, output.OutputFile)
	t.Cleanup(func() {
		_ = os.Remove(output.OutputFile)
	})
	expectedTempDir, err := filepath.EvalSymlinks(os.TempDir())
	require.NoError(t, err)
	actualTempDir, err := filepath.EvalSymlinks(filepath.Dir(output.OutputFile))
	require.NoError(t, err)
	require.Equal(t, expectedTempDir, actualTempDir)
	require.True(t, strings.HasPrefix(filepath.Base(output.OutputFile), "agn-shell-output-"))

	data, err := os.ReadFile(output.OutputFile)
	require.NoError(t, err)
	require.Equal(t, "1234567890abc", string(data))
}

func TestParseShellArgumentsTimeoutCapping(t *testing.T) {
	tests := []struct {
		name            string
		raw             string
		cfg             ShellToolConfig
		expectedTimeout time.Duration
		expectedIdle    time.Duration
		errContains     string
	}{
		{
			name:            "per-call overrides defaults",
			raw:             `{"command":"echo hi","timeout":2,"idle_timeout":4}`,
			cfg:             ShellToolConfig{Timeout: 5, IdleTimeout: 3},
			expectedTimeout: 2 * time.Second,
			expectedIdle:    4 * time.Second,
		},
		{
			name:            "timeout capped by max",
			raw:             `{"command":"echo hi","timeout":10}`,
			cfg:             ShellToolConfig{Timeout: 1, MaxTimeout: 3},
			expectedTimeout: 3 * time.Second,
			expectedIdle:    0,
		},
		{
			name:            "timeout zero capped by max",
			raw:             `{"command":"echo hi","timeout":0}`,
			cfg:             ShellToolConfig{Timeout: 0, MaxTimeout: 4},
			expectedTimeout: 4 * time.Second,
			expectedIdle:    0,
		},
		{
			name:            "idle timeout capped by max",
			raw:             `{"command":"echo hi","idle_timeout":7}`,
			cfg:             ShellToolConfig{IdleTimeout: 1, MaxIdleTimeout: 2},
			expectedTimeout: 0,
			expectedIdle:    2 * time.Second,
		},
		{
			name:            "idle timeout zero capped by max",
			raw:             `{"command":"echo hi","idle_timeout":0}`,
			cfg:             ShellToolConfig{IdleTimeout: 0, MaxIdleTimeout: 3},
			expectedTimeout: 0,
			expectedIdle:    3 * time.Second,
		},
		{
			name:        "missing command",
			raw:         `{}`,
			cfg:         ShellToolConfig{},
			errContains: "shell command is required",
		},
		{
			name:        "empty command",
			raw:         `{"command":"   "}`,
			cfg:         ShellToolConfig{},
			errContains: "shell command is required",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config, err := parseShellArguments(json.RawMessage(test.raw), test.cfg)
			if test.errContains != "" {
				require.Error(t, err)
				require.ErrorContains(t, err, test.errContains)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.expectedTimeout, config.Timeout)
			require.Equal(t, test.expectedIdle, config.IdleTimeout)
		})
	}
}

func decodeShellOutput(t *testing.T, result ToolResult) shellOutput {
	t.Helper()
	require.Len(t, result.Content, 1)
	item := result.Content[0]
	require.Equal(t, ContentTypeText, item.Type)

	var output shellOutput
	require.NoError(t, json.Unmarshal([]byte(item.Text), &output))
	return output
}
