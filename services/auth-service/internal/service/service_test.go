package service

import (
	"golang.org/x/crypto/bcrypt"
	"strings"
	"testing"

	"github.com/barber-appointment/auth-service/internal/domain"
)

func TestTokenDigestRejectsMalformedCookieValue(t *testing.T) {
	if digest := tokenDigest("not a base64url token"); digest != nil {
		t.Fatal("malformed session token should not produce a digest")
	}
}

func TestGeneratedPasswordHasEntropyAndAuthenticatesAgainstStoredHash(t *testing.T) {
	first, err := newPassword()
	if err != nil {
		t.Fatal(err)
	}
	second, err := newPassword()
	if err != nil {
		t.Fatal(err)
	}
	if first == second || len(first) < 20 {
		t.Fatalf("password generation is not sufficiently random")
	}
	hash, err := hashPassword(first)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(hash, first) {
		t.Fatal("hash contains plaintext password")
	}
	if err = bcrypt.CompareHashAndPassword([]byte(hash), []byte(first)); err != nil {
		t.Fatalf("generated password does not authenticate: %v", err)
	}
}

func TestRegistrationValidationPreservesUnicodeAndRejectsBlankNames(t *testing.T) {
	name, email, role, err := validateRegistration(domain.RegisterInput{Name: "  Furkan Barış  ", Email: "FURKAN@example.com", Role: domain.RoleBarber})
	if err != nil || name != "Furkan Barış" || email != "furkan@example.com" || role != domain.RoleBarber {
		t.Fatalf("validated name=%q email=%q role=%q err=%v", name, email, role, err)
	}
	for _, value := range []string{"", " \t\n ", string(make([]rune, 201))} {
		if _, _, _, err := validateRegistration(domain.RegisterInput{Name: value, Email: "valid@example.com", Role: domain.RoleReceptionist}); err != ErrInvalidInput {
			t.Fatalf("name %q error=%v", value, err)
		}
	}
	for _, role := range []domain.Role{"", domain.RoleOwner} {
		if _, _, _, err := validateRegistration(domain.RegisterInput{Name: "Valid", Email: "valid@example.com", Role: role}); err != ErrInvalidInput {
			t.Fatalf("role %q error=%v", role, err)
		}
	}
	for _, value := range []string{"", "not-email", "a@"} {
		if _, _, _, err := validateRegistration(domain.RegisterInput{Name: "Valid", Email: value, Role: domain.RoleBarber}); err != ErrInvalidInput {
			t.Fatalf("email %q error=%v", value, err)
		}
	}
	if err := validateNewPassword("short"); err != ErrInvalidInput {
		t.Fatalf("short password error=%v", err)
	}
}
