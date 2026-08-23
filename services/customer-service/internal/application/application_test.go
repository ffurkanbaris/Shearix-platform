package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/barber-appointment/customer-service/internal/domain"
	platformemail "github.com/barber-appointment/platform/email"
	"github.com/google/uuid"
)

type fakeRepository struct {
	customer                           domain.Customer
	hash                               string
	resetCalls, markCalls, createCalls int
	claim                              bool
}

func (f *fakeRepository) Create(context.Context, uuid.UUID, string, string, string, string) (domain.Customer, error) {
	f.createCalls++
	return f.customer, nil
}
func (f *fakeRepository) EnsureGuest(context.Context, uuid.UUID, string, string) (domain.Customer, error) {
	return f.customer, nil
}
func (f *fakeRepository) FindByEmail(context.Context, uuid.UUID, string) (domain.Customer, string, error) {
	return f.customer, f.hash, nil
}
func (f *fakeRepository) ClaimInitialDelivery(context.Context, uuid.UUID, uuid.UUID) (uuid.UUID, bool, error) {
	return uuid.New(), f.claim, nil
}
func (f *fakeRepository) MarkInitialDelivery(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, string, string) error {
	f.markCalls++
	return nil
}
func (f *fakeRepository) ActivateRetriedCredential(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, string) error {
	return nil
}
func (f *fakeRepository) ByID(context.Context, uuid.UUID, uuid.UUID) (domain.Customer, error) {
	return f.customer, nil
}
func (f *fakeRepository) UpdateName(context.Context, uuid.UUID, uuid.UUID, string) error { return nil }
func (f *fakeRepository) UpdatePassword(context.Context, uuid.UUID, uuid.UUID, string) error {
	return nil
}
func (f *fakeRepository) ResetPassword(context.Context, uuid.UUID, uuid.UUID, string) error {
	f.resetCalls++
	return nil
}
func (f *fakeRepository) CreateSession(context.Context, uuid.UUID, uuid.UUID, string, time.Time) error {
	return nil
}
func (f *fakeRepository) Session(context.Context, uuid.UUID, string) (domain.Customer, error) {
	return f.customer, nil
}
func (f *fakeRepository) Revoke(context.Context, uuid.UUID, string) error { return nil }
func (f *fakeRepository) RevokeOthers(context.Context, uuid.UUID, uuid.UUID, string) error {
	return nil
}
func (f *fakeRepository) RevokeAll(context.Context, uuid.UUID, uuid.UUID) error { return nil }

type fakeSender struct {
	calls int
	err   error
}

func (f *fakeSender) Send(context.Context, platformemail.Message) (platformemail.Result, error) {
	f.calls++
	return platformemail.Result{}, f.err
}

func TestForgotPasswordDoesNotReplaceHashWhenDeliveryFails(t *testing.T) {
	repo := &fakeRepository{customer: domain.Customer{ID: uuid.New(), Email: "person@example.com", Status: "active"}, hash: "existing"}
	sender := &fakeSender{err: errors.New("provider down")}
	svc := New(repo, sender, nil)
	if err := svc.ForgotPassword(context.Background(), uuid.New(), "person@example.com"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("error=%v", err)
	}
	if repo.resetCalls != 0 {
		t.Fatal("password hash changed before provider submission")
	}
	if sender.calls != 1 {
		t.Fatalf("provider calls=%d", sender.calls)
	}
}
func TestRegisterProviderFailureMarksAccountAndDoesNotDelete(t *testing.T) {
	repo := &fakeRepository{customer: domain.Customer{ID: uuid.New(), Email: "person@example.com", Status: "active", InitialDeliveryStatus: "pending"}, claim: true}
	sender := &fakeSender{err: errors.New("provider down")}
	svc := New(repo, sender, nil)
	_, err := svc.Register(context.Background(), uuid.New(), Registration{Name: "Person", Email: "person@example.com"})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("error=%v", err)
	}
	if repo.createCalls != 1 || repo.markCalls != 1 {
		t.Fatalf("create=%d mark=%d", repo.createCalls, repo.markCalls)
	}
}
