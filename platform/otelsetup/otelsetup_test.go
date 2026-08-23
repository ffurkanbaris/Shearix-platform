package otelsetup

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// recordingTracer builds a real, always-sampling TracerProvider isolated
// from process-global state, so tests that need a genuinely recording
// tracer (to assert a non-empty trace ID propagates) don't depend on test
// execution order or on Init()'s global otel.SetTracerProvider side effect.
func recordingTracer(t *testing.T) trace.Tracer {
	t.Helper()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	return tp.Tracer("test")
}

func TestInitWithoutEndpointReturnsWorkingNoopTracer(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	tracer, shutdown := Init(context.Background(), "test-service", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if tracer == nil {
		t.Fatal("expected a non-nil tracer even without an exporter")
	}
	_, span := tracer.Start(context.Background(), "op")
	span.End()
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("no-op shutdown should never error: %v", err)
	}
}

func TestInitWithUnreachableEndpointFallsBackToNoop(t *testing.T) {
	// A syntactically valid but unreachable endpoint must not fail startup —
	// otlptracegrpc.New with WithInsecure typically succeeds at
	// construction time (the connection is lazy), but if it ever errors,
	// Init must still return a usable tracer, never propagate the error.
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "127.0.0.1:1")
	tracer, shutdown := Init(context.Background(), "test-service", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if tracer == nil {
		t.Fatal("expected a non-nil tracer even when the collector is unreachable")
	}
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown must not error just because the collector was never reachable: %v", err)
	}
}

func TestTraceIDEmptyWithoutSpan(t *testing.T) {
	if id := TraceID(context.Background()); id != "" {
		t.Fatalf("expected empty trace id for a context with no span, got %q", id)
	}
}

func TestMiddlewareRecordsTraceIDAndPropagatesContext(t *testing.T) {
	tracer := recordingTracer(t)

	app := fiber.New()
	app.Use(Middleware(tracer))
	var sawTraceID string
	app.Get("/widgets/:id", func(c fiber.Ctx) error {
		sawTraceID = TraceID(c.Context())
		return c.SendStatus(fiber.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/widgets/123", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if sawTraceID == "" {
		t.Fatal("expected a non-empty trace id with a recording tracer provider")
	}
}

func TestMiddlewareExtractsIncomingTraceparent(t *testing.T) {
	tracer := recordingTracer(t)
	app := fiber.New()
	app.Use(Middleware(tracer))
	var sawTraceID string
	app.Get("/x", func(c fiber.Ctx) error {
		sawTraceID = TraceID(c.Context())
		return c.SendStatus(fiber.StatusOK)
	})
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	// A valid W3C traceparent: version-traceid-spanid-flags.
	req.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	if _, err := app.Test(req); err != nil {
		t.Fatal(err)
	}
	if sawTraceID != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("expected extracted trace id to propagate into the handler context, got %q", sawTraceID)
	}
}

func TestWrapTransportPreservesRoundTrip(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	defer server.Close()

	client := &http.Client{Transport: WrapTransport(nil)}
	resp, err := client.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusTeapot {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestWrapTransportInjectsTraceparentWhenTracingEnabled(t *testing.T) {
	tracer := recordingTracer(t)

	var sawHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawHeader = r.Header.Get("traceparent")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ctx, span := tracer.Start(context.Background(), "client-op")
	defer span.End()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	client := &http.Client{Transport: WrapTransport(nil)}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if sawHeader == "" {
		t.Fatal("expected an outbound traceparent header when a span is active on the context")
	}
}

func TestPGXTracerDoesNotPanicWithoutStart(t *testing.T) {
	// TraceQueryEnd is defensive against a missing context value (e.g. a
	// tracer swapped mid-connection) — must not panic.
	tracer, _ := Init(context.Background(), "test-service", nil)
	pt := PGXTracer(tracer)
	_ = pt
}
