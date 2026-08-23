package application

import (
	"context"
	"errors"
	"strings"

	"github.com/barber-appointment/barber-service/internal/domain"
	"github.com/barber-appointment/barber-service/internal/repository"
	"github.com/barber-appointment/platform/adminauth"
	"github.com/barber-appointment/platform/tenantctx"
	"github.com/google/uuid"
)

var (
	ErrInvalid       = errors.New("invalid input")
	ErrUnauthorized  = errors.New("unauthorized")
	ErrForbidden     = errors.New("forbidden")
	ErrNotFound      = errors.New("not found")
	ErrConflict      = errors.New("conflict")
	ErrUnavailable   = errors.New("unavailable")
	ErrUnprocessable = errors.New("unprocessable")
)

type Repository interface {
	Assigned(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, *bool) error
	Branches(context.Context, uuid.UUID, bool) ([]domain.Branch, error)
	Branch(context.Context, uuid.UUID, uuid.UUID) (domain.Branch, error)
	SaveBranch(context.Context, uuid.UUID, uuid.UUID, domain.BranchInput) (domain.Branch, error)
	Barbers(context.Context, uuid.UUID, bool) ([]domain.Barber, error)
	Barber(context.Context, uuid.UUID, uuid.UUID) (domain.Barber, error)
	SaveBarber(context.Context, uuid.UUID, uuid.UUID, domain.BarberInput) (domain.Barber, error)
	LinkIdentity(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) error
	UnlinkIdentity(context.Context, uuid.UUID, uuid.UUID) error
}

type MembershipClient interface {
	Authenticate(context.Context, tenantctx.Context, string) (adminauth.Principal, error)
	EnsureBarberMembership(context.Context, tenantctx.Context, uuid.UUID) error
}

type Service struct {
	repo Repository
	auth MembershipClient
}

func New(repo Repository, auth MembershipClient) Service { return Service{repo: repo, auth: auth} }

func repoError(err error) error {
	switch {
	case errors.Is(err, repository.ErrNotFound):
		return ErrNotFound
	case errors.Is(err, repository.ErrConflict):
		return ErrConflict
	default:
		return err
	}
}

func (s Service) authorize(ctx context.Context, tenant tenantctx.Context, cookie string, write bool) error {
	if tenant.AppType != "admin" {
		return ErrUnauthorized
	}
	principal, err := s.auth.Authenticate(ctx, tenant, cookie)
	if err != nil {
		if write {
			return ErrForbidden
		}
		return ErrUnauthorized
	}
	if !adminauth.CanRead(principal.Role) {
		if !write {
			return ErrUnauthorized
		}
		return ErrForbidden
	}
	if write && !adminauth.CanWrite(principal.Role) {
		return ErrForbidden
	}
	return nil
}

func (s Service) PublicBranches(ctx context.Context, tenant tenantctx.Context) ([]domain.Branch, error) {
	if tenant.AppType != "booking" {
		return nil, ErrUnauthorized
	}
	return s.repo.Branches(ctx, tenant.TenantID, true)
}
func (s Service) PublicBarbers(ctx context.Context, tenant tenantctx.Context) ([]domain.Barber, error) {
	if tenant.AppType != "booking" {
		return nil, ErrUnauthorized
	}
	return s.repo.Barbers(ctx, tenant.TenantID, true)
}
func (s Service) Branches(ctx context.Context, tenant tenantctx.Context, cookie string) ([]domain.Branch, error) {
	if err := s.authorize(ctx, tenant, cookie, false); err != nil {
		return nil, err
	}
	return s.repo.Branches(ctx, tenant.TenantID, false)
}
func (s Service) Branch(ctx context.Context, tenant tenantctx.Context, cookie string, id uuid.UUID) (domain.Branch, error) {
	if err := s.authorize(ctx, tenant, cookie, false); err != nil {
		return domain.Branch{}, err
	}
	v, err := s.repo.Branch(ctx, tenant.TenantID, id)
	return v, repoError(err)
}
func (s Service) SaveBranch(ctx context.Context, tenant tenantctx.Context, cookie string, id uuid.UUID, in domain.BranchInput) (domain.Branch, error) {
	if err := s.authorize(ctx, tenant, cookie, true); err != nil {
		return domain.Branch{}, err
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return domain.Branch{}, ErrInvalid
	}
	v, err := s.repo.SaveBranch(ctx, tenant.TenantID, id, in)
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return v, ErrConflict
	}
	return v, repoError(err)
}
func (s Service) Barbers(ctx context.Context, tenant tenantctx.Context, cookie string) ([]domain.Barber, error) {
	if err := s.authorize(ctx, tenant, cookie, false); err != nil {
		return nil, err
	}
	return s.repo.Barbers(ctx, tenant.TenantID, false)
}
func (s Service) Barber(ctx context.Context, tenant tenantctx.Context, cookie string, id uuid.UUID) (domain.Barber, error) {
	if err := s.authorize(ctx, tenant, cookie, false); err != nil {
		return domain.Barber{}, err
	}
	v, err := s.repo.Barber(ctx, tenant.TenantID, id)
	return v, repoError(err)
}
func (s Service) SaveBarber(ctx context.Context, tenant tenantctx.Context, cookie string, id uuid.UUID, in domain.BarberInput) (domain.Barber, error) {
	if err := s.authorize(ctx, tenant, cookie, true); err != nil {
		return domain.Barber{}, err
	}
	in.DisplayName = strings.TrimSpace(in.DisplayName)
	if in.DisplayName == "" {
		return domain.Barber{}, ErrInvalid
	}
	v, err := s.repo.SaveBarber(ctx, tenant.TenantID, id, in)
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return v, ErrConflict
	}
	return v, repoError(err)
}
func (s Service) BookingAccess(ctx context.Context, tenant tenantctx.Context, branchID, barberID uuid.UUID) error {
	if tenant.AppType != "admin" && tenant.AppType != "booking" {
		return ErrUnauthorized
	}
	branch, err := s.repo.Branch(ctx, tenant.TenantID, branchID)
	if err != nil || !branch.Active {
		return repoErrorOrNotFound(err)
	}
	barber, err := s.repo.Barber(ctx, tenant.TenantID, barberID)
	if err != nil || !barber.Active {
		return repoErrorOrNotFound(err)
	}
	var assigned bool
	if err = s.repo.Assigned(ctx, tenant.TenantID, branchID, barberID, &assigned); err != nil {
		return err
	}
	if !assigned {
		return ErrNotFound
	}
	return nil
}
func repoErrorOrNotFound(err error) error {
	if err == nil || errors.Is(err, repository.ErrNotFound) {
		return ErrNotFound
	}
	return err
}
func (s Service) SchedulingAccess(ctx context.Context, tenant tenantctx.Context, id uuid.UUID) (*uuid.UUID, error) {
	if tenant.AppType != "admin" && tenant.AppType != "booking" {
		return nil, ErrUnauthorized
	}
	b, err := s.repo.Barber(ctx, tenant.TenantID, id)
	if err != nil || !b.Active {
		return nil, repoErrorOrNotFound(err)
	}
	return b.IdentityID, nil
}
func (s Service) Exists(ctx context.Context, tenant tenantctx.Context, id uuid.UUID) error {
	if tenant.AppType != "admin" {
		return ErrUnauthorized
	}
	_, err := s.repo.Barber(ctx, tenant.TenantID, id)
	return repoError(err)
}
func (s Service) LinkIdentity(ctx context.Context, tenant tenantctx.Context, cookie string, barberID, identityID uuid.UUID) error {
	if identityID == uuid.Nil {
		return ErrInvalid
	}
	if err := s.authorize(ctx, tenant, cookie, true); err != nil {
		return err
	}
	if err := s.auth.EnsureBarberMembership(ctx, tenant, identityID); err != nil {
		return ErrUnprocessable
	}
	return repoError(s.repo.LinkIdentity(ctx, tenant.TenantID, barberID, identityID))
}
func (s Service) UnlinkIdentity(ctx context.Context, tenant tenantctx.Context, cookie string, barberID uuid.UUID) error {
	if err := s.authorize(ctx, tenant, cookie, true); err != nil {
		return err
	}
	return repoError(s.repo.UnlinkIdentity(ctx, tenant.TenantID, barberID))
}
