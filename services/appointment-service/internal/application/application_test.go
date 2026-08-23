package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/barber-appointment/appointment-service/internal/domain"
	"github.com/barber-appointment/platform/schedulingdeps"
	"github.com/barber-appointment/platform/tenantctx"
	"github.com/google/uuid"
)

type fakeRepo struct {
	replay                       domain.Appointment
	found                        bool
	replayErr                    error
	createCalls, transitionCalls int
}

func (f *fakeRepo) Replay(context.Context, uuid.UUID, string, string, string, string) (domain.Appointment, bool, error) {
	return f.replay, f.found, f.replayErr
}
func (f *fakeRepo) Create(context.Context, uuid.UUID, domain.CreateInput, time.Time, time.Time, time.Time, string, string, string, string) (domain.Appointment, error) {
	f.createCalls++
	return domain.Appointment{ID: uuid.New()}, nil
}
func (f *fakeRepo) Get(context.Context, uuid.UUID, uuid.UUID) (domain.Appointment, error) {
	return domain.Appointment{}, nil
}
func (f *fakeRepo) List(context.Context, uuid.UUID) ([]domain.Appointment, error) { return nil, nil }
func (f *fakeRepo) ListForCustomer(context.Context, uuid.UUID, uuid.UUID, bool) ([]domain.Appointment, error) {
	return nil, nil
}
func (f *fakeRepo) GetForCustomer(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (domain.Appointment, error) {
	return domain.Appointment{}, nil
}
func (f *fakeRepo) Transition(context.Context, uuid.UUID, uuid.UUID, string) (domain.Appointment, error) {
	f.transitionCalls++
	return domain.Appointment{}, nil
}
func (f *fakeRepo) Reschedule(context.Context, uuid.UUID, uuid.UUID, time.Time, time.Time, time.Time, time.Time) (domain.Appointment, error) {
	return domain.Appointment{}, nil
}
func (f *fakeRepo) Occupancy(context.Context, uuid.UUID, uuid.UUID, time.Time, time.Time) ([]domain.Appointment, error) {
	return nil, nil
}

type fakeDeps struct{ calls int }

func (f *fakeDeps) BranchBarber(context.Context, tenantctx.Context, uuid.UUID, uuid.UUID) error {
	f.calls++
	return nil
}
func (f *fakeDeps) Barber(context.Context, tenantctx.Context, uuid.UUID) error { f.calls++; return nil }
func (f *fakeDeps) Service(context.Context, tenantctx.Context, uuid.UUID, uuid.UUID) (schedulingdeps.Service, error) {
	f.calls++
	return schedulingdeps.Service{DurationMinutes: 30}, nil
}
func (f *fakeDeps) Settings(context.Context, tenantctx.Context) (schedulingdeps.TenantSettings, error) {
	return schedulingdeps.TenantSettings{}, nil
}
func (f *fakeDeps) Available(context.Context, tenantctx.Context, domain.CreateInput, time.Time) error {
	f.calls++
	return nil
}
func (f *fakeDeps) Customer(context.Context, tenantctx.Context, uuid.UUID) (CustomerSnapshot, error) {
	f.calls++
	return CustomerSnapshot{Name: "Customer", Email: "customer@example.test"}, nil
}

func command() CreateCommand {
	return CreateCommand{Tenant: tenantctx.Context{TenantID: uuid.New(), AppType: "booking"}, Principal: Principal{Kind: "guest"}, Operation: "public.create_appointment", IdempotencyKey: uuid.NewString(), Input: domain.CreateInput{BranchID: uuid.New(), BarberID: uuid.New(), ServiceID: uuid.New(), CustomerName: "Guest", CustomerContact: "guest@example.test", StartAt: time.Now().Add(time.Hour)}}
}

func TestBookingReplayPrecedesDependencies(t *testing.T) {
	existing := domain.Appointment{ID: uuid.New()}
	repo := &fakeRepo{replay: existing, found: true}
	deps := &fakeDeps{}
	got, err := NewBooking(repo, deps).Create(context.Background(), command())
	if err != nil || got.ID != existing.ID || deps.calls != 0 || repo.createCalls != 0 {
		t.Fatalf("replay=%s dependency_calls=%d create_calls=%d err=%v", got.ID, deps.calls, repo.createCalls, err)
	}
}

func TestBookingPropagatesIdempotencyConflict(t *testing.T) {
	repo := &fakeRepo{replayErr: errors.New("wrapped: " + ErrIdempotencyConflict.Error())}
	// errors.Is requires the actual sentinel, as repository adapters provide.
	repo.replayErr = ErrIdempotencyConflict
	_, err := NewBooking(repo, &fakeDeps{}).Create(context.Background(), command())
	if !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("conflict=%v", err)
	}
}

func TestApplicationAuthorizationAndTransitions(t *testing.T) {
	repo := &fakeRepo{}
	lifecycle := NewLifecycle(repo, &fakeDeps{})
	if _, err := lifecycle.Transition(context.Background(), uuid.New(), Principal{Kind: "admin", Role: "BARBER", IdentityID: uuid.New()}, uuid.New(), "confirmed"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("barber transition=%v", err)
	}
	if _, err := lifecycle.Transition(context.Background(), uuid.New(), Principal{Kind: "admin", Role: "MANAGER", IdentityID: uuid.New()}, uuid.New(), "invalid"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid transition=%v", err)
	}
	if _, err := lifecycle.Transition(context.Background(), uuid.New(), Principal{Kind: "admin", Role: "MANAGER", IdentityID: uuid.New()}, uuid.New(), "confirmed"); err != nil || repo.transitionCalls != 1 {
		t.Fatalf("valid transition calls=%d err=%v", repo.transitionCalls, err)
	}
}

func TestIdempotencyKeyAndCanonicalFingerprint(t *testing.T) {
	if !ValidIdempotencyKey("request-123") || ValidIdempotencyKey("") || ValidIdempotencyKey(string(make([]byte, 129))) {
		t.Fatal("idempotency key contract")
	}
	cmd := command()
	in := cmd.Input
	same := in
	same.CustomerName = " " + in.CustomerName + " "
	same.StartAt = in.StartAt.UTC()
	if fingerprint(cmd.Tenant.TenantID, "guest:x", cmd.Operation, in) != fingerprint(cmd.Tenant.TenantID, "guest:x", cmd.Operation, same) {
		t.Fatal("semantic fingerprint is not canonical")
	}
}
