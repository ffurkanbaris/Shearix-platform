package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/barber-appointment/platform/httpx"
	"github.com/barber-appointment/platform/logging"
	"github.com/barber-appointment/platform/obsmetrics"
	"github.com/barber-appointment/platform/otelsetup"
	"github.com/gofiber/fiber/v3"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// recordingTracer builds a real, always-sampling TracerProvider isolated
// from process-global state, matching platform/otelsetup/otelsetup_test.go's
// helper of the same name.
func recordingTracer(t *testing.T) trace.Tracer {
	t.Helper()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	return tp.Tracer("test")
}

func TestMetricsEndpointNotGatedBehindAuth(t *testing.T) {
	metrics := obsmetrics.New("barber-service")
	app := fiber.New()
	app.Use(httpx.RequestID())
	app.Get("/metrics", metrics.Handler())

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	contentType := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "text/plain") {
		t.Fatalf("Content-Type = %q, want text/plain prefix", contentType)
	}
}

func TestHTTPCompletionLogIncludesTraceIDWithMiddlewareOrdering(t *testing.T) {
	tracer := recordingTracer(t)
	metrics := obsmetrics.New("barber-service")

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil)).With("service", "barber-service")

	app := fiber.New()
	app.Use(httpx.RequestID())
	app.Use(otelsetup.Middleware(tracer))
	app.Use(logging.HTTPCompletionMiddleware(logger))
	app.Use(metrics.HTTPMiddleware())
	app.Get("/widgets/:id", func(c fiber.Ctx) error {
		return c.SendStatus(fiber.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/widgets/123", nil)
	req.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var logLine map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &logLine); err != nil {
		t.Fatalf("failed to parse log line %q: %v", buf.String(), err)
	}
	traceID, _ := logLine["trace_id"].(string)
	if traceID == "" {
		t.Fatalf("expected non-empty trace_id in completion log, got line: %s", buf.String())
	}
	if traceID != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("trace_id = %q, want the propagated trace id", traceID)
	}
}
