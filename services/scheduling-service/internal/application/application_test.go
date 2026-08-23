package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/barber-appointment/platform/schedulingdeps"
	"github.com/barber-appointment/platform/tenantctx"
	"github.com/barber-appointment/scheduling-service/internal/domain"
	"github.com/barber-appointment/scheduling-service/internal/service"
	"github.com/google/uuid"
)

type fakeRepo struct{}

func (fakeRepo) ReplaceHours(context.Context, uuid.UUID, uuid.UUID, domain.WorkingHoursInput) error {
	return nil
}
func (fakeRepo) Hours(context.Context, uuid.UUID, uuid.UUID) (domain.WorkingHoursInput, error) {
	return nil, nil
}
func (fakeRepo) Overrides(context.Context, uuid.UUID, uuid.UUID) ([]domain.Override, error) {
	return nil, nil
}
func (fakeRepo) SaveOverride(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, domain.OverrideInput) (domain.Override, error) {
	return domain.Override{}, nil
}
func (fakeRepo) DeleteOverride(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) error { return nil }
func (fakeRepo) Blocks(context.Context, uuid.UUID, uuid.UUID) ([]domain.BlockedPeriod, error) {
	return nil, nil
}
func (fakeRepo) SaveBlock(context.Context, uuid.UUID, uuid.UUID, domain.BlockedPeriodInput) (domain.BlockedPeriod, error) {
	return domain.BlockedPeriod{}, nil
}
func (fakeRepo) DeleteBlock(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) error { return nil }
func (fakeRepo) Day(context.Context, uuid.UUID, uuid.UUID, time.Time) (domain.Override, []domain.BlockedPeriod, error) {
	return domain.Override{}, nil, nil
}

type fakeDeps struct {
	barber      schedulingdeps.Barber
	settingsErr error
}

func (f fakeDeps) Barber(context.Context, tenantctx.Context, uuid.UUID) (schedulingdeps.Barber, error) {
	return f.barber, nil
}
func (fakeDeps) Service(context.Context, tenantctx.Context, uuid.UUID, uuid.UUID) (schedulingdeps.Service, error) {
	return schedulingdeps.Service{DurationMinutes: 30}, nil
}
func (f fakeDeps) Settings(context.Context, tenantctx.Context) (schedulingdeps.TenantSettings, error) {
	return schedulingdeps.TenantSettings{Timezone: "UTC", BookingIntervalMinutes: 15}, f.settingsErr
}

type fakeOccupancy struct{ err error }

func (f fakeOccupancy) Occupied(context.Context, tenantctx.Context, uuid.UUID, time.Time, time.Time) ([]domain.BlockedPeriod, error) {
	return nil, f.err
}

func TestBarberAuthorizationIsResourceScoped(t *testing.T) {
	identity := uuid.New()
	barber := uuid.New()
	tenant := tenantctx.Context{TenantID: uuid.New(), AppType: "admin"}
	app := New(fakeRepo{}, fakeDeps{barber: schedulingdeps.Barber{IdentityID: &identity}}, fakeOccupancy{}, 15)
	if _, err := app.Schedule(context.Background(), tenant, Principal{Role: "BARBER", IdentityID: uuid.New()}, barber); !errors.Is(err, ErrForbidden) {
		t.Fatalf("wrong barber identity=%v", err)
	}
	if _, err := app.Schedule(context.Background(), tenant, Principal{Role: "BARBER", IdentityID: identity}, barber); err != nil {
		t.Fatalf("own schedule=%v", err)
	}
}
func TestAvailabilityFailsClosedOnOccupancyOutage(t *testing.T) {
	app := New(fakeRepo{}, fakeDeps{}, fakeOccupancy{err: service.ErrOccupancyUnavailable}, 15)
	_, err := app.Availability(context.Background(), AvailabilityQuery{Tenant: tenantctx.Context{TenantID: uuid.New(), AppType: "booking"}, BarberID: uuid.New(), ServiceID: uuid.New(), Date: time.Now()})
	if !errors.Is(err, ErrOccupancyUnavailable) {
		t.Fatalf("occupancy error=%v", err)
	}
}
