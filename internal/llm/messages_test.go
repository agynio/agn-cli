package llm

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/agynio/agn-cli/internal/message"
	"github.com/openai/openai-go/v3/responses"
)

func TestMessagesToInputMappings(t *testing.T) {
	toolCalls := []message.ToolCall{
		{ID: "call-1", Name: "tool-one", Arguments: json.RawMessage(`{"path":"/tmp"}`)},
		{ID: "call-2", Name: "tool-two", Arguments: json.RawMessage(`{"verbose":true}`)},
	}
	output := message.ToolCallOutput{ToolCallID: "call-1", ToolName: "tool-one", Output: "ok"}

	inputs, err := MessagesToInput([]message.Message{
		message.NewSystemMessage("system"),
		message.NewSummaryMessage("summary"),
		message.NewHumanMessage("human"),
		message.NewAIMessage("assistant"),
		message.NewToolCallMessage(toolCalls),
		message.NewToolCallOutputMessage(output),
	})
	require.NoError(t, err)
	require.Len(t, inputs, 7)

	requireMessageInput(t, inputs[0], responses.EasyInputMessageRoleSystem, "system")
	requireMessageInput(t, inputs[1], responses.EasyInputMessageRoleSystem, "summary")
	requireMessageInput(t, inputs[2], responses.EasyInputMessageRoleUser, "human")
	requireMessageInput(t, inputs[3], responses.EasyInputMessageRoleAssistant, "assistant")
	requireFunctionCallInput(t, inputs[4], toolCalls[0])
	requireFunctionCallInput(t, inputs[5], toolCalls[1])
	requireFunctionCallOutputInput(t, inputs[6], output)
}

func TestMessagesToInputResponseMessageError(t *testing.T) {
	_, err := MessagesToInput([]message.Message{message.NewResponseMessage("raw")})
	require.Error(t, err)
}

func TestMessagesToInputEmpty(t *testing.T) {
	inputs, err := MessagesToInput(nil)
	require.NoError(t, err)
	require.Empty(t, inputs)
}

func requireMessageInput(t *testing.T, item responses.ResponseInputItemUnionParam, role responses.EasyInputMessageRole, text string) {
	t.Helper()
	require.NotNil(t, item.OfMessage)
	require.Equal(t, role, item.OfMessage.Role)
	require.True(t, item.OfMessage.Content.OfString.Valid())
	require.Equal(t, text, item.OfMessage.Content.OfString.Value)
}

func requireFunctionCallInput(t *testing.T, item responses.ResponseInputItemUnionParam, call message.ToolCall) {
	t.Helper()
	require.NotNil(t, item.OfFunctionCall)
	require.Equal(t, call.ID, item.OfFunctionCall.CallID)
	require.Equal(t, call.Name, item.OfFunctionCall.Name)
	require.Equal(t, string(call.Arguments), item.OfFunctionCall.Arguments)
}

func requireFunctionCallOutputInput(t *testing.T, item responses.ResponseInputItemUnionParam, output message.ToolCallOutput) {
	t.Helper()
	require.NotNil(t, item.OfFunctionCallOutput)
	require.Equal(t, output.ToolCallID, item.OfFunctionCallOutput.CallID)
	require.True(t, item.OfFunctionCallOutput.Output.OfString.Valid())
	require.Equal(t, output.Output, item.OfFunctionCallOutput.Output.OfString.Value)
}
