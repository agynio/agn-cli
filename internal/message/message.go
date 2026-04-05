package message

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/agynio/agn-cli/internal/mcp"
)

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type Kind string

const (
	KindSystem         Kind = "system"
	KindSummary        Kind = "summary"
	KindHuman          Kind = "human"
	KindAI             Kind = "assistant"
	KindToolCall       Kind = "tool_call"
	KindToolCallOutput Kind = "tool_output"
	KindResponse       Kind = "response"
)

type Message interface {
	Role() Role
	Kind() Kind
}

type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type ToolCallOutput struct {
	ToolCallID string            `json:"tool_call_id"`
	ToolName   string            `json:"tool_name"`
	Output     []mcp.ContentItem `json:"output"`
}

type SystemMessage struct {
	Variant Kind   `json:"variant"`
	Text    string `json:"text"`
}

func NewSystemMessage(text string) SystemMessage {
	return SystemMessage{Variant: KindSystem, Text: text}
}

func NewSummaryMessage(text string) SystemMessage {
	return SystemMessage{Variant: KindSummary, Text: text}
}

func (m SystemMessage) Role() Role {
	return RoleSystem
}

func (m SystemMessage) Kind() Kind {
	return m.Variant
}

type HumanMessage struct {
	Text string `json:"text"`
}

func NewHumanMessage(text string) HumanMessage {
	return HumanMessage{Text: text}
}

func (m HumanMessage) Role() Role {
	return RoleUser
}

func (m HumanMessage) Kind() Kind {
	return KindHuman
}

type AIMessage struct {
	Text string `json:"text"`
}

func NewAIMessage(text string) AIMessage {
	return AIMessage{Text: text}
}

func (m AIMessage) Role() Role {
	return RoleAssistant
}

func (m AIMessage) Kind() Kind {
	return KindAI
}

type ToolCallMessage struct {
	ToolCalls []ToolCall `json:"tool_calls"`
}

func NewToolCallMessage(toolCalls []ToolCall) ToolCallMessage {
	return ToolCallMessage{ToolCalls: toolCalls}
}

func (m ToolCallMessage) Role() Role {
	return RoleAssistant
}

func (m ToolCallMessage) Kind() Kind {
	return KindToolCall
}

type ToolCallOutputMessage struct {
	Output ToolCallOutput `json:"output"`
}

func NewToolCallOutputMessage(output ToolCallOutput) ToolCallOutputMessage {
	return ToolCallOutputMessage{Output: output}
}

func (m ToolCallOutputMessage) Role() Role {
	return RoleTool
}

func (m ToolCallOutputMessage) Kind() Kind {
	return KindToolCallOutput
}

type ResponseMessage struct {
	Raw string `json:"raw"`
}

func NewResponseMessage(raw string) ResponseMessage {
	return ResponseMessage{Raw: raw}
}

func (m ResponseMessage) Role() Role {
	return RoleAssistant
}

func (m ResponseMessage) Kind() Kind {
	return KindResponse
}

type Envelope struct {
	Role       Role            `json:"role"`
	Kind       Kind            `json:"kind"`
	Text       string          `json:"text,omitempty"`
	ToolCalls  []ToolCall      `json:"tool_calls,omitempty"`
	ToolOutput *ToolCallOutput `json:"tool_output,omitempty"`
	Raw        string          `json:"raw,omitempty"`
}

func Encode(msg Message) (Envelope, error) {
	switch typed := msg.(type) {
	case SystemMessage:
		return Envelope{Role: typed.Role(), Kind: typed.Kind(), Text: typed.Text}, nil
	case HumanMessage:
		return Envelope{Role: typed.Role(), Kind: typed.Kind(), Text: typed.Text}, nil
	case AIMessage:
		return Envelope{Role: typed.Role(), Kind: typed.Kind(), Text: typed.Text}, nil
	case ToolCallMessage:
		return Envelope{Role: typed.Role(), Kind: typed.Kind(), ToolCalls: typed.ToolCalls}, nil
	case ToolCallOutputMessage:
		return Envelope{Role: typed.Role(), Kind: typed.Kind(), ToolOutput: &typed.Output}, nil
	case ResponseMessage:
		return Envelope{Role: typed.Role(), Kind: typed.Kind(), Raw: typed.Raw}, nil
	default:
		return Envelope{}, fmt.Errorf("unsupported message type %T", msg)
	}
}

func Decode(env Envelope) (Message, error) {
	switch env.Kind {
	case KindSystem, KindSummary:
		return SystemMessage{Variant: env.Kind, Text: env.Text}, nil
	case KindHuman:
		return HumanMessage{Text: env.Text}, nil
	case KindAI:
		return AIMessage{Text: env.Text}, nil
	case KindToolCall:
		return ToolCallMessage{ToolCalls: env.ToolCalls}, nil
	case KindToolCallOutput:
		if env.ToolOutput == nil {
			return nil, errors.New("tool_output message missing output")
		}
		return ToolCallOutputMessage{Output: *env.ToolOutput}, nil
	case KindResponse:
		return ResponseMessage{Raw: env.Raw}, nil
	default:
		return nil, fmt.Errorf("unsupported message kind %q", env.Kind)
	}
}

func IsContextMessage(msg Message) bool {
	return msg.Kind() != KindResponse
}

func TextForSummary(msg Message) (string, bool) {
	switch typed := msg.(type) {
	case SystemMessage:
		return strings.TrimSpace(typed.Text), typed.Text != ""
	case HumanMessage:
		return strings.TrimSpace(typed.Text), typed.Text != ""
	case AIMessage:
		return strings.TrimSpace(typed.Text), typed.Text != ""
	case ToolCallMessage:
		if len(typed.ToolCalls) == 0 {
			return "", false
		}
		payload, err := json.Marshal(typed.ToolCalls)
		if err != nil {
			return "", false
		}
		return string(payload), true
	case ToolCallOutputMessage:
		return toolOutputSummary(typed.Output)
	default:
		return "", false
	}
}

func toolOutputSummary(output ToolCallOutput) (string, bool) {
	if len(output.Output) == 0 {
		return "", false
	}
	parts := make([]string, 0, len(output.Output))
	for _, item := range output.Output {
		switch item.Type {
		case mcp.ContentTypeText:
			text := strings.TrimSpace(item.Text)
			if text != "" {
				parts = append(parts, text)
			}
		case mcp.ContentTypeResource:
			if item.Resource == nil {
				continue
			}
			text := strings.TrimSpace(item.Resource.Text)
			if text != "" {
				parts = append(parts, text)
				continue
			}
			parts = append(parts, fmt.Sprintf("[resource:%s]", item.Resource.URI))
		case mcp.ContentTypeImage:
			parts = append(parts, fmt.Sprintf("[image:%s]", item.MIMEType))
		case mcp.ContentTypeAudio:
			parts = append(parts, fmt.Sprintf("[audio:%s]", item.MIMEType))
		default:
			parts = append(parts, fmt.Sprintf("[content:%s]", item.Type))
		}
	}
	summary := strings.TrimSpace(strings.Join(parts, "\n"))
	if summary == "" {
		return "", false
	}
	return summary, true
}
