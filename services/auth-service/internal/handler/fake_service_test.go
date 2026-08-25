package handler

import (
	"context"

	"github.com/barber-appointment/auth-service/internal/domain"
	"github.com/google/uuid"
)

// fakeService is a minimal authService double used to prove that requests
// blocked before reaching the handler methods never touch the underlying
// service/repository, and that requests correctly allowed do reach it.
type fakeService struct {
	principal domain.Principal
	err       error

	membersCalled          bool
	memberCalled           bool
	registerCalled         bool
	changeMemberRoleCalled bool
	setMemberStatusCalled  bool
	changePasswordCalled   bool
	logoutCalled           bool
}

func (f *fakeService) Login(context.Context, uuid.UUID, domain.LoginInput) (domain.Session, error) {
	return domain.Session{}, nil
}
func (f *fakeService) Register(context.Context, uuid.UUID, domain.Principal, domain.RegisterInput) (domain.Registration, error) {
	f.registerCalled = true
	return domain.Registration{MembershipCreated: true}, nil
}
func (f *fakeService) Members(context.Context, uuid.UUID) ([]domain.Member, error) {
	f.membersCalled = true
	return []domain.Member{}, nil
}
func (f *fakeService) Owners(context.Context, uuid.UUID) ([]domain.OwnerState, error) {
	return []domain.OwnerState{}, nil
}
func (f *fakeService) Member(context.Context, uuid.UUID, uuid.UUID) (domain.Member, error) {
	f.memberCalled = true
	return domain.Member{}, nil
}
func (f *fakeService) ChangeMemberRole(context.Context, uuid.UUID, domain.Principal, uuid.UUID, domain.Role) (domain.Member, error) {
	f.changeMemberRoleCalled = true
	return domain.Member{}, nil
}
func (f *fakeService) SetMemberStatus(context.Context, uuid.UUID, domain.Principal, uuid.UUID, bool) (domain.Member, error) {
	f.setMemberStatusCalled = true
	return domain.Member{}, nil
}
func (f *fakeService) BarberEligible(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	return true, nil
}
func (f *fakeService) ChangePassword(context.Context, uuid.UUID, domain.Principal, domain.ChangePasswordInput) error {
	f.changePasswordCalled = true
	return nil
}
func (f *fakeService) ForgotPassword(context.Context, uuid.UUID, domain.ForgotPasswordInput) error {
	return nil
}
func (f *fakeService) Current(context.Context, uuid.UUID, string) (domain.Principal, error) {
	return f.principal, f.err
}
func (f *fakeService) Logout(context.Context, uuid.UUID, string) error {
	f.logoutCalled = true
	return nil
}

// trustedVerifier accepts any internal-auth token, letting requests reach
// past trustedContext so tests can exercise Authenticate and the routes
// behind it without a real internal-auth secret.
type trustedVerifier struct{}

func (trustedVerifier) Verify(string) error { return nil }
