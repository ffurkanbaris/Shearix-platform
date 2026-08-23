// Package authcontract provides a small reusable contract harness for private
// HTTP routes. It is intended for service tests; production code should use
// internalauth and tenantctx directly.
package authcontract

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/barber-appointment/platform/internalauth"
	"github.com/barber-appointment/platform/tenantctx"
	"github.com/gofiber/fiber/v3"
)

const (
	ValidToken = "internal-contract-secret"
	TenantID   = "00000000-0000-0000-0000-000000000001"
)

type Route struct {
	Method          string
	Path            string
	TenantScoped    bool
	AppType         string
	MissingStatus   int
	NoTenantStatus  int
	BadTenantStatus int
}

func NewRoute(method, path string, tenantScoped bool, appType string, missingStatus, noTenantStatus, badTenantStatus int) Route {
	return Route{Method: method, Path: path, TenantScoped: tenantScoped, AppType: appType, MissingStatus: missingStatus, NoTenantStatus: noTenantStatus, BadTenantStatus: badTenantStatus}
}

func Run(t *testing.T, app *fiber.App, routes []Route) {
	t.Helper()
	registered := make(map[string]bool)
	for _, route := range app.GetRoutes(true) {
		if strings.HasPrefix(route.Path, "/internal/") {
			registered[route.Method+" "+route.Path] = true
		}
	}
	declared := make(map[string]bool, len(routes))
	for _, route := range routes {
		key := route.Method + " " + route.Path
		declared[key] = true
		if !registered[key] {
			t.Errorf("declared internal route is not registered: %s", key)
		}
		t.Run(key, func(t *testing.T) {
			assertRequest(t, app, route, "", "", route.MissingStatus)
			assertRequest(t, app, route, "invalid-internal-token", "", route.MissingStatus)
			if route.TenantScoped {
				assertRequest(t, app, route, ValidToken, "", route.NoTenantStatus)
				assertRequest(t, app, route, ValidToken, "invalid", route.BadTenantStatus)
			}
		})
	}
	for key := range registered {
		if !declared[key] {
			t.Errorf("registered internal route has no auth contract: %s", key)
		}
	}
}

func assertRequest(t *testing.T, app *fiber.App, route Route, token, tenant string, want int) {
	t.Helper()
	req := httptest.NewRequest(route.Method, materialize(route.Path), nil)
	if token != "" {
		req.Header.Set(internalauth.HeaderName, token)
	}
	if tenant != "" {
		req.Header.Set(tenantctx.TenantIDHeader, tenant)
	}
	if route.AppType != "" {
		req.Header.Set(tenantctx.AppTypeHeader, route.AppType)
	}
	res, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
	if res.StatusCode != want {
		t.Fatalf("status=%d want=%d body=%q", res.StatusCode, want, body)
	}
	if strings.Contains(string(body), ValidToken) || strings.Contains(string(body), "invalid-internal-token") {
		t.Fatal("response disclosed internal authentication material")
	}
}

func materialize(path string) string {
	parts := strings.Split(path, "/")
	for i, part := range parts {
		if strings.HasPrefix(part, ":") {
			parts[i] = "00000000-0000-0000-0000-000000000099"
		}
	}
	return strings.Join(parts, "/")
}

func Verifier() internalauth.Verifier { return internalauth.NewTokenVerifier(ValidToken) }
