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

func TestMultiTokenVerifier(t *testing.T) {
	verifier := NewMultiTokenVerifier("gateway-secret", "service-secret")
	if err := verifier.Verify("gateway-secret"); err != nil {
		t.Fatalf("expected the first configured token to be accepted: %v", err)
	}
	if err := verifier.Verify("service-secret"); err != nil {
		t.Fatalf("expected the second configured token to be accepted: %v", err)
	}
	if err := verifier.Verify("spoofed"); err == nil {
		t.Fatal("expected an unrecognized token to be rejected")
	}
	if err := verifier.Verify(""); err == nil {
		t.Fatal("expected an empty presented token to be rejected")
	}
}

// TestMultiTokenVerifierIgnoresUnconfiguredCandidates proves an empty
// candidate (e.g. an unset SERVICE_INTERNAL_TOKEN) is never itself a valid
// credential - it must not be possible to "authenticate" by presenting an
// empty string just because one of the configured slots was left empty.
func TestMultiTokenVerifierIgnoresUnconfiguredCandidates(t *testing.T) {
	verifier := NewMultiTokenVerifier("gateway-secret", "")
	if err := verifier.Verify(""); err == nil {
		t.Fatal("expected an empty presented token to be rejected even when a candidate slot is empty")
	}
	if err := verifier.Verify("gateway-secret"); err != nil {
		t.Fatalf("expected the configured token to still be accepted: %v", err)
	}
}

// TestMultiTokenVerifierSingleTokenDegradesToTokenVerifier proves passing
// only one non-empty token behaves exactly like TokenVerifier, so a service
// that never receives SERVICE_INTERNAL_TOKEN keeps today's exact contract.
func TestMultiTokenVerifierSingleTokenDegradesToTokenVerifier(t *testing.T) {
	verifier := NewMultiTokenVerifier("only-token", "")
	if err := verifier.Verify("only-token"); err != nil {
		t.Fatalf("expected the single configured token to be accepted: %v", err)
	}
	if err := verifier.Verify("wrong"); err == nil {
		t.Fatal("expected a wrong token to be rejected")
	}
}
