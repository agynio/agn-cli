package llm

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/agynio/agn-cli/internal/mcp"
	"github.com/agynio/agn-cli/internal/message"
	"github.com/openai/openai-go/v3/responses"
)

func TestMessagesToInputMappings(t *testing.T) {
	toolCalls := []message.ToolCall{
		{ID: "call-1", Name: "tool-one", Arguments: `{"path":"/tmp"}`},
		{ID: "call-2", Name: "tool-two", Arguments: `{"verbose":true}`},
	}
	resourceText := "report"
	output := message.ToolCallOutput{
		ToolCallID: "call-1",
		ToolName:   "tool-one",
		Output: []mcp.ContentItem{
			{Type: mcp.ContentTypeText, Text: "ok"},
			{Type: mcp.ContentTypeImage, MIMEType: "image/png", Data: "ZmFrZQ=="},
			{Type: mcp.ContentTypeAudio, MIMEType: "audio/wav", Data: "c291bmQ="},
			{
				Type: mcp.ContentTypeResource,
				Resource: &mcp.ResourceContent{
					URI:  "file:///report.txt",
					Text: resourceText,
				},
			},
		},
	}

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

func TestMessagesToInputTextOnlyToolOutput(t *testing.T) {
	output := message.ToolCallOutput{
		ToolCallID: "call-1",
		ToolName:   "tool-one",
		Output: []mcp.ContentItem{
			{Type: mcp.ContentTypeText, Text: "first"},
			{Type: mcp.ContentTypeText, Text: "second"},
		},
	}

	inputs, err := MessagesToInput([]message.Message{message.NewToolCallOutputMessage(output)})
	require.NoError(t, err)
	require.Len(t, inputs, 1)

	item := inputs[0]
	require.NotNil(t, item.OfFunctionCallOutput)
	require.Equal(t, output.ToolCallID, item.OfFunctionCallOutput.CallID)
	require.True(t, item.OfFunctionCallOutput.Output.OfString.Valid())
	require.Equal(t, "first\nsecond", item.OfFunctionCallOutput.Output.OfString.Value)
	require.Empty(t, item.OfFunctionCallOutput.Output.OfResponseFunctionCallOutputItemArray)
}

func TestToolDefinitionsFromMCPStrictFalse(t *testing.T) {
	tools, err := ToolDefinitionsFromMCP([]mcp.Tool{
		{
			Name:        "optional-arg-tool",
			InputSchema: []byte(`{"type":"object","properties":{"path":{"type":"string"},"verbose":{"type":"boolean"}},"required":["path"]}`),
		},
	})
	require.NoError(t, err)
	require.Len(t, tools, 1)
	require.NotNil(t, tools[0].OfFunction)
	require.True(t, tools[0].OfFunction.Strict.Valid())
	require.False(t, tools[0].OfFunction.Strict.Value)
}

func TestSanitizeMCPToolSchema(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "object_without_properties",
			input:    `{"type":"object"}`,
			expected: `{"type":"object","properties":{}}`,
		},
		{
			name:     "empty_schema",
			input:    `{}`,
			expected: `{"type":"string"}`,
		},
		{
			name:     "object_with_properties",
			input:    `{"type":"object","properties":{"q":{"type":"string"}}}`,
			expected: `{"type":"object","properties":{"q":{"type":"string"}}}`,
		},
		{
			name:     "array_without_items",
			input:    `{"type":"array"}`,
			expected: `{"type":"array","items":{"type":"string"}}`,
		},
		{
			name:     "array_with_object_items",
			input:    `{"type":"array","items":{"type":"object"}}`,
			expected: `{"type":"array","items":{"type":"object","properties":{}}}`,
		},
		{
			name:     "nested_object_property",
			input:    `{"type":"object","properties":{"f":{"type":"object"}}}`,
			expected: `{"type":"object","properties":{"f":{"type":"object","properties":{}}}}`,
		},
		{
			name:     "infer_object_type",
			input:    `{"properties":{"x":{"type":"string"}}}`,
			expected: `{"type":"object","properties":{"x":{"type":"string"}}}`,
		},
		{
			name:     "first_type_from_array",
			input:    `{"type":["string","null"]}`,
			expected: `{"type":"string"}`,
		},
		{
			name:     "nested_array_items",
			input:    `{"type":"object","properties":{"tags":{"type":"array"}}}`,
			expected: `{"type":"object","properties":{"tags":{"type":"array","items":{"type":"string"}}}}`,
		},
		{
			name:     "boolean_property_schema",
			input:    `{"type":"object","properties":{"m":true}}`,
			expected: `{"type":"object","properties":{"m":{"type":"string"}}}`,
		},
		{
			name:     "anyof_object_schema",
			input:    `{"type":"object","properties":{"opts":{"anyOf":[{"type":"object"},{"type":"string"}]}}}`,
			expected: `{"type":"object","properties":{"opts":{"type":"string","anyOf":[{"type":"object","properties":{}},{"type":"string"}]}}}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			schema := schemaFromJSON(t, test.input)
			sanitizeMCPToolSchema(schema)
			require.Equal(t, schemaFromJSON(t, test.expected), schema)
		})
	}
}

func schemaFromJSON(t *testing.T, raw string) map[string]any {
	t.Helper()
	var schema map[string]any
	require.NoError(t, json.Unmarshal([]byte(raw), &schema))
	return schema
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
	require.Equal(t, call.Arguments, item.OfFunctionCall.Arguments)
}

func requireFunctionCallOutputInput(t *testing.T, item responses.ResponseInputItemUnionParam, output message.ToolCallOutput) {
	t.Helper()
	require.NotNil(t, item.OfFunctionCallOutput)
	require.Equal(t, output.ToolCallID, item.OfFunctionCallOutput.CallID)
	outputItems := item.OfFunctionCallOutput.Output.OfResponseFunctionCallOutputItemArray
	require.Len(t, outputItems, len(output.Output))
	requireOutputText(t, outputItems[0], "ok")
	requireOutputImage(t, outputItems[1], "data:image/png;base64,ZmFrZQ==")
	requireOutputFile(t, outputItems[2], "c291bmQ=", "audio.wav")
	requireOutputText(t, outputItems[3], "report")
}

func requireOutputText(t *testing.T, item responses.ResponseFunctionCallOutputItemUnionParam, text string) {
	t.Helper()
	require.NotNil(t, item.OfInputText)
	require.Equal(t, text, item.OfInputText.Text)
}

func requireOutputImage(t *testing.T, item responses.ResponseFunctionCallOutputItemUnionParam, url string) {
	t.Helper()
	require.NotNil(t, item.OfInputImage)
	require.True(t, item.OfInputImage.ImageURL.Valid())
	require.Equal(t, url, item.OfInputImage.ImageURL.Value)
}

func requireOutputFile(t *testing.T, item responses.ResponseFunctionCallOutputItemUnionParam, data, filename string) {
	t.Helper()
	require.NotNil(t, item.OfInputFile)
	require.True(t, item.OfInputFile.FileData.Valid())
	require.Equal(t, data, item.OfInputFile.FileData.Value)
	require.True(t, item.OfInputFile.Filename.Valid())
	require.Equal(t, filename, item.OfInputFile.Filename.Value)
}
