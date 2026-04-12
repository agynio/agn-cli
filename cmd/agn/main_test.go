package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/agynio/agn-cli/internal/config"
	"github.com/agynio/agn-cli/internal/loop"
	"github.com/agynio/agn-cli/internal/mcp"
)

func TestResolveMaxStepsDefault(t *testing.T) {
	maxSteps := resolveMaxSteps(config.Config{})
	require.Equal(t, loop.DefaultMaxSteps, maxSteps)
}

func TestResolveMaxStepsConfig(t *testing.T) {
	value := 321
	cfg := config.Config{Loop: config.LoopConfig{MaxSteps: &value}}
	maxSteps := resolveMaxSteps(cfg)
	require.Equal(t, value, maxSteps)
}

func TestNewToolProviderShellEnabled(t *testing.T) {
	provider, err := newToolProvider(context.Background(), config.ToolsConfig{}, config.MCPConfig{})
	require.NoError(t, err)
	require.NotNil(t, provider)

	tools, err := provider.ListTools(context.Background())
	require.NoError(t, err)
	require.Len(t, tools, 1)
	require.Equal(t, mcp.ShellToolName, tools[0].Name)
}

func TestNewToolProviderShellDisabled(t *testing.T) {
	disabled := false
	provider, err := newToolProvider(
		context.Background(),
		config.ToolsConfig{Shell: config.ShellToolConfig{Enabled: &disabled}},
		config.MCPConfig{},
	)
	require.NoError(t, err)
	require.Nil(t, provider)
}
