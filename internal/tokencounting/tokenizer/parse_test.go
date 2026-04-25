package tokenizer

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseMessageShapes(t *testing.T) {
	t.Run("message", func(t *testing.T) {
		payload := []byte(`{"type":"message","role":"user","content":[{"type":"input_text","text":"Hello"},{"type":"refusal","refusal":"No"}]}`)
		msg, err := ParseMessage(payload)
		require.NoError(t, err)
		item, ok := msg.Item.(MessageItem)
		require.True(t, ok)
		require.Equal(t, RoleUser, item.Role)
		require.Len(t, item.Content, 2)
		require.Equal(t, ContentTypeInputText, item.Content[0].Type)
		require.Equal(t, "Hello", item.Content[0].Text)
		require.Equal(t, ContentTypeRefusal, item.Content[1].Type)
		require.Equal(t, "No", item.Content[1].Text)
	})

	t.Run("function_call", func(t *testing.T) {
		payload := []byte(`{"type":"function_call","arguments":"{\"tool\":\"weather\"}"}`)
		msg, err := ParseMessage(payload)
		require.NoError(t, err)
		item, ok := msg.Item.(FunctionCallItem)
		require.True(t, ok)
		require.Equal(t, `{"tool":"weather"}`, item.Arguments)
	})

	t.Run("function_call_output_text", func(t *testing.T) {
		payload := []byte(`{"type":"function_call_output","output":"done"}`)
		msg, err := ParseMessage(payload)
		require.NoError(t, err)
		item, ok := msg.Item.(FunctionCallOutputItem)
		require.True(t, ok)
		require.True(t, item.Output.IsText)
		require.Equal(t, "done", item.Output.Text)
	})

	t.Run("function_call_output_array", func(t *testing.T) {
		payload := []byte(`{"type":"function_call_output","output":[{"type":"output_text","text":"done"}]}`)
		msg, err := ParseMessage(payload)
		require.NoError(t, err)
		item, ok := msg.Item.(FunctionCallOutputItem)
		require.True(t, ok)
		require.False(t, item.Output.IsText)
		require.Len(t, item.Output.Content, 1)
		part := item.Output.Content[0]
		require.Equal(t, ContentTypeOutputText, part.Type)
		require.Equal(t, "done", part.Text)
	})
}

func TestParseMessageRejectsAudio(t *testing.T) {
	payload := []byte(`{"type":"message","role":"user","content":[{"type":"input_audio"}]}`)
	_, err := ParseMessage(payload)
	require.Error(t, err)
	require.ErrorContains(t, err, "audio content is not supported")
}
