package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	require.Equal(t, os.TempDir(), filepath.Dir(output.OutputFile))
	require.True(t, strings.HasPrefix(filepath.Base(output.OutputFile), "agn-shell-output-"))

	data, err := os.ReadFile(output.OutputFile)
	require.NoError(t, err)
	require.Equal(t, "1234567890abc", string(data))
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
