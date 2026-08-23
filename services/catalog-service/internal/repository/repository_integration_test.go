package repository_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/barber-appointment/catalog-service/internal/domain"
	"github.com/barber-appointment/catalog-service/internal/repository"
	"github.com/barber-appointment/platform/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCatalogFoundationIntegration(t *testing.T) {
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
		_, _ = owner.Exec(ctx, `DELETE FROM public.barber_services WHERE tenant_id=$1`, tenantA)
		_, _ = owner.Exec(ctx, `DELETE FROM public.services WHERE tenant_id=$1`, tenantA)
	}()
	repo := repository.New(pool)
	input := domain.ServiceInput{Name: "Cut", DurationMinutes: 30, BufferBeforeMinutes: 5, Price: "25.50", Currency: "TRY"}
	service, err := repo.Save(ctx, tenantA, uuid.Nil, input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repo.Save(ctx, tenantA, uuid.Nil, input); err == nil {
		t.Fatal("expected tenant-scoped duplicate service failure")
	}
	if _, err = repo.Service(ctx, tenantB, service.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("wrong-tenant service: %v", err)
	}
	barberID := uuid.New()
	if err = repo.Assign(ctx, tenantA, barberID, service.ID); err != nil {
		t.Fatal(err)
	}
	assignments, err := repo.Assignments(ctx, tenantA, barberID)
	if err != nil || len(assignments) != 1 {
		t.Fatalf("same-tenant assignment: %d %v", len(assignments), err)
	}
	assignments, err = repo.Assignments(ctx, tenantB, barberID)
	if err != nil || len(assignments) != 0 {
		t.Fatalf("assignment isolation: %d %v", len(assignments), err)
	}
	inactive := false
	if _, err = repo.Save(ctx, tenantA, service.ID, domain.ServiceInput{Name: "Cut", DurationMinutes: 30, BufferBeforeMinutes: 5, Price: "25.50", Currency: "TRY", Active: &inactive}); err != nil {
		t.Fatal(err)
	}
	active, err := repo.Services(ctx, tenantA, true)
	if err != nil || len(active) != 0 {
		t.Fatalf("inactive service filtering: %d %v", len(active), err)
	}
	var count int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM public.services`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("RLS without context: %d %v", count, err)
	}
	if err = db.WithTenantTx(ctx, pool, tenantA, func(tx pgx.Tx) error { return tx.QueryRow(ctx, `SELECT count(*) FROM public.services`).Scan(&count) }); err != nil || count != 1 {
		t.Fatalf("RLS tenant transaction: %d %v", count, err)
	}
}
