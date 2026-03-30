package mcp

import "context"

type ToolProvider interface {
	ListTools(ctx context.Context) ([]Tool, error)
	CallTool(ctx context.Context, call ToolCall) (ToolResult, error)
	Close() error
}
