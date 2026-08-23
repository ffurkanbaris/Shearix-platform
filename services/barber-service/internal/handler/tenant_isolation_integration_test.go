package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/barber-appointment/barber-service/internal/application"
	"github.com/barber-appointment/barber-service/internal/domain"
	"github.com/barber-appointment/barber-service/internal/repository"
	"github.com/barber-appointment/platform/adminauth"
	"github.com/barber-appointment/platform/internalauth"
	"github.com/barber-appointment/platform/tenantctx"
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type isolationAuth struct{}

func (isolationAuth) Authenticate(context.Context, tenantctx.Context, string) (adminauth.Principal, error) {
	return adminauth.Principal{Role: "OWNER", IdentityID: uuid.New()}, nil
}
func (isolationAuth) EnsureBarberMembership(context.Context, tenantctx.Context, uuid.UUID) error {
	return nil
}

func TestBarberHTTPTenantIsolationIntegration(t *testing.T) {
	appDSN, ownerDSN := os.Getenv("BARBER_TEST_DATABASE_URL"), os.Getenv("BARBER_TEST_OWNER_DATABASE_URL")
	if appDSN == "" || ownerDSN == "" {
		t.Skip("BARBER_TEST_DATABASE_URL and BARBER_TEST_OWNER_DATABASE_URL are required")
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
	if _, err = owner.Exec(ctx, "SET ROLE barber_db_owner"); err != nil {
		t.Fatal(err)
	}
	tenantA, tenantB := uuid.New(), uuid.New()
	defer func() {
		for _, tenant := range []uuid.UUID{tenantA, tenantB} {
			_, _ = owner.Exec(ctx, `DELETE FROM public.barber_branch_assignments WHERE tenant_id=$1; DELETE FROM public.barbers WHERE tenant_id=$1; DELETE FROM public.branches WHERE tenant_id=$1`, tenant)
		}
	}()
	repo := repository.New(pool)
	branchB, err := repo.SaveBranch(ctx, tenantB, uuid.Nil, domain.BranchInput{Name: "Tenant B Branch"})
	if err != nil {
		t.Fatal(err)
	}
	barberB, err := repo.SaveBarber(ctx, tenantB, uuid.Nil, domain.BarberInput{DisplayName: "Tenant B Barber", BranchIDs: []uuid.UUID{branchB.ID}})
	if err != nil {
		t.Fatal(err)
	}
	app := fiber.New()
	Handler{app: application.New(repo, isolationAuth{}), verifier: internalauth.NewTokenVerifier("secret")}.Register(app)
	request := func(method, path, body string) *http.Response {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set(internalauth.HeaderName, "secret")
		req.Header.Set(tenantctx.TenantIDHeader, tenantA.String())
		req.Header.Set(tenantctx.AppTypeHeader, "admin")
		req.Header.Set("Cookie", "session")
		res, requestErr := app.Test(req)
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		return res
	}
	for _, tc := range []struct{ method, path, body string }{
		{http.MethodGet, "/internal/v1/admin/branches/" + branchB.ID.String(), ""},
		{http.MethodPatch, "/internal/v1/admin/branches/" + branchB.ID.String(), `{"name":"hijack"}`},
		{http.MethodGet, "/internal/v1/admin/barbers/" + barberB.ID.String(), ""},
		{http.MethodPatch, "/internal/v1/admin/barbers/" + barberB.ID.String(), `{"display_name":"hijack"}`},
		{http.MethodPost, "/internal/v1/admin/barbers/" + barberB.ID.String() + "/link-identity", `{"identity_id":"` + uuid.NewString() + `"}`},
		{http.MethodDelete, "/internal/v1/admin/barbers/" + barberB.ID.String() + "/link-identity", ""},
	} {
		res := request(tc.method, tc.path, tc.body)
		if res.StatusCode != http.StatusNotFound {
			t.Fatalf("%s %s status=%d", tc.method, tc.path, res.StatusCode)
		}
		_ = res.Body.Close()
	}
	unchanged, err := repo.Barber(ctx, tenantB, barberB.ID)
	if err != nil || unchanged.DisplayName != "Tenant B Barber" || unchanged.IdentityID != nil {
		t.Fatalf("tenant B barber mutated: %+v err=%v", unchanged, err)
	}

	spoof := request(http.MethodPost, "/internal/v1/admin/barbers?tenant_id="+tenantB.String(), `{"tenant_id":"`+tenantB.String()+`","display_name":"Tenant A Barber"}`)
	if spoof.StatusCode != http.StatusCreated {
		t.Fatalf("spoofed create status=%d", spoof.StatusCode)
	}
	_ = spoof.Body.Close()
	barbersA, err := repo.Barbers(ctx, tenantA, false)
	if err != nil || len(barbersA) != 1 {
		t.Fatalf("trusted tenant did not own create: count=%d err=%v", len(barbersA), err)
	}
	if _, err = repo.Barber(ctx, tenantB, barbersA[0].ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("body/query tenant spoof crossed boundary: %v", err)
	}
}
