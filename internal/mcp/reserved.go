package mcp

import (
	"context"
	"fmt"
	"strings"
)

type ReservedToolProvider struct {
	provider ToolProvider
	reserved map[string]struct{}
}

func NewReservedToolProvider(provider ToolProvider, reserved []string) *ReservedToolProvider {
	index := make(map[string]struct{}, len(reserved))
	for _, name := range reserved {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		index[trimmed] = struct{}{}
	}
	return &ReservedToolProvider{provider: provider, reserved: index}
}

func (r *ReservedToolProvider) ListTools(ctx context.Context) ([]Tool, error) {
	tools, err := r.provider.ListTools(ctx)
	if err != nil {
		return nil, err
	}
	if err := r.checkReserved(tools); err != nil {
		return nil, err
	}
	return tools, nil
}

func (r *ReservedToolProvider) CallTool(ctx context.Context, call ToolCall) (ToolResult, error) {
	name := strings.TrimSpace(call.Name)
	if _, reserved := r.reserved[name]; reserved {
		return ToolResult{}, fmt.Errorf("mcp tool name %q is reserved", name)
	}
	return r.provider.CallTool(ctx, call)
}

func (r *ReservedToolProvider) Close() error {
	return r.provider.Close()
}

func (r *ReservedToolProvider) checkReserved(tools []Tool) error {
	for _, tool := range tools {
		name := strings.TrimSpace(tool.Name)
		if _, reserved := r.reserved[name]; reserved {
			return fmt.Errorf("mcp tool name %q is reserved", name)
		}
	}
	return nil
}
