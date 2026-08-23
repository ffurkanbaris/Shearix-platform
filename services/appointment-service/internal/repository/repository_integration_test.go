package repository_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/barber-appointment/appointment-service/internal/domain"
	"github.com/barber-appointment/appointment-service/internal/repository"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAppointmentFoundationIntegration(t *testing.T) {
	appDSN, ownerDSN := os.Getenv("APPOINTMENT_TEST_DATABASE_URL"), os.Getenv("APPOINTMENT_TEST_OWNER_DATABASE_URL")
	if appDSN == "" || ownerDSN == "" {
		t.Skip("APPOINTMENT_TEST_DATABASE_URL and APPOINTMENT_TEST_OWNER_DATABASE_URL are required")
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
	if _, err = owner.Exec(ctx, "SET ROLE appointment_db_owner"); err != nil {
		t.Fatal(err)
	}
	tenantA, tenantB := uuid.New(), uuid.New()
	defer func() {
		for _, tenant := range []uuid.UUID{tenantA, tenantB} {
			_, _ = owner.Exec(ctx, `DELETE FROM public.outbox_events WHERE tenant_id=$1; DELETE FROM public.idempotency_keys WHERE tenant_id=$1; DELETE FROM public.appointments WHERE tenant_id=$1`, tenant)
		}
	}()
	r := repository.New(pool)
	base := time.Date(2032, 2, 3, 10, 0, 0, 0, time.UTC)
	input := func(start time.Time) domain.CreateInput {
		return domain.CreateInput{BranchID: uuid.New(), BarberID: uuid.New(), ServiceID: uuid.New(), CustomerName: "Integration Test", CustomerContact: "test@example.invalid", StartAt: start}
	}
	// Keep all cases on one barber to exercise the GiST exclusion constraint.
	in := input(base)
	fingerprint := func(value string) string {
		sum := sha256.Sum256([]byte(value))
		return hex.EncodeToString(sum[:])
	}
	createFor := func(tenant uuid.UUID, v domain.CreateInput, principal, operation, key, semantic string) (domain.Appointment, error) {
		return r.Create(ctx, tenant, v, v.StartAt.Add(30*time.Minute), v.StartAt.Add(-5*time.Minute), v.StartAt.Add(40*time.Minute), principal, operation, key, fingerprint(semantic))
	}
	create := func(tenant uuid.UUID, v domain.CreateInput, key string) (domain.Appointment, error) {
		return createFor(tenant, v, "customer:"+tenant.String(), "public.create_appointment", key, key)
	}
	first, err := create(tenantA, in, "valid")
	if err != nil {
		t.Fatalf("valid creation: %v", err)
	}
	if _, err = r.Get(ctx, tenantB, first.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("wrong tenant access: %v", err)
	}
	var hidden int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM public.appointments`).Scan(&hidden); err != nil || hidden != 0 {
		t.Fatalf("RLS without context count=%d err=%v", hidden, err)
	}
	if _, err = create(tenantA, in, "overlap"); !repository.IsConflict(err) {
		t.Fatalf("expected exclusion conflict, got %v", err)
	}
	if _, err = create(tenantA, func() domain.CreateInput { x := in; x.StartAt = base.Add(time.Hour); return x }(), "non-overlap"); err != nil {
		t.Fatalf("non-overlap: %v", err)
	}
	if _, err = r.Transition(ctx, tenantA, first.ID, "cancelled"); err != nil {
		t.Fatal(err)
	}
	if _, err = create(tenantA, in, "after-cancel"); err != nil {
		t.Fatalf("cancelled appointment should release range: %v", err)
	}

	// Concurrent independently-keyed attempts must leave exactly one booking.
	concurrent := input(base.Add(3 * time.Hour))
	concurrent.BarberID = in.BarberID
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, key := range []string{"race-a", "race-b"} {
		wg.Add(1)
		go func(key string) { defer wg.Done(); _, e := create(tenantA, concurrent, key); results <- e }(key)
	}
	wg.Wait()
	close(results)
	successes := 0
	for e := range results {
		if e == nil {
			successes++
		} else if !repository.IsConflict(e) {
			t.Fatalf("race error %v", e)
		}
	}
	if successes != 1 {
		t.Fatalf("expected exactly one racing success, got %d", successes)
	}

	// Same idempotency key is serialized and returns the original aggregate.
	idem := input(base.Add(5 * time.Hour))
	idem.BarberID = in.BarberID
	results = make(chan error, 2)
	ids := make(chan uuid.UUID, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a, e := create(tenantA, idem, "duplicate")
			if e == nil {
				ids <- a.ID
			}
			results <- e
		}()
	}
	wg.Wait()
	close(results)
	close(ids)
	var got []uuid.UUID
	for e := range results {
		if e != nil {
			t.Fatalf("idempotent retry: %v", e)
		}
	}
	for id := range ids {
		got = append(got, id)
	}
	if len(got) != 2 || got[0] != got[1] {
		t.Fatalf("idempotency did not return stable aggregate: %v", got)
	}
	replayed, found, err := r.Replay(ctx, tenantA, "customer:"+tenantA.String(), "public.create_appointment", "duplicate", fingerprint("duplicate"))
	if err != nil || !found || replayed.ID != got[0] {
		t.Fatalf("pre-validation replay id=%s found=%v err=%v", replayed.ID, found, err)
	}
	if _, _, err = r.Replay(ctx, tenantA, "customer:"+tenantA.String(), "public.create_appointment", "duplicate", fingerprint("different")); !errors.Is(err, repository.ErrIdempotencyConflict) {
		t.Fatalf("pre-validation replay conflict: %v", err)
	}
	_, err = createFor(tenantA, idem, "customer:"+tenantA.String(), "public.create_appointment", "duplicate", "different")
	if !errors.Is(err, repository.ErrIdempotencyConflict) {
		t.Fatalf("mismatched key payload: %v", err)
	}

	// Principal and operation are independent scopes for the same tenant/key.
	if _, err = r.Transition(ctx, tenantA, got[0], "cancelled"); err != nil {
		t.Fatal(err)
	}
	otherPrincipal, err := createFor(tenantA, idem, "customer:"+uuid.NewString(), "public.create_appointment", "duplicate", "duplicate")
	if err != nil || otherPrincipal.ID == got[0] {
		t.Fatalf("different principal collided: id=%s err=%v", otherPrincipal.ID, err)
	}
	if _, err = r.Transition(ctx, tenantA, otherPrincipal.ID, "cancelled"); err != nil {
		t.Fatal(err)
	}
	otherOperation, err := createFor(tenantA, idem, "customer:"+tenantA.String(), "admin.create_appointment", "duplicate", "duplicate")
	if err != nil || otherOperation.ID == got[0] {
		t.Fatalf("different operation collided: id=%s err=%v", otherOperation.ID, err)
	}

	// Concurrent conflicting semantic requests serialize on the complete scope:
	// exactly one wins and the other deterministically reports idempotency conflict.
	conflictA := input(base.Add(12 * time.Hour))
	conflictB := input(base.Add(14 * time.Hour))
	results = make(chan error, 2)
	for _, candidate := range []domain.CreateInput{conflictA, conflictB} {
		wg.Add(1)
		go func(v domain.CreateInput) {
			defer wg.Done()
			_, e := createFor(tenantA, v, "customer:conflict", "public.create_appointment", "concurrent-conflict", v.StartAt.String())
			results <- e
		}(candidate)
	}
	wg.Wait()
	close(results)
	successes, conflicts := 0, 0
	for e := range results {
		switch {
		case e == nil:
			successes++
		case errors.Is(e, repository.ErrIdempotencyConflict):
			conflicts++
		default:
			t.Fatalf("concurrent conflicting request: %v", e)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("concurrent conflict outcomes success=%d conflict=%d", successes, conflicts)
	}

	// Reschedule cannot bypass overlap protection, and a successful move releases old occupancy.
	moving := input(base.Add(7 * time.Hour))
	moving.BarberID = in.BarberID
	moved, err := create(tenantA, moving, "moving")
	if err != nil {
		t.Fatal(err)
	}
	occupied := input(base.Add(9 * time.Hour))
	occupied.BarberID = in.BarberID
	if _, err = create(tenantA, occupied, "occupied"); err != nil {
		t.Fatal(err)
	}
	if _, err = r.Reschedule(ctx, tenantA, moved.ID, occupied.StartAt, occupied.StartAt.Add(30*time.Minute), occupied.StartAt.Add(-5*time.Minute), occupied.StartAt.Add(40*time.Minute)); !repository.IsConflict(err) {
		t.Fatalf("reschedule conflict: %v", err)
	}
	newStart := base.Add(8 * time.Hour)
	if _, err = r.Reschedule(ctx, tenantA, moved.ID, newStart, newStart.Add(30*time.Minute), newStart.Add(-5*time.Minute), newStart.Add(40*time.Minute)); err != nil {
		t.Fatalf("successful reschedule: %v", err)
	}
	if _, err = create(tenantA, moving, "old-range-free"); err != nil {
		t.Fatalf("old range not released: %v", err)
	}

	blocking, err := r.Occupancy(ctx, tenantA, in.BarberID, base.Add(-time.Hour), base.Add(12*time.Hour))
	if err != nil || len(blocking) == 0 {
		t.Fatalf("blocking occupancy: %d %v", len(blocking), err)
	}
	other, err := r.Occupancy(ctx, tenantB, in.BarberID, base.Add(-time.Hour), base.Add(12*time.Hour))
	if err != nil || len(other) != 0 {
		t.Fatalf("occupancy tenant isolation: %d %v", len(other), err)
	}
	var events, outbox int
	if err = owner.QueryRow(ctx, `SELECT count(*) FROM public.appointment_events WHERE tenant_id=$1`, tenantA).Scan(&events); err == nil {
		err = owner.QueryRow(ctx, `SELECT count(*) FROM public.outbox_events WHERE tenant_id=$1`, tenantA).Scan(&outbox)
	}
	if err != nil || events != outbox {
		t.Fatalf("event/outbox atomicity events=%d outbox=%d err=%v", events, outbox, err)
	}

	// Tenant-aware FK rejects an idempotency reference to another tenant.
	if _, err = owner.Exec(ctx, `INSERT INTO public.idempotency_keys(tenant_id,principal_scope,operation,key,fingerprint,appointment_id) VALUES($1,'customer:test','public.create_appointment','cross-tenant',$2,$3)`, tenantB, fingerprint("cross-tenant"), first.ID); err == nil {
		t.Fatal("cross-tenant idempotency foreign key was accepted")
	}

	// Retention deletes only expired keys and acknowledged outbox rows, in a
	// bounded batch. Pending and recent rows must survive.
	var oldIdempotencyID uuid.UUID
	if err = owner.QueryRow(ctx, `SELECT appointment_id FROM public.idempotency_keys WHERE tenant_id=$1 LIMIT 1`, tenantA).Scan(&oldIdempotencyID); err != nil {
		t.Fatal(err)
	}
	if _, err = owner.Exec(ctx, `UPDATE public.idempotency_keys SET created_at=now()-interval '8 days' WHERE tenant_id=$1 AND appointment_id=$2`, tenantA, oldIdempotencyID); err != nil {
		t.Fatal(err)
	}
	var oldPublished, recentPublished, pending uuid.UUID
	if err = owner.QueryRow(ctx, `INSERT INTO public.outbox_events(tenant_id,aggregate_id,type,payload,published_at) VALUES($1,$2,'test.old','{}',now()-interval '31 days') RETURNING id`, tenantA, first.ID).Scan(&oldPublished); err != nil {
		t.Fatal(err)
	}
	if err = owner.QueryRow(ctx, `INSERT INTO public.outbox_events(tenant_id,aggregate_id,type,payload,published_at) VALUES($1,$2,'test.recent','{}',now()) RETURNING id`, tenantA, first.ID).Scan(&recentPublished); err != nil {
		t.Fatal(err)
	}
	if err = owner.QueryRow(ctx, `INSERT INTO public.outbox_events(tenant_id,aggregate_id,type,payload) VALUES($1,$2,'test.pending','{}') RETURNING id`, tenantA, first.ID).Scan(&pending); err != nil {
		t.Fatal(err)
	}
	idemDeleted, outboxDeleted, err := r.CleanupRetention(ctx, 1)
	if err != nil || idemDeleted != 1 || outboxDeleted != 1 {
		t.Fatalf("bounded cleanup idempotency=%d outbox=%d err=%v", idemDeleted, outboxDeleted, err)
	}
	var survivors int
	if err = owner.QueryRow(ctx, `SELECT count(*) FROM public.outbox_events WHERE id=ANY($1)`, []uuid.UUID{recentPublished, pending}).Scan(&survivors); err != nil || survivors != 2 {
		t.Fatalf("retention removed recent/pending rows survivors=%d err=%v", survivors, err)
	}
	if err = owner.QueryRow(ctx, `SELECT count(*) FROM public.outbox_events WHERE id=$1`, oldPublished).Scan(&survivors); err != nil || survivors != 0 {
		t.Fatalf("old published row survived cleanup count=%d err=%v", survivors, err)
	}
	var activeKeys int
	if err = owner.QueryRow(ctx, `SELECT count(*) FROM public.idempotency_keys WHERE tenant_id=$1 AND created_at>=now()-interval '7 days'`, tenantA).Scan(&activeKeys); err != nil || activeKeys == 0 {
		t.Fatalf("active idempotency rows were not retained count=%d err=%v", activeKeys, err)
	}

	var appointmentDelete, eventSelect, outboxUpdate bool
	if err = owner.QueryRow(ctx, `SELECT has_table_privilege('appointment_db_app','public.appointments','DELETE'),has_table_privilege('appointment_db_app','public.appointment_events','SELECT'),has_table_privilege('appointment_db_app','public.outbox_events','UPDATE')`).Scan(&appointmentDelete, &eventSelect, &outboxUpdate); err != nil {
		t.Fatal(err)
	}
	if appointmentDelete || eventSelect || outboxUpdate {
		t.Fatalf("runtime role retained broad grants appointment_delete=%v event_select=%v outbox_update=%v", appointmentDelete, eventSelect, outboxUpdate)
	}
}
