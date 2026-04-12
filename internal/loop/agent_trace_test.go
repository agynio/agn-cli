package loop

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/agynio/agn-cli/internal/llm"
	"github.com/agynio/agn-cli/internal/message"
	"github.com/agynio/agn-cli/internal/state"
	"github.com/agynio/agn-cli/internal/summarize"
)

func TestCallModelSpanCoalescingFirstCallSuccess(t *testing.T) {
	server, errCh := newLLMServer(t, []llmResponse{
		{status: http.StatusOK, body: textResponseBody(t, "hello")},
	})
	client := newTestLLMClient(t, server.URL)
	spanRecorder, agent := newTestAgent(t, client)
	state := newTestState(t, agent)

	err := agent.callModel(context.Background(), state)
	require.NoError(t, err)
	assertNoServerErrors(t, errCh)

	spans := spanRecorder.Ended()
	require.Len(t, spans, 1)
	require.Equal(t, "llm.call", spans[0].Name())
}

func TestCallModelSpanCoalescingFirstCallError(t *testing.T) {
	body := errorResponseBody(t, "boom")
	server, errCh := newLLMServer(t, []llmResponse{
		{status: http.StatusInternalServerError, body: body},
		{status: http.StatusInternalServerError, body: body},
		{status: http.StatusInternalServerError, body: body},
	})
	client := newTestLLMClient(t, server.URL)
	spanRecorder, agent := newTestAgent(t, client)
	state := newTestState(t, agent)

	err := agent.callModel(context.Background(), state)
	require.Error(t, err)
	assertNoServerErrors(t, errCh)

	spans := spanRecorder.Ended()
	require.Len(t, spans, 1)
	require.Equal(t, "llm.call", spans[0].Name())
}

func TestCallModelSpanCoalescingToolCallOnlySkipsSpan(t *testing.T) {
	server, errCh := newLLMServer(t, []llmResponse{
		{status: http.StatusOK, body: toolCallResponseBody(t, "call-1", "tool")},
	})
	client := newTestLLMClient(t, server.URL)
	spanRecorder, agent := newTestAgent(t, client)
	state := newTestState(t, agent)
	state.LLMCallCount = 1

	err := agent.callModel(context.Background(), state)
	require.NoError(t, err)
	assertNoServerErrors(t, errCh)

	spans := spanRecorder.Ended()
	require.Empty(t, spans)
}

func TestCallModelSpanCoalescingSubsequentTextRecordsSpan(t *testing.T) {
	server, errCh := newLLMServer(t, []llmResponse{
		{status: http.StatusOK, body: textResponseBody(t, "done")},
	})
	client := newTestLLMClient(t, server.URL)
	spanRecorder, agent := newTestAgent(t, client)
	state := newTestState(t, agent)
	state.LLMCallCount = 1

	err := agent.callModel(context.Background(), state)
	require.NoError(t, err)
	assertNoServerErrors(t, errCh)

	spans := spanRecorder.Ended()
	require.Len(t, spans, 1)
	require.Equal(t, "llm.call", spans[0].Name())
}

type llmResponse struct {
	status int
	body   []byte
}

type llmServer struct {
	mu        sync.Mutex
	responses []llmResponse
	index     int
	errCh     chan error
}

func newLLMServer(t *testing.T, responses []llmResponse) (*httptest.Server, <-chan error) {
	t.Helper()
	serverState := &llmServer{responses: responses, errCh: make(chan error, 1)}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverState.recordUnexpected(r)
		serverState.respond(w)
	}))
	t.Cleanup(server.Close)
	return server, serverState.errCh
}

func (s *llmServer) recordUnexpected(r *http.Request) {
	if r.Method != http.MethodPost {
		s.recordError(fmt.Errorf("unexpected method %s", r.Method))
	}
	_, _ = io.Copy(io.Discard, r.Body)
	_ = r.Body.Close()
}

func (s *llmServer) respond(w http.ResponseWriter) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.index >= len(s.responses) {
		s.recordError(fmt.Errorf("unexpected llm request"))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	resp := s.responses[s.index]
	s.index++
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.status)
	_, _ = w.Write(resp.body)
}

func (s *llmServer) recordError(err error) {
	select {
	case s.errCh <- err:
	default:
	}
}

func assertNoServerErrors(t *testing.T, errCh <-chan error) {
	t.Helper()
	select {
	case err := <-errCh:
		require.NoError(t, err)
	default:
	}
}

type stubStore struct{}

func (s stubStore) Load(ctx context.Context, threadID string) (state.Thread, error) {
	return state.Thread{ID: threadID}, nil
}

func (s stubStore) Save(ctx context.Context, thread state.Thread) error {
	return nil
}

func (s stubStore) List(ctx context.Context) ([]state.ThreadSummary, error) {
	return nil, nil
}

func newTestLLMClient(t *testing.T, endpoint string) *llm.Client {
	t.Helper()
	client, err := llm.NewClient(endpoint, "test-key", "gpt-test")
	require.NoError(t, err)
	return client
}

func newTestAgent(t *testing.T, client *llm.Client) (*tracetest.SpanRecorder, *Agent) {
	t.Helper()
	spanRecorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
	t.Cleanup(func() {
		_ = provider.Shutdown(context.Background())
	})
	summarizer, err := summarize.New(client, summarize.Config{})
	require.NoError(t, err)
	agent, err := NewAgent(AgentConfig{
		Store:      stubStore{},
		LLM:        client,
		Summarizer: summarizer,
		MaxSteps:   1,
		Tracer:     provider.Tracer("test"),
	})
	require.NoError(t, err)
	return spanRecorder, agent
}

func newTestState(t *testing.T, agent *Agent) *State {
	t.Helper()
	record, err := agent.recordFromMessage("", message.NewHumanMessage("hello"))
	require.NoError(t, err)
	return &State{
		Thread:             state.Thread{ID: "thread-1", Messages: []state.MessageRecord{record}},
		TurnID:             "turn-1",
		LoadedMessageCount: 1,
	}
}

func textResponseBody(t *testing.T, text string) []byte {
	t.Helper()
	return mustJSON(t, map[string]any{
		"id":     "resp-1",
		"model":  "gpt-test",
		"status": "completed",
		"output": []map[string]any{
			{
				"id":     "msg-1",
				"type":   "message",
				"role":   "assistant",
				"status": "completed",
				"content": []map[string]any{
					{
						"type":        "output_text",
						"text":        text,
						"annotations": []any{},
					},
				},
			},
		},
	})
}

func toolCallResponseBody(t *testing.T, callID, name string) []byte {
	t.Helper()
	return mustJSON(t, map[string]any{
		"id":     "resp-2",
		"model":  "gpt-test",
		"status": "completed",
		"output": []map[string]any{
			{
				"id":        callID,
				"type":      "function_call",
				"call_id":   callID,
				"name":      name,
				"arguments": "{\"foo\":\"bar\"}",
			},
		},
	})
}

func errorResponseBody(t *testing.T, message string) []byte {
	t.Helper()
	return mustJSON(t, map[string]any{
		"error": map[string]any{
			"message": message,
		},
	})
}

func mustJSON(t *testing.T, payload any) []byte {
	t.Helper()
	data, err := json.Marshal(payload)
	require.NoError(t, err)
	return data
}
