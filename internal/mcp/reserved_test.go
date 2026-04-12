package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReservedToolProviderRejectsReservedName(t *testing.T) {
	provider := &fakeProvider{tools: []Tool{{Name: ShellToolName}}}
	wrapper := NewReservedToolProvider(provider, []string{ShellToolName})

	_, err := wrapper.ListTools(context.Background())
	require.Error(t, err)
	require.ErrorContains(t, err, "reserved")
}

func TestReservedToolProviderAllowsOtherNames(t *testing.T) {
	provider := &fakeProvider{tools: []Tool{{Name: "other"}}}
	wrapper := NewReservedToolProvider(provider, []string{ShellToolName})

	tools, err := wrapper.ListTools(context.Background())
	require.NoError(t, err)
	require.Equal(t, []Tool{{Name: "other"}}, tools)
}
