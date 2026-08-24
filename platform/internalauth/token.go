package internalauth

import (
	"crypto/subtle"
	"errors"
)

const HeaderName = "X-Internal-Token"

// Verifier is infrastructure authentication. Alternative implementations can
// use mTLS or workload identity without changing domain handlers.
type Verifier interface {
	Verify(presented string) error
}

type TokenVerifier struct{ expected string }

func NewTokenVerifier(expected string) TokenVerifier { return TokenVerifier{expected: expected} }

func (v TokenVerifier) Verify(presented string) error {
	if v.expected == "" || presented == "" || subtle.ConstantTimeCompare([]byte(v.expected), []byte(presented)) != 1 {
		return errors.New("internal authentication failed")
	}
	return nil
}

// MultiTokenVerifier accepts any one of several configured credentials. This
// backs a two-tier internal-auth model that reduces blast radius without
// changing the existing single-shared-secret contract any caller already
// relies on: the gateway presents its own token when proxying browser-facing
// requests to a backend, while backend-to-backend calls (e.g. catalog-service
// calling barber-service) present a separate token - see
// platform/config.ServiceConfig.ServiceInternalToken. A backend accepts
// either, since every internal route can legitimately be reached by both
// paths; leaking one tier's credential does not also leak the other's. Empty
// candidates are ignored (never treated as "accept anything"), so passing an
// unset ServiceInternalToken degrades safely to single-token behavior.
type MultiTokenVerifier struct{ expected []string }

func NewMultiTokenVerifier(expected ...string) MultiTokenVerifier {
	nonEmpty := make([]string, 0, len(expected))
	for _, token := range expected {
		if token != "" {
			nonEmpty = append(nonEmpty, token)
		}
	}
	return MultiTokenVerifier{expected: nonEmpty}
}

func (v MultiTokenVerifier) Verify(presented string) error {
	if presented == "" {
		return errors.New("internal authentication failed")
	}
	for _, expected := range v.expected {
		if subtle.ConstantTimeCompare([]byte(expected), []byte(presented)) == 1 {
			return nil
		}
	}
	return errors.New("internal authentication failed")
}
