package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/mail"
	"regexp"
	"strings"
	"time"

	"github.com/barber-appointment/appointment-service/internal/domain"
	"github.com/barber-appointment/appointment-service/internal/repository"
	"github.com/barber-appointment/platform/schedulingdeps"
	"github.com/barber-appointment/platform/tenantctx"
	"github.com/google/uuid"
)

var (
	ErrInvalid               = errors.New("invalid input")
	ErrInvalidIdempotencyKey = errors.New("invalid idempotency key")
	ErrUnauthorized          = errors.New("unauthorized")
	ErrForbidden             = errors.New("forbidden")
	ErrNotFound              = errors.New("not found")
	ErrConflict              = errors.New("conflict")
	ErrIdempotencyConflict   = errors.New("idempotency conflict")
	ErrUnavailable           = errors.New("dependency unavailable")
)

type Principal struct {
	Kind       string
	IdentityID uuid.UUID
	CustomerID *uuid.UUID
	Role       string
}

type AppointmentRepository interface {
	Replay(context.Context, uuid.UUID, string, string, string, string) (domain.Appointment, bool, error)
	Create(context.Context, uuid.UUID, domain.CreateInput, time.Time, time.Time, time.Time, string, string, string, string) (domain.Appointment, error)
	Get(context.Context, uuid.UUID, uuid.UUID) (domain.Appointment, error)
	List(context.Context, uuid.UUID) ([]domain.Appointment, error)
	ListForCustomer(context.Context, uuid.UUID, uuid.UUID, bool) ([]domain.Appointment, error)
	GetForCustomer(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (domain.Appointment, error)
	Transition(context.Context, uuid.UUID, uuid.UUID, string) (domain.Appointment, error)
	Reschedule(context.Context, uuid.UUID, uuid.UUID, time.Time, time.Time, time.Time, time.Time) (domain.Appointment, error)
	Occupancy(context.Context, uuid.UUID, uuid.UUID, time.Time, time.Time) ([]domain.Appointment, error)
}

type Dependencies interface {
	BranchBarber(context.Context, tenantctx.Context, uuid.UUID, uuid.UUID) error
	Barber(context.Context, tenantctx.Context, uuid.UUID) error
	Service(context.Context, tenantctx.Context, uuid.UUID, uuid.UUID) (schedulingdeps.Service, error)
	Settings(context.Context, tenantctx.Context) (schedulingdeps.TenantSettings, error)
	Available(context.Context, tenantctx.Context, domain.CreateInput, time.Time) error
	Customer(context.Context, tenantctx.Context, uuid.UUID) (CustomerSnapshot, error)
}

type CustomerSnapshot struct{ Name, Email string }

type CreateCommand struct {
	Tenant         tenantctx.Context
	Principal      Principal
	Operation      string
	IdempotencyKey string
	Input          domain.CreateInput
}

type BookingService struct {
	repo AppointmentRepository
	deps Dependencies
}

func NewBooking(repo AppointmentRepository, deps Dependencies) BookingService {
	return BookingService{repo: repo, deps: deps}
}

var keyPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]+$`)

func ValidIdempotencyKey(key string) bool {
	return len(key) >= 1 && len(key) <= 128 && keyPattern.MatchString(key)
}

func canManage(role string) bool {
	return role == "OWNER" || role == "MANAGER" || role == "RECEPTIONIST"
}

func canRead(role string) bool {
	return canManage(role) || role == "BARBER"
}

func principalScope(p Principal, email string) (string, error) {
	switch p.Kind {
	case "customer":
		if p.CustomerID == nil || *p.CustomerID == uuid.Nil {
			return "", ErrUnauthorized
		}
		return "customer:" + p.CustomerID.String(), nil
	case "admin":
		if p.IdentityID == uuid.Nil || !canManage(p.Role) {
			return "", ErrForbidden
		}
		return "admin:" + p.IdentityID.String(), nil
	case "guest":
		sum := sha256.Sum256([]byte(email))
		return "guest:" + hex.EncodeToString(sum[:]), nil
	default:
		return "", ErrUnauthorized
	}
}

func normalizeCreate(in domain.CreateInput) (domain.CreateInput, error) {
	in.CustomerName = strings.TrimSpace(in.CustomerName)
	in.CustomerContact = strings.ToLower(strings.TrimSpace(in.CustomerContact))
	parsed, err := mail.ParseAddress(in.CustomerContact)
	if err != nil || parsed.Address != in.CustomerContact || len(in.CustomerContact) > 320 || in.CustomerName == "" ||
		in.BranchID == uuid.Nil || in.BarberID == uuid.Nil || in.ServiceID == uuid.Nil || in.StartAt.IsZero() {
		return in, ErrInvalid
	}
	return in, nil
}

func fingerprint(t uuid.UUID, scope, operation string, in domain.CreateInput) string {
	canonical, _ := json.Marshal(struct {
		TenantID, PrincipalScope, Operation, BranchID, BarberID, ServiceID, CustomerName, CustomerEmail, StartAt string
	}{t.String(), scope, operation, in.BranchID.String(), in.BarberID.String(), in.ServiceID.String(), strings.TrimSpace(in.CustomerName), in.CustomerContact, in.StartAt.UTC().Format(time.RFC3339Nano)})
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:])
}

func (s BookingService) Create(ctx context.Context, cmd CreateCommand) (domain.Appointment, error) {
	if !ValidIdempotencyKey(cmd.IdempotencyKey) {
		return domain.Appointment{}, ErrInvalidIdempotencyKey
	}
	in, err := normalizeCreate(cmd.Input)
	if err != nil {
		return domain.Appointment{}, err
	}
	if cmd.Principal.CustomerID != nil {
		snapshot, snapshotErr := s.deps.Customer(ctx, cmd.Tenant, *cmd.Principal.CustomerID)
		if snapshotErr != nil {
			return domain.Appointment{}, ErrUnauthorized
		}
		in.CustomerID = cmd.Principal.CustomerID
		in.CustomerName = snapshot.Name
		in.CustomerContact = strings.ToLower(strings.TrimSpace(snapshot.Email))
		if in, err = normalizeCreate(in); err != nil {
			return domain.Appointment{}, ErrUnauthorized
		}
	}
	scope, err := principalScope(cmd.Principal, in.CustomerContact)
	if err != nil {
		return domain.Appointment{}, err
	}
	fp := fingerprint(cmd.Tenant.TenantID, scope, cmd.Operation, in)
	if replay, found, replayErr := s.repo.Replay(ctx, cmd.Tenant.TenantID, scope, cmd.Operation, cmd.IdempotencyKey, fp); replayErr != nil {
		return domain.Appointment{}, mapRepositoryError(replayErr)
	} else if found {
		return replay, nil
	}
	if err = s.deps.BranchBarber(ctx, cmd.Tenant, in.BranchID, in.BarberID); err != nil {
		return domain.Appointment{}, ErrConflict
	}
	if err = s.deps.Barber(ctx, cmd.Tenant, in.BarberID); err != nil {
		return domain.Appointment{}, ErrConflict
	}
	svc, err := s.deps.Service(ctx, cmd.Tenant, in.BarberID, in.ServiceID)
	if err != nil {
		return domain.Appointment{}, ErrConflict
	}
	end := in.StartAt.Add(time.Duration(svc.DurationMinutes) * time.Minute)
	occupiedStart := in.StartAt.Add(-time.Duration(svc.BufferBeforeMinutes) * time.Minute)
	occupiedEnd := end.Add(time.Duration(svc.BufferAfterMinutes) * time.Minute)
	if err = s.deps.Available(ctx, cmd.Tenant, in, in.StartAt); err != nil {
		return domain.Appointment{}, ErrConflict
	}
	a, err := s.repo.Create(ctx, cmd.Tenant.TenantID, in, end, occupiedStart, occupiedEnd, scope, cmd.Operation, cmd.IdempotencyKey, fp)
	return a, mapRepositoryError(err)
}

type LifecycleService struct {
	repo AppointmentRepository
	deps Dependencies
}

func NewLifecycle(repo AppointmentRepository, deps Dependencies) LifecycleService {
	return LifecycleService{repo: repo, deps: deps}
}

func (s LifecycleService) Transition(ctx context.Context, tenant uuid.UUID, principal Principal, id uuid.UUID, to string) (domain.Appointment, error) {
	allowed := principal.Kind == "admin" && canManage(principal.Role)
	allowed = allowed || (principal.Kind == "guest" && to == "cancelled")
	if !allowed {
		return domain.Appointment{}, ErrForbidden
	}
	if to != "cancelled" && to != "confirmed" && to != "completed" && to != "no_show" {
		return domain.Appointment{}, ErrInvalid
	}
	a, err := s.repo.Transition(ctx, tenant, id, to)
	return a, mapRepositoryError(err)
}

func (s LifecycleService) CancelCustomer(ctx context.Context, tenant tenantctx.Context, customerID, id uuid.UUID, now time.Time) (domain.Appointment, error) {
	a, err := s.repo.GetForCustomer(ctx, tenant.TenantID, customerID, id)
	if err != nil {
		return domain.Appointment{}, mapRepositoryError(err)
	}
	settings, err := s.deps.Settings(ctx, tenant)
	if err != nil {
		return domain.Appointment{}, ErrUnavailable
	}
	if settings.CancellationPolicy == "no_cancellation" || !now.Before(a.StartAt.Add(-time.Duration(settings.CancellationNoticeMinutes)*time.Minute)) {
		return domain.Appointment{}, ErrConflict
	}
	a, err = s.repo.Transition(ctx, tenant.TenantID, id, "cancelled")
	return a, mapRepositoryError(err)
}

func (s LifecycleService) Reschedule(ctx context.Context, tenant tenantctx.Context, principal Principal, id uuid.UUID, start time.Time) (domain.Appointment, error) {
	if principal.Kind != "admin" || !canManage(principal.Role) {
		return domain.Appointment{}, ErrForbidden
	}
	old, err := s.repo.Get(ctx, tenant.TenantID, id)
	if err != nil {
		return domain.Appointment{}, mapRepositoryError(err)
	}
	in := domain.CreateInput{BranchID: old.BranchID, BarberID: old.BarberID, ServiceID: old.ServiceID, StartAt: start, CustomerName: old.CustomerName, CustomerContact: old.CustomerContact}
	if err = s.deps.BranchBarber(ctx, tenant, in.BranchID, in.BarberID); err != nil {
		return domain.Appointment{}, ErrConflict
	}
	if err = s.deps.Barber(ctx, tenant, in.BarberID); err != nil {
		return domain.Appointment{}, ErrConflict
	}
	svc, err := s.deps.Service(ctx, tenant, in.BarberID, in.ServiceID)
	if err != nil || s.deps.Available(ctx, tenant, in, start) != nil {
		return domain.Appointment{}, ErrConflict
	}
	end := start.Add(time.Duration(svc.DurationMinutes) * time.Minute)
	a, err := s.repo.Reschedule(ctx, tenant.TenantID, id, start, end, start.Add(-time.Duration(svc.BufferBeforeMinutes)*time.Minute), end.Add(time.Duration(svc.BufferAfterMinutes)*time.Minute))
	return a, mapRepositoryError(err)
}

type QueryService struct {
	repo AppointmentRepository
	deps Dependencies
}

func NewQuery(repo AppointmentRepository, deps Dependencies) QueryService {
	return QueryService{repo, deps}
}

func (s QueryService) Get(ctx context.Context, tenant, id uuid.UUID) (domain.Appointment, error) {
	a, err := s.repo.Get(ctx, tenant, id)
	return a, mapRepositoryError(err)
}
func (s QueryService) GetAdmin(ctx context.Context, tenant, id uuid.UUID, p Principal) (domain.Appointment, error) {
	if p.Kind != "admin" || !canRead(p.Role) {
		return domain.Appointment{}, ErrForbidden
	}
	return s.Get(ctx, tenant, id)
}
func (s QueryService) NotificationRecipient(ctx context.Context, tenant, id uuid.UUID) (string, error) {
	a, err := s.Get(ctx, tenant, id)
	if err != nil {
		return "", err
	}
	recipient := strings.ToLower(strings.TrimSpace(a.CustomerContact))
	parsed, parseErr := mail.ParseAddress(recipient)
	if parseErr != nil || parsed.Address != recipient {
		return "", ErrInvalid
	}
	return recipient, nil
}
func (s QueryService) List(ctx context.Context, tenant uuid.UUID, p Principal) ([]domain.Appointment, error) {
	if p.Kind != "admin" || !canRead(p.Role) {
		return nil, ErrForbidden
	}
	v, err := s.repo.List(ctx, tenant)
	return v, mapRepositoryError(err)
}
func (s QueryService) CustomerGet(ctx context.Context, tenant, customer, id uuid.UUID) (domain.Appointment, error) {
	a, err := s.repo.GetForCustomer(ctx, tenant, customer, id)
	return a, mapRepositoryError(err)
}
func (s QueryService) CustomerList(ctx context.Context, tenant tenantctx.Context, customer uuid.UUID, upcoming bool, now time.Time) ([]domain.Appointment, error) {
	v, err := s.repo.ListForCustomer(ctx, tenant.TenantID, customer, upcoming)
	if err != nil {
		return nil, mapRepositoryError(err)
	}
	if upcoming {
		settings, e := s.deps.Settings(ctx, tenant)
		if e != nil {
			return nil, ErrUnavailable
		}
		for i := range v {
			v[i] = cancellationState(v[i], settings, now)
		}
	}
	return v, nil
}
func (s QueryService) Occupancy(ctx context.Context, tenant, barber uuid.UUID, from, to time.Time) ([]domain.Appointment, error) {
	if !from.Before(to) {
		return nil, ErrInvalid
	}
	v, err := s.repo.Occupancy(ctx, tenant, barber, from, to)
	return v, mapRepositoryError(err)
}
func cancellationState(a domain.Appointment, settings schedulingdeps.TenantSettings, now time.Time) domain.Appointment {
	if settings.CancellationPolicy == "no_cancellation" {
		a.CanCancel = false
		a.CancellationDeadline = nil
		return a
	}
	deadline := a.StartAt.Add(-time.Duration(settings.CancellationNoticeMinutes) * time.Minute)
	a.CancellationDeadline = &deadline
	a.CanCancel = a.Status != "cancelled" && a.Status != "completed" && a.Status != "no_show" && now.Before(deadline)
	return a
}

func mapRepositoryError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, repository.ErrNotFound):
		return ErrNotFound
	case errors.Is(err, repository.ErrIdempotencyConflict):
		return ErrIdempotencyConflict
	case errors.Is(err, repository.ErrInvalidTransition):
		return ErrConflict
	case repository.IsConflict(err):
		return ErrConflict
	default:
		return err
	}
}
