package repository_test

import (
	"context"
	"errors"
	"github.com/barber-appointment/platform/db"
	"github.com/barber-appointment/scheduling-service/internal/domain"
	"github.com/barber-appointment/scheduling-service/internal/repository"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"testing"
	"time"
)

func TestSchedulingFoundationIntegration(t *testing.T) {
	appDSN, ownerDSN := os.Getenv("SCHEDULING_TEST_DATABASE_URL"), os.Getenv("SCHEDULING_TEST_OWNER_DATABASE_URL")
	if appDSN == "" || ownerDSN == "" {
		t.Skip("SCHEDULING_TEST_DATABASE_URL and SCHEDULING_TEST_OWNER_DATABASE_URL are required")
	}
	ctx := context.Background()
	pool, e := pgxpool.New(ctx, appDSN)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	owner, e := pgx.Connect(ctx, ownerDSN)
	if e != nil {
		t.Fatal(e)
	}
	defer owner.Close(ctx)
	if _, e = owner.Exec(ctx, "SET ROLE scheduling_db_owner"); e != nil {
		t.Fatal(e)
	}
	ta, tb, b := uuid.New(), uuid.New(), uuid.New()
	defer func() {
		_, _ = owner.Exec(ctx, `DELETE FROM public.schedule_overrides WHERE tenant_id=$1; DELETE FROM public.blocked_periods WHERE tenant_id=$1; DELETE FROM public.barber_working_hours WHERE tenant_id=$1`, ta)
	}()
	r := repository.New(pool)
	hours := domain.WorkingHoursInput{{Weekday: 1, Intervals: []domain.Interval{{Start: "09:00", End: "12:00"}}}}
	if e = r.ReplaceHours(ctx, ta, b, hours); e != nil {
		t.Fatal(e)
	}
	got, e := r.Hours(ctx, ta, b)
	if e != nil || len(got) != 1 {
		t.Fatalf("hours %v %v", got, e)
	}
	wrong, e := r.Hours(ctx, tb, b)
	if e != nil || len(wrong) != 0 {
		t.Fatalf("wrong tenant hours %v %v", wrong, e)
	}
	overlap := domain.WorkingHoursInput{{Weekday: 1, Intervals: []domain.Interval{{Start: "09:00", End: "11:00"}, {Start: "10:00", End: "12:00"}}}}
	if e = r.ReplaceHours(ctx, ta, b, overlap); e == nil {
		t.Fatal("expected overlap exclusion failure")
	}
	o, e := r.SaveOverride(ctx, ta, b, uuid.Nil, domain.OverrideInput{Date: "2030-01-07", Kind: "custom_hours", Intervals: []domain.Interval{{Start: "10:00", End: "12:00"}}})
	if e != nil {
		t.Fatal(e)
	}
	overrides, e := r.Overrides(ctx, ta, b)
	if e != nil || len(overrides) != 1 || len(overrides[0].Intervals) != 1 || overrides[0].Intervals[0] != (domain.Interval{Start: "10:00", End: "12:00"}) {
		t.Fatalf("overrides with intervals %v %v", overrides, e)
	}
	if _, e = r.SaveOverride(ctx, ta, b, uuid.Nil, domain.OverrideInput{Date: "2030-01-08", Kind: "unavailable"}); e != nil {
		t.Fatal(e)
	}
	overrides, e = r.Overrides(ctx, ta, b)
	if e != nil || len(overrides) != 2 || len(overrides[1].Intervals) != 0 {
		t.Fatalf("overrides without intervals %v %v", overrides, e)
	}
	if e = r.DeleteOverride(ctx, tb, b, o.ID); !errors.Is(e, repository.ErrNotFound) {
		t.Fatalf("override isolation: %v", e)
	}
	if _, e = r.SaveBlock(ctx, ta, b, domain.BlockedPeriodInput{StartAt: time.Date(2030, 1, 7, 10, 0, 0, 0, time.UTC), EndAt: time.Date(2030, 1, 7, 11, 0, 0, 0, time.UTC)}); e != nil {
		t.Fatal(e)
	}
	var n int
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM public.barber_working_hours`).Scan(&n); e != nil || n != 0 {
		t.Fatalf("RLS without context %d %v", n, e)
	}
	if e = db.WithTenantTx(ctx, pool, ta, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM public.schedule_overrides`).Scan(&n)
	}); e != nil || n != 2 {
		t.Fatalf("RLS in tenant transaction %d %v", n, e)
	}
}
