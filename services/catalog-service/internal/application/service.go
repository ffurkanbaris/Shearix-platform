package application

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/barber-appointment/catalog-service/internal/domain"
	"github.com/barber-appointment/catalog-service/internal/repository"
	"github.com/barber-appointment/platform/adminauth"
	"github.com/barber-appointment/platform/barberclient"
	"github.com/barber-appointment/platform/tenantctx"
	"github.com/google/uuid"
)

var (
	ErrInvalid      = errors.New("invalid input")
	ErrUnauthorized = errors.New("unauthorized")
	ErrForbidden    = errors.New("forbidden")
	ErrNotFound     = errors.New("not found")
	ErrConflict     = errors.New("conflict")
	ErrUnavailable  = errors.New("unavailable")
	amount          = regexp.MustCompile(`^[0-9]+(\.[0-9]{1,2})?$`)
	currency        = regexp.MustCompile(`^[A-Z]{3}$`)
)

type Repository interface {
	Services(context.Context, uuid.UUID, bool) ([]domain.Service, error)
	PublicBarberServices(context.Context, uuid.UUID) ([]domain.PublicBarberServices, error)
	Service(context.Context, uuid.UUID, uuid.UUID) (domain.Service, error)
	Save(context.Context, uuid.UUID, uuid.UUID, domain.ServiceInput) (domain.Service, error)
	Assign(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) error
	Assignments(context.Context, uuid.UUID, uuid.UUID) ([]domain.Service, error)
	Unassign(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) error
	ActiveAssignedService(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (domain.Service, error)
}
type AuthClient interface {
	Authenticate(context.Context, tenantctx.Context, string) (adminauth.Principal, error)
}
type BarberClient interface {
	EnsureExists(context.Context, tenantctx.Context, uuid.UUID) error
}
type Service struct {
	repo   Repository
	auth   AuthClient
	barber BarberClient
}

func New(r Repository, a AuthClient, b BarberClient) Service { return Service{r, a, b} }
func mapRepo(err error) error {
	if errors.Is(err, repository.ErrNotFound) {
		return ErrNotFound
	}
	return err
}
func (s Service) authorize(ctx context.Context, t tenantctx.Context, cookie string, write bool) error {
	if t.AppType != "admin" {
		return ErrUnauthorized
	}
	p, e := s.auth.Authenticate(ctx, t, cookie)
	if e != nil {
		if write {
			return ErrForbidden
		}
		return ErrUnauthorized
	}
	if !adminauth.CanRead(p.Role) {
		if !write {
			return ErrUnauthorized
		}
		return ErrForbidden
	}
	if write && !adminauth.CanWrite(p.Role) {
		return ErrForbidden
	}
	return nil
}
func valid(in domain.ServiceInput) bool {
	return strings.TrimSpace(in.Name) != "" && in.DurationMinutes >= 0 && in.BufferBeforeMinutes >= 0 && in.BufferAfterMinutes >= 0 && amount.MatchString(in.Price) && currency.MatchString(in.Currency)
}
func (s Service) Public(ctx context.Context, t tenantctx.Context) ([]domain.Service, error) {
	if t.AppType != "booking" {
		return nil, ErrUnauthorized
	}
	return s.repo.Services(ctx, t.TenantID, true)
}
func (s Service) PublicBarberServices(ctx context.Context, t tenantctx.Context) ([]domain.PublicBarberServices, error) {
	if t.AppType != "booking" {
		return nil, ErrUnauthorized
	}
	return s.repo.PublicBarberServices(ctx, t.TenantID)
}
func (s Service) Available(ctx context.Context, t tenantctx.Context, b, id uuid.UUID) (domain.Service, error) {
	if t.AppType != "booking" && t.AppType != "admin" {
		return domain.Service{}, ErrUnauthorized
	}
	v, e := s.repo.ActiveAssignedService(ctx, t.TenantID, b, id)
	return v, mapRepo(e)
}
func (s Service) List(ctx context.Context, t tenantctx.Context, cookie string) ([]domain.Service, error) {
	if e := s.authorize(ctx, t, cookie, false); e != nil {
		return nil, e
	}
	return s.repo.Services(ctx, t.TenantID, false)
}
func (s Service) Get(ctx context.Context, t tenantctx.Context, cookie string, id uuid.UUID) (domain.Service, error) {
	if e := s.authorize(ctx, t, cookie, false); e != nil {
		return domain.Service{}, e
	}
	v, e := s.repo.Service(ctx, t.TenantID, id)
	return v, mapRepo(e)
}
func (s Service) Save(ctx context.Context, t tenantctx.Context, cookie string, id uuid.UUID, in domain.ServiceInput) (domain.Service, error) {
	if e := s.authorize(ctx, t, cookie, true); e != nil {
		return domain.Service{}, e
	}
	in.Name = strings.TrimSpace(in.Name)
	if !valid(in) {
		return domain.Service{}, ErrInvalid
	}
	v, e := s.repo.Save(ctx, t.TenantID, id, in)
	if e != nil && !errors.Is(e, repository.ErrNotFound) {
		return v, ErrConflict
	}
	return v, mapRepo(e)
}
func (s Service) ensureBarber(ctx context.Context, t tenantctx.Context, id uuid.UUID) error {
	e := s.barber.EnsureExists(ctx, t, id)
	if errors.Is(e, barberclient.ErrNotFound) {
		return ErrNotFound
	}
	if e != nil {
		return ErrUnavailable
	}
	return nil
}
func (s Service) Assign(ctx context.Context, t tenantctx.Context, cookie string, b, service uuid.UUID) error {
	if e := s.authorize(ctx, t, cookie, true); e != nil {
		return e
	}
	if service == uuid.Nil {
		return ErrInvalid
	}
	if e := s.ensureBarber(ctx, t, b); e != nil {
		return e
	}
	e := s.repo.Assign(ctx, t.TenantID, b, service)
	if errors.Is(e, repository.ErrNotFound) {
		return ErrNotFound
	}
	if e != nil {
		return ErrConflict
	}
	return nil
}
func (s Service) Assignments(ctx context.Context, t tenantctx.Context, cookie string, b uuid.UUID) ([]domain.Service, error) {
	if e := s.authorize(ctx, t, cookie, false); e != nil {
		return nil, e
	}
	if e := s.ensureBarber(ctx, t, b); e != nil {
		return nil, e
	}
	return s.repo.Assignments(ctx, t.TenantID, b)
}
func (s Service) Unassign(ctx context.Context, t tenantctx.Context, cookie string, b, service uuid.UUID) error {
	if e := s.authorize(ctx, t, cookie, true); e != nil {
		return e
	}
	if e := s.ensureBarber(ctx, t, b); e != nil {
		return e
	}
	return mapRepo(s.repo.Unassign(ctx, t.TenantID, b, service))
}
