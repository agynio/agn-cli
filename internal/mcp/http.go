package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type HTTPClient struct {
	session *mcpsdk.ClientSession
}

const (
	mcpClientName    = "agn"
	mcpClientVersion = "0.1.0"
)

func NewHTTPClient(ctx context.Context, url string) (*HTTPClient, error) {
	client := mcpsdk.NewClient(
		&mcpsdk.Implementation{Name: mcpClientName, Version: mcpClientVersion},
		nil,
	)
	transport := &mcpsdk.StreamableClientTransport{
		Endpoint:             url,
		DisableStandaloneSSE: true,
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
	items := make([]ContentItem, 0, len(result.Content))
	for _, content := range result.Content {
		switch typed := content.(type) {
		case *mcpsdk.TextContent:
			items = append(items, ContentItem{Type: ContentTypeText, Text: typed.Text})
		case *mcpsdk.ImageContent:
			encoded := base64.StdEncoding.EncodeToString(typed.Data)
			items = append(items, ContentItem{Type: ContentTypeImage, MIMEType: typed.MIMEType, Data: encoded})
		case *mcpsdk.AudioContent:
			encoded := base64.StdEncoding.EncodeToString(typed.Data)
			items = append(items, ContentItem{Type: ContentTypeAudio, MIMEType: typed.MIMEType, Data: encoded})
		case *mcpsdk.EmbeddedResource:
			if typed.Resource == nil {
				return ToolResult{}, errors.New("embedded resource is missing data")
			}
			resource := &ResourceContent{
				URI:      typed.Resource.URI,
				MIMEType: typed.Resource.MIMEType,
				Text:     typed.Resource.Text,
			}
			if len(typed.Resource.Blob) > 0 {
				resource.Blob = base64.StdEncoding.EncodeToString(typed.Resource.Blob)
			}
			items = append(items, ContentItem{Type: ContentTypeResource, Resource: resource})
		case nil:
			return ToolResult{}, errors.New("tool result content is nil")
		default:
			return ToolResult{}, fmt.Errorf("unsupported tool content type %T", content)
		}
	}
	if err := ValidateContentItems(items); err != nil {
		return ToolResult{}, err
	}
	return ToolResult{Content: items}, nil
}

func (h *HTTPClient) Close() error {
	return h.session.Close()
}
