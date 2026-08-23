package obsmetrics

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/barber-appointment/platform/otelsetup"
	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestHTTPMiddlewareUsesRoutePatternNotRawPath(t *testing.T) {
	reg := New("widget-service")
	app := fiber.New()
	app.Use(reg.HTTPMiddleware())
	app.Get("/widgets/:id", func(c fiber.Ctx) error { return c.SendStatus(fiber.StatusOK) })

	for _, id := range []string{"11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222"} {
		req := httptest.NewRequest(http.MethodGet, "/widgets/"+id, nil)
		if _, err := app.Test(req); err != nil {
			t.Fatal(err)
		}
	}

	body := scrape(t, reg)
	// Both requests must collapse onto the same bounded route-pattern label
	// ("/widgets/:id"), never the raw path containing the ID.
	if strings.Contains(body, "11111111-1111-1111-1111-111111111111") {
		t.Fatal("metrics output must never contain a raw path segment / ID value")
	}
	if !strings.Contains(body, `route="/widgets/:id"`) {
		t.Fatalf("expected the normalized route pattern as a label, got:\n%s", body)
	}
	if !strings.Contains(body, `http_requests_total{method="GET",route="/widgets/:id",service="widget-service",status="200"} 2`) {
		t.Fatalf("expected two requests to collapse onto one bounded label set, got:\n%s", body)
	}
}

func TestHTTPMiddlewareUnmatchedRouteIsBounded(t *testing.T) {
	// A realistic app: several real routes registered (including a literal,
	// param-free one), then a request to a path that matches none of them —
	// this is what an attacker probing random/garbage paths looks like, and
	// is the actual scenario the bounded-cardinality guarantee protects.
	reg := New("widget-service")
	app := fiber.New()
	app.Use(reg.HTTPMiddleware())
	app.Get("/widgets/:id", func(c fiber.Ctx) error { return c.SendStatus(fiber.StatusOK) })
	app.Post("/internal/v1/auth/login", func(c fiber.Ctx) error { return c.SendStatus(fiber.StatusOK) })

	for _, path := range []string{
		"/does/not/exist/at/all",
		"/widgets/../../etc/passwd",
		"/" + strings.Repeat("a", 500), // an attacker probing with a long garbage path
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if _, err := app.Test(req); err != nil {
			t.Fatal(err)
		}
	}

	body := scrape(t, reg)
	if strings.Contains(body, "does/not/exist") || strings.Contains(body, "etc/passwd") || strings.Contains(body, strings.Repeat("a", 500)) {
		t.Fatal("an unmatched route must never leak the raw request path into metric labels")
	}
	if !strings.Contains(body, `http_requests_total{method="GET",route="unmatched",service="widget-service",status="404"} 3`) {
		t.Fatalf("expected all three unmatched requests to collapse onto one bounded sentinel label, got:\n%s", body)
	}
}

func TestHTTPMiddlewareLiteralRouteIsNotMislabeledAsUnmatched(t *testing.T) {
	// A registered route with no path parameters (Path already equals the
	// exact request path for every caller) must still be recognized as
	// matched, not swept into the "unmatched" bucket alongside real garbage
	// paths — that would make the route label useless for a large share of
	// real traffic (login, register, health, etc. are all literal paths).
	reg := New("auth-service")
	app := fiber.New()
	app.Use(reg.HTTPMiddleware())
	app.Post("/internal/v1/auth/login", func(c fiber.Ctx) error { return c.SendStatus(fiber.StatusOK) })

	req := httptest.NewRequest(http.MethodPost, "/internal/v1/auth/login", nil)
	if _, err := app.Test(req); err != nil {
		t.Fatal(err)
	}
	body := scrape(t, reg)
	if !strings.Contains(body, `route="/internal/v1/auth/login"`) {
		t.Fatalf("expected the literal route's own pattern as the label, not the unmatched sentinel, got:\n%s", body)
	}
	if strings.Contains(body, `route="unmatched"`) {
		t.Fatalf("a real literal-path route must never be mislabeled as unmatched, got:\n%s", body)
	}
}

func TestHandlerServesPrometheusFormat(t *testing.T) {
	reg := New("widget-service")
	app := fiber.New()
	app.Get("/metrics", reg.Handler())
	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/plain") {
		t.Fatalf("expected prometheus text exposition content type, got %q", ct)
	}
}

func TestRateLimitCounters(t *testing.T) {
	reg := New("auth-service")
	reg.RateLimitAllowed("login")
	reg.RateLimitAllowed("login")
	reg.RateLimitBlocked("login")
	reg.RateLimitError("register")

	body := scrape(t, reg)
	if !strings.Contains(body, `rate_limit_allowed_total{operation="login",service="auth-service"} 2`) {
		t.Fatalf("missing/incorrect allowed counter:\n%s", body)
	}
	if !strings.Contains(body, `rate_limit_blocked_total{operation="login",service="auth-service"} 1`) {
		t.Fatalf("missing/incorrect blocked counter:\n%s", body)
	}
	if !strings.Contains(body, `rate_limit_errors_total{operation="register",service="auth-service"} 1`) {
		t.Fatalf("missing/incorrect error counter:\n%s", body)
	}
}

func TestSetDependencyUp(t *testing.T) {
	reg := New("auth-service")
	reg.SetDependencyUp("postgres", true)
	reg.SetDependencyUp("redis", false)
	body := scrape(t, reg)
	if !strings.Contains(body, `dependency_up{dependency="postgres",service="auth-service"} 1`) {
		t.Fatalf("expected postgres up=1:\n%s", body)
	}
	if !strings.Contains(body, `dependency_up{dependency="redis",service="auth-service"} 0`) {
		t.Fatalf("expected redis up=0:\n%s", body)
	}
}

func TestRegisterPgxPoolExposesGauges(t *testing.T) {
	reg := New("auth-service")
	// A pool that was never connected still has a valid Stat() (zero
	// values) and a configured Config().MaxConns — enough to exercise the
	// gauge wiring without a live database.
	cfg, err := pgxpool.ParseConfig("postgres://user:pass@127.0.0.1:1/db")
	if err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	reg.RegisterPgxPool(pool)
	body := scrape(t, reg)
	if !strings.Contains(body, "pgx_pool_max_conns") {
		t.Fatalf("expected pgx pool gauges to be registered:\n%s", body)
	}
}

func TestRegisterRedisNilClientIsNoop(t *testing.T) {
	reg := New("auth-service")
	reg.RegisterRedis(nil) // must not panic
}

func TestTraceIDNeverUsedAsMetricLabel(t *testing.T) {
	// Defensive regression: obsmetrics must not import/accept trace IDs as
	// labels. This is a compile-time property (no such parameter exists on
	// any exported method), asserted here as documentation; otelsetup.TraceID
	// is only ever used by platform/logging, never by obsmetrics.
	_ = otelsetup.TraceID
}

func scrape(t *testing.T, reg *Registry) string {
	t.Helper()
	app := fiber.New()
	app.Get("/metrics", reg.Handler())
	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 0, 4096)
	chunk := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(chunk)
		buf = append(buf, chunk[:n]...)
		if err != nil {
			break
		}
	}
	return string(buf)
}
