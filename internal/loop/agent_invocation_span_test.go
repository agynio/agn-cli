package loop

import (
	"context"
	"net/http"
	"testing"

	"github.com/agynio/agn-cli/internal/message"
	"github.com/agynio/agn-cli/internal/summarize"
	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// agynd records invocation.message before the message reaches this process, so
// a second one here puts two on a single turn -- and a reader counting turns
// sees two. The platform hands the trace over in TRACEPARENT; standalone there
// is no such caller and the span is this turn's root.
func TestRunSkipsTheInvocationSpanUnderAnInheritedTrace(t *testing.T) {
	names := runTurnAndCollectSpanNames(t, inheritedTraceContext(t))
	require.NotContains(t, names, "invocation.message",
		"the caller already recorded the turn; a second one double-counts it")
}

func TestRunOpensAnInvocationSpanWhenItIsTheRoot(t *testing.T) {
	names := runTurnAndCollectSpanNames(t, context.Background())
	require.Contains(t, names, "invocation.message",
		"standalone, nothing else marks where the turn began")
}

func runTurnAndCollectSpanNames(t *testing.T, ctx context.Context) []string {
	t.Helper()
	server, errCh := newLLMServer(t, []llmResponse{
		{status: http.StatusOK, body: textResponseBody(t, "hi")},
	})
	client := newTestLLMClient(t, server.URL)
	summarizer, err := summarize.New(client, summarize.Config{})
	require.NoError(t, err)
	spanRecorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	agent, err := NewAgent(AgentConfig{
		Store:      stubStore{},
		LLM:        client,
		Summarizer: summarizer,
		MaxSteps:   10,
		Tracer:     provider.Tracer("test"),
	})
	require.NoError(t, err)

	_, err = agent.Run(ctx, Input{ThreadID: "thread-1", Prompt: message.NewHumanMessage("hello")})
	require.NoError(t, err)
	assertNoServerErrors(t, errCh)

	names := make([]string, 0, 4)
	for _, span := range spanRecorder.Ended() {
		names = append(names, span.Name())
	}
	return names
}

func inheritedTraceContext(t *testing.T) context.Context {
	t.Helper()
	traceID, err := trace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
	require.NoError(t, err)
	spanID, err := trace.SpanIDFromHex("00f067aa0ba902b7")
	require.NoError(t, err)
	return trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		Remote:     true,
		TraceFlags: trace.FlagsSampled,
	}))
}
