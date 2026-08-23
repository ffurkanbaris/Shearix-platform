package platformauth

import "testing"

func TestTokenVerifier(t *testing.T) {
	verifier := NewTokenVerifier("platform-secret")
	if err := verifier.Verify("platform-secret"); err != nil {
		t.Fatalf("valid token: %v", err)
	}
	if err := verifier.Verify("wrong"); err == nil {
		t.Fatal("wrong token was accepted")
	}
	if err := NewTokenVerifier("").Verify("anything"); err == nil {
		t.Fatal("empty expected token was accepted")
	}
}
