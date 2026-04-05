package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type fakeProvider struct {
	tools       []Tool
	callResults map[string]ToolResult
	listErr     error
	callErr     error
	closeErr    error
	calls       []ToolCall
}

func (f *fakeProvider) ListTools(ctx context.Context) ([]Tool, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.tools, nil
}

func (f *fakeProvider) CallTool(ctx context.Context, call ToolCall) (ToolResult, error) {
	f.calls = append(f.calls, call)
	if f.callErr != nil {
		return ToolResult{}, f.callErr
	}
	result, ok := f.callResults[call.Name]
	if !ok {
		return ToolResult{}, errors.New("unexpected tool")
	}
	return result, nil
}

func (f *fakeProvider) Close() error {
	return f.closeErr
}

func TestMultiClientListTools(t *testing.T) {
	providerOne := &fakeProvider{tools: []Tool{{Name: "one"}}}
	providerTwo := &fakeProvider{tools: []Tool{{Name: "two"}}}

	client, err := NewMultiClient([]ToolProvider{providerOne, providerTwo})
	require.NoError(t, err)

	tools, err := client.ListTools(context.Background())
	require.NoError(t, err)
	require.Equal(t, []Tool{{Name: "one"}, {Name: "two"}}, tools)
}

func TestMultiClientCallToolRoutesToProvider(t *testing.T) {
	providerOne := &fakeProvider{
		tools:       []Tool{{Name: "one"}},
		callResults: map[string]ToolResult{"one": {Content: []ContentItem{{Type: ContentTypeText, Text: "first"}}}},
	}
	providerTwo := &fakeProvider{
		tools:       []Tool{{Name: "two"}},
		callResults: map[string]ToolResult{"two": {Content: []ContentItem{{Type: ContentTypeText, Text: "second"}}}},
	}

	client, err := NewMultiClient([]ToolProvider{providerOne, providerTwo})
	require.NoError(t, err)

	_, err = client.ListTools(context.Background())
	require.NoError(t, err)

	result, err := client.CallTool(context.Background(), ToolCall{
		ID:        "call-2",
		Name:      "two",
		Arguments: json.RawMessage(`{"value":2}`),
	})
	require.NoError(t, err)
	require.Equal(t, ToolResult{Content: []ContentItem{{Type: ContentTypeText, Text: "second"}}}, result)
	require.Len(t, providerOne.calls, 0)
	require.Len(t, providerTwo.calls, 1)
}

func TestMultiClientDuplicateToolName(t *testing.T) {
	providerOne := &fakeProvider{tools: []Tool{{Name: "dup"}}}
	providerTwo := &fakeProvider{tools: []Tool{{Name: "dup"}}}

	client, err := NewMultiClient([]ToolProvider{providerOne, providerTwo})
	require.NoError(t, err)

	_, err = client.ListTools(context.Background())
	require.Error(t, err)
	require.ErrorContains(t, err, "duplicate tool name")
}

func TestMultiClientUnknownToolName(t *testing.T) {
	providerOne := &fakeProvider{tools: []Tool{{Name: "one"}}}

	client, err := NewMultiClient([]ToolProvider{providerOne})
	require.NoError(t, err)

	_, err = client.ListTools(context.Background())
	require.NoError(t, err)

	_, err = client.CallTool(context.Background(), ToolCall{Name: "missing"})
	require.Error(t, err)
	require.ErrorContains(t, err, "unknown tool name")
	require.Len(t, providerOne.calls, 0)
}
