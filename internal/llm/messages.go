package llm

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/agynio/agn-cli/internal/mcp"
	"github.com/agynio/agn-cli/internal/message"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
)

func MessagesToInput(messages []message.Message) ([]responses.ResponseInputItemUnionParam, error) {
	inputs := make([]responses.ResponseInputItemUnionParam, 0, len(messages))
	for _, msg := range messages {
		items, err := messageToInputItems(msg)
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, items...)
	}
	return inputs, nil
}

func messageToInputItems(msg message.Message) ([]responses.ResponseInputItemUnionParam, error) {
	switch typed := msg.(type) {
	case message.SystemMessage:
		return []responses.ResponseInputItemUnionParam{
			responses.ResponseInputItemParamOfMessage(typed.Text, responses.EasyInputMessageRoleDeveloper),
		}, nil
	case message.HumanMessage:
		return []responses.ResponseInputItemUnionParam{
			responses.ResponseInputItemParamOfMessage(typed.Text, responses.EasyInputMessageRoleUser),
		}, nil
	case message.AIMessage:
		return []responses.ResponseInputItemUnionParam{
			responses.ResponseInputItemParamOfMessage(typed.Text, responses.EasyInputMessageRoleAssistant),
		}, nil
	case message.ToolCallMessage:
		items := make([]responses.ResponseInputItemUnionParam, 0, len(typed.ToolCalls))
		for _, call := range typed.ToolCalls {
			items = append(items, responses.ResponseInputItemParamOfFunctionCall(string(call.Arguments), call.ID, call.Name))
		}
		return items, nil
	case message.ToolCallOutputMessage:
		return []responses.ResponseInputItemUnionParam{
			responses.ResponseInputItemParamOfFunctionCallOutput(typed.Output.ToolCallID, typed.Output.Output),
		}, nil
	case message.ResponseMessage:
		return nil, errors.New("response messages are not valid LLM input")
	default:
		return nil, fmt.Errorf("unsupported message type %T", msg)
	}
}

func ToolDefinitionsFromMCP(tools []mcp.Tool) ([]responses.ToolUnionParam, error) {
	result := make([]responses.ToolUnionParam, 0, len(tools))
	for _, tool := range tools {
		if strings.TrimSpace(tool.Name) == "" {
			return nil, errors.New("tool name is required")
		}
		parameters := map[string]any{}
		if len(tool.InputSchema) > 0 {
			if err := json.Unmarshal(tool.InputSchema, &parameters); err != nil {
				return nil, fmt.Errorf("parse tool schema for %s: %w", tool.Name, err)
			}
		}
		function := responses.FunctionToolParam{
			Name:       tool.Name,
			Parameters: parameters,
			Strict:     openai.Bool(true),
		}
		if strings.TrimSpace(tool.Description) != "" {
			function.Description = openai.String(tool.Description)
		}
		result = append(result, responses.ToolUnionParam{OfFunction: &function})
	}
	return result, nil
}
