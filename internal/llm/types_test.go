package llm

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInputMessageMarshalJSON(t *testing.T) {
	cases := []struct {
		name     string
		input    InputMessage
		expected string
	}{
		{
			name: "single input_text becomes string",
			input: InputMessage{
				Role:    "user",
				Content: []ContentItem{{Type: "input_text", Text: "hello"}},
			},
			expected: `{"role":"user","content":"hello"}`,
		},
		{
			name: "multiple content items stay array",
			input: InputMessage{
				Role: "user",
				Content: []ContentItem{
					{Type: "input_text", Text: "hello"},
					{Type: "input_text", Text: "world"},
				},
			},
			expected: `{"role":"user","content":[{"type":"input_text","text":"hello"},{"type":"input_text","text":"world"}]}`,
		},
		{
			name: "empty content omitted",
			input: InputMessage{
				Role: "assistant",
			},
			expected: `{"role":"assistant"}`,
		},
		{
			name: "non-text content stays array",
			input: InputMessage{
				Role:    "user",
				Content: []ContentItem{{Type: "input_image", Text: "data"}},
			},
			expected: `{"role":"user","content":[{"type":"input_image","text":"data"}]}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload, err := json.Marshal(tc.input)
			require.NoError(t, err)
			require.JSONEq(t, tc.expected, string(payload))
		})
	}
}
