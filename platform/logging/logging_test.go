package logging

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/barber-appointment/platform/httpx"
	"github.com/barber-appointment/platform/tenantctx"
	"github.com/gofiber/fiber/v3"
)

func TestHTTPCompletionMiddlewareLogsOnlyAllowlistedFields(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil)).With("service", "auth-service")

	app := fiber.New()
	app.Use(httpx.RequestID())
	app.Use(HTTPCompletionMiddleware(logger))
	app.Post("/internal/v1/auth/login", func(c fiber.Ctx) error {
		return c.SendStatus(fiber.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/internal/v1/auth/login", strings.NewReader(`{"email":"person@example.com","password":"super-secret-password"}`))
	req.Header.Set(tenantctx.TenantIDHeader, "11111111-1111-1111-1111-111111111111")
	req.Header.Set("Cookie", "barber_session=top-secret-cookie-value")
	req.Header.Set("X-Internal-Token", "internal-trusted-secret")
	req.Header.Set("Authorization", "Bearer super-secret-bearer-token")
	if _, err := app.Test(req); err != nil {
		t.Fatal(err)
	}

	line := strings.TrimSpace(buf.String())
	var record map[string]any
	if err := json.Unmarshal([]byte(line), &record); err != nil {
		t.Fatalf("expected exactly one JSON log line, got: %s (%v)", line, err)
	}

	for _, secret := range []string{
		"super-secret-password",
		"top-secret-cookie-value",
		"internal-trusted-secret",
		"super-secret-bearer-token",
		"person@example.com",
	} {
		if strings.Contains(line, secret) {
			t.Fatalf("log line leaked a value it must never record (%q): %s", secret, line)
		}
	}

	for _, field := range []string{"service", "request_id", "trace_id", "method", "route", "status", "duration_ms", "tenant_id"} {
		if _, ok := record[field]; !ok {
			t.Fatalf("expected field %q in the completion log, got: %s", field, line)
		}
	}
	if record["route"] != "/internal/v1/auth/login" {
		t.Fatalf("route = %v, want the bounded route pattern", record["route"])
	}
	if record["method"] != "POST" {
		t.Fatalf("method = %v", record["method"])
	}
	if record["tenant_id"] != "11111111-1111-1111-1111-111111111111" {
		t.Fatalf("tenant_id = %v", record["tenant_id"])
	}
	if _, ok := record["msg"]; !ok || record["msg"] != "http_request" {
		t.Fatalf("expected msg=http_request, got: %s", line)
	}
	// No field named after headers/body/cookies must exist at all.
	for _, forbidden := range []string{"body", "cookie", "cookies", "authorization", "headers", "password", "email"} {
		if _, ok := record[forbidden]; ok {
			t.Fatalf("log record must never contain a %q field", forbidden)
		}
	}
}

func TestHTTPCompletionMiddlewareEmitsExactlyOneLinePerRequest(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil)).With("service", "auth-service")
	app := fiber.New()
	app.Use(httpx.RequestID())
	app.Use(HTTPCompletionMiddleware(logger))
	app.Get("/x", func(c fiber.Ctx) error { return c.SendStatus(fiber.StatusOK) })

	if _, err := app.Test(httptest.NewRequest(http.MethodGet, "/x", nil)); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected exactly one log line per request, got %d: %v", len(lines), lines)
	}
}

func TestHTTPCompletionMiddlewareUnmatchedRouteUsesBoundedSentinel(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil)).With("service", "auth-service")
	app := fiber.New()
	app.Use(HTTPCompletionMiddleware(logger))
	app.Get("/widgets/:id", func(c fiber.Ctx) error { return c.SendStatus(fiber.StatusOK) })

	if _, err := app.Test(httptest.NewRequest(http.MethodGet, "/definitely/not/a/registered/path", nil)); err != nil {
		t.Fatal(err)
	}
	line := buf.String()
	if strings.Contains(line, "definitely/not/a/registered/path") {
		t.Fatal("an unmatched request must never leak the raw request path into the log line")
	}
	var record map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(line)), &record); err != nil {
		t.Fatalf("expected valid JSON, got %s (%v)", line, err)
	}
	if record["route"] != "unmatched" {
		t.Fatalf("route = %v, want the bounded unmatched sentinel", record["route"])
	}
	if record["status"] != float64(http.StatusNotFound) {
		t.Fatalf("status = %v, want 404", record["status"])
	}
}

func TestHTTPCompletionMiddlewareOmitsTenantIDWhenAbsent(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil)).With("service", "gateway-service")
	app := fiber.New()
	app.Use(HTTPCompletionMiddleware(logger))
	app.Get("/public", func(c fiber.Ctx) error { return c.SendStatus(fiber.StatusOK) })

	if _, err := app.Test(httptest.NewRequest(http.MethodGet, "/public", nil)); err != nil {
		t.Fatal(err)
	}
	var record map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &record); err != nil {
		t.Fatal(err)
	}
	if _, ok := record["tenant_id"]; ok {
		t.Fatalf("tenant_id must be omitted, not empty, when no tenant header is present: %v", record["tenant_id"])
	}
}
