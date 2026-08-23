package httpx

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
)

func TestUnexpectedListenerErrorPropagates(t *testing.T) {
	want := errors.New("address already in use")
	err := run(fiber.New(), func() error { return want }, slog.New(slog.NewTextHandler(io.Discard, nil)), make(chan os.Signal))
	if !errors.Is(err, want) {
		t.Fatalf("run error = %v", err)
	}
}

func TestHealthIsLivenessAndReadyChecksDependencies(t *testing.T) {
	app := fiber.New()
	Health(app, func() error { return errors.New("database unavailable") })
	health, err := app.Test(httptest.NewRequest("GET", "/health", nil))
	if err != nil || health.StatusCode != 200 {
		t.Fatalf("health status=%v err=%v", health.StatusCode, err)
	}
	ready, err := app.Test(httptest.NewRequest("GET", "/ready", nil))
	if err != nil || ready.StatusCode != 503 {
		t.Fatalf("ready status=%v err=%v", ready.StatusCode, err)
	}
}

func TestRequestIDGeneratesWhenAbsent(t *testing.T) {
	app := fiber.New()
	app.Use(RequestID())
	var stored string
	app.Get("/x", func(c fiber.Ctx) error {
		stored, _ = c.Locals(RequestIDLocalsKey).(string)
		return c.SendStatus(fiber.StatusOK)
	})
	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/x", nil))
	if err != nil {
		t.Fatal(err)
	}
	header := resp.Header.Get("X-Request-ID")
	if header == "" {
		t.Fatal("expected a generated X-Request-ID response header")
	}
	if stored != header {
		t.Fatalf("Locals value %q must match the response header %q", stored, header)
	}
}

func TestRequestIDPreservesValidCallerSuppliedValue(t *testing.T) {
	app := fiber.New()
	app.Use(RequestID())
	app.Get("/x", func(c fiber.Ctx) error { return c.SendStatus(fiber.StatusOK) })
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("X-Request-ID", "caller-supplied-id-123")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if got := resp.Header.Get("X-Request-ID"); got != "caller-supplied-id-123" {
		t.Fatalf("X-Request-ID = %q, want the caller-supplied value preserved", got)
	}
}

func TestRequestIDRejectsUnboundedOrHostileValue(t *testing.T) {
	app := fiber.New()
	app.Use(RequestID())
	app.Get("/x", func(c fiber.Ctx) error { return c.SendStatus(fiber.StatusOK) })

	for name, value := range map[string]string{
		"too long":               strings.Repeat("a", maxRequestIDLen+1),
		"header injection chars": "id\r\nX-Injected: evil",
		"unicode lookalikes":     "id  ",
	} {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/x", nil)
			req.Header.Set("X-Request-ID", value)
			resp, err := app.Test(req)
			if err != nil {
				t.Fatal(err)
			}
			got := resp.Header.Get("X-Request-ID")
			if got == value {
				t.Fatalf("hostile/unbounded request id %q must never be echoed back verbatim", value)
			}
			if !isBoundedToken(got) {
				t.Fatalf("generated fallback id %q must itself be a bounded token", got)
			}
		})
	}
}

func TestRoutePatternUnmatchedIsBoundedSentinel(t *testing.T) {
	app := fiber.New()
	app.Get("/widgets/:id", func(c fiber.Ctx) error { return c.SendStatus(fiber.StatusOK) })
	var route string
	app.Use(func(c fiber.Ctx) error {
		err := c.Next()
		route = RoutePattern(c, err)
		return err
	})
	if _, err := app.Test(httptest.NewRequest(http.MethodGet, "/nothing/registered/here", nil)); err != nil {
		t.Fatal(err)
	}
	if route != "unmatched" {
		t.Fatalf("route = %q, want the bounded unmatched sentinel", route)
	}
}

func TestRoutePatternMatchedReturnsPattern(t *testing.T) {
	app := fiber.New()
	var route string
	app.Use(func(c fiber.Ctx) error {
		err := c.Next()
		route = RoutePattern(c, err)
		return err
	})
	app.Get("/widgets/:id", func(c fiber.Ctx) error { return c.SendStatus(fiber.StatusOK) })
	if _, err := app.Test(httptest.NewRequest(http.MethodGet, "/widgets/abc-123", nil)); err != nil {
		t.Fatal(err)
	}
	if route != "/widgets/:id" {
		t.Fatalf("route = %q, want the registered pattern (never the raw path)", route)
	}
}

func TestResolveStatusAppliesErrorHandlerBeforeReadingStatus(t *testing.T) {
	app := fiber.New()
	var status int
	app.Use(func(c fiber.Ctx) error {
		err := c.Next()
		status = ResolveStatus(c, err)
		return err
	})
	// No routes registered at all: c.Next() returns fiber.ErrNotFound, whose
	// mapped status (404) is not written to the response until something
	// invokes the app's ErrorHandler — reading c.Response().StatusCode()
	// without going through ResolveStatus would observe a stale 200 here.
	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/anything", nil))
	if err != nil {
		t.Fatal(err)
	}
	if status != fiber.StatusNotFound {
		t.Fatalf("ResolveStatus observed %d, want 404", status)
	}
	if resp.StatusCode != fiber.StatusNotFound {
		t.Fatalf("final response status = %d, want 404", resp.StatusCode)
	}
}

func TestResolveStatusMatchedRouteReturnsHandlerStatus(t *testing.T) {
	app := fiber.New()
	var status int
	app.Use(func(c fiber.Ctx) error {
		err := c.Next()
		status = ResolveStatus(c, err)
		return err
	})
	app.Get("/x", func(c fiber.Ctx) error { return c.SendStatus(fiber.StatusTeapot) })
	if _, err := app.Test(httptest.NewRequest(http.MethodGet, "/x", nil)); err != nil {
		t.Fatal(err)
	}
	if status != fiber.StatusTeapot {
		t.Fatalf("status = %d, want 418", status)
	}
}

func TestIsBoundedTokenRejectsControlCharacters(t *testing.T) {
	if isBoundedToken("id\x00\x01") {
		t.Fatal("a request id containing control characters must never be treated as bounded/safe")
	}
	if !isBoundedToken("safe-request-id_123.45:67") {
		t.Fatal("a genuinely bounded token using only the allowed character set must be accepted")
	}
}
