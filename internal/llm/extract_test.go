package llm

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/openai/openai-go/v3/responses"
)

func TestExtractToolCallsEmpty(t *testing.T) {
	resp := &responses.Response{}
	calls := ExtractToolCalls(resp)
	require.Nil(t, calls)
}

func TestExtractToolCallsMessageOnly(t *testing.T) {
	resp := &responses.Response{
		Output: []responses.ResponseOutputItemUnion{mustOutputItem(t, `{"id":"msg-1","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"hello"}]}`)},
	}
	calls := ExtractToolCalls(resp)
	require.Nil(t, calls)
}

func TestExtractToolCallsFunctionCalls(t *testing.T) {
	resp := &responses.Response{
		Output: []responses.ResponseOutputItemUnion{
			mustOutputItem(t, `{"type":"function_call","call_id":"call-1","name":"do","arguments":"{\"value\":1}"}`),
			mustOutputItem(t, `{"type":"function_call","call_id":"call-2","name":"run","arguments":"{\"value\":2}"}`),
		},
	}

	calls := ExtractToolCalls(resp)
	require.Len(t, calls, 2)
	require.Equal(t, "call-1", calls[0].ID)
	require.Equal(t, "do", calls[0].Name)
	require.Equal(t, json.RawMessage(`{"value":1}`), calls[0].Arguments)
	require.Equal(t, "call-2", calls[1].ID)
	require.Equal(t, "run", calls[1].Name)
	require.Equal(t, json.RawMessage(`{"value":2}`), calls[1].Arguments)
}

func TestExtractToolCallsMixedItems(t *testing.T) {
	resp := &responses.Response{
		Output: []responses.ResponseOutputItemUnion{
			mustOutputItem(t, `{"id":"msg-1","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"hello"}]}`),
			mustOutputItem(t, `{"type":"function_call","call_id":"call-1","name":"do","arguments":"{\"value\":1}"}`),
		},
	}

	calls := ExtractToolCalls(resp)
	require.Len(t, calls, 1)
	require.Equal(t, json.RawMessage(`{"value":1}`), calls[0].Arguments)
}

func mustOutputItem(t *testing.T, payload string) responses.ResponseOutputItemUnion {
	t.Helper()
	var item responses.ResponseOutputItemUnion
	require.NoError(t, json.Unmarshal([]byte(payload), &item))
	return item
}
