package summarize

import (
	"context"
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

func mustClient(t *testing.T) *llm.Client {
	t.Helper()
	client, err := llm.NewClient("https://example.com", "key", "model")
	require.NoError(t, err)
	return client
}
