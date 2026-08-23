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
