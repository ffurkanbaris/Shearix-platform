package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/barber-appointment/auth-service/internal/domain"
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
)

const testTenantHeader = "00000000-0000-0000-0000-000000000001"

func authenticatedRequest(method, path, body string) *http.Request {
	var reader *strings.Reader
	if body != "" {
		reader = strings.NewReader(body)
	} else {
		reader = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", testTenantHeader)
	req.Header.Set("X-App-Type", "admin")
	req.Header.Set("Cookie", "session=opaque-token")
	return req
}

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

func TestRequireCurrentPasswordBlocksPendingReset(t *testing.T) {
	app := fiber.New()
	app.Get("/gate", func(c fiber.Ctx) error {
		c.Locals(principalKey, domain.Principal{Role: domain.RoleOwner, MustChangePassword: true})
		return c.Next()
	}, RequireCurrentPassword(), func(c fiber.Ctx) error { return c.SendStatus(fiber.StatusNoContent) })
	response, err := app.Test(httptest.NewRequest("GET", "/gate", nil))
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != fiber.StatusForbidden {
		t.Fatalf("expected 403, got %d", response.StatusCode)
	}
}

func TestRequireCurrentPasswordAllowsCurrentPassword(t *testing.T) {
	app := fiber.New()
	app.Get("/gate", func(c fiber.Ctx) error {
		c.Locals(principalKey, domain.Principal{Role: domain.RoleOwner, MustChangePassword: false})
		return c.Next()
	}, RequireCurrentPassword(), func(c fiber.Ctx) error { return c.SendStatus(fiber.StatusNoContent) })
	response, err := app.Test(httptest.NewRequest("GET", "/gate", nil))
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != fiber.StatusNoContent {
		t.Fatalf("expected 204, got %d", response.StatusCode)
	}
}

// pendingResetOwner is a session for an OWNER who is authenticated but still
// has a forced password change outstanding.
func pendingResetOwner() *fakeService {
	return &fakeService{principal: domain.Principal{
		IdentityID:         uuid.New(),
		Role:               domain.RoleOwner,
		MustChangePassword: true,
	}}
}

func TestPendingPasswordChangeBlocksMemberManagement(t *testing.T) {
	cases := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"list members", "GET", "/internal/v1/auth/members", ""},
		{"get member", "GET", "/internal/v1/auth/members/00000000-0000-0000-0000-000000000002", ""},
		{"create member", "POST", "/internal/v1/auth/members", `{"name":"New","email":"new@example.com","role":"BARBER"}`},
		{"register", "POST", "/internal/v1/auth/register", `{"name":"New","email":"new@example.com","role":"BARBER"}`},
		{"change role", "PATCH", "/internal/v1/auth/members/00000000-0000-0000-0000-000000000002/role", `{"role":"MANAGER"}`},
		{"activate", "POST", "/internal/v1/auth/members/00000000-0000-0000-0000-000000000002/activate", ""},
		{"deactivate", "POST", "/internal/v1/auth/members/00000000-0000-0000-0000-000000000002/deactivate", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := pendingResetOwner()
			app := fiber.New()
			New(svc, trustedVerifier{}, "session", true).Register(app)
			response, err := app.Test(authenticatedRequest(tc.method, tc.path, tc.body))
			if err != nil {
				t.Fatal(err)
			}
			if response.StatusCode != fiber.StatusForbidden {
				t.Fatalf("expected 403, got %d", response.StatusCode)
			}
			if svc.membersCalled || svc.memberCalled || svc.registerCalled || svc.changeMemberRoleCalled || svc.setMemberStatusCalled {
				t.Fatalf("service was called despite pending password change: %+v", svc)
			}
		})
	}
}

func TestPendingPasswordChangeAllowsEscapeHatchAndSessionRoutes(t *testing.T) {
	t.Run("change-password succeeds", func(t *testing.T) {
		svc := pendingResetOwner()
		app := fiber.New()
		New(svc, trustedVerifier{}, "session", true).Register(app)
		response, err := app.Test(authenticatedRequest("POST", "/internal/v1/auth/change-password", `{"current_password":"old","new_password":"newpassword"}`))
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != fiber.StatusNoContent {
			t.Fatalf("expected 204, got %d", response.StatusCode)
		}
		if !svc.changePasswordCalled {
			t.Fatal("expected ChangePassword to be called")
		}
	})

	t.Run("me succeeds", func(t *testing.T) {
		svc := pendingResetOwner()
		app := fiber.New()
		New(svc, trustedVerifier{}, "session", true).Register(app)
		response, err := app.Test(authenticatedRequest("GET", "/internal/v1/auth/me", ""))
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != fiber.StatusOK {
			t.Fatalf("expected 200, got %d", response.StatusCode)
		}
	})

	t.Run("logout succeeds", func(t *testing.T) {
		svc := pendingResetOwner()
		app := fiber.New()
		New(svc, trustedVerifier{}, "session", true).Register(app)
		response, err := app.Test(authenticatedRequest("POST", "/internal/v1/auth/logout", ""))
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != fiber.StatusNoContent {
			t.Fatalf("expected 204, got %d", response.StatusCode)
		}
		if !svc.logoutCalled {
			t.Fatal("expected Logout to be called")
		}
	})
}

func TestCurrentPasswordOwnerCanManageMembers(t *testing.T) {
	svc := &fakeService{principal: domain.Principal{
		IdentityID:         uuid.New(),
		Role:               domain.RoleOwner,
		MustChangePassword: false,
	}}
	app := fiber.New()
	New(svc, trustedVerifier{}, "session", true).Register(app)
	response, err := app.Test(authenticatedRequest("GET", "/internal/v1/auth/members", ""))
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", response.StatusCode)
	}
	if !svc.membersCalled {
		t.Fatal("expected Members to be called for a caller with a current password")
	}
}
