package application

import (
	"context"
	"errors"
	"testing"

	"github.com/barber-appointment/barber-service/internal/domain"
	"github.com/barber-appointment/platform/adminauth"
	"github.com/barber-appointment/platform/tenantctx"
	"github.com/google/uuid"
)

type fakeRepo struct {
	branch   domain.Branch
	barber   domain.Barber
	assigned bool
}

func (f fakeRepo) Assigned(_ context.Context, _, _, _ uuid.UUID, out *bool) error {
	*out = f.assigned
	return nil
}
func (f fakeRepo) Branches(context.Context, uuid.UUID, bool) ([]domain.Branch, error) {
	return nil, nil
}
func (f fakeRepo) Branch(context.Context, uuid.UUID, uuid.UUID) (domain.Branch, error) {
	return f.branch, nil
}
func (f fakeRepo) SaveBranch(context.Context, uuid.UUID, uuid.UUID, domain.BranchInput) (domain.Branch, error) {
	return domain.Branch{}, nil
}
func (f fakeRepo) Barbers(context.Context, uuid.UUID, bool) ([]domain.Barber, error) { return nil, nil }
func (f fakeRepo) Barber(context.Context, uuid.UUID, uuid.UUID) (domain.Barber, error) {
	return f.barber, nil
}
func (f fakeRepo) SaveBarber(context.Context, uuid.UUID, uuid.UUID, domain.BarberInput) (domain.Barber, error) {
	return domain.Barber{}, nil
}
func (f fakeRepo) LinkIdentity(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) error { return nil }
func (f fakeRepo) UnlinkIdentity(context.Context, uuid.UUID, uuid.UUID) error          { return nil }

type fakeAuth struct {
	principal adminauth.Principal
	err       error
}

func (f fakeAuth) Authenticate(context.Context, tenantctx.Context, string) (adminauth.Principal, error) {
	return f.principal, f.err
}
func (f fakeAuth) EnsureBarberMembership(context.Context, tenantctx.Context, uuid.UUID) error {
	return f.err
}
func TestBookingAccessRequiresActiveTenantAssignment(t *testing.T) {
	tenant := tenantctx.Context{TenantID: uuid.New(), AppType: "booking"}
	svc := New(fakeRepo{branch: domain.Branch{Active: true}, barber: domain.Barber{Active: true}, assigned: false}, fakeAuth{})
	if err := svc.BookingAccess(context.Background(), tenant, uuid.New(), uuid.New()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("error=%v", err)
	}
}
func TestWriteAuthorizationLivesInApplication(t *testing.T) {
	tenant := tenantctx.Context{TenantID: uuid.New(), AppType: "admin"}
	svc := New(fakeRepo{}, fakeAuth{principal: adminauth.Principal{Role: "BARBER"}})
	if _, err := svc.SaveBranch(context.Background(), tenant, "cookie", uuid.Nil, domain.BranchInput{Name: "Branch"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("error=%v", err)
	}
}
