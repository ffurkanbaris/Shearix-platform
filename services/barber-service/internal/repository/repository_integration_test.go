package repository_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/barber-appointment/barber-service/internal/domain"
	"github.com/barber-appointment/barber-service/internal/repository"
	"github.com/barber-appointment/platform/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestBarberFoundationIntegration(t *testing.T) {
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
		_, _ = owner.Exec(ctx, `DELETE FROM public.barber_branch_assignments WHERE tenant_id=$1`, tenantA)
		_, _ = owner.Exec(ctx, `DELETE FROM public.barbers WHERE tenant_id=$1`, tenantA)
		_, _ = owner.Exec(ctx, `DELETE FROM public.branches WHERE tenant_id=$1`, tenantA)
	}()
	repo := repository.New(pool)
	branch, err := repo.SaveBranch(ctx, tenantA, uuid.Nil, domain.BranchInput{Name: "Central", Address: "Main"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repo.SaveBranch(ctx, tenantA, uuid.Nil, domain.BranchInput{Name: "Central"}); err == nil {
		t.Fatal("expected tenant-scoped duplicate branch failure")
	}
	if _, err = repo.Branch(ctx, tenantB, branch.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("wrong-tenant branch: %v", err)
	}
	barber, err := repo.SaveBarber(ctx, tenantA, uuid.Nil, domain.BarberInput{DisplayName: "Ada", BranchIDs: []uuid.UUID{branch.ID}})
	if err != nil {
		t.Fatal(err)
	}
	publicBarbers, err := repo.Barbers(ctx, tenantA, true)
	if err != nil || len(publicBarbers) != 1 {
		t.Fatalf("active public barber: %d %v", len(publicBarbers), err)
	}
	if len(publicBarbers[0].BranchIDs) != 1 || publicBarbers[0].BranchIDs[0] != branch.ID {
		t.Fatalf("public barber branch assignments: %+v", publicBarbers[0].BranchIDs)
	}
	inactive := false
	if _, err = repo.SaveBranch(ctx, tenantA, branch.ID, domain.BranchInput{Name: "Central", Address: "Main", Active: &inactive}); err != nil {
		t.Fatal(err)
	}
	publicBarbers, err = repo.Barbers(ctx, tenantA, true)
	if err != nil || len(publicBarbers) != 0 {
		t.Fatalf("inactive branch filtering: %d %v", len(publicBarbers), err)
	}
	if _, err = repo.Barber(ctx, tenantB, barber.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("wrong-tenant barber: %v", err)
	}
	// Identity links remain entirely local to barber_db. Auth membership
	// eligibility is validated by the private HTTP boundary before this method
	// is called; the tenant-scoped unique index is the final duplicate guard.
	identityID := uuid.New()
	if err = repo.LinkIdentity(ctx, tenantA, barber.ID, identityID); err != nil {
		t.Fatalf("link identity: %v", err)
	}
	linked, err := repo.Barber(ctx, tenantA, barber.ID)
	if err != nil || linked.IdentityID == nil || *linked.IdentityID != identityID {
		t.Fatalf("linked barber=%+v err=%v", linked, err)
	}
	if err = repo.LinkIdentity(ctx, tenantA, barber.ID, identityID); err != nil {
		t.Fatalf("idempotent same link: %v", err)
	}
	secondBarber, err := repo.SaveBarber(ctx, tenantA, uuid.Nil, domain.BarberInput{DisplayName: "Bora"})
	if err != nil {
		t.Fatal(err)
	}
	if err = repo.LinkIdentity(ctx, tenantA, secondBarber.ID, identityID); !errors.Is(err, repository.ErrConflict) {
		t.Fatalf("duplicate identity link=%v", err)
	}
	if err = repo.LinkIdentity(ctx, tenantB, barber.ID, uuid.New()); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("wrong-tenant identity link=%v", err)
	}
	if err = repo.UnlinkIdentity(ctx, tenantA, barber.ID); err != nil {
		t.Fatalf("unlink identity: %v", err)
	}
	linked, err = repo.Barber(ctx, tenantA, barber.ID)
	if err != nil || linked.IdentityID != nil {
		t.Fatalf("unlinked barber=%+v err=%v", linked, err)
	}
	var count int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM public.branches`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("RLS without context: %d %v", count, err)
	}
	if err = db.WithTenantTx(ctx, pool, tenantA, func(tx pgx.Tx) error { return tx.QueryRow(ctx, `SELECT count(*) FROM public.branches`).Scan(&count) }); err != nil || count != 1 {
		t.Fatalf("RLS tenant transaction: %d %v", count, err)
	}
}
