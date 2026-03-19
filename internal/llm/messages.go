package llm

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/agynio/agn-cli/internal/mcp"
	"github.com/agynio/agn-cli/internal/message"
)

func MessagesToInput(messages []message.Message) ([]InputMessage, error) {
	inputs := make([]InputMessage, 0, len(messages))
	for _, msg := range messages {
		input, err := messageToInput(msg)
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, input)
	}
	return inputs, nil
}

func messageToInput(msg message.Message) (InputMessage, error) {
	switch typed := msg.(type) {
	case message.SystemMessage:
		return InputMessage{
			Role:    "system",
			Content: []ContentItem{{Type: "input_text", Text: typed.Text}},
		}, nil
	case message.HumanMessage:
		return InputMessage{
			Role:    "user",
			Content: []ContentItem{{Type: "input_text", Text: typed.Text}},
		}, nil
	case message.AIMessage:
		return InputMessage{
			Role:    "assistant",
			Content: []ContentItem{{Type: "input_text", Text: typed.Text}},
		}, nil
	case message.ToolCallMessage:
		calls := make([]ToolCall, 0, len(typed.ToolCalls))
		for _, call := range typed.ToolCalls {
			calls = append(calls, ToolCall{ID: call.ID, Name: call.Name, Arguments: call.Arguments})
		}
		return InputMessage{
			Role:      "assistant",
			ToolCalls: calls,
		}, nil
	case message.ToolCallOutputMessage:
		return InputMessage{
			Role:       "tool",
			ToolCallID: typed.Output.ToolCallID,
			Name:       typed.Output.ToolName,
			Content:    []ContentItem{{Type: "input_text", Text: typed.Output.Output}},
		}, nil
	case message.ResponseMessage:
		return InputMessage{}, errors.New("response messages are not valid LLM input")
	default:
		return InputMessage{}, fmt.Errorf("unsupported message type %T", msg)
	}
}

func ToolDefinitionsFromMCP(tools []mcp.Tool) ([]ToolDefinition, error) {
	result := make([]ToolDefinition, 0, len(tools))
	for _, tool := range tools {
		if strings.TrimSpace(tool.Name) == "" {
			return nil, errors.New("tool name is required")
		}
		params := tool.InputSchema
		if len(params) == 0 {
			params = json.RawMessage("{}")
		}
		result = append(result, ToolDefinition{
			Type: "function",
			Function: ToolFunction{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  params,
			},
		})
	}
	return result, nil
}
