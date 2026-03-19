package llm

import "encoding/json"

type ContentItem struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type InputMessage struct {
	Role       string        `json:"role"`
	Content    []ContentItem `json:"content,omitempty"`
	ToolCalls  []ToolCall    `json:"tool_calls,omitempty"`
	ToolCallID string        `json:"tool_call_id,omitempty"`
	Name       string        `json:"name,omitempty"`
}

type ToolDefinition struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

type ToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type ToolCall struct {
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type Request struct {
	Model      string           `json:"model"`
	Input      []InputMessage   `json:"input"`
	Tools      []ToolDefinition `json:"tools,omitempty"`
	ToolChoice string           `json:"tool_choice,omitempty"`
	Stream     bool             `json:"stream,omitempty"`
}

type Response struct {
	ID     string       `json:"id"`
	Output []OutputItem `json:"output"`
}

type OutputItem struct {
	Type      string          `json:"type"`
	Role      string          `json:"role,omitempty"`
	Content   []ContentItem   `json:"content,omitempty"`
	ToolCalls []ToolCall      `json:"tool_calls,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}
