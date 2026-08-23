package handler

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/barber-appointment/gateway-service/internal/service"
	"github.com/barber-appointment/platform/internalauth"
	"github.com/barber-appointment/platform/platformauth"
	"github.com/barber-appointment/platform/tenantctx"
	"github.com/gofiber/fiber/v3"
)

func TestPlatformRoutesRequireSeparateCredentialAndStripTenantContext(t *testing.T) {
	var calls atomic.Int32
	var internalToken, tenantHeader string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		internalToken = r.Header.Get(internalauth.HeaderName)
		tenantHeader = r.Header.Get(tenantctx.TenantIDHeader)
		if r.URL.Path != "/internal/v1/platform/tenants" {
			t.Fatalf("unexpected backend path %q", r.URL.Path)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"tenant"}`))
	}))
	defer backend.Close()

	app := fiber.New()
	New(service.Resolver{}, backend.URL, "", "", "", "", "", "", "", "internal-secret", "platform-secret", 0).Register(app)

	unauthorized := httptest.NewRequest(http.MethodPost, "/api/v1/platform/tenants", nil)
	if response, err := app.Test(unauthorized); err != nil || response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthorized platform request status=%v err=%v", status(response), err)
	}
	if calls.Load() != 0 {
		t.Fatal("unauthorized request reached tenant-service")
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/platform/tenants", nil)
	request.Header.Set(platformauth.HeaderName, "platform-secret")
	request.Header.Set(tenantctx.TenantIDHeader, "spoofed")
	response, err := app.Test(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusCreated || string(body) != `{"id":"tenant"}` {
		t.Fatalf("platform response status=%d body=%s", response.StatusCode, body)
	}
	if calls.Load() != 1 || internalToken != "internal-secret" || tenantHeader != "" {
		t.Fatalf("forwarding calls=%d internal=%q tenant=%q", calls.Load(), internalToken, tenantHeader)
	}
}

func TestPublicRouteClassesReplaceEverySpoofedTrustedHeader(t *testing.T) {
	const tenant = "00000000-0000-0000-0000-000000000001"
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(internalauth.HeaderName) != "internal-secret" || r.Header.Get(tenantctx.TenantIDHeader) != tenant || r.Header.Get(tenantctx.AppTypeHeader) != "booking" {
			t.Fatalf("gateway did not establish trusted context: headers=%v", r.Header)
		}
		for _, header := range []string{"X-Internal-Auth", "X-User-ID", "X-Customer-ID"} {
			if r.Header.Get(header) != "" {
				t.Fatalf("spoofed %s reached %s", header, r.URL.Path)
			}
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer backend.Close()
	resolver := service.Resolver{}
	resolverBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"tenant_id":"` + tenant + `","app_type":"booking"}`))
	}))
	defer resolverBackend.Close()
	resolver = service.NewResolver(resolverBackend.URL, "internal-secret")
	app := fiber.New()
	New(resolver, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, "internal-secret", "platform-secret", 0).Register(app)

	for _, path := range []string{
		"/api/v1/public/customer/auth/me", "/api/v1/public/branches", "/api/v1/public/services", "/api/v1/public/availability", "/api/v1/public/appointments/00000000-0000-0000-0000-000000000099",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Host = "booking.example"
		for _, header := range []string{"X-Tenant-ID", "x-app-type", "X-Internal-Token", "x-internal-auth", "X-User-ID", "x-customer-id"} {
			req.Header.Set(header, "spoofed")
		}
		res, err := app.Test(req)
		if err != nil || res.StatusCode != http.StatusNoContent {
			t.Fatalf("path=%s status=%v err=%v", path, status(res), err)
		}
		_ = res.Body.Close()
	}
}

func TestHostnameResolutionIsAuthoritativeAndWrongAppTypeIsRejected(t *testing.T) {
	const tenantA = "00000000-0000-0000-0000-0000000000aa"
	const tenantB = "00000000-0000-0000-0000-0000000000bb"
	var gotTenant, gotAppType string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTenant = r.Header.Get(tenantctx.TenantIDHeader)
		gotAppType = r.Header.Get(tenantctx.AppTypeHeader)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer backend.Close()
	resolverBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("hostname") {
		case "booking.localhost":
			_, _ = w.Write([]byte(`{"tenant_id":"` + tenantA + `","app_type":"booking"}`))
		case "admin.localhost":
			_, _ = w.Write([]byte(`{"tenant_id":"` + tenantA + `","app_type":"admin"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer resolverBackend.Close()
	app := fiber.New()
	New(service.NewResolver(resolverBackend.URL, "internal-secret"), resolverBackend.URL, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, "internal-secret", "platform-secret", 0).Register(app)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/public/branches?tenant_id="+tenantB, nil)
	request.Host = "booking.localhost"
	request.Header.Set(tenantctx.TenantIDHeader, tenantB)
	request.Header.Set(tenantctx.AppTypeHeader, "admin")
	request.Header.Set("X-User-ID", "spoofed")
	request.Header.Set("X-Customer-ID", "spoofed")
	request.Header.Set(internalauth.HeaderName, "spoofed")
	response, err := app.Test(request)
	if err != nil || response.StatusCode != http.StatusNoContent {
		t.Fatalf("authoritative host status=%v err=%v", status(response), err)
	}
	_ = response.Body.Close()
	if gotTenant != tenantA || gotAppType != "booking" {
		t.Fatalf("gateway trusted client values instead of hostname: tenant=%q app_type=%q", gotTenant, gotAppType)
	}

	request = httptest.NewRequest(http.MethodGet, "/api/v1/public/branches", nil)
	request.Host = "admin.localhost"
	response, err = app.Test(request)
	if err != nil || response.StatusCode != http.StatusForbidden {
		t.Fatalf("wrong app type status=%v err=%v", status(response), err)
	}
	_ = response.Body.Close()

	request = httptest.NewRequest(http.MethodGet, "/api/v1/public/branches", nil)
	request.Host = "unknown.localhost"
	response, err = app.Test(request)
	if err != nil || response.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown hostname status=%v err=%v", status(response), err)
	}
	_ = response.Body.Close()
}

func TestGatewayDoesNotRegisterPrivateServiceRoutes(t *testing.T) {
	var privateCalls atomic.Int32
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		privateCalls.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer backend.Close()
	resolverBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"tenant_id":"00000000-0000-0000-0000-000000000001","app_type":"booking"}`))
	}))
	defer resolverBackend.Close()
	frontend := httptest.NewServer(http.NotFoundHandler())
	defer frontend.Close()
	app := fiber.New()
	New(service.NewResolver(resolverBackend.URL, "internal-secret"), resolverBackend.URL, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, backend.URL, "internal-secret", "platform-secret", 0, frontend.URL, frontend.URL).Register(app)

	for _, path := range []string{
		"/internal/v1/occupancy", "/internal/v1/admin/barbers", "/internal/v1/admin/services", "/internal/v1/admin/barbers/id/schedule", "/internal/v1/internal/customer/session",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Host = "booking.localhost"
		res, err := app.Test(req)
		if err != nil || res.StatusCode != http.StatusNotFound {
			t.Fatalf("private path=%s status=%v err=%v", path, status(res), err)
		}
		_ = res.Body.Close()
	}
	if privateCalls.Load() != 0 {
		t.Fatalf("gateway proxied %d private requests", privateCalls.Load())
	}
}

func status(response *http.Response) int {
	if response == nil {
		return 0
	}
	return response.StatusCode
}
