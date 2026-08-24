package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/barber-appointment/gateway-service/internal/service"
	"github.com/barber-appointment/platform/httpx"
	"github.com/barber-appointment/platform/obsmetrics"
	"github.com/barber-appointment/platform/otelsetup"
	"github.com/gofiber/fiber/v3"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// TestMetricsRouteTableNeverProxiesToABackend is a static regression check:
// no entry in the gateway's registered proxy route table (routes.go) ever
// targets a backend "/metrics" path. This is the contract that keeps every
// backend service's private Prometheus endpoint unreachable from outside the
// Docker-internal network — the gateway is the only thing that could ever
// bridge it to the public internet, and it must never register such a route.
func TestMetricsRouteTableNeverProxiesToABackend(t *testing.T) {
	app := fiber.New()
	h := New(service.Resolver{}, "http://tenant", "http://auth", "http://barber", "http://catalog", "http://scheduling", "http://appointment", "http://notification", "http://customer", "token", "platform-token", time.Second)
	h.Register(app)

	for _, route := range app.GetRoutes(true) {
		if strings.HasSuffix(route.Path, "/metrics") && route.Path != "/metrics" {
			t.Fatalf("gateway route %q targets a backend metrics-shaped path — this must never be proxied publicly", route.Path)
		}
	}
}

// TestMetricsIsServedLocallyNeverProxied proves the gateway's own /metrics
// endpoint is answered by its local registry, not forwarded to any backend:
// every backend service URL is pointed at an address nothing is listening
// on, so a proxied request would fail/timeout — /metrics must still succeed.
func TestMetricsIsServedLocallyNeverProxied(t *testing.T) {
	metrics := obsmetrics.New("gateway-service")
	app := fiber.New()
	app.Get("/metrics", metrics.Handler())

	unreachable := "http://127.0.0.1:1"
	h := New(service.Resolver{}, unreachable, unreachable, unreachable, unreachable, unreachable, unreachable, unreachable, unreachable, "token", "platform-token", time.Second)
	h.Register(app)

	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("gateway's own /metrics must succeed even when every backend URL is unreachable (proving it is never proxied), got status %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/plain") {
		t.Fatalf("expected prometheus exposition format, got %q", ct)
	}
}

// TestFrontendCatchAllNeverReachesBackendMetrics proves that even the
// catch-all page/static dispatcher (which forwards unmatched non-/api/
// non-/webhooks/ paths to admin-web/booking-web) is not a path by which a
// client could reach a *backend* service's /metrics: it only ever targets
// adminWebURL/bookingWebURL, both of which are distinct Next.js frontends
// with no /metrics endpoint of their own, never one of the tenant/auth/
// barber/catalog/scheduling/appointment/notification/customer URLs.
func TestFrontendCatchAllNeverReachesBackendMetrics(t *testing.T) {
	var backendHit bool
	backendMetrics := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		backendHit = true
		w.WriteHeader(http.StatusOK)
	}))
	defer backendMetrics.Close()

	resolverBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tenant_id":"00000000-0000-0000-0000-000000000001","app_type":"booking"}`))
	}))
	defer resolverBackend.Close()

	frontendBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer frontendBackend.Close()

	app := fiber.New()
	resolver := service.NewResolver(resolverBackend.URL, "internal")
	// Every backend service URL is deliberately pointed at backendMetrics —
	// if the frontend catch-all could somehow reach a backend, this test
	// would observe backendHit=true.
	h := New(resolver, backendMetrics.URL, backendMetrics.URL, backendMetrics.URL, backendMetrics.URL, backendMetrics.URL, backendMetrics.URL, backendMetrics.URL, backendMetrics.URL, "internal", "platform-token", time.Second, frontendBackend.URL, frontendBackend.URL)
	h.Register(app)

	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp
	if backendHit {
		t.Fatal("a request for /metrics must never reach a backend service through the frontend catch-all dispatcher")
	}
}

// TestRequestIDForwardedToBackendMatchesResponseHeader proves the gateway
// forwards the SAME final resolved request ID it returns to the caller —
// not a stale/empty re-read of the inbound header — to every internal
// service it proxies to.
func TestRequestIDForwardedToBackendMatchesResponseHeader(t *testing.T) {
	var receivedRequestID string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedRequestID = r.Header.Get("X-Request-ID")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"timezone":"UTC"}`))
	}))
	defer backend.Close()

	app := fiber.New()
	app.Use(httpx.RequestID())
	h := New(testResolver(t), backend.URL, "http://auth", "http://barber", "http://catalog", "http://scheduling", "http://appointment", "http://notification", "http://customer", "internal", "platform-token", time.Second)
	h.Register(app)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/public/config", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	responseRequestID := resp.Header.Get("X-Request-ID")
	if responseRequestID == "" {
		t.Fatal("expected a generated X-Request-ID on the gateway's own response")
	}
	if receivedRequestID == "" {
		t.Fatal("expected the backend to receive a non-empty X-Request-ID")
	}
	if receivedRequestID != responseRequestID {
		t.Fatalf("backend received X-Request-ID %q, want it to match the response header %q exactly", receivedRequestID, responseRequestID)
	}
}

// TestRequestIDCallerSuppliedIsPreservedEndToEnd proves a valid caller-
// supplied X-Request-ID survives unchanged through the gateway to both the
// backend and the response.
func TestRequestIDCallerSuppliedIsPreservedEndToEnd(t *testing.T) {
	var receivedRequestID string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedRequestID = r.Header.Get("X-Request-ID")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"timezone":"UTC"}`))
	}))
	defer backend.Close()

	app := fiber.New()
	app.Use(httpx.RequestID())
	h := New(testResolver(t), backend.URL, "http://auth", "http://barber", "http://catalog", "http://scheduling", "http://appointment", "http://notification", "http://customer", "internal", "platform-token", time.Second)
	h.Register(app)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/public/config", nil)
	req.Header.Set("X-Request-ID", "caller-supplied-abc-123")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if got := resp.Header.Get("X-Request-ID"); got != "caller-supplied-abc-123" {
		t.Fatalf("response X-Request-ID = %q, want the caller-supplied value preserved", got)
	}
	if receivedRequestID != "caller-supplied-abc-123" {
		t.Fatalf("backend received X-Request-ID = %q, want the caller-supplied value forwarded unchanged", receivedRequestID)
	}
}

// TestTraceparentPropagatedToBackend proves an inbound W3C traceparent is
// forwarded to the proxied backend request once the gateway's outbound
// client transport is wrapped with otelsetup.WrapTransport and a recording
// tracer is active on the request context via otelsetup.Middleware.
func TestTraceparentPropagatedToBackend(t *testing.T) {
	var receivedTraceparent string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedTraceparent = r.Header.Get("traceparent")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"timezone":"UTC"}`))
	}))
	defer backend.Close()

	tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()))
	defer func() { _ = tp.Shutdown(context.Background()) }()
	tracer := tp.Tracer("test")

	app := fiber.New()
	app.Use(httpx.RequestID())
	app.Use(otelsetup.Middleware(tracer))
	h := New(testResolver(t), backend.URL, "http://auth", "http://barber", "http://catalog", "http://scheduling", "http://appointment", "http://notification", "http://customer", "internal", "platform-token", time.Second)
	h.Register(app)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/public/config", nil)
	req.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	if _, err := app.Test(req); err != nil {
		t.Fatal(err)
	}
	if receivedTraceparent == "" {
		t.Fatal("expected the outbound proxied request to carry a traceparent header when the inbound request had an active trace context")
	}
	if !strings.Contains(receivedTraceparent, "4bf92f3577b34da6a3ce929d0e0e4736") {
		t.Fatalf("forwarded traceparent %q does not carry the propagated trace id", receivedTraceparent)
	}
}

// testResolver builds a working service.Resolver backed by a fake HTTP
// tenant-service that always resolves any hostname to a fixed tenant/app
// type, for tests that exercise a route requiring hostname resolution but
// aren't testing resolution behavior itself.
func testResolver(t *testing.T) service.Resolver {
	t.Helper()
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tenant_id":"00000000-0000-0000-0000-000000000001","app_type":"booking"}`))
	}))
	t.Cleanup(backend.Close)
	return service.NewResolver(backend.URL, "internal")
}
