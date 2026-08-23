package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/barber-appointment/catalog-service/internal/application"
	"github.com/barber-appointment/catalog-service/internal/domain"
	"github.com/barber-appointment/catalog-service/internal/repository"
	"github.com/barber-appointment/platform/adminauth"
	"github.com/barber-appointment/platform/internalauth"
	"github.com/barber-appointment/platform/tenantctx"
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type catalogIsolationAuth struct{}

func (catalogIsolationAuth) Authenticate(context.Context, tenantctx.Context, string) (adminauth.Principal, error) {
	return adminauth.Principal{Role: "OWNER", IdentityID: uuid.New()}, nil
}

type catalogIsolationBarber struct{}

func (catalogIsolationBarber) EnsureExists(context.Context, tenantctx.Context, uuid.UUID) error {
	return nil
}

func TestCatalogHTTPTenantIsolationIntegration(t *testing.T) {
	appDSN, ownerDSN := os.Getenv("CATALOG_TEST_DATABASE_URL"), os.Getenv("CATALOG_TEST_OWNER_DATABASE_URL")
	if appDSN == "" || ownerDSN == "" {
		t.Skip("CATALOG_TEST_DATABASE_URL and CATALOG_TEST_OWNER_DATABASE_URL are required")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, appDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	owner, err := pgx.Connect(ctx, ownerDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close(ctx)
	if _, err = owner.Exec(ctx, "SET ROLE catalog_db_owner"); err != nil {
		t.Fatal(err)
	}
	tenantA, tenantB := uuid.New(), uuid.New()
	defer func() {
		for _, tenant := range []uuid.UUID{tenantA, tenantB} {
			_, _ = owner.Exec(ctx, `DELETE FROM public.barber_services WHERE tenant_id=$1; DELETE FROM public.services WHERE tenant_id=$1`, tenant)
		}
	}()
	repo := repository.New(pool)
	serviceB, err := repo.Save(ctx, tenantB, uuid.Nil, domain.ServiceInput{Name: "Tenant B Service", DurationMinutes: 30, Price: "20.00", Currency: "TRY"})
	if err != nil {
		t.Fatal(err)
	}
	barberB := uuid.New()
	if err = repo.Assign(ctx, tenantB, barberB, serviceB.ID); err != nil {
		t.Fatal(err)
	}
	app := fiber.New()
	Handler{app: application.New(repo, catalogIsolationAuth{}, catalogIsolationBarber{}), verify: internalauth.NewTokenVerifier("secret")}.Register(app)
	request := func(method, path, body string) *http.Response {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set(internalauth.HeaderName, "secret")
		req.Header.Set(tenantctx.TenantIDHeader, tenantA.String())
		req.Header.Set(tenantctx.AppTypeHeader, "admin")
		req.Header.Set("Cookie", "session")
		res, e := app.Test(req)
		if e != nil {
			t.Fatal(e)
		}
		return res
	}
	for _, tc := range []struct{ method, path, body string }{
		{http.MethodGet, "/internal/v1/admin/services/" + serviceB.ID.String(), ""},
		{http.MethodPatch, "/internal/v1/admin/services/" + serviceB.ID.String(), `{"name":"hijack","duration_minutes":30,"price":"20.00","currency":"TRY"}`},
		{http.MethodPost, "/internal/v1/admin/barbers/" + barberB.String() + "/services", `{"service_id":"` + serviceB.ID.String() + `"}`},
		{http.MethodDelete, "/internal/v1/admin/barbers/" + barberB.String() + "/services/" + serviceB.ID.String(), ""},
	} {
		res := request(tc.method, tc.path, tc.body)
		if res.StatusCode != http.StatusNotFound {
			t.Fatalf("%s %s status=%d", tc.method, tc.path, res.StatusCode)
		}
		_ = res.Body.Close()
	}
	unchanged, err := repo.Service(ctx, tenantB, serviceB.ID)
	if err != nil || unchanged.Name != "Tenant B Service" {
		t.Fatalf("tenant B service mutated: %+v err=%v", unchanged, err)
	}
	spoof := request(http.MethodPost, "/internal/v1/admin/services?tenant_id="+tenantB.String(), `{"tenant_id":"`+tenantB.String()+`","name":"Tenant A Service","duration_minutes":30,"price":"20.00","currency":"TRY"}`)
	if spoof.StatusCode != http.StatusCreated {
		t.Fatalf("spoof create status=%d", spoof.StatusCode)
	}
	_ = spoof.Body.Close()
	servicesA, err := repo.Services(ctx, tenantA, false)
	if err != nil || len(servicesA) != 1 {
		t.Fatalf("trusted tenant create count=%d err=%v", len(servicesA), err)
	}
}
