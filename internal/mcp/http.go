package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type HTTPClient struct {
	session *mcpsdk.ClientSession
}

func NewHTTPClient(ctx context.Context, url string) (*HTTPClient, error) {
	client := mcpsdk.NewClient(
		&mcpsdk.Implementation{Name: "agn", Version: "0.1.0"},
		nil,
	)
	transport := &mcpsdk.StreamableClientTransport{
		Endpoint: url,
	}
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, fmt.Errorf("connect to %s: %w", url, err)
	}
	return &HTTPClient{session: session}, nil
}

func (h *HTTPClient) ListTools(ctx context.Context) ([]Tool, error) {
	result, err := h.session.ListTools(ctx, &mcpsdk.ListToolsParams{})
	if err != nil {
		return nil, err
	}
	tools := make([]Tool, 0, len(result.Tools))
	for _, tool := range result.Tools {
		if tool == nil {
			return nil, errors.New("tool definition is nil")
		}
		var inputSchema json.RawMessage
		if tool.InputSchema != nil {
			payload, err := json.Marshal(tool.InputSchema)
			if err != nil {
				return nil, fmt.Errorf("marshal tool schema for %s: %w", tool.Name, err)
			}
			inputSchema = payload
		}
		tools = append(tools, Tool{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: inputSchema,
		})
	}
	return tools, nil
}

func (h *HTTPClient) CallTool(ctx context.Context, call ToolCall) (ToolResult, error) {
	arguments := map[string]any{}
	if len(call.Arguments) > 0 {
		if err := json.Unmarshal(call.Arguments, &arguments); err != nil {
			return ToolResult{}, fmt.Errorf("parse tool arguments: %w", err)
		}
		if arguments == nil {
			arguments = map[string]any{}
		}
	}
	result, err := h.session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      call.Name,
		Arguments: arguments,
	})
	if err != nil {
		return ToolResult{}, err
	}
	if result == nil {
		return ToolResult{}, errors.New("tool result is nil")
	}
	var textParts []string
	for _, content := range result.Content {
		if textContent, ok := content.(*mcpsdk.TextContent); ok {
			textParts = append(textParts, textContent.Text)
		}
	}
	content := strings.TrimSpace(strings.Join(textParts, "\n"))
	if content == "" {
		return ToolResult{}, errors.New("tool result content is empty")
	}
	return ToolResult{Content: content}, nil
}

func (h *HTTPClient) Close() error {
	return h.session.Close()
}
