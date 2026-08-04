package loop

import (
	"context"
	"net/http"

	"github.com/agynio/agn-cli/internal/message"
	"github.com/agynio/agn-cli/internal/summarize"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAdjustLoadedMessageCount(t *testing.T) {
	tests := []struct {
		name            string
		previousCount   int
		previousLoaded  int
		newContextCount int
		newTotal        int
		expected        int
	}{
		{
			name:            "no summarization",
			previousCount:   4,
			previousLoaded:  2,
			newContextCount: 4,
			newTotal:        4,
			expected:        2,
		},
		{
			name:            "all messages new",
			previousCount:   5,
			previousLoaded:  0,
			newContextCount: 2,
			newTotal:        3,
			expected:        1,
		},
		{
			name:            "all messages old",
			previousCount:   5,
			previousLoaded:  5,
			newContextCount: 2,
			newTotal:        3,
			expected:        3,
		},
		{
			name:            "clamps negative old kept",
			previousCount:   6,
			previousLoaded:  2,
			newContextCount: 3,
			newTotal:        4,
			expected:        1,
		},
		{
			name:            "clamps to total",
			previousCount:   3,
			previousLoaded:  5,
			newContextCount: 1,
			newTotal:        2,
			expected:        2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := adjustLoadedMessageCount(tt.previousCount, tt.previousLoaded, tt.newContextCount, tt.newTotal)
			require.Equal(t, tt.expected, result)
		})
	}
}

// A turn that answers by calling a tool produces no assistant text. Reporting
// that as a failure made agynd replay the message, and the agent sent its reply
// again on every attempt.
func TestTurnReturnsAnEmptyResponseWithoutFailing(t *testing.T) {
	server, errCh := newLLMServer(t, []llmResponse{
		{status: http.StatusOK, body: emptyResponseBody(t)},
	})
	client := newTestLLMClient(t, server.URL)
	summarizer, err := summarize.New(client, summarize.Config{})
	require.NoError(t, err)
	agent, err := NewAgent(AgentConfig{
		Store:      stubStore{},
		LLM:        client,
		Summarizer: summarizer,
		MaxSteps:   10,
	})
	require.NoError(t, err)

	result, err := agent.Run(context.Background(), Input{
		ThreadID: "thread-1",
		Prompt:   message.NewHumanMessage("hello"),
	})
	require.NoError(t, err)
	require.Equal(t, "", result.Response)
	assertNoServerErrors(t, errCh)
}
