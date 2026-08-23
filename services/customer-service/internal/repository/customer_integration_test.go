package repository_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/barber-appointment/customer-service/internal/repository"
	"github.com/barber-appointment/platform/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

func TestCustomerCredentialIntegration(t *testing.T) {
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
	email := "tenant-scoped@example.test"
	defer func() {
		_, _ = owner.Exec(ctx, `DELETE FROM public.customers WHERE tenant_id=ANY($1)`, []uuid.UUID{tenantA, tenantB})
	}()
	repo := repository.New(pool)
	hashA, _ := bcrypt.GenerateFromPassword([]byte("initial-password-a"), bcrypt.MinCost)
	a, err := repo.Create(ctx, tenantA, "Tenant A", email, "", string(hashA))
	if err != nil {
		t.Fatalf("create tenant A customer: %v", err)
	}
	if !a.MustChangePassword || a.InitialDeliveryStatus != "pending" {
		t.Fatalf("new account state = must_change:%v delivery:%q", a.MustChangePassword, a.InitialDeliveryStatus)
	}
	if _, err = repo.Create(ctx, tenantA, "Duplicate", email, "", string(hashA)); err == nil {
		t.Fatal("same email in one tenant must be unique")
	}
	b, err := repo.Create(ctx, tenantB, "Tenant B", email, "", string(hashA))
	if err != nil {
		t.Fatalf("same email in another tenant should be allowed: %v", err)
	}
	if _, err = repo.Create(ctx, tenantA, "Tenant A Private", "only-in-a@example.test", "", string(hashA)); err != nil {
		t.Fatal(err)
	}

	var visible int
	err = pool.QueryRow(ctx, `SELECT count(*) FROM public.customers`).Scan(&visible)
	if err == nil && visible != 0 {
		t.Fatalf("missing tenant context exposed %d rows", visible)
	}
	if _, _, err = repo.FindByEmail(ctx, tenantB, "only-in-a@example.test"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("cross-tenant read did not fail closed: %v", err)
	}
	if _, _, err = repo.FindByEmail(ctx, tenantB, email); err != nil {
		t.Fatalf("tenant B should see only its own same-email row: %v", err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO public.customers(tenant_id,name,normalized_email,password_hash) VALUES($1,'cross tenant','blocked@example.test','hash')`, tenantA); err == nil {
		t.Fatal("write without tenant context must fail closed")
	}
	if tag, e := pool.Exec(ctx, `UPDATE public.customers SET name='blocked' WHERE tenant_id=$1`, tenantA); e != nil || tag.RowsAffected() != 0 {
		t.Fatalf("update without tenant context did not fail closed rows=%d err=%v", tag.RowsAffected(), e)
	}
	if tag, e := pool.Exec(ctx, `DELETE FROM public.customers WHERE tenant_id=$1`, tenantA); e != nil || tag.RowsAffected() != 0 {
		t.Fatalf("delete without tenant context did not fail closed rows=%d err=%v", tag.RowsAffected(), e)
	}
	if err = db.WithTenantTx(ctx, pool, tenantB, func(tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `INSERT INTO public.customers(tenant_id,name,normalized_email,password_hash) VALUES($1,'cross tenant','blocked@example.test','hash')`, tenantA)
		return e
	}); err == nil {
		t.Fatal("cross-tenant insert unexpectedly succeeded")
	}
	if err = db.WithTenantTx(ctx, pool, tenantB, func(tx pgx.Tx) error {
		update, e := tx.Exec(ctx, `UPDATE public.customers SET name='blocked' WHERE tenant_id=$1 AND id=$2`, tenantA, a.ID)
		if e != nil || update.RowsAffected() != 0 {
			return errors.New("cross-tenant update was not hidden")
		}
		return nil
	}); err != nil {
		t.Fatalf("cross-tenant UPDATE RLS: %v", err)
	}
	if err = db.WithTenantTx(ctx, pool, tenantB, func(tx pgx.Tx) error {
		deleted, e := tx.Exec(ctx, `DELETE FROM public.customers WHERE tenant_id=$1 AND id=$2`, tenantA, a.ID)
		if e != nil || deleted.RowsAffected() != 0 {
			return errors.New("cross-tenant delete was not hidden")
		}
		return nil
	}); err != nil {
		t.Fatalf("cross-tenant DELETE RLS: %v", err)
	}

	// Session tokens are unique within a tenant, not globally, and every lookup
	// remains tenant-qualified.
	const sharedToken = "shared-opaque-session-token"
	if err = repo.CreateSession(ctx, tenantA, a.ID, sharedToken, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("create tenant A session: %v", err)
	}
	if err = repo.CreateSession(ctx, tenantA, a.ID, sharedToken, time.Now().Add(time.Hour)); err == nil {
		t.Fatal("duplicate token hash in one tenant was accepted")
	}
	if err = repo.CreateSession(ctx, tenantB, b.ID, sharedToken, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("same token hash across tenants should be allowed: %v", err)
	}
	if sessionCustomer, e := repo.Session(ctx, tenantA, sharedToken); e != nil || sessionCustomer.ID != a.ID {
		t.Fatalf("tenant A session lookup id=%s err=%v", sessionCustomer.ID, e)
	}
	if sessionCustomer, e := repo.Session(ctx, tenantB, sharedToken); e != nil || sessionCustomer.ID != b.ID {
		t.Fatalf("tenant B session lookup id=%s err=%v", sessionCustomer.ID, e)
	}

	newHash, _ := bcrypt.GenerateFromPassword([]byte("changed-password-a"), bcrypt.MinCost)
	if err = repo.UpdatePassword(ctx, tenantA, a.ID, string(newHash)); err != nil {
		t.Fatalf("change password: %v", err)
	}
	updated, storedHash, err := repo.FindByEmail(ctx, tenantA, email)
	if err != nil {
		t.Fatal(err)
	}
	if updated.MustChangePassword {
		t.Fatal("successful password change did not clear must_change_password")
	}
	if bcrypt.CompareHashAndPassword([]byte(storedHash), []byte("changed-password-a")) != nil {
		t.Fatal("changed password hash does not authenticate")
	}
	if storedHash == "changed-password-a" {
		t.Fatal("plaintext password was persisted")
	}
	if err = db.WithTenantTx(ctx, pool, tenantA, func(tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `UPDATE public.customers SET initial_delivery_status='sending' WHERE tenant_id=$1 AND id=$2`, tenantA, a.ID)
		return e
	}); err == nil {
		t.Fatal("sending state without claim fields was accepted")
	}
	if err = db.WithTenantTx(ctx, pool, tenantA, func(tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `UPDATE public.customers SET initial_delivery_claim_token=gen_random_uuid(),initial_delivery_claim_until=now()+interval '1 minute' WHERE tenant_id=$1 AND id=$2`, tenantA, a.ID)
		return e
	}); err == nil {
		t.Fatal("non-sending state retained claim fields")
	}
	if err = db.WithTenantTx(ctx, pool, tenantA, func(tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `UPDATE public.customers SET initial_delivery_status='sending',initial_delivery_claim_token=gen_random_uuid(),initial_delivery_claim_until=now()-interval '1 second' WHERE tenant_id=$1 AND id=$2`, tenantA, a.ID)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	_, claimed, err := repo.ClaimInitialDelivery(ctx, tenantA, a.ID)
	if err != nil || !claimed {
		t.Fatalf("expired delivery claim was not recoverable: claimed=%v err=%v", claimed, err)
	}
}
