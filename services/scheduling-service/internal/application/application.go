package application

import (
	"context"
	"errors"
	"time"

	"github.com/barber-appointment/platform/schedulingdeps"
	"github.com/barber-appointment/platform/tenantctx"
	"github.com/barber-appointment/scheduling-service/internal/domain"
	"github.com/barber-appointment/scheduling-service/internal/repository"
	"github.com/barber-appointment/scheduling-service/internal/service"
	"github.com/google/uuid"
)

var (
	ErrInvalid              = errors.New("invalid input")
	ErrForbidden            = errors.New("forbidden")
	ErrNotFound             = errors.New("not found")
	ErrConflict             = errors.New("conflict")
	ErrUnavailable          = errors.New("dependency unavailable")
	ErrOccupancyUnavailable = errors.New("occupancy unavailable")
)

type Principal struct {
	IdentityID uuid.UUID
	Role       string
}
type Repository interface {
	ReplaceHours(context.Context, uuid.UUID, uuid.UUID, domain.WorkingHoursInput) error
	Hours(context.Context, uuid.UUID, uuid.UUID) (domain.WorkingHoursInput, error)
	Overrides(context.Context, uuid.UUID, uuid.UUID) ([]domain.Override, error)
	SaveOverride(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, domain.OverrideInput) (domain.Override, error)
	DeleteOverride(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) error
	Blocks(context.Context, uuid.UUID, uuid.UUID) ([]domain.BlockedPeriod, error)
	SaveBlock(context.Context, uuid.UUID, uuid.UUID, domain.BlockedPeriodInput) (domain.BlockedPeriod, error)
	DeleteBlock(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) error
	Day(context.Context, uuid.UUID, uuid.UUID, time.Time) (domain.Override, []domain.BlockedPeriod, error)
}
type Dependencies interface {
	Barber(context.Context, tenantctx.Context, uuid.UUID) (schedulingdeps.Barber, error)
	Service(context.Context, tenantctx.Context, uuid.UUID, uuid.UUID) (schedulingdeps.Service, error)
	Settings(context.Context, tenantctx.Context) (schedulingdeps.TenantSettings, error)
}
type Occupancy interface {
	Occupied(context.Context, tenantctx.Context, uuid.UUID, time.Time, time.Time) ([]domain.BlockedPeriod, error)
}

type Service struct {
	repo             Repository
	deps             Dependencies
	occupancy        Occupancy
	fallbackInterval int
	now              func() time.Time
}

func New(repo Repository, deps Dependencies, occupancy Occupancy, interval int) Service {
	return Service{repo: repo, deps: deps, occupancy: occupancy, fallbackInterval: interval, now: time.Now}
}

func (s Service) authorize(ctx context.Context, t tenantctx.Context, p Principal, barber uuid.UUID, write bool) error {
	switch p.Role {
	case "OWNER", "MANAGER":
		return nil
	case "RECEPTIONIST":
		if !write {
			return nil
		}
	case "BARBER":
		b, err := s.deps.Barber(ctx, t, barber)
		if err == nil && b.IdentityID != nil && *b.IdentityID == p.IdentityID {
			return nil
		}
	}
	return ErrForbidden
}
func validHours(in domain.WorkingHoursInput) bool {
	for _, d := range in {
		if d.Weekday < 0 || d.Weekday > 6 || !service.ValidIntervals(d.Intervals) {
			return false
		}
	}
	return true
}
func validOverride(in domain.OverrideInput) bool {
	if _, err := time.Parse("2006-01-02", in.Date); err != nil {
		return false
	}
	if in.Kind == "custom_hours" {
		return len(in.Intervals) > 0 && service.ValidIntervals(in.Intervals)
	}
	return (in.Kind == "unavailable" || in.Kind == "vacation") && len(in.Intervals) == 0
}
func mapRepo(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, repository.ErrNotFound) {
		return ErrNotFound
	}
	return err
}

func (s Service) Schedule(ctx context.Context, t tenantctx.Context, p Principal, barber uuid.UUID) (domain.Schedule, error) {
	if err := s.authorize(ctx, t, p, barber, false); err != nil {
		return domain.Schedule{}, err
	}
	hours, err := s.repo.Hours(ctx, t.TenantID, barber)
	if err != nil {
		return domain.Schedule{}, err
	}
	overrides, err := s.repo.Overrides(ctx, t.TenantID, barber)
	if err != nil {
		return domain.Schedule{}, err
	}
	blocks, err := s.repo.Blocks(ctx, t.TenantID, barber)
	return domain.Schedule{WorkingHours: hours, Overrides: overrides, BlockedPeriods: blocks}, err
}
func (s Service) ReplaceHours(ctx context.Context, t tenantctx.Context, p Principal, barber uuid.UUID, in domain.WorkingHoursInput) error {
	if err := s.authorize(ctx, t, p, barber, true); err != nil {
		return err
	}
	if !validHours(in) {
		return ErrInvalid
	}
	if err := s.repo.ReplaceHours(ctx, t.TenantID, barber, in); err != nil {
		return ErrConflict
	}
	return nil
}
func (s Service) Overrides(ctx context.Context, t tenantctx.Context, p Principal, barber uuid.UUID) ([]domain.Override, error) {
	if err := s.authorize(ctx, t, p, barber, false); err != nil {
		return nil, err
	}
	return s.repo.Overrides(ctx, t.TenantID, barber)
}
func (s Service) SaveOverride(ctx context.Context, t tenantctx.Context, p Principal, barber, id uuid.UUID, in domain.OverrideInput) (domain.Override, error) {
	if err := s.authorize(ctx, t, p, barber, true); err != nil {
		return domain.Override{}, err
	}
	if !validOverride(in) {
		return domain.Override{}, ErrInvalid
	}
	v, err := s.repo.SaveOverride(ctx, t.TenantID, barber, id, in)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return v, ErrNotFound
		}
		return v, ErrConflict
	}
	return v, nil
}
func (s Service) DeleteOverride(ctx context.Context, t tenantctx.Context, p Principal, barber, id uuid.UUID) error {
	if err := s.authorize(ctx, t, p, barber, true); err != nil {
		return err
	}
	return mapRepo(s.repo.DeleteOverride(ctx, t.TenantID, barber, id))
}
func (s Service) Blocks(ctx context.Context, t tenantctx.Context, p Principal, barber uuid.UUID) ([]domain.BlockedPeriod, error) {
	if err := s.authorize(ctx, t, p, barber, false); err != nil {
		return nil, err
	}
	return s.repo.Blocks(ctx, t.TenantID, barber)
}
func (s Service) SaveBlock(ctx context.Context, t tenantctx.Context, p Principal, barber uuid.UUID, in domain.BlockedPeriodInput) (domain.BlockedPeriod, error) {
	if err := s.authorize(ctx, t, p, barber, true); err != nil {
		return domain.BlockedPeriod{}, err
	}
	if !in.StartAt.Before(in.EndAt) {
		return domain.BlockedPeriod{}, ErrInvalid
	}
	v, err := s.repo.SaveBlock(ctx, t.TenantID, barber, in)
	if err != nil {
		return v, ErrConflict
	}
	return v, nil
}
func (s Service) DeleteBlock(ctx context.Context, t tenantctx.Context, p Principal, barber, id uuid.UUID) error {
	if err := s.authorize(ctx, t, p, barber, true); err != nil {
		return err
	}
	return mapRepo(s.repo.DeleteBlock(ctx, t.TenantID, barber, id))
}

type AvailabilityQuery struct {
	Tenant              tenantctx.Context
	Principal           *Principal
	BarberID, ServiceID uuid.UUID
	Date                time.Time
}

func (s Service) Availability(ctx context.Context, q AvailabilityQuery) ([]domain.Slot, error) {
	if q.Principal != nil && q.Principal.Role != "OWNER" && q.Principal.Role != "MANAGER" && q.Principal.Role != "BARBER" && q.Principal.Role != "RECEPTIONIST" {
		return nil, ErrForbidden
	}
	if q.BarberID == uuid.Nil || q.ServiceID == uuid.Nil || q.Date.IsZero() {
		return nil, ErrInvalid
	}
	if _, err := s.deps.Barber(ctx, q.Tenant, q.BarberID); err != nil {
		return nil, ErrNotFound
	}
	svc, err := s.deps.Service(ctx, q.Tenant, q.BarberID, q.ServiceID)
	if err != nil {
		return nil, ErrNotFound
	}
	settings, err := s.deps.Settings(ctx, q.Tenant)
	if err != nil {
		return nil, ErrUnavailable
	}
	zone, err := time.LoadLocation(settings.Timezone)
	if err != nil {
		return nil, err
	}
	localDate := time.Date(q.Date.Year(), q.Date.Month(), q.Date.Day(), 0, 0, 0, 0, zone)
	hours, err := s.repo.Hours(ctx, q.Tenant.TenantID, q.BarberID)
	if err != nil {
		return nil, err
	}
	override, blocked, err := s.repo.Day(ctx, q.Tenant.TenantID, q.BarberID, localDate)
	if err != nil {
		return nil, err
	}
	windows := service.IntervalsForDate(hours, override, localDate, zone)
	occupied, err := s.occupancy.Occupied(ctx, q.Tenant, q.BarberID, localDate.Add(-24*time.Hour), localDate.AddDate(0, 0, 1).Add(24*time.Hour))
	if errors.Is(err, service.ErrOccupancyUnavailable) {
		return nil, ErrOccupancyUnavailable
	}
	if err != nil {
		return nil, ErrUnavailable
	}
	step := settings.BookingIntervalMinutes
	if step < 1 {
		step = s.fallbackInterval
	}
	return service.Available(windows, blocked, occupied, svc.DurationMinutes, svc.BufferBeforeMinutes, svc.BufferAfterMinutes, step, s.now().UTC()), nil
}
