package handler

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/barber-appointment/auth-service/internal/domain"
	"github.com/gofiber/fiber/v3"
)

func TestLoginRejectsSpoofedTenantContext(t *testing.T) {
	app := fiber.New()
	New(serviceZero(), nilVerifier{}, "session", true).Register(app)
	req := httptest.NewRequest("POST", "/internal/v1/auth/login", strings.NewReader(`{"email":"a@example.com","password":"password"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", "00000000-0000-0000-0000-000000000001")
	req.Header.Set("X-App-Type", "admin")
	response, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", response.StatusCode)
	}
}

func TestRequireRoles(t *testing.T) {
	app := fiber.New()
	app.Get("/role", func(c fiber.Ctx) error {
		c.Locals(principalKey, domain.Principal{Role: domain.RoleManager})
		return c.Next()
	}, RequireRoles(domain.RoleOwner), func(c fiber.Ctx) error { return c.SendStatus(fiber.StatusNoContent) })
	response, err := app.Test(httptest.NewRequest("GET", "/role", nil))
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != fiber.StatusForbidden {
		t.Fatalf("expected 403, got %d", response.StatusCode)
	}
}

func TestSessionCookieIsSecureAndHTTPOnly(t *testing.T) {
	app := fiber.New()
	handler := New(serviceZero(), nilVerifier{}, "__Host-barber_session", true)
	app.Get("/cookie", func(c fiber.Ctx) error {
		handler.setCookie(c, "opaque", time.Now().Add(time.Hour))
		return c.SendStatus(fiber.StatusNoContent)
	})
	response, err := app.Test(httptest.NewRequest("GET", "/cookie", nil))
	if err != nil {
		t.Fatal(err)
	}
	cookie := response.Header.Get("Set-Cookie")
	if !strings.Contains(cookie, "HttpOnly") || !strings.Contains(strings.ToLower(cookie), "secure") || !strings.Contains(cookie, "SameSite=Strict") {
		t.Fatalf("cookie lacks required flags: %s", cookie)
	}
}
