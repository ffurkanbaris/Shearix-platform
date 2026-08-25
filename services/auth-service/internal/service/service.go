package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/barber-appointment/auth-service/internal/domain"
	"github.com/barber-appointment/auth-service/internal/repository"
	platformemail "github.com/barber-appointment/platform/email"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrUnauthorized        = errors.New("unauthorized")
	ErrInvalidInput        = errors.New("invalid input")
	ErrForbidden           = errors.New("forbidden")
	ErrDeliveryUnavailable = errors.New("credential delivery unavailable")
	ErrDeliveryInProgress  = errors.New("credential delivery already in progress")
)

type Service struct {
	repository repository.Repository
	sessionTTL time.Duration
	email      platformemail.Sender
}

func New(repository repository.Repository, sessionTTL time.Duration, senders ...platformemail.Sender) Service {
	service := Service{repository: repository, sessionTTL: sessionTTL}
	if len(senders) > 0 {
		service.email = senders[0]
	}
	return service
}

func (s Service) Login(ctx context.Context, tenantID uuid.UUID, input domain.LoginInput) (domain.Session, error) {
	email, err := normalizeEmail(input.Email)
	if err != nil || input.Password == "" {
		return domain.Session{}, ErrUnauthorized
	}
	identity, err := s.repository.CredentialByEmail(ctx, email)
	if err != nil || !identity.Active || bcrypt.CompareHashAndPassword([]byte(identity.PasswordHash), []byte(input.Password)) != nil {
		return domain.Session{}, ErrUnauthorized
	}
	token, tokenHash, err := newToken()
	if err != nil {
		return domain.Session{}, err
	}
	expiresAt := time.Now().UTC().Add(s.sessionTTL)
	principal, err := s.repository.CreateSession(ctx, tenantID, identity.IdentityID, tokenHash, expiresAt)
	if errors.Is(err, repository.ErrNotFound) {
		return domain.Session{}, ErrUnauthorized
	}
	if err != nil {
		return domain.Session{}, err
	}
	return domain.Session{Principal: principal, Token: token, ExpiresAt: expiresAt}, nil
}

// Register creates an ordinary tenant staff membership. OWNER creation is
// deliberately outside this application flow and remains platform-controlled.
func (s Service) Register(ctx context.Context, tenantID uuid.UUID, actor domain.Principal, input domain.RegisterInput) (domain.Registration, error) {
	if actor.TenantID != tenantID || actor.Role != domain.RoleOwner {
		return domain.Registration{}, ErrForbidden
	}
	name, email, role, err := validateRegistration(input)
	if err != nil {
		return domain.Registration{}, err
	}
	if s.email == nil {
		return domain.Registration{}, ErrDeliveryUnavailable
	}

	// Existing global identities retain their own name and password. They need
	// only a membership in this tenant and no new password delivery.
	existing, findErr := s.repository.CredentialByEmail(ctx, email)
	if findErr == nil {
		result, err := s.repository.Register(ctx, tenantID, name, email, input.Phone, role, "")
		if err != nil {
			return domain.Registration{}, err
		}
		if existing.DeliveryStatus == "sent" {
			return domain.Registration{IdentityID: result.IdentityID, MembershipCreated: result.CreatedMembership, CredentialScheduled: false, Role: result.Role}, nil
		}
		claim, claimed, claimErr := s.repository.ClaimInitialDelivery(ctx, result.IdentityID)
		if claimErr != nil {
			return domain.Registration{}, claimErr
		}
		if !claimed {
			return domain.Registration{}, ErrDeliveryInProgress
		}
		password, err := newPassword()
		if err != nil {
			return domain.Registration{}, err
		}
		passwordHash, err := hashPassword(password)
		if err != nil {
			return domain.Registration{}, err
		}
		if _, err = platformemail.SendWithRetry(ctx, s.email, initialPasswordMessage(email, password, result.IdentityID), 3); err != nil {
			_ = s.repository.MarkInitialDelivery(ctx, result.IdentityID, claim, "failed", "email provider unavailable")
			return domain.Registration{}, ErrDeliveryUnavailable
		}
		if err = s.repository.ActivateRetriedCredential(ctx, result.IdentityID, claim, passwordHash); err != nil {
			return domain.Registration{}, err
		}
		return domain.Registration{IdentityID: result.IdentityID, MembershipCreated: result.CreatedMembership, CredentialScheduled: true, Role: result.Role}, nil
	}
	if !errors.Is(findErr, repository.ErrNotFound) {
		return domain.Registration{}, findErr
	}

	password, err := newPassword()
	if err != nil {
		return domain.Registration{}, err
	}
	passwordHash, err := hashPassword(password)
	if err != nil {
		return domain.Registration{}, err
	}
	result, err := s.repository.Register(ctx, tenantID, name, email, input.Phone, role, passwordHash)
	if err != nil {
		return domain.Registration{}, err
	}
	if !result.CreatedIdentity {
		return domain.Registration{IdentityID: result.IdentityID, MembershipCreated: result.CreatedMembership, CredentialScheduled: false, Role: result.Role}, nil
	}
	claim, claimed, err := s.repository.ClaimInitialDelivery(ctx, result.IdentityID)
	if err != nil {
		return domain.Registration{}, err
	}
	if !claimed {
		return domain.Registration{}, ErrDeliveryInProgress
	}
	if _, err = platformemail.SendWithRetry(ctx, s.email, initialPasswordMessage(email, password, result.IdentityID), 3); err != nil {
		_ = s.repository.MarkInitialDelivery(ctx, result.IdentityID, claim, "failed", "email provider unavailable")
		return domain.Registration{}, ErrDeliveryUnavailable
	}
	if err = s.repository.MarkInitialDelivery(ctx, result.IdentityID, claim, "sent", ""); err != nil {
		return domain.Registration{}, err
	}
	return domain.Registration{IdentityID: result.IdentityID, MembershipCreated: result.CreatedMembership, CredentialScheduled: true, Role: result.Role}, nil
}

func (s Service) Members(ctx context.Context, tenantID uuid.UUID) ([]domain.Member, error) {
	return s.repository.Members(ctx, tenantID)
}

func (s Service) Owners(ctx context.Context, tenantID uuid.UUID) ([]domain.OwnerState, error) {
	return s.repository.Owners(ctx, tenantID)
}

func (s Service) Member(ctx context.Context, tenantID, identityID uuid.UUID) (domain.Member, error) {
	return s.repository.Member(ctx, tenantID, identityID)
}

// ChangeMemberRole accepts only staff roles and never lets the normal member
// API modify an OWNER membership. The handler separately requires OWNER, so
// MANAGER permissions are explicit: they can manage business resources but not
// tenant identity/role administration.
func (s Service) ChangeMemberRole(ctx context.Context, tenantID uuid.UUID, actor domain.Principal, targetID uuid.UUID, role domain.Role) (domain.Member, error) {
	if actor.TenantID != tenantID || actor.Role != domain.RoleOwner || actor.IdentityID == targetID || !staffRole(role) {
		return domain.Member{}, ErrForbidden
	}
	target, err := s.repository.Member(ctx, tenantID, targetID)
	if err != nil {
		return domain.Member{}, err
	}
	if target.Role == domain.RoleOwner {
		return domain.Member{}, ErrForbidden
	}
	return s.repository.ChangeMemberRole(ctx, tenantID, targetID, role)
}

func (s Service) SetMemberStatus(ctx context.Context, tenantID uuid.UUID, actor domain.Principal, targetID uuid.UUID, active bool) (domain.Member, error) {
	if actor.TenantID != tenantID || actor.Role != domain.RoleOwner || actor.IdentityID == targetID {
		return domain.Member{}, ErrForbidden
	}
	target, err := s.repository.Member(ctx, tenantID, targetID)
	if err != nil {
		return domain.Member{}, err
	}
	// OWNER provisioning and recovery are privileged operations. Blocking this
	// path also makes accidental last-owner deactivation impossible.
	if target.Role == domain.RoleOwner {
		return domain.Member{}, ErrForbidden
	}
	status := "inactive"
	if active {
		status = "active"
	}
	return s.repository.SetMemberStatus(ctx, tenantID, targetID, status)
}

func (s Service) BarberEligible(ctx context.Context, tenantID, identityID uuid.UUID) (bool, error) {
	return s.repository.BarberEligible(ctx, tenantID, identityID)
}

func (s Service) ChangePassword(ctx context.Context, tenantID uuid.UUID, principal domain.Principal, input domain.ChangePasswordInput) error {
	if err := validateNewPassword(input.NewPassword); err != nil || input.CurrentPassword == "" {
		return ErrInvalidInput
	}
	identity, err := s.repository.CredentialByEmail(ctx, principal.Email)
	if err != nil || identity.IdentityID != principal.IdentityID || !identity.Active || bcrypt.CompareHashAndPassword([]byte(identity.PasswordHash), []byte(input.CurrentPassword)) != nil {
		return ErrUnauthorized
	}
	hash, err := hashPassword(input.NewPassword)
	if err != nil {
		return err
	}
	if err = s.repository.ChangePassword(ctx, tenantID, principal.IdentityID, principal.SessionID, hash); errors.Is(err, repository.ErrNotFound) {
		return ErrUnauthorized
	}
	return err
}

// ForgotPassword intentionally returns no existence signal. A valid active
// membership gets a fresh password/outbox entry; unknown identities are a
// successful no-op to callers.
func (s Service) ForgotPassword(ctx context.Context, tenantID uuid.UUID, input domain.ForgotPasswordInput) error {
	email, err := normalizeEmail(input.Email)
	if err != nil || s.email == nil {
		return nil
	}
	identity, err := s.repository.CredentialByEmail(ctx, email)
	if errors.Is(err, repository.ErrNotFound) || !identity.Active {
		return nil
	}
	if err != nil {
		return err
	}
	active, err := s.repository.HasActiveMembership(ctx, tenantID, identity.IdentityID)
	if err != nil || !active {
		return err
	}
	password, err := newPassword()
	if err != nil {
		return err
	}
	hash, err := hashPassword(password)
	if err != nil {
		return err
	}
	if _, err = platformemail.SendWithRetry(ctx, s.email, platformemail.Message{To: email, Subject: "Password reset", Text: fmt.Sprintf("Your temporary password is: %s\nYou must change it after signing in.", password), Template: "user_password_reset", IdempotencyKey: uuid.NewString()}, 3); err != nil {
		return ErrDeliveryUnavailable
	}
	reset, err := s.repository.ResetPassword(ctx, tenantID, identity.IdentityID, hash)
	if err != nil || !reset {
		return err
	}
	return nil
}

func initialPasswordMessage(email, password string, identityID uuid.UUID) platformemail.Message {
	return platformemail.Message{To: email, Subject: "Your initial password", Text: fmt.Sprintf("Your initial password is: %s\nYou must change it after signing in.", password), Template: "user_initial_password", IdempotencyKey: identityID.String()}
}

func (s Service) Current(ctx context.Context, tenantID uuid.UUID, token string) (domain.Principal, error) {
	if token == "" {
		return domain.Principal{}, ErrUnauthorized
	}
	principal, err := s.repository.Session(ctx, tenantID, tokenDigest(token))
	if errors.Is(err, repository.ErrNotFound) {
		return domain.Principal{}, ErrUnauthorized
	}
	return principal, err
}

func (s Service) Logout(ctx context.Context, tenantID uuid.UUID, token string) error {
	if token == "" {
		return nil
	}
	return s.repository.RevokeSession(ctx, tenantID, tokenDigest(token))
}

func validateRegistration(input domain.RegisterInput) (string, string, domain.Role, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" || len([]rune(name)) > 200 {
		return "", "", "", ErrInvalidInput
	}
	email, err := normalizeEmail(input.Email)
	if err != nil {
		return "", "", "", ErrInvalidInput
	}
	if !staffRole(input.Role) {
		return "", "", "", ErrInvalidInput
	}
	return name, email, input.Role, nil
}

func normalizeEmail(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	parsed, err := mail.ParseAddress(value)
	if err != nil || parsed.Address != value || len(value) > 320 {
		return "", ErrInvalidInput
	}
	return value, nil
}

func staffRole(role domain.Role) bool {
	return role == domain.RoleManager || role == domain.RoleBarber || role == domain.RoleReceptionist
}

func validateNewPassword(password string) error {
	if len(password) < 10 || len(password) > 256 {
		return ErrInvalidInput
	}
	return nil
}

func newPassword() (string, error) {
	bytes := make([]byte, 18)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

func hashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(hash), err
}

func newToken() (string, []byte, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", nil, err
	}
	return base64.RawURLEncoding.EncodeToString(bytes), digest(bytes), nil
}

func tokenDigest(token string) []byte {
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return nil
	}
	return digest(decoded)
}

func digest(value []byte) []byte { hash := sha256.Sum256(value); return hash[:] }
