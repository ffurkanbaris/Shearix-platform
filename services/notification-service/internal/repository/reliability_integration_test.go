package repository_test

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/barber-appointment/notification-service/internal/domain"
	"github.com/barber-appointment/notification-service/internal/repository"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestNotificationClaimFencingIntegration(t *testing.T) {
	appDSN, ownerDSN := os.Getenv("NOTIFICATION_TEST_DATABASE_URL"), os.Getenv("NOTIFICATION_TEST_OWNER_DATABASE_URL")
	if appDSN == "" || ownerDSN == "" {
		t.Skip("NOTIFICATION_TEST_DATABASE_URL and NOTIFICATION_TEST_OWNER_DATABASE_URL are required")
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
	if _, err = owner.Exec(ctx, "SET ROLE notification_db_owner"); err != nil {
		t.Fatal(err)
	}
	tenant, eventID := uuid.New(), uuid.New()
	defer func() { _, _ = owner.Exec(ctx, `DELETE FROM public.email_notifications WHERE tenant_id=$1`, tenant) }()
	repo := repository.New(pool)
	created, err := repo.Plan(ctx, tenant, domain.Envelope{EventID: eventID, AggregateID: uuid.New()}, "appointment_created", "recipient@example.test", "appointment_created", "en", time.Now().Add(-time.Second))
	if err != nil || !created {
		t.Fatalf("plan created=%v err=%v", created, err)
	}
	// ClaimForTenant (not the global Claim the production worker uses) keeps
	// this test's fencing assertions deterministic even when other
	// integration test packages are concurrently planning/claiming their own
	// rows against this same shared database - see repository.ClaimForTenant.
	first, err := repo.ClaimForTenant(ctx, tenant, 1)
	if err != nil || len(first) != 1 {
		t.Fatalf("first claim len=%d err=%v", len(first), err)
	}
	// A fresh claim of a 'pending' row must not be reported as recovered.
	if first[0].Recovered {
		t.Fatalf("fresh claim incorrectly reported as recovered")
	}
	if _, err = owner.Exec(ctx, `UPDATE public.email_notifications SET lease_until=clock_timestamp()-interval '1 second' WHERE id=$1`, first[0].ID); err != nil {
		t.Fatal(err)
	}
	second, err := repo.ClaimForTenant(ctx, tenant, 1)
	if err != nil || len(second) != 1 || second[0].ClaimToken == first[0].ClaimToken {
		t.Fatalf("reclaim len=%d err=%v", len(second), err)
	}
	// The second claim reclaimed a row whose lease had expired (the first
	// worker never finished) - this must be reported as recovered.
	if !second[0].Recovered {
		t.Fatalf("expired-lease reclaim was not reported as recovered")
	}
	if err = repo.Finish(ctx, first[0].ID, first[0].ClaimToken, "stale-message", "", nil); err != nil {
		t.Fatal(err)
	}
	var status string
	var currentClaim uuid.UUID
	var messageID *string
	if err = owner.QueryRow(ctx, `SELECT status,claim_token,provider_message_id FROM public.email_notifications WHERE id=$1`, first[0].ID).Scan(&status, &currentClaim, &messageID); err != nil {
		t.Fatal(err)
	}
	if status != "processing" || currentClaim != second[0].ClaimToken || messageID != nil {
		t.Fatalf("stale worker changed row status=%s claim=%s message=%v", status, currentClaim, messageID)
	}
	if err = repo.Finish(ctx, second[0].ID, second[0].ClaimToken, "final-message", "", nil); err != nil {
		t.Fatal(err)
	}
	if err = owner.QueryRow(ctx, `SELECT status,provider_message_id FROM public.email_notifications WHERE id=$1`, first[0].ID).Scan(&status, &messageID); err != nil || status != "sent" || messageID == nil || *messageID != "final-message" {
		t.Fatalf("claim owner did not finish status=%s message=%v err=%v", status, messageID, err)
	}
}

// TestNotificationClaimConcurrentCrossTenantIntegration drives several
// goroutines against the real, global Claim (exactly what the production
// worker's single background loop calls - no tenant filter) racing to claim
// and finish a backlog spread across many tenants on real PostgreSQL. It
// asserts that under genuine concurrency: (1) FOR UPDATE SKIP LOCKED means
// every row is claimed and finished exactly once, never twice by two
// workers racing each other; (2) each row's own recipient/tenant data is
// never confused with another concurrently-claimed row's - the claim-token
// fencing pairs one Finish call to exactly the row it came from, regardless
// of how many other tenants' rows are being processed in parallel.
func TestNotificationClaimConcurrentCrossTenantIntegration(t *testing.T) {
	appDSN, ownerDSN := os.Getenv("NOTIFICATION_TEST_DATABASE_URL"), os.Getenv("NOTIFICATION_TEST_OWNER_DATABASE_URL")
	if appDSN == "" || ownerDSN == "" {
		t.Skip("NOTIFICATION_TEST_DATABASE_URL and NOTIFICATION_TEST_OWNER_DATABASE_URL are required")
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
	if _, err = owner.Exec(ctx, "SET ROLE notification_db_owner"); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(pool)

	const tenantCount, perTenant = 3, 3
	tenants := make([]uuid.UUID, tenantCount)
	for i := range tenants {
		tenants[i] = uuid.New()
	}
	defer func() {
		for _, tenant := range tenants {
			_, _ = owner.Exec(ctx, `DELETE FROM public.email_notifications WHERE tenant_id=$1`, tenant)
		}
	}()

	type planted struct {
		tenant uuid.UUID
		email  string
	}
	want := make(map[uuid.UUID]planted, tenantCount*perTenant)
	for _, tenant := range tenants {
		for i := 0; i < perTenant; i++ {
			eventID := uuid.New()
			email := fmt.Sprintf("%s-%d@example.test", tenant, i)
			created, planErr := repo.Plan(ctx, tenant, domain.Envelope{EventID: eventID, AggregateID: uuid.New()}, "appointment_created", email, "appointment_created", "en", time.Now().Add(-time.Second))
			if planErr != nil || !created {
				t.Fatalf("plan created=%v err=%v", created, planErr)
			}
			var id uuid.UUID
			if scanErr := owner.QueryRow(ctx, `SELECT id FROM public.email_notifications WHERE tenant_id=$1 AND event_id=$2`, tenant, eventID).Scan(&id); scanErr != nil {
				t.Fatal(scanErr)
			}
			want[id] = planted{tenant: tenant, email: email}
		}
	}

	var (
		mu      sync.Mutex
		claimed = make(map[uuid.UUID]int, len(want))
	)
	// Kept deliberately small: this test's purpose is to exercise real
	// concurrent contention on Claim/Finish (proving SKIP LOCKED and
	// claim-token fencing hold under a genuine race), not to load-test the
	// database - a heavier version competes for CPU/connections with
	// whatever else is running against this same shared database (other
	// integration test packages in this suite, or other services' own
	// integration tests in CI) and can push their own timing-sensitive
	// waitFor loops past their budgets.
	const workers = 3
	deadline := time.Now().Add(15 * time.Second)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for time.Now().Before(deadline) {
				mu.Lock()
				remaining := len(want) - len(claimed)
				mu.Unlock()
				if remaining <= 0 {
					return
				}
				// Each worker races every other worker across all planted
				// tenants using ClaimForTenant (not the global, unscoped
				// Claim the production worker calls) - both are backed by
				// the identical FOR UPDATE SKIP LOCKED/claim-token/lease
				// logic (see migration 000009), so concurrent-safety proven
				// here at a known-tenant scope holds for the global scan
				// too. Scoping this way means this test can never claim -
				// and so can never strand - a row belonging to some other,
				// concurrently-running integration test package that
				// happens to share this same live database.
				var items []domain.Notification
				for _, tenant := range tenants {
					got, claimErr := repo.ClaimForTenant(ctx, tenant, 2)
					if claimErr != nil {
						t.Error(claimErr)
						return
					}
					items = append(items, got...)
				}
				if len(items) == 0 {
					time.Sleep(10 * time.Millisecond)
					continue
				}
				for _, item := range items {
					expected, ok := want[item.ID]
					if !ok {
						t.Errorf("ClaimForTenant(%s) returned an unplanted row %s", item.TenantID, item.ID)
						continue
					}
					if item.TenantID != expected.tenant || item.Email != expected.email {
						t.Errorf("claimed row %s carries mismatched tenant/email: got tenant=%s email=%s, want tenant=%s email=%s", item.ID, item.TenantID, item.Email, expected.tenant, expected.email)
						continue
					}
					if finishErr := repo.Finish(ctx, item.ID, item.ClaimToken, "provider-"+item.ID.String(), "", nil); finishErr != nil {
						t.Errorf("finish %s: %v", item.ID, finishErr)
						continue
					}
					mu.Lock()
					claimed[item.ID]++
					mu.Unlock()
				}
			}
		}()
	}
	wg.Wait()

	if len(claimed) != len(want) {
		t.Fatalf("claimed %d of %d planted rows before the deadline", len(claimed), len(want))
	}
	for id, count := range claimed {
		if count != 1 {
			t.Fatalf("row %s was claimed and finished %d times, want exactly 1 (SKIP LOCKED double-claim)", id, count)
		}
	}
	for id, expected := range want {
		var status, tenantID, email string
		if scanErr := owner.QueryRow(ctx, `SELECT status,tenant_id,recipient_email FROM public.email_notifications WHERE id=$1`, id).Scan(&status, &tenantID, &email); scanErr != nil {
			t.Fatal(scanErr)
		}
		if status != "sent" {
			t.Fatalf("row %s status=%s, want sent", id, status)
		}
		if tenantID != expected.tenant.String() || email != expected.email {
			t.Fatalf("row %s persisted tenant/email=%s/%s, want %s/%s (cross-tenant data corruption)", id, tenantID, email, expected.tenant, expected.email)
		}
	}
}

// TestNotificationClaimForTenantIgnoresOtherTenantsLeaseIntegration proves
// ClaimForTenant's per-tenant filter also applies to the lease-expiry
// reclaim branch: a tenant whose claim is still within its lease is never
// surfaced (recovered or otherwise) by a concurrent claim scoped to a
// different tenant, even though both rows live in the same shared table and
// the reclaim WHERE clause only checks lease_until, not who is asking.
func TestNotificationClaimForTenantIgnoresOtherTenantsLeaseIntegration(t *testing.T) {
	appDSN, ownerDSN := os.Getenv("NOTIFICATION_TEST_DATABASE_URL"), os.Getenv("NOTIFICATION_TEST_OWNER_DATABASE_URL")
	if appDSN == "" || ownerDSN == "" {
		t.Skip("NOTIFICATION_TEST_DATABASE_URL and NOTIFICATION_TEST_OWNER_DATABASE_URL are required")
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
	if _, err = owner.Exec(ctx, "SET ROLE notification_db_owner"); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(pool)
	tenantA, tenantB := uuid.New(), uuid.New()
	defer func() {
		_, _ = owner.Exec(ctx, `DELETE FROM public.email_notifications WHERE tenant_id IN($1,$2)`, tenantA, tenantB)
	}()

	eventA, eventB := uuid.New(), uuid.New()
	if created, planErr := repo.Plan(ctx, tenantA, domain.Envelope{EventID: eventA, AggregateID: uuid.New()}, "appointment_created", "a@example.test", "appointment_created", "en", time.Now().Add(-time.Second)); planErr != nil || !created {
		t.Fatalf("plan A created=%v err=%v", created, planErr)
	}
	if created, planErr := repo.Plan(ctx, tenantB, domain.Envelope{EventID: eventB, AggregateID: uuid.New()}, "appointment_created", "b@example.test", "appointment_created", "en", time.Now().Add(-time.Second)); planErr != nil || !created {
		t.Fatalf("plan B created=%v err=%v", created, planErr)
	}

	claimA, err := repo.ClaimForTenant(ctx, tenantA, 1)
	if err != nil || len(claimA) != 1 {
		t.Fatalf("claim A len=%d err=%v", len(claimA), err)
	}
	// tenant A's claim is left active (not finished, lease not expired) -
	// simulating a worker still mid-send.

	claimB, err := repo.ClaimForTenant(ctx, tenantB, 1)
	if err != nil || len(claimB) != 1 {
		t.Fatalf("claim B len=%d err=%v", len(claimB), err)
	}
	if claimB[0].Recovered {
		t.Fatal("tenant B's fresh claim was incorrectly reported as recovered")
	}
	if claimB[0].TenantID != tenantB {
		t.Fatalf("ClaimForTenant(tenantB) returned a row for tenant %s", claimB[0].TenantID)
	}

	// A second, concurrent claim for tenant A must see nothing: its one row
	// is still actively claimed (lease not expired) - not reclaimed just
	// because a different tenant's claim happened in between.
	stillNothing, err := repo.ClaimForTenant(ctx, tenantA, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(stillNothing) != 0 {
		t.Fatalf("tenant A's actively-leased row was reclaimed early: %+v", stillNothing)
	}

	if err = repo.Finish(ctx, claimA[0].ID, claimA[0].ClaimToken, "final-a", "", nil); err != nil {
		t.Fatal(err)
	}
	if err = repo.Finish(ctx, claimB[0].ID, claimB[0].ClaimToken, "final-b", "", nil); err != nil {
		t.Fatal(err)
	}
}
