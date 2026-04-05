package message

import (
	"testing"

	"github.com/agynio/agn-cli/internal/mcp"
	"github.com/stretchr/testify/require"
)

func TestEncodeDecodeMessages(t *testing.T) {
	toolArgs := `{"path":"/tmp"}`
	toolCalls := []ToolCall{{ID: "call-1", Name: "read", Arguments: toolArgs}}
	output := ToolCallOutput{ToolCallID: "call-1", ToolName: "read", Output: []mcp.ContentItem{{Type: mcp.ContentTypeText, Text: "ok"}}}

	cases := []Message{
		NewSystemMessage("system"),
		NewSummaryMessage("summary"),
		NewHumanMessage("hello"),
		NewAIMessage("response"),
		NewToolCallMessage(toolCalls),
		NewToolCallOutputMessage(output),
		NewResponseMessage("raw"),
	}

	for _, msg := range cases {
		env, err := Encode(msg)
		require.NoError(t, err)
		decoded, err := Decode(env)
		require.NoError(t, err)
		assertMessageEqual(t, msg, decoded)
	}
}

func TestTextForSummary(t *testing.T) {
	text, ok := TextForSummary(NewHumanMessage("hello"))
	require.True(t, ok)
	require.Equal(t, "hello", text)

	text, ok = TextForSummary(NewResponseMessage("raw"))
	require.False(t, ok)
	require.Empty(t, text)
}

func assertMessageEqual(t *testing.T, expected Message, actual Message) {
	switch exp := expected.(type) {
	case SystemMessage:
		got, ok := actual.(SystemMessage)
		require.True(t, ok)
		require.Equal(t, exp, got)
	case HumanMessage:
		got, ok := actual.(HumanMessage)
		require.True(t, ok)
		require.Equal(t, exp, got)
	case AIMessage:
		got, ok := actual.(AIMessage)
		require.True(t, ok)
		require.Equal(t, exp, got)
	case ToolCallMessage:
		got, ok := actual.(ToolCallMessage)
		require.True(t, ok)
		require.Equal(t, exp, got)
	case ToolCallOutputMessage:
		got, ok := actual.(ToolCallOutputMessage)
		require.True(t, ok)
		require.Equal(t, exp, got)
	case ResponseMessage:
		got, ok := actual.(ResponseMessage)
		require.True(t, ok)
		require.Equal(t, exp, got)
	default:
		t.Fatalf("unsupported message type %T", expected)
	}
}
