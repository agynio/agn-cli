package mcp

import (
	"context"
	"fmt"
)

type MultiClient struct {
	providers []ToolProvider
	toolIndex map[string]ToolProvider
}

func NewMultiClient(providers []ToolProvider) (*MultiClient, error) {
	return &MultiClient{providers: providers}, nil
}

func (m *MultiClient) ListTools(ctx context.Context) ([]Tool, error) {
	tools := make([]Tool, 0)
	index := make(map[string]ToolProvider)
	for _, provider := range m.providers {
		providerTools, err := provider.ListTools(ctx)
		if err != nil {
			return nil, err
		}
		for _, tool := range providerTools {
			if _, exists := index[tool.Name]; exists {
				return nil, fmt.Errorf("duplicate tool name %q", tool.Name)
			}
			index[tool.Name] = provider
			tools = append(tools, tool)
		}
	}
	m.toolIndex = index
	return tools, nil
}

func (m *MultiClient) CallTool(ctx context.Context, call ToolCall) (ToolResult, error) {
	provider, ok := m.toolIndex[call.Name]
	if !ok {
		return ToolResult{}, fmt.Errorf("unknown tool name %q", call.Name)
	}
	return provider.CallTool(ctx, call)
}

func (m *MultiClient) Close() error {
	var firstErr error
	for _, provider := range m.providers {
		if err := provider.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
