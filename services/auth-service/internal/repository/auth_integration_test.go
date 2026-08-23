package repository_test

import (
	"context"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/barber-appointment/auth-service/internal/domain"
	"github.com/barber-appointment/auth-service/internal/repository"
	"github.com/barber-appointment/auth-service/internal/service"
	"github.com/barber-appointment/platform/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

func TestAuthenticationIntegration(t *testing.T) {
	appDSN, ownerDSN := os.Getenv("AUTH_TEST_DATABASE_URL"), os.Getenv("AUTH_TEST_OWNER_DATABASE_URL")
	if appDSN == "" || ownerDSN == "" {
		t.Skip("AUTH_TEST_DATABASE_URL and AUTH_TEST_OWNER_DATABASE_URL are required")
	}
	ctx := context.Background()
	appPool, err := pgxpool.New(ctx, appDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer appPool.Close()
	owner, err := pgx.Connect(ctx, ownerDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close(ctx)
	if _, err = owner.Exec(ctx, "SET ROLE auth_db_owner"); err != nil {
		t.Fatal(err)
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("correct-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	tenantA, tenantB := uuid.New(), uuid.New()
	identityOK, identityNoMembership, identityInactive := uuid.New(), uuid.New(), uuid.New()
	defer func() {
		for _, identity := range []uuid.UUID{identityOK, identityNoMembership, identityInactive} {
			_, _ = owner.Exec(ctx, `DELETE FROM public.identities WHERE id=$1`, identity)
		}
	}()
	emails := map[uuid.UUID]string{identityOK: "ok@example.com", identityNoMembership: "none@example.com", identityInactive: "inactive@example.com"}
	for _, identity := range []uuid.UUID{identityOK, identityNoMembership, identityInactive} {
		if _, err := owner.Exec(ctx, `INSERT INTO public.identities (id,name,email) VALUES ($1,$2,$3)`, identity, "Integration User", emails[identity]); err != nil {
			t.Fatal(err)
		}
		if _, err := owner.Exec(ctx, `INSERT INTO public.credentials (identity_id,password_hash) VALUES ($1,$2)`, identity, string(passwordHash)); err != nil {
			t.Fatal(err)
		}
	}
	insertMembership := func(tenant, identity uuid.UUID, role, status string) {
		tx, err := owner.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id',$1,true)`, tenant.String()); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO public.tenant_memberships (tenant_id,identity_id,role,status) VALUES ($1,$2,$3,$4)`, tenant, identity, role, status); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}
	insertMembership(tenantA, identityOK, "OWNER", "active")
	insertMembership(tenantB, identityOK, "MANAGER", "active")
	insertMembership(tenantA, identityInactive, "BARBER", "inactive")
	// The database trigger is defense in depth: even the least-privileged app
	// role cannot deactivate the last active owner inside a tenant transaction.
	if err := db.WithTenantTx(ctx, appPool, tenantA, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE public.tenant_memberships SET status='inactive' WHERE tenant_id=$1 AND identity_id=$2`, tenantA, identityOK)
		return err
	}); err == nil {
		t.Fatal("last active OWNER deactivation should be rejected")
	}
	repository := repository.New(appPool)
	auth := service.New(repository, time.Hour)
	valid, err := auth.Login(ctx, tenantA, domain.LoginInput{Email: emails[identityOK], Password: "correct-password"})
	if err != nil {
		t.Fatalf("valid login: %v", err)
	}
	if valid.Principal.Role != domain.RoleOwner {
		t.Fatalf("expected OWNER, got %s", valid.Principal.Role)
	}
	if _, err := auth.Login(ctx, tenantA, domain.LoginInput{Email: emails[identityOK], Password: "wrong"}); err != service.ErrUnauthorized {
		t.Fatalf("invalid password: %v", err)
	}
	if _, err := auth.Login(ctx, tenantA, domain.LoginInput{Email: emails[identityNoMembership], Password: "correct-password"}); err != service.ErrUnauthorized {
		t.Fatalf("missing membership: %v", err)
	}
	if _, err := auth.Login(ctx, uuid.New(), domain.LoginInput{Email: emails[identityOK], Password: "correct-password"}); err != service.ErrUnauthorized {
		t.Fatalf("wrong tenant: %v", err)
	}
	if _, err := auth.Login(ctx, tenantA, domain.LoginInput{Email: emails[identityInactive], Password: "correct-password"}); err != service.ErrUnauthorized {
		t.Fatalf("inactive membership: %v", err)
	}
	if _, err := auth.Current(ctx, tenantB, valid.Token); err != service.ErrUnauthorized {
		t.Fatalf("cross-tenant session: %v", err)
	}
	var count int
	if err := appPool.QueryRow(ctx, `SELECT count(*) FROM public.tenant_memberships`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("RLS should deny rows outside context: count=%d err=%v", count, err)
	}
	if err := db.WithTenantTx(ctx, appPool, tenantA, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM public.tenant_memberships`).Scan(&count)
	}); err != nil || count != 2 {
		t.Fatalf("tenant RLS count: %d err=%v", count, err)
	}
	if err := auth.Logout(ctx, tenantA, valid.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := auth.Current(ctx, tenantA, valid.Token); err != service.ErrUnauthorized {
		t.Fatalf("revoked session: %v", err)
	}
	if _, err := owner.Exec(ctx, `UPDATE public.credentials SET initial_delivery_status='failed' WHERE identity_id=$1`, identityNoMembership); err != nil {
		t.Fatal(err)
	}
	var claims atomic.Int32
	var group sync.WaitGroup
	group.Add(2)
	for range 2 {
		go func() {
			defer group.Done()
			_, claimed, claimErr := repository.ClaimInitialDelivery(ctx, identityNoMembership)
			if claimErr != nil {
				t.Errorf("claim initial delivery: %v", claimErr)
				return
			}
			if claimed {
				claims.Add(1)
			}
		}()
	}
	group.Wait()
	if claims.Load() != 1 {
		t.Fatalf("concurrent delivery claims = %d", claims.Load())
	}
}
