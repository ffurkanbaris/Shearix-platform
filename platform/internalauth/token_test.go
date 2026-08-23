package internalauth

import "testing"

func TestTokenVerifier(t *testing.T) {
	verifier := NewTokenVerifier("trusted-secret")
	if err := verifier.Verify("trusted-secret"); err != nil {
		t.Fatalf("expected valid token: %v", err)
	}
	if err := verifier.Verify("spoofed"); err == nil {
		t.Fatal("expected spoofed token to be rejected")
	}
	if err := NewTokenVerifier("").Verify("anything"); err == nil {
		t.Fatal("expected empty configured token to be rejected")
	}
}
