package application

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/barber-appointment/customer-service/internal/domain"
	"github.com/barber-appointment/customer-service/internal/repository"
	platformemail "github.com/barber-appointment/platform/email"
	"github.com/barber-appointment/platform/tenantctx"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalid      = errors.New("invalid input")
	ErrUnauthorized = errors.New("unauthorized")
	ErrForbidden    = errors.New("forbidden")
	ErrNotFound     = errors.New("not found")
	ErrConflict     = errors.New("conflict")
	ErrUnavailable  = errors.New("unavailable")
)

type Repository interface {
	Create(context.Context, uuid.UUID, string, string, string, string) (domain.Customer, error)
	EnsureGuest(context.Context, uuid.UUID, string, string) (domain.Customer, error)
	FindByEmail(context.Context, uuid.UUID, string) (domain.Customer, string, error)
	ClaimInitialDelivery(context.Context, uuid.UUID, uuid.UUID) (uuid.UUID, bool, error)
	MarkInitialDelivery(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, string, string) error
	ActivateRetriedCredential(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, string) error
	ByID(context.Context, uuid.UUID, uuid.UUID) (domain.Customer, error)
	UpdateName(context.Context, uuid.UUID, uuid.UUID, string) error
	UpdatePassword(context.Context, uuid.UUID, uuid.UUID, string) error
	ResetPassword(context.Context, uuid.UUID, uuid.UUID, string) error
	CreateSession(context.Context, uuid.UUID, uuid.UUID, string, time.Time) error
	Session(context.Context, uuid.UUID, string) (domain.Customer, error)
	Revoke(context.Context, uuid.UUID, string) error
	RevokeOthers(context.Context, uuid.UUID, uuid.UUID, string) error
	RevokeAll(context.Context, uuid.UUID, uuid.UUID) error
}
type EmailSender interface {
	Send(context.Context, platformemail.Message) (platformemail.Result, error)
}
type AppointmentClient interface {
	Forward(context.Context, tenantctx.Context, uuid.UUID, string, string, string) (ForwardResult, error)
}
type ForwardResult struct {
	Status int
	Body   []byte
}
type Service struct {
	repo         Repository
	email        EmailSender
	appointments AppointmentClient
	now          func() time.Time
}

func New(repo Repository, email EmailSender, appointments AppointmentClient) Service {
	return Service{repo: repo, email: email, appointments: appointments, now: time.Now}
}

func normalizeEmail(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	parsed, err := mail.ParseAddress(value)
	if err != nil || parsed.Address != value || len(value) > 320 {
		return "", ErrInvalid
	}
	return value, nil
}
func random(bytes int) (string, error) {
	b := make([]byte, bytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
func initialEmail(to, password string, id uuid.UUID) platformemail.Message {
	return platformemail.Message{To: to, Subject: "Your initial password", Text: fmt.Sprintf("Your initial password is: %s\nYou must change it after signing in.", password), Template: "user_initial_password", IdempotencyKey: id.String()}
}
func resetEmail(to, password string) platformemail.Message {
	return platformemail.Message{To: to, Subject: "Password reset", Text: fmt.Sprintf("Your temporary password is: %s\nYou must change it after signing in.", password), Template: "user_password_reset", IdempotencyKey: uuid.NewString()}
}
func (s Service) send(ctx context.Context, message platformemail.Message) error {
	_, err := platformemail.SendWithRetry(ctx, s.email, message, 3)
	return err
}
func mapRepo(err error) error {
	if errors.Is(err, repository.ErrNotFound) {
		return ErrNotFound
	}
	return err
}

type Registration struct{ Name, Email, Phone string }
type RegistrationResult struct {
	Customer domain.Customer
	Retried  bool
}

func (s Service) Register(ctx context.Context, tenant uuid.UUID, in Registration) (RegistrationResult, error) {
	in.Name = strings.TrimSpace(in.Name)
	email, err := normalizeEmail(in.Email)
	if err != nil || in.Name == "" || len(in.Name) > 200 {
		return RegistrationResult{}, ErrInvalid
	}
	password, err := random(12)
	if err != nil {
		return RegistrationResult{}, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return RegistrationResult{}, err
	}
	customer, err := s.repo.Create(ctx, tenant, in.Name, email, in.Phone, string(hash))
	retried := false
	if err != nil {
		customer, _, err = s.repo.FindByEmail(ctx, tenant, email)
		if err != nil || customer.InitialDeliveryStatus == "sent" {
			return RegistrationResult{}, ErrConflict
		}
		retried = true
	}
	claim, claimed, err := s.repo.ClaimInitialDelivery(ctx, tenant, customer.ID)
	if err != nil {
		return RegistrationResult{}, err
	}
	if !claimed {
		return RegistrationResult{}, ErrConflict
	}
	if err = s.send(ctx, initialEmail(email, password, customer.ID)); err != nil {
		_ = s.repo.MarkInitialDelivery(ctx, tenant, customer.ID, claim, "failed", "email provider unavailable")
		return RegistrationResult{}, ErrUnavailable
	}
	if retried {
		if err = s.repo.ActivateRetriedCredential(ctx, tenant, customer.ID, claim, string(hash)); err != nil {
			return RegistrationResult{}, err
		}
	} else if err = s.repo.MarkInitialDelivery(ctx, tenant, customer.ID, claim, "sent", ""); err != nil {
		return RegistrationResult{}, err
	}
	return RegistrationResult{Customer: customer, Retried: retried}, nil
}

type LoginResult struct {
	Customer domain.Customer
	Token    string
	Expires  time.Time
}

func (s Service) Login(ctx context.Context, tenant uuid.UUID, email, password string) (LoginResult, error) {
	email, err := normalizeEmail(email)
	if err != nil {
		return LoginResult{}, ErrUnauthorized
	}
	customer, hash, err := s.repo.FindByEmail(ctx, tenant, email)
	if err != nil || customer.Status != "active" || bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return LoginResult{}, ErrUnauthorized
	}
	token, err := random(32)
	if err != nil {
		return LoginResult{}, err
	}
	expires := s.now().Add(30 * 24 * time.Hour)
	if err = s.repo.CreateSession(ctx, tenant, customer.ID, token, expires); err != nil {
		return LoginResult{}, err
	}
	return LoginResult{customer, token, expires}, nil
}
func (s Service) Current(ctx context.Context, tenant uuid.UUID, token string) (domain.Customer, error) {
	if token == "" {
		return domain.Customer{}, ErrUnauthorized
	}
	c, err := s.repo.Session(ctx, tenant, token)
	if err != nil {
		return c, ErrUnauthorized
	}
	return c, nil
}
func (s Service) Logout(ctx context.Context, tenant uuid.UUID, token string) {
	_ = s.repo.Revoke(ctx, tenant, token)
}
func (s Service) Profile(ctx context.Context, tenant uuid.UUID, token string) (domain.Customer, error) {
	c, err := s.Current(ctx, tenant, token)
	if err != nil {
		return c, err
	}
	if c.MustChangePassword {
		return c, ErrForbidden
	}
	return c, nil
}
func (s Service) UpdateProfile(ctx context.Context, tenant uuid.UUID, token, name string) (domain.Customer, error) {
	c, err := s.Profile(ctx, tenant, token)
	if err != nil {
		return c, err
	}
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 200 {
		return c, ErrInvalid
	}
	if err = s.repo.UpdateName(ctx, tenant, c.ID, name); err != nil {
		return c, err
	}
	c.Name = name
	return c, nil
}
func (s Service) ChangePassword(ctx context.Context, tenant uuid.UUID, token, current, next string) error {
	c, err := s.Current(ctx, tenant, token)
	if err != nil {
		return err
	}
	if len(next) < 10 {
		return ErrInvalid
	}
	_, hash, err := s.repo.FindByEmail(ctx, tenant, c.Email)
	if err != nil || bcrypt.CompareHashAndPassword([]byte(hash), []byte(current)) != nil {
		return ErrUnauthorized
	}
	nextHash, err := bcrypt.GenerateFromPassword([]byte(next), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	if err = s.repo.UpdatePassword(ctx, tenant, c.ID, string(nextHash)); err != nil {
		return err
	}
	_ = s.repo.RevokeOthers(ctx, tenant, c.ID, token)
	return nil
}
func (s Service) ForgotPassword(ctx context.Context, tenant uuid.UUID, value string) error {
	email, err := normalizeEmail(value)
	if err != nil {
		return nil
	}
	c, _, err := s.repo.FindByEmail(ctx, tenant, email)
	if err != nil {
		return nil
	}
	password, err := random(12)
	if err != nil {
		return nil
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil
	}
	if err = s.send(ctx, resetEmail(email, password)); err != nil {
		return ErrUnavailable
	}
	if err = s.repo.ResetPassword(ctx, tenant, c.ID, string(hash)); err != nil {
		return ErrUnavailable
	}
	_ = s.repo.RevokeAll(ctx, tenant, c.ID)
	return nil
}
func (s Service) ResolveGuest(ctx context.Context, tenant uuid.UUID, name, phone string) (domain.Customer, error) {
	name = strings.TrimSpace(name)
	normalized, err := domain.NormalizePhone(phone)
	if err != nil || name == "" || len(name) > 200 {
		return domain.Customer{}, ErrInvalid
	}
	return s.repo.EnsureGuest(ctx, tenant, name, normalized)
}
func (s Service) ByID(ctx context.Context, tenant, id uuid.UUID) (domain.Customer, error) {
	c, err := s.repo.ByID(ctx, tenant, id)
	return c, mapRepo(err)
}
func (s Service) InternalSession(ctx context.Context, tenant uuid.UUID, token string) (domain.Customer, error) {
	c, err := s.Current(ctx, tenant, token)
	if err != nil {
		return c, err
	}
	if c.MustChangePassword {
		return c, ErrForbidden
	}
	return c, nil
}
func (s Service) Appointments(ctx context.Context, t tenantctx.Context, token, method, path, query string) (ForwardResult, error) {
	c, err := s.Profile(ctx, t.TenantID, token)
	if err != nil {
		return ForwardResult{}, err
	}
	result, err := s.appointments.Forward(ctx, t, c.ID, method, path, query)
	if err != nil {
		return result, ErrUnavailable
	}
	return result, nil
}
