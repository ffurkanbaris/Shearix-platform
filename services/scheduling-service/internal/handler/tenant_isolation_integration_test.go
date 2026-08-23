package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/barber-appointment/platform/adminauth"
	"github.com/barber-appointment/platform/internalauth"
	"github.com/barber-appointment/platform/schedulingdeps"
	"github.com/barber-appointment/platform/tenantctx"
	"github.com/barber-appointment/scheduling-service/internal/application"
	"github.com/barber-appointment/scheduling-service/internal/domain"
	"github.com/barber-appointment/scheduling-service/internal/handler"
	"github.com/barber-appointment/scheduling-service/internal/repository"
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type isolationDependencies struct{}

func (isolationDependencies) Barber(context.Context, tenantctx.Context, uuid.UUID) (schedulingdeps.Barber, error) {
	return schedulingdeps.Barber{}, nil
}
func (isolationDependencies) Service(context.Context, tenantctx.Context, uuid.UUID, uuid.UUID) (schedulingdeps.Service, error) {
	return schedulingdeps.Service{}, nil
}
func (isolationDependencies) Settings(context.Context, tenantctx.Context) (schedulingdeps.TenantSettings, error) {
	return schedulingdeps.TenantSettings{Timezone: "UTC"}, nil
}

type isolationOccupancy struct{}

func (isolationOccupancy) Occupied(context.Context, tenantctx.Context, uuid.UUID, time.Time, time.Time) ([]domain.BlockedPeriod, error) {
	return nil, nil
}

func TestSchedulingHTTPTenantIsolationIntegration(t *testing.T) {
	appDSN, ownerDSN := os.Getenv("SCHEDULING_TEST_DATABASE_URL"), os.Getenv("SCHEDULING_TEST_OWNER_DATABASE_URL")
	if appDSN == "" || ownerDSN == "" {
		t.Skip("SCHEDULING_TEST_DATABASE_URL and SCHEDULING_TEST_OWNER_DATABASE_URL are required")
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
	if _, err = owner.Exec(ctx, "SET ROLE scheduling_db_owner"); err != nil {
		t.Fatal(err)
	}
	tenantA, tenantB, barberB := uuid.New(), uuid.New(), uuid.New()
	defer func() {
		for _, tenantID := range []uuid.UUID{tenantA, tenantB} {
			_, _ = owner.Exec(ctx, `DELETE FROM public.schedule_overrides WHERE tenant_id=$1; DELETE FROM public.blocked_periods WHERE tenant_id=$1; DELETE FROM public.barber_working_hours WHERE tenant_id=$1`, tenantID)
		}
	}()
	repo := repository.New(pool)
	hoursB := domain.WorkingHoursInput{{Weekday: 1, Intervals: []domain.Interval{{Start: "09:00", End: "17:00"}}}}
	if err = repo.ReplaceHours(ctx, tenantB, barberB, hoursB); err != nil {
		t.Fatal(err)
	}
	overrideB, err := repo.SaveOverride(ctx, tenantB, barberB, uuid.Nil, domain.OverrideInput{Date: "2031-01-06", Kind: "vacation"})
	if err != nil {
		t.Fatal(err)
	}
	blockB, err := repo.SaveBlock(ctx, tenantB, barberB, domain.BlockedPeriodInput{StartAt: time.Date(2031, 1, 7, 10, 0, 0, 0, time.UTC), EndAt: time.Date(2031, 1, 7, 11, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}

	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(adminauth.Principal{TenantID: tenantA.String(), IdentityID: uuid.New(), Role: "OWNER"})
	}))
	defer authServer.Close()
	app := fiber.New()
	handler.New(application.New(repo, isolationDependencies{}, isolationOccupancy{}, 15), internalauth.NewTokenVerifier("secret"), adminauth.New(authServer.URL, "secret")).Register(app)
	request := func(method, path, body string) *http.Response {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set(internalauth.HeaderName, "secret")
		req.Header.Set(tenantctx.TenantIDHeader, tenantA.String())
		req.Header.Set(tenantctx.AppTypeHeader, "admin")
		req.Header.Set("Cookie", "session=x")
		res, requestErr := app.Test(req)
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		return res
	}

	res := request(http.MethodGet, "/internal/v1/admin/barbers/"+barberB.String()+"/schedule", "")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("isolated schedule status=%d", res.StatusCode)
	}
	var schedule domain.Schedule
	if err = json.NewDecoder(res.Body).Decode(&schedule); err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if len(schedule.WorkingHours) != 0 || len(schedule.Overrides) != 0 || len(schedule.BlockedPeriods) != 0 {
		t.Fatalf("tenant B schedule leaked: %+v", schedule)
	}

	for _, tc := range []struct{ method, path, body string }{
		{http.MethodPatch, "/internal/v1/admin/barbers/" + barberB.String() + "/overrides/" + overrideB.ID.String(), `{"date":"2031-01-08","kind":"vacation"}`},
		{http.MethodDelete, "/internal/v1/admin/barbers/" + barberB.String() + "/overrides/" + overrideB.ID.String(), ""},
		{http.MethodDelete, "/internal/v1/admin/barbers/" + barberB.String() + "/blocked-periods/" + blockB.ID.String(), ""},
	} {
		res = request(tc.method, tc.path, tc.body)
		if res.StatusCode != http.StatusNotFound {
			t.Fatalf("%s %s status=%d", tc.method, tc.path, res.StatusCode)
		}
		_ = res.Body.Close()
	}
	if got, err := repo.Overrides(ctx, tenantB, barberB); err != nil || len(got) != 1 || got[0].Date != "2031-01-06" {
		t.Fatalf("tenant B override mutated: %+v err=%v", got, err)
	}
	if got, err := repo.Blocks(ctx, tenantB, barberB); err != nil || len(got) != 1 {
		t.Fatalf("tenant B block mutated: %+v err=%v", got, err)
	}

	spoofBody := `[{"tenant_id":"` + tenantB.String() + `","weekday":2,"intervals":[{"start":"10:00","end":"12:00"}]}]`
	res = request(http.MethodPut, "/internal/v1/admin/barbers/"+barberB.String()+"/working-hours?tenant_id="+tenantB.String(), spoofBody)
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("spoofed hours status=%d", res.StatusCode)
	}
	_ = res.Body.Close()
	if got, err := repo.Hours(ctx, tenantA, barberB); err != nil || len(got) != 1 || got[0].Weekday != 2 {
		t.Fatalf("trusted tenant did not own hours: %+v err=%v", got, err)
	}
	if got, err := repo.Hours(ctx, tenantB, barberB); err != nil || len(got) != 1 || got[0].Weekday != 1 {
		t.Fatalf("body/query tenant spoof mutated tenant B: %+v err=%v", got, err)
	}
}
