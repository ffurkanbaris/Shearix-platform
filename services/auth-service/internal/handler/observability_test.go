package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/barber-appointment/platform/authcontract"
	"github.com/barber-appointment/platform/logging"
	"github.com/barber-appointment/platform/obsmetrics"
	"github.com/barber-appointment/platform/otelsetup"
	"github.com/gofiber/fiber/v3"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// recordingTracer builds a real, always-sampling TracerProvider isolated
// from process-global state, mirroring platform/otelsetup's own test helper,
// so trace-id propagation can be asserted without depending on
// OTEL_EXPORTER_OTLP_ENDPOINT or Init's global side effects.
func recordingTracer(t *testing.T) trace.Tracer {
	t.Helper()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	return tp.Tracer("test")
}

// TestMetricsEndpointServesPrometheusFormatWithoutAuth confirms /metrics is
// wired the same way as /health and /ready: a bare request with no internal
// auth or tenant headers succeeds, returning Prometheus text exposition
// format. The endpoint's real protection is network isolation (the gateway
// never proxies to it), not an in-service auth check.
func TestMetricsEndpointServesPrometheusFormatWithoutAuth(t *testing.T) {
	metrics := obsmetrics.New("auth-service")
	app := fiber.New()
	app.Get("/metrics", metrics.Handler())

	res, err := app.Test(httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	contentType := res.Header.Get("Content-Type")
	if !strings.Contains(contentType, "text/plain") {
		t.Fatalf("Content-Type = %q, want text/plain prefix", contentType)
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "# HELP") {
		t.Fatalf("expected Prometheus exposition format, got: %s", body)
	}
}

// TestRateLimitBlockedIncrementsMetric drives a real login route to its
// configured limit through the registered Handler and asserts the
// registry's rate_limit_blocked_total counter for the "login" operation
// increments on the request that gets a 429.
func TestRateLimitBlockedIncrementsMetric(t *testing.T) {
	limiter := newFakeLimiter()
	metrics := obsmetrics.New("auth-service")
	h := New(serviceZero(), authcontract.Verifier(), "session", true, metrics, limiter)
	app := fiber.New()
	h.Register(app)

	for i := 0; i < 10; i++ {
		res := loginRequest(t, app)
		res.Body.Close()
	}
	blocked := loginRequest(t, app)
	defer blocked.Body.Close()
	if blocked.StatusCode != fiber.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", blocked.StatusCode)
	}

	if got := scrapeCounter(t, metrics, "rate_limit_blocked_total", "login"); got < 1 {
		t.Fatalf("rate_limit_blocked_total{operation=login} = %v, want >= 1", got)
	}
	if got := scrapeCounter(t, metrics, "rate_limit_allowed_total", "login"); got < 10 {
		t.Fatalf("rate_limit_allowed_total{operation=login} = %v, want >= 10", got)
	}
}

func loginRequest(t *testing.T, app *fiber.App) *http.Response {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/auth/login", bytes.NewReader([]byte(`{"email":"a@example.test","password":"x"}`)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Auth", authcontract.ValidToken)
	req.Header.Set("X-Tenant-ID", authcontract.TenantID)
	req.Header.Set("X-App-Type", "admin")
	res, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// scrapeCounter scrapes the registry's own /metrics handler and extracts a
// single counter sample's value for the given operation label, avoiding a
// direct dependency on internal collector fields.
func scrapeCounter(t *testing.T, metrics *obsmetrics.Registry, name, operation string) float64 {
	t.Helper()
	app := fiber.New()
	app.Get("/metrics", metrics.Handler())
	res, err := app.Test(httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	var total float64
	for _, line := range strings.Split(string(body), "\n") {
		if !strings.HasPrefix(line, name+"{") {
			continue
		}
		if !strings.Contains(line, `operation="`+operation+`"`) {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) != 2 {
			continue
		}
		var v float64
		if _, err := parseFloatField(parts[1], &v); err != nil {
			t.Fatalf("parse metric value %q: %v", parts[1], err)
		}
		total += v
	}
	return total
}

func parseFloatField(s string, out *float64) (int, error) {
	n, err := parseFloat(s)
	*out = n
	return 0, err
}

func parseFloat(s string) (float64, error) {
	var f float64
	_, err := jsonNumber(s, &f)
	return f, err
}

func jsonNumber(s string, out *float64) (int, error) {
	return 0, json.Unmarshal([]byte(s), out)
}

// TestCompletionLogIncludesTraceID exercises the exact middleware ordering
// used in main.go — RequestID -> otelsetup.Middleware ->
// logging.HTTPCompletionMiddleware -> metrics.HTTPMiddleware — and asserts
// the emitted structured log line carries a non-empty trace_id when the
// caller supplies a valid traceparent header, proving tracing runs (and
// stores its span on the fiber context) before logging reads it.
func TestCompletionLogIncludesTraceID(t *testing.T) {
	tracer := recordingTracer(t)
	metrics := obsmetrics.New("auth-service")

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil)).With("service", "auth-service")

	app := fiber.New()
	app.Use(otelsetup.Middleware(tracer))
	app.Use(logging.HTTPCompletionMiddleware(logger))
	app.Use(metrics.HTTPMiddleware())
	app.Get("/widgets/:id", func(c fiber.Ctx) error { return c.SendStatus(fiber.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/widgets/123", nil)
	req.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	res, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}

	var logLine map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &logLine); err != nil {
		t.Fatalf("expected exactly one JSON log line, got %q: %v", buf.String(), err)
	}
	traceID, _ := logLine["trace_id"].(string)
	if traceID == "" {
		t.Fatalf("expected non-empty trace_id in completion log, got %v", logLine)
	}
	if traceID != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("trace_id = %q, want the propagated traceparent trace id", traceID)
	}
}
