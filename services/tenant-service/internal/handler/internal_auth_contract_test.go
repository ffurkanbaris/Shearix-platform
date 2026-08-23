package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/barber-appointment/platform/authcontract"
	"github.com/barber-appointment/platform/internalauth"
	"github.com/barber-appointment/tenant-service/internal/service"
	"github.com/gofiber/fiber/v3"
)

func TestInternalAuthContract(t *testing.T) {
	app := fiber.New()
	New(service.TenantService{}, authcontract.Verifier()).Register(app)
	routes := []authcontract.Route{
		authcontract.NewRoute(http.MethodGet, "/internal/v1/domains/resolve", false, "", 401, 0, 0),
		authcontract.NewRoute(http.MethodGet, "/internal/v1/domains/tls-authorize", false, "", 401, 0, 0),
		authcontract.NewRoute(http.MethodGet, "/internal/v1/public/config", true, "booking", 401, 400, 400),
		authcontract.NewRoute(http.MethodGet, "/internal/v1/settings", true, "booking", 401, 400, 400),
		authcontract.NewRoute(http.MethodGet, "/internal/v1/admin/settings", true, "admin", 401, 401, 401),
		authcontract.NewRoute(http.MethodPatch, "/internal/v1/admin/settings", true, "admin", 401, 401, 401),
		authcontract.NewRoute(http.MethodPost, "/internal/v1/platform/tenants", false, "", 401, 0, 0),
		authcontract.NewRoute(http.MethodGet, "/internal/v1/platform/tenants/:id", false, "", 401, 0, 0),
		authcontract.NewRoute(http.MethodPost, "/internal/v1/platform/tenants/:id/domains", false, "", 401, 0, 0),
		authcontract.NewRoute(http.MethodGet, "/internal/v1/platform/tenants/:id/domains", false, "", 401, 0, 0),
		authcontract.NewRoute(http.MethodPost, "/internal/v1/platform/tenants/:id/domains/:domain_id/verify", false, "", 401, 0, 0),
		authcontract.NewRoute(http.MethodPost, "/internal/v1/platform/tenants/:id/domains/:domain_id/activate", false, "", 401, 0, 0),
		authcontract.NewRoute(http.MethodPost, "/internal/v1/platform/tenants/:id/domains/:domain_id/deactivate", false, "", 401, 0, 0),
	}
	authcontract.Run(t, app, routes)

	// Non-tenant control-plane/resolver routes still prove valid-token admission
	// by reaching input validation rather than the 401 authentication boundary.
	for _, tc := range []struct {
		method, path string
		want         int
	}{
		{http.MethodGet, "/internal/v1/domains/resolve", 400},
		{http.MethodGet, "/internal/v1/domains/tls-authorize", 403},
		{http.MethodPost, "/internal/v1/platform/tenants", 400},
		{http.MethodGet, "/internal/v1/platform/tenants/not-a-uuid", 400},
		{http.MethodPost, "/internal/v1/platform/tenants/not-a-uuid/domains", 400},
		{http.MethodGet, "/internal/v1/platform/tenants/not-a-uuid/domains", 400},
		{http.MethodPost, "/internal/v1/platform/tenants/not-a-uuid/domains/not-a-uuid/verify", 400},
		{http.MethodPost, "/internal/v1/platform/tenants/not-a-uuid/domains/not-a-uuid/activate", 400},
		{http.MethodPost, "/internal/v1/platform/tenants/not-a-uuid/domains/not-a-uuid/deactivate", 400},
	} {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		req.Header.Set(internalauth.HeaderName, authcontract.ValidToken)
		res, err := app.Test(req)
		if err != nil || res.StatusCode != tc.want {
			t.Fatalf("valid token %s %s status=%v want=%d err=%v", tc.method, tc.path, responseStatus(res), tc.want, err)
		}
		_ = res.Body.Close()
	}
}
