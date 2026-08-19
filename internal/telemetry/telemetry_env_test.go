package telemetry

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/trace"
)

// agynd derives the trace from WORKLOAD_ID and hands it over, so a turn agn
// reports lands in the cycle that asked for it rather than in one of its own.
func TestContextFromEnvironmentAdoptsTheCallersTrace(t *testing.T) {
	t.Setenv("TRACEPARENT", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")

	spanContext := trace.SpanContextFromContext(ContextFromEnvironment(context.Background()))
	if !spanContext.IsValid() {
		t.Fatal("expected the inherited trace to be adopted")
	}
	if got := spanContext.TraceID().String(); got != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("expected the caller's trace, got %s", got)
	}
	if !spanContext.IsRemote() {
		t.Fatal("expected the parent to be recorded as remote")
	}
}

func TestContextFromEnvironmentWithoutOneIsUnchanged(t *testing.T) {
	t.Setenv("TRACEPARENT", "")
	if trace.SpanContextFromContext(ContextFromEnvironment(context.Background())).IsValid() {
		t.Fatal("expected no span context when nothing was handed over")
	}
}

func TestContextFromEnvironmentIgnoresGarbage(t *testing.T) {
	t.Setenv("TRACEPARENT", "not-a-traceparent")
	if trace.SpanContextFromContext(ContextFromEnvironment(context.Background())).IsValid() {
		t.Fatal("expected an unparseable traceparent to be ignored, not adopted")
	}
}
