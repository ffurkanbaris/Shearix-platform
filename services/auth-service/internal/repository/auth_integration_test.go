package repository_test

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/barber-appointment/auth-service/internal/domain"
	"github.com/barber-appointment/auth-service/internal/repository"
	"github.com/barber-appointment/auth-service/internal/service"
	"github.com/barber-appointment/platform/db"
	platformemail "github.com/barber-appointment/platform/email"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

type captureEmailSender struct{ message platformemail.Message }

func (s *captureEmailSender) Send(_ context.Context, message platformemail.Message) (platformemail.Result, error) {
	s.message = message
	return platformemail.Result{ProviderMessageID: "integration-message"}, nil
}

func TestEmailOnlyRegistrationIntegration(t *testing.T) {
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

	tenantID := uuid.New()
	email := "email-only-" + uuid.NewString() + "@example.test"
	sender := &captureEmailSender{}
	auth := service.New(repository.New(appPool), time.Hour, sender)
	registration, err := auth.Register(ctx, tenantID, domain.Principal{TenantID: tenantID, Role: domain.RoleOwner}, domain.RegisterInput{Name: "Email Only User", Email: email, Role: domain.RoleBarber})
	if err != nil {
		t.Fatalf("email registration: %v", err)
	}
	defer owner.Exec(ctx, `DELETE FROM public.identities WHERE id=$1`, registration.IdentityID)
	if !registration.MembershipCreated || !registration.CredentialScheduled || registration.Role != domain.RoleBarber {
		t.Fatalf("registration result=%+v", registration)
	}
	if sender.message.To != email || sender.message.Template != "user_initial_password" {
		t.Fatalf("email delivery metadata=%+v", sender.message)
	}
	const prefix, suffix = "Your initial password is: ", "\nYou must change it after signing in."
	if !strings.HasPrefix(sender.message.Text, prefix) || !strings.HasSuffix(sender.message.Text, suffix) {
		t.Fatalf("unexpected initial credential message format")
	}
	temporaryPassword := strings.TrimSuffix(strings.TrimPrefix(sender.message.Text, prefix), suffix)
	if len(temporaryPassword) < 20 {
		t.Fatal("temporary credential lacks expected entropy")
	}
	publicResult, err := json.Marshal(registration)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(publicResult), temporaryPassword) || strings.Contains(strings.ToLower(string(publicResult)), "password") {
		t.Fatalf("registration contract exposed credential material: %s", publicResult)
	}

	var passwordHash, deliveryStatus string
	var mustChange bool
	if err = owner.QueryRow(ctx, `SELECT password_hash,must_change_password,initial_delivery_status FROM public.credentials WHERE identity_id=$1`, registration.IdentityID).Scan(&passwordHash, &mustChange, &deliveryStatus); err != nil {
		t.Fatal(err)
	}
	if passwordHash == temporaryPassword || strings.Contains(passwordHash, temporaryPassword) {
		t.Fatal("temporary credential was persisted as plaintext")
	}
	if err = bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(temporaryPassword)); err != nil {
		t.Fatalf("stored hash does not authenticate delivered credential: %v", err)
	}
	if !mustChange || deliveryStatus != "sent" {
		t.Fatalf("must_change_password=%v delivery_status=%q", mustChange, deliveryStatus)
	}

	var plaintextPersisted bool
	if err = owner.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM public.identities WHERE id=$1 AND (name LIKE '%'||$2||'%' OR email LIKE '%'||$2||'%')) OR EXISTS(SELECT 1 FROM public.credentials WHERE identity_id=$1 AND (password_hash LIKE '%'||$2||'%' OR COALESCE(initial_delivery_last_error,'') LIKE '%'||$2||'%'))`, registration.IdentityID, temporaryPassword).Scan(&plaintextPersisted); err != nil {
		t.Fatal(err)
	}
	if plaintextPersisted {
		t.Fatal("temporary credential leaked into persisted identity or credential fields")
	}
	var obsoleteOutboxPresent bool
	if err = owner.QueryRow(ctx, `SELECT to_regclass('public.credential_delivery_outbox') IS NOT NULL`).Scan(&obsoleteOutboxPresent); err != nil {
		t.Fatal(err)
	}
	if obsoleteOutboxPresent {
		t.Fatal("obsolete credential_delivery_outbox still exists")
	}
}

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
