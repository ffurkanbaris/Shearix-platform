package otelsetup

import (
	"context"

	"github.com/nats-io/nats.go"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// natsHeaderCarrier adapts nats.Header (map[string][]string, identical shape
// to http.Header) to propagation.TextMapCarrier.
type natsHeaderCarrier nats.Header

func (c natsHeaderCarrier) Get(key string) string { return nats.Header(c).Get(key) }
func (c natsHeaderCarrier) Set(key, value string) { nats.Header(c).Set(key, value) }
func (c natsHeaderCarrier) Keys() []string {
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}
	return keys
}

var natsPropagator = propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{})

// InjectNATSHeaders writes the W3C traceparent/tracestate (and baggage, if
// any) from ctx's active span into a JetStream message's headers, creating
// the Header map if the message doesn't have one yet. Safe to call with a
// no-op/disabled tracer — it then writes nothing observable, but never
// errors or panics.
func InjectNATSHeaders(ctx context.Context, msg *nats.Msg) {
	if msg.Header == nil {
		msg.Header = nats.Header{}
	}
	natsPropagator.Inject(ctx, natsHeaderCarrier(msg.Header))
}

// ExtractNATSContext returns a context carrying the remote span context
// found in a JetStream message's headers (if any), so a consumer span can
// be started as a child of the publisher's span. When the message has no
// propagated trace context (or tracing is disabled), it returns ctx
// unchanged.
func ExtractNATSContext(ctx context.Context, msg *nats.Msg) context.Context {
	if msg.Header == nil {
		return ctx
	}
	return natsPropagator.Extract(ctx, natsHeaderCarrier(msg.Header))
}

// StartConsumerSpan starts a span for processing one JetStream message,
// as a child of any trace context propagated in its headers via
// InjectNATSHeaders. Callers should defer span.End().
func StartConsumerSpan(ctx context.Context, tracer trace.Tracer, msg *nats.Msg, spanName string) (context.Context, trace.Span) {
	ctx = ExtractNATSContext(ctx, msg)
	return tracer.Start(ctx, spanName, trace.WithSpanKind(trace.SpanKindConsumer))
}
