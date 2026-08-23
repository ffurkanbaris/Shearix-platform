// Package otelsetup provides shared OpenTelemetry tracing wiring for every
// service. Tracing is strictly optional: when OTEL_EXPORTER_OTLP_ENDPOINT is
// unset, or the exporter cannot be constructed, every function here falls
// back to a no-op tracer provider. Nothing in this package may fail service
// startup or affect the /ready endpoint — an exporter outage degrades
// tracing silently, never business traffic.
package otelsetup

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/barber-appointment/platform/httpx"
	"github.com/gofiber/fiber/v3"
)

// Shutdown flushes and closes the tracer provider. Safe to call even when
// tracing never initialized an exporter (the no-op path returns a no-op
// shutdown).
type Shutdown func(context.Context) error

// Init configures the global TracerProvider and W3C tracecontext/baggage
// propagator, and returns a Tracer for the given service plus a Shutdown
// func the caller should invoke during graceful shutdown. It never returns
// an error: any failure to reach the collector degrades to a no-op tracer
// so the service still starts and serves traffic normally.
func Init(ctx context.Context, serviceName string, logger *slog.Logger) (trace.Tracer, Shutdown) {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))

	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if endpoint == "" {
		return otel.Tracer(serviceName), func(context.Context) error { return nil }
	}

	initCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	exporter, err := otlptracegrpc.New(initCtx,
		otlptracegrpc.WithEndpoint(endpoint),
		otlptracegrpc.WithInsecure(),
		otlptracegrpc.WithTimeout(5*time.Second),
	)
	if err != nil {
		if logger != nil {
			logger.Warn("otel exporter unavailable, tracing disabled", "error", err)
		}
		return otel.Tracer(serviceName), func(context.Context) error { return nil }
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(newResource(serviceName)),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(sampleRatio()))),
	)
	otel.SetTracerProvider(tp)
	return tp.Tracer(serviceName), func(shutdownCtx context.Context) error {
		shutdownCtx, cancel := context.WithTimeout(shutdownCtx, 5*time.Second)
		defer cancel()
		return tp.Shutdown(shutdownCtx)
	}
}

func newResource(serviceName string) *resource.Resource {
	r, err := resource.Merge(resource.Default(), resource.NewSchemaless(semconv.ServiceName(serviceName)))
	if err != nil {
		return resource.NewSchemaless(semconv.ServiceName(serviceName))
	}
	return r
}

func sampleRatio() float64 {
	v := os.Getenv("OTEL_TRACES_SAMPLER_RATIO")
	if v == "" {
		return 1.0
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil || f < 0 || f > 1 {
		return 1.0
	}
	return f
}

// Middleware starts one server span per inbound HTTP request, extracting any
// W3C traceparent/tracestate the caller supplied, and stores the resulting
// context back onto the fiber Ctx so downstream repository/HTTP/NATS calls
// that thread ctx through automatically become children of this span. It
// never logs or records request bodies, headers, cookies, or credentials —
// only route, method and status, matching the same fields used in
// platform/logging and platform/obsmetrics.
func Middleware(tracer trace.Tracer) fiber.Handler {
	propagator := propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{})
	return func(c fiber.Ctx) error {
		carrier := propagation.HeaderCarrier{}
		c.Request().Header.VisitAll(func(key, value []byte) {
			carrier.Set(string(key), string(value))
		})
		ctx := propagator.Extract(c.Context(), carrier)

		// The route pattern isn't known until routing finishes resolving
		// inside c.Next(), so the span starts with a bounded placeholder
		// name and is renamed once the real (still bounded) route pattern
		// or the "unmatched" sentinel is known — never the raw path.
		ctx, span := tracer.Start(ctx, c.Method()+" request", trace.WithSpanKind(trace.SpanKindServer))
		defer span.End()
		c.SetContext(ctx)

		err := c.Next()

		route := httpx.RoutePattern(c, err)
		span.SetName(c.Method() + " " + route)
		span.SetAttributes(
			attribute.String("http.method", c.Method()),
			attribute.String("http.route", route),
			attribute.Int("http.status_code", httpx.ResolveStatus(c, err)),
		)
		return err
	}
}

// TraceID returns the active trace ID for the request's context, or "" when
// tracing is disabled/no-op or no span is recorded. Safe to call from
// logging middleware — never panics.
func TraceID(ctx context.Context) string {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return ""
	}
	return sc.TraceID().String()
}

// WrapTransport instruments an outbound http.RoundTripper so every request
// made through it becomes a client span (when tracing is enabled) and
// carries a W3C traceparent header to the callee. base may be nil, in which
// case http.DefaultTransport is used. Safe to call unconditionally — with a
// no-op tracer provider this only adds propagation headers.
func WrapTransport(base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	// Pass the W3C propagator explicitly rather than relying on whatever is
	// currently installed as the process-global propagator (via
	// otel.SetTextMapPropagator, set in Init) — this keeps outbound header
	// injection correct even if a caller wraps a transport before Init runs,
	// or in tests that construct a transport in isolation.
	return otelhttp.NewTransport(base, otelhttp.WithPropagators(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{})))
}
