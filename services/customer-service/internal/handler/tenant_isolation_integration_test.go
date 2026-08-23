package handler_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/barber-appointment/customer-service/internal/handler"
	"github.com/barber-appointment/customer-service/internal/repository"
	"github.com/barber-appointment/platform/internalauth"
	"github.com/barber-appointment/platform/tenantctx"
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

func TestCustomerHTTPSessionTenantIsolationIntegration(t *testing.T) {
	appDSN, ownerDSN := os.Getenv("CUSTOMER_TEST_DATABASE_URL"), os.Getenv("CUSTOMER_TEST_OWNER_DATABASE_URL")
	if appDSN == "" || ownerDSN == "" {
		t.Skip("CUSTOMER_TEST_DATABASE_URL and CUSTOMER_TEST_OWNER_DATABASE_URL are required")
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
	if _, err = owner.Exec(ctx, "SET ROLE customer_db_owner"); err != nil {
		t.Fatal(err)
	}
	tenantA, tenantB := uuid.New(), uuid.New()
	defer func() {
		_, _ = owner.Exec(ctx, `DELETE FROM public.customers WHERE tenant_id=ANY($1)`, []uuid.UUID{tenantA, tenantB})
	}()
	repo := repository.New(pool)
	hash, err := bcrypt.GenerateFromPassword([]byte("not-a-test-output"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	customerB, err := repo.Create(ctx, tenantB, "Tenant B Customer", "isolated-b@example.test", "", string(hash))
	if err != nil {
		t.Fatal(err)
	}
	if err = repo.UpdatePassword(ctx, tenantB, customerB.ID, string(hash)); err != nil {
		t.Fatal(err)
	}
	const sessionB = "opaque-session-b"
	if err = repo.CreateSession(ctx, tenantB, customerB.ID, sessionB, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	const internalToken = "test-internal-token"
	app := fiber.New()
	handler.New(repo, internalauth.NewTokenVerifier(internalToken), nil, internalToken, "http://unused").Register(app)
	request := func(tenantID uuid.UUID, path string, session string) *http.Response {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set(internalauth.HeaderName, internalToken)
		req.Header.Set(tenantctx.TenantIDHeader, tenantID.String())
		req.Header.Set(tenantctx.AppTypeHeader, "booking")
		if session != "" {
			req.Header.Set("X-Customer-Session", session)
		}
		res, requestErr := app.Test(req)
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		return res
	}

	wrongSession := request(tenantA, "/internal/v1/internal/customer/session", sessionB)
	if wrongSession.StatusCode != http.StatusUnauthorized {
		t.Fatalf("cross-tenant session status=%d", wrongSession.StatusCode)
	}
	_ = wrongSession.Body.Close()
	correctSession := request(tenantB, "/internal/v1/internal/customer/session", sessionB)
	if correctSession.StatusCode != http.StatusOK {
		t.Fatalf("same-tenant session status=%d", correctSession.StatusCode)
	}
	_ = correctSession.Body.Close()

	wrongCustomer := request(tenantA, "/internal/v1/customer/"+customerB.ID.String(), "")
	if wrongCustomer.StatusCode != http.StatusNotFound {
		t.Fatalf("cross-tenant customer status=%d", wrongCustomer.StatusCode)
	}
	_ = wrongCustomer.Body.Close()
}
