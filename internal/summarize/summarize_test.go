package summarize

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/agynio/agn-cli/internal/llm"
	"github.com/agynio/agn-cli/internal/message"
	"github.com/agynio/agn-cli/internal/state"
)

func TestNewSummarizerValidation(t *testing.T) {
	_, err := New(nil, Config{})
	require.Error(t, err)

	client := mustClient(t)
	_, err = New(client, Config{KeepTokens: 10, MaxTokens: 5})
	require.Error(t, err)
}

func TestNewSummarizerDefaults(t *testing.T) {
	client := mustClient(t)
	instance, err := New(client, Config{})
	require.NoError(t, err)
	require.Equal(t, DefaultKeepTokens, instance.keepTokens)
	require.Equal(t, DefaultMaxTokens, instance.maxTokens)

	count, err := instance.CountTokens(message.NewHumanMessage("abcd"))
	require.NoError(t, err)
	require.Equal(t, 2, count)
}

func TestDefaultTokenCounter(t *testing.T) {
	count, err := DefaultTokenCounter(message.NewHumanMessage("abcdefgh"))
	require.NoError(t, err)
	require.Equal(t, 3, count)

	count, err = DefaultTokenCounter(message.NewHumanMessage(""))
	require.NoError(t, err)
	require.Equal(t, 1, count)
}

func TestSummarizeUnderThreshold(t *testing.T) {
	client := mustClient(t)
	instance, err := New(client, Config{
		KeepTokens: 5,
		MaxTokens:  10,
		TokenCounter: func(msg message.Message) (int, error) {
			return 1, nil
		},
	})
	require.NoError(t, err)

	records := []state.MessageRecord{
		{
			ID:         "msg-1",
			CreatedAt:  time.Now().UTC(),
			TokenCount: 3,
			Message:    message.NewHumanMessage("hello"),
		},
		{
			ID:         "msg-2",
			CreatedAt:  time.Now().UTC(),
			TokenCount: 2,
			Message:    message.NewAIMessage("world"),
		},
	}

	result, err := instance.Summarize(context.Background(), records)
	require.NoError(t, err)
	require.Equal(t, records, result)
}

func TestSummarizeEmptyInput(t *testing.T) {
	client := mustClient(t)
	instance, err := New(client, Config{})
	require.NoError(t, err)

	result, err := instance.Summarize(context.Background(), nil)
	require.NoError(t, err)
	require.Empty(t, result)
}

func TestSummarizeEmptySummaryInput(t *testing.T) {
	client := mustClient(t)
	instance, err := New(client, Config{KeepTokens: 1, MaxTokens: 2})
	require.NoError(t, err)

	records := []state.MessageRecord{
		{
			ID:         "msg-1",
			CreatedAt:  time.Now().UTC().Add(-2 * time.Minute),
			TokenCount: 2,
			Message:    message.NewResponseMessage("raw"),
		},
		{
			ID:         "msg-2",
			CreatedAt:  time.Now().UTC().Add(-1 * time.Minute),
			TokenCount: 2,
			Message:    message.NewResponseMessage("raw"),
		},
		{
			ID:         "msg-3",
			CreatedAt:  time.Now().UTC(),
			TokenCount: 2,
			Message:    message.NewHumanMessage("keep"),
		},
	}

	result, err := instance.Summarize(context.Background(), records)
	require.NoError(t, err)
	require.Equal(t, records, result)
}

func TestSummarizeToolOutputBoundary(t *testing.T) {
	client, requests, closeServer := mockClient(t, "summary")
	t.Cleanup(closeServer)

	instance, err := New(client, Config{
		KeepTokens: 4,
		MaxTokens:  12,
		TokenCounter: func(msg message.Message) (int, error) {
			return 1, nil
		},
	})
	require.NoError(t, err)

	baseTime := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	records := []state.MessageRecord{
		{
			ID:         "msg-1",
			CreatedAt:  baseTime.Add(-4 * time.Minute),
			TokenCount: 10,
			Message:    message.NewHumanMessage("What is the weather?"),
		},
		{
			ID:         "msg-2",
			CreatedAt:  baseTime.Add(-3 * time.Minute),
			TokenCount: 3,
			Message:    message.NewToolCallMessage([]message.ToolCall{testToolCall()}),
		},
		{
			ID:         "msg-3",
			CreatedAt:  baseTime.Add(-2 * time.Minute),
			TokenCount: 3,
			Message:    message.NewToolCallOutputMessage(testToolOutput()),
		},
		{
			ID:         "msg-4",
			CreatedAt:  baseTime.Add(-1 * time.Minute),
			TokenCount: 2,
			Message:    message.NewAIMessage("18C and sunny"),
		},
	}

	result, err := instance.Summarize(context.Background(), records)
	require.NoError(t, err)
	require.Len(t, result, 4)
	require.Equal(t, int32(1), requests.Load())

	summaryMessage, ok := result[0].Message.(message.SystemMessage)
	require.True(t, ok)
	require.Equal(t, message.KindSummary, summaryMessage.Kind())
	require.Equal(t, "summary", summaryMessage.Text)
	require.Equal(t, records[0].CreatedAt, result[0].CreatedAt)
	require.Equal(t, message.KindToolCall, result[1].Message.Kind())
	require.Equal(t, message.KindToolCallOutput, result[2].Message.Kind())
	require.Equal(t, message.KindAI, result[3].Message.Kind())
}

func TestSummarizeToolOutputBoundaryMultipleOutputs(t *testing.T) {
	client, requests, closeServer := mockClient(t, "summary")
	t.Cleanup(closeServer)

	instance, err := New(client, Config{
		KeepTokens: 5,
		MaxTokens:  12,
		TokenCounter: func(msg message.Message) (int, error) {
			return 1, nil
		},
	})
	require.NoError(t, err)

	baseTime := time.Date(2025, 1, 2, 12, 0, 0, 0, time.UTC)
	records := []state.MessageRecord{
		{
			ID:         "msg-1",
			CreatedAt:  baseTime.Add(-5 * time.Minute),
			TokenCount: 8,
			Message:    message.NewHumanMessage("Fetch the forecast."),
		},
		{
			ID:         "msg-2",
			CreatedAt:  baseTime.Add(-4 * time.Minute),
			TokenCount: 3,
			Message:    message.NewToolCallMessage([]message.ToolCall{testToolCall()}),
		},
		{
			ID:         "msg-3",
			CreatedAt:  baseTime.Add(-3 * time.Minute),
			TokenCount: 3,
			Message:    message.NewToolCallOutputMessage(testToolOutput()),
		},
		{
			ID:         "msg-4",
			CreatedAt:  baseTime.Add(-2 * time.Minute),
			TokenCount: 3,
			Message: message.NewToolCallOutputMessage(message.ToolCallOutput{
				ToolCallID: "call-1",
				ToolName:   "weather",
				Output:     "Forecast updated",
			}),
		},
		{
			ID:         "msg-5",
			CreatedAt:  baseTime.Add(-1 * time.Minute),
			TokenCount: 2,
			Message:    message.NewAIMessage("Here is the forecast."),
		},
	}

	result, err := instance.Summarize(context.Background(), records)
	require.NoError(t, err)
	require.Len(t, result, 5)
	require.Equal(t, int32(1), requests.Load())
	require.Equal(t, message.KindToolCall, result[1].Message.Kind())
	require.Equal(t, message.KindToolCallOutput, result[2].Message.Kind())
	require.Equal(t, message.KindToolCallOutput, result[3].Message.Kind())
	require.Equal(t, message.KindAI, result[4].Message.Kind())
}

func TestSummarizeToolOutputNoAdjustment(t *testing.T) {
	client, requests, closeServer := mockClient(t, "summary")
	t.Cleanup(closeServer)

	instance, err := New(client, Config{
		KeepTokens: 2,
		MaxTokens:  12,
		TokenCounter: func(msg message.Message) (int, error) {
			return 1, nil
		},
	})
	require.NoError(t, err)

	baseTime := time.Date(2025, 1, 3, 12, 0, 0, 0, time.UTC)
	records := []state.MessageRecord{
		{
			ID:         "msg-1",
			CreatedAt:  baseTime.Add(-4 * time.Minute),
			TokenCount: 10,
			Message:    message.NewHumanMessage("What is the weather?"),
		},
		{
			ID:         "msg-2",
			CreatedAt:  baseTime.Add(-3 * time.Minute),
			TokenCount: 3,
			Message:    message.NewToolCallMessage([]message.ToolCall{testToolCall()}),
		},
		{
			ID:         "msg-3",
			CreatedAt:  baseTime.Add(-2 * time.Minute),
			TokenCount: 3,
			Message:    message.NewToolCallOutputMessage(testToolOutput()),
		},
		{
			ID:         "msg-4",
			CreatedAt:  baseTime.Add(-1 * time.Minute),
			TokenCount: 2,
			Message:    message.NewAIMessage("18C and sunny"),
		},
	}

	result, err := instance.Summarize(context.Background(), records)
	require.NoError(t, err)
	require.Len(t, result, 2)
	require.Equal(t, int32(1), requests.Load())
	require.Equal(t, records[2].CreatedAt, result[0].CreatedAt)
	require.Equal(t, message.KindAI, result[1].Message.Kind())
}

func TestSummarizeToolOutputKeepIndexZero(t *testing.T) {
	client, requests, closeServer := mockClient(t, "summary")
	t.Cleanup(closeServer)

	instance, err := New(client, Config{KeepTokens: 8, MaxTokens: 10})
	require.NoError(t, err)

	baseTime := time.Date(2025, 1, 4, 12, 0, 0, 0, time.UTC)
	records := []state.MessageRecord{
		{
			ID:         "msg-1",
			CreatedAt:  baseTime.Add(-3 * time.Minute),
			TokenCount: 3,
			Message:    message.NewToolCallMessage([]message.ToolCall{testToolCall()}),
		},
		{
			ID:         "msg-2",
			CreatedAt:  baseTime.Add(-2 * time.Minute),
			TokenCount: 3,
			Message:    message.NewToolCallOutputMessage(testToolOutput()),
		},
		{
			ID:         "msg-3",
			CreatedAt:  baseTime.Add(-1 * time.Minute),
			TokenCount: 5,
			Message:    message.NewAIMessage("18C and sunny"),
		},
	}

	result, err := instance.Summarize(context.Background(), records)
	require.NoError(t, err)
	require.Equal(t, records, result)
	require.Equal(t, int32(0), requests.Load())
}

func mustClient(t *testing.T) *llm.Client {
	t.Helper()
	client, err := llm.NewClient("https://example.com", "key", "model")
	require.NoError(t, err)
	return client
}

func mockClient(t *testing.T, summaryText string) (*llm.Client, *atomic.Int32, func()) {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"id": "resp-1",
		"output": []map[string]any{
			{
				"id":     "msg-1",
				"type":   "message",
				"role":   "assistant",
				"status": "completed",
				"content": []map[string]any{
					{
						"type": "output_text",
						"text": summaryText,
					},
				},
			},
		},
	})
	require.NoError(t, err)

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path != "/responses" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(payload)
	}))

	client, err := llm.NewClient(server.URL, "key", "model")
	require.NoError(t, err)
	return client, &requests, server.Close
}

func testToolCall() message.ToolCall {
	return message.ToolCall{
		ID:        "call-1",
		Name:      "weather",
		Arguments: json.RawMessage(`{"city":"Paris"}`),
	}
}

func testToolOutput() message.ToolCallOutput {
	return message.ToolCallOutput{
		ToolCallID: "call-1",
		ToolName:   "weather",
		Output:     "18C and sunny",
	}
}
