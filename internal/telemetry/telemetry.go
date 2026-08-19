package telemetry

import (
	"context"
	"os"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

const FlushTimeout = 5 * time.Second

func Init(ctx context.Context) (*sdktrace.TracerProvider, error) {
	res, err := resource.New(
		ctx,
		resource.WithAttributes(attribute.String("service.name", "agn")),
	)
	if err != nil {
		return nil, err
	}

	endpoint := strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"))
	if endpoint == "" {
		return sdktrace.NewTracerProvider(sdktrace.WithResource(res)), nil
	}

	exporter, err := otlptracegrpc.New(ctx, otlptracegrpc.WithInsecure())
	if err != nil {
		return nil, err
	}

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)
	return provider, nil
}

// ContextFromEnvironment roots spans in the trace the caller already opened.
//
// A workload is one wake cycle and one trace: agynd opens it, derives the id
// from WORKLOAD_ID so a restart reopens rather than splits it, and hands it to
// whatever produces spans inside. Without this, agn's turns land in a trace of
// their own and a run view shows the message and the model call as unrelated
// things that happened near each other.
//
// TRACEPARENT is the W3C spelling, which is how a process hands a trace to one
// it starts. Absent or unparseable, spans root themselves as before.
func ContextFromEnvironment(ctx context.Context) context.Context {
	traceparent := strings.TrimSpace(os.Getenv("TRACEPARENT"))
	if traceparent == "" {
		return ctx
	}
	carrier := propagation.MapCarrier{"traceparent": traceparent}
	if tracestate := strings.TrimSpace(os.Getenv("TRACESTATE")); tracestate != "" {
		carrier["tracestate"] = tracestate
	}
	extracted := propagation.TraceContext{}.Extract(ctx, carrier)
	if !trace.SpanContextFromContext(extracted).IsValid() {
		return ctx
	}
	return extracted
}
