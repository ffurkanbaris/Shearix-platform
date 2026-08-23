package repository_test

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"

	"github.com/barber-appointment/platform/db"
	"github.com/barber-appointment/tenant-service/internal/domain"
	"github.com/barber-appointment/tenant-service/internal/repository"
	"github.com/barber-appointment/tenant-service/internal/service"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type txtResolver struct {
	mu      sync.Mutex
	answers map[string][]string
	errs    map[string]error
}

func (r *txtResolver) LookupTXT(_ context.Context, name string) ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.errs[name]; err != nil {
		return nil, err
	}
	return append([]string(nil), r.answers[name]...), nil
}

func TestTenantOnboardingDomainIntegration(t *testing.T) {
	appDSN, ownerDSN := os.Getenv("TENANT_TEST_DATABASE_URL"), os.Getenv("TENANT_TEST_OWNER_DATABASE_URL")
	if appDSN == "" || ownerDSN == "" {
		t.Skip("TENANT_TEST_DATABASE_URL and TENANT_TEST_OWNER_DATABASE_URL are required")
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
	if _, err = owner.Exec(ctx, "SET ROLE tenant_db_owner"); err != nil {
		t.Fatal(err)
	}

	dns := &txtResolver{answers: map[string][]string{}, errs: map[string]error{}}
	repo := repository.New(pool, nil)
	tenantService := service.New(repo, service.Dependencies{DNS: dns, AllowLocalhostDomains: false})

	created, err := tenantService.CreateTenant(ctx, domain.CreateTenantInput{Name: "  Onboarding Test  "})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = owner.Exec(ctx, `DELETE FROM public.tenant_domains WHERE tenant_id=$1`, created.ID)
		_, _ = owner.Exec(ctx, `DELETE FROM public.tenant_settings WHERE tenant_id=$1`, created.ID)
		_, _ = owner.Exec(ctx, `DELETE FROM public.tenants WHERE id=$1`, created.ID)
	}()
	if created.Name != "Onboarding Test" || created.Status != "active" || created.Settings.TenantID != created.ID || created.Settings.BookingIntervalMinutes != 15 || len(created.Settings.ReminderOffsetsMinutes) != 2 {
		t.Fatalf("tenant settings were not explicitly initialized: %+v", created)
	}

	registered, err := tenantService.CreateDomain(ctx, created.ID.String(), domain.CreateDomainInput{Hostname: "  Admin.Example.Test.  ", DomainType: domain.DomainTypeAdmin})
	if err != nil {
		t.Fatal(err)
	}
	if registered.Hostname != "admin.example.test" || registered.Active || registered.Verified || registered.VerificationToken == "" || registered.VerificationValue == "" {
		t.Fatalf("unexpected newly registered domain: %+v", registered)
	}
	if eligible, err := tenantService.TLSAuthorized(ctx, " ADMIN.EXAMPLE.TEST. "); err != nil || eligible {
		t.Fatalf("pending domain TLS eligibility=%v err=%v", eligible, err)
	}
	var hash []byte
	if err = owner.QueryRow(ctx, `SELECT verification_token_hash FROM public.tenant_domains WHERE id=$1`, registered.ID).Scan(&hash); err != nil || len(hash) != 32 {
		t.Fatalf("expected only token hash in postgres length=%d err=%v", len(hash), err)
	}

	if _, err = tenantService.CreateDomain(ctx, created.ID.String(), domain.CreateDomainInput{Hostname: "admin.example.test", DomainType: domain.DomainTypeAdmin}); !errors.Is(err, repository.ErrConflict) {
		t.Fatalf("duplicate hostname error=%v", err)
	}
	if failed, err := tenantService.VerifyDomain(ctx, created.ID.String(), registered.ID.String()); !errors.Is(err, service.ErrVerificationFailed) || failed.VerificationState != domain.DomainVerificationFailed {
		t.Fatalf("failed verification result=%+v err=%v", failed, err)
	}
	if eligible, err := tenantService.TLSAuthorized(ctx, registered.Hostname); err != nil || eligible {
		t.Fatalf("failed-verification TLS eligibility=%v err=%v", eligible, err)
	}
	if _, err = tenantService.ActivateDomain(ctx, created.ID.String(), registered.ID.String()); !errors.Is(err, service.ErrVerificationRequired) {
		t.Fatalf("activation before verification error=%v", err)
	}
	if _, err = repo.ResolveDomain(ctx, registered.Hostname); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("unverified/inactive domain resolved: %v", err)
	}

	dns.answers[registered.VerificationRecord] = []string{registered.VerificationValue}
	verified, err := tenantService.VerifyDomain(ctx, created.ID.String(), registered.ID.String())
	if err != nil || !verified.Verified || verified.VerificationState != domain.DomainVerificationVerified {
		t.Fatalf("successful verification=%+v err=%v", verified, err)
	}
	if eligible, err := tenantService.TLSAuthorized(ctx, registered.Hostname); err != nil || eligible {
		t.Fatalf("verified but inactive TLS eligibility=%v err=%v", eligible, err)
	}
	if err = owner.QueryRow(ctx, `SELECT verification_token_hash FROM public.tenant_domains WHERE id=$1`, registered.ID).Scan(&hash); err != nil || hash != nil {
		t.Fatalf("verification hash should be removed after success hash=%x err=%v", hash, err)
	}
	activated, err := tenantService.ActivateDomain(ctx, created.ID.String(), registered.ID.String())
	if err != nil || !activated.Active {
		t.Fatalf("activation=%+v err=%v", activated, err)
	}
	if eligible, err := tenantService.TLSAuthorized(ctx, "ADMIN.EXAMPLE.TEST."); err != nil || !eligible {
		t.Fatalf("verified active TLS eligibility=%v err=%v", eligible, err)
	}
	resolved, err := repo.ResolveDomain(ctx, registered.Hostname)
	if err != nil || resolved.TenantID != created.ID || resolved.AppType != domain.DomainTypeAdmin {
		t.Fatalf("active verified domain did not resolve: %+v err=%v", resolved, err)
	}
	// A tenant may own both frontend types. Resolution is the real tenant
	// service source of truth used by gateway hostname dispatch.
	booking, err := tenantService.CreateDomain(ctx, created.ID.String(), domain.CreateDomainInput{Hostname: "booking.example.test", DomainType: domain.DomainTypeBooking})
	if err != nil {
		t.Fatal(err)
	}
	dns.answers[booking.VerificationRecord] = []string{booking.VerificationValue}
	if _, err = tenantService.VerifyDomain(ctx, created.ID.String(), booking.ID.String()); err != nil {
		t.Fatal(err)
	}
	if _, err = tenantService.ActivateDomain(ctx, created.ID.String(), booking.ID.String()); err != nil {
		t.Fatal(err)
	}
	bookingResolved, err := tenantService.Resolve(ctx, "BOOKING.EXAMPLE.TEST.")
	if err != nil || bookingResolved.TenantID != created.ID || bookingResolved.AppType != domain.DomainTypeBooking {
		t.Fatalf("booking domain did not resolve to the same tenant: %+v err=%v", bookingResolved, err)
	}
	deactivated, err := tenantService.DeactivateDomain(ctx, created.ID.String(), registered.ID.String())
	if err != nil || deactivated.Active {
		t.Fatalf("deactivation=%+v err=%v", deactivated, err)
	}
	if _, err = repo.ResolveDomain(ctx, registered.Hostname); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("inactive domain resolved: %v", err)
	}
	if eligible, err := tenantService.TLSAuthorized(ctx, registered.Hostname); err != nil || eligible {
		t.Fatalf("deactivated TLS eligibility=%v err=%v", eligible, err)
	}
	if eligible, err := tenantService.TLSAuthorized(ctx, "unknown.example.test"); err != nil || eligible {
		t.Fatalf("unknown domain TLS eligibility=%v err=%v", eligible, err)
	}
	if _, err = tenantService.ActivateDomain(ctx, created.ID.String(), registered.ID.String()); err != nil {
		t.Fatalf("reactivation: %v", err)
	}
	if _, err = owner.Exec(ctx, `UPDATE public.tenants SET status='suspended' WHERE id=$1`, created.ID); err != nil {
		t.Fatal(err)
	}
	if eligible, err := tenantService.TLSAuthorized(ctx, registered.Hostname); err != nil || eligible {
		t.Fatalf("inactive tenant TLS eligibility=%v err=%v", eligible, err)
	}
	if _, err = owner.Exec(ctx, `UPDATE public.tenants SET status='active' WHERE id=$1`, created.ID); err != nil {
		t.Fatal(err)
	}

	// Operational settings are RLS-protected tenant data. A mutation uses the
	// same transaction-local set_config helper as every other tenant query and
	// must become visible immediately to downstream settings readers.
	interval, horizon, notice := 30, 90, 45
	updated, err := tenantService.UpdateSettings(ctx, created.ID.String(), domain.UpdateSettingsInput{
		Timezone:                    stringRef("America/New_York"),
		BookingIntervalMinutes:      &interval,
		ReminderOffsetsMinutes:      intsRef([]int{2880, 180}),
		CancellationPolicy:          stringRef("no_cancellation"),
		BookingHorizonDays:          &horizon,
		MinimumBookingNoticeMinutes: &notice,
	})
	if err != nil || updated.BusinessTimezone != "America/New_York" || updated.BookingIntervalMinutes != 30 || len(updated.ReminderOffsetsMinutes) != 2 || updated.ReminderOffsetsMinutes[0] != 2880 || updated.BookingHorizonDays != 90 || updated.MinimumBookingNoticeMinutes != 45 {
		t.Fatalf("settings update=%+v err=%v", updated, err)
	}
	readBack, err := repo.Settings(ctx, created.ID)
	if err != nil || readBack.BookingIntervalMinutes != 30 || readBack.CancellationPolicy != "no_cancellation" {
		t.Fatalf("updated settings were not immediately readable: %+v err=%v", readBack, err)
	}

	other, err := tenantService.CreateTenant(ctx, domain.CreateTenantInput{Name: "Other Tenant"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = owner.Exec(ctx, `DELETE FROM public.tenant_settings WHERE tenant_id=$1`, other.ID)
		_, _ = owner.Exec(ctx, `DELETE FROM public.tenants WHERE id=$1`, other.ID)
	}()
	if _, err = tenantService.DeactivateDomain(ctx, other.ID.String(), registered.ID.String()); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("wrong tenant domain modification error=%v", err)
	}

	// RLS is deny-by-default for the application role and a tenant context can
	// see only its own domain rows.
	var noContext int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM public.tenant_domains WHERE id=$1`, registered.ID).Scan(&noContext); err != nil || noContext != 0 {
		t.Fatalf("application role bypassed RLS count=%d err=%v", noContext, err)
	}
	var wrongTenant int
	if err = db.WithTenantTx(ctx, pool, other.ID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM public.tenant_domains WHERE id=$1`, registered.ID).Scan(&wrongTenant)
	}); err != nil || wrongTenant != 0 {
		t.Fatalf("cross-tenant RLS count=%d err=%v", wrongTenant, err)
	}
	var hiddenSettings int
	if err = db.WithTenantTx(ctx, pool, other.ID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM public.tenant_settings WHERE tenant_id=$1`, created.ID).Scan(&hiddenSettings)
	}); err != nil || hiddenSettings != 0 {
		t.Fatalf("cross-tenant settings RLS count=%d err=%v", hiddenSettings, err)
	}
	var noContextSettings int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM public.tenant_settings WHERE tenant_id=$1`, created.ID).Scan(&noContextSettings); err != nil || noContextSettings != 0 {
		t.Fatalf("application role bypassed settings RLS count=%d err=%v", noContextSettings, err)
	}
	var ownCount int
	if err = db.WithTenantTx(ctx, pool, created.ID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM public.tenant_domains WHERE id=$1`, registered.ID).Scan(&ownCount)
	}); err != nil || ownCount != 1 {
		t.Fatalf("own tenant RLS count=%d err=%v", ownCount, err)
	}

	// Ensure the app role cannot SET ROLE to the no-login owner role and bypass
	// the FORCE RLS policy.
	if _, err = pool.Exec(ctx, "SET ROLE tenant_db_owner"); err == nil {
		t.Fatal("application role unexpectedly assumed owner role")
	}
}

func stringRef(value string) *string { return &value }
func intsRef(value []int) *[]int     { return &value }
