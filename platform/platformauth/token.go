// Package platformauth contains the narrow infrastructure trust boundary for
// platform-control-plane requests. It is intentionally separate from tenant
// member authorization and can be replaced by an IdP/workload identity later.
package platformauth

import (
	"crypto/subtle"
	"errors"
)

const HeaderName = "X-Platform-Admin-Token"

// Verifier validates platform administrator infrastructure credentials. The
// presented value is never logged by callers.
type Verifier interface {
	Verify(presented string) error
}

type TokenVerifier struct{ expected string }

func NewTokenVerifier(expected string) TokenVerifier { return TokenVerifier{expected: expected} }

func (v TokenVerifier) Verify(presented string) error {
	if v.expected == "" || presented == "" || subtle.ConstantTimeCompare([]byte(v.expected), []byte(presented)) != 1 {
		return errors.New("platform administrator authentication failed")
	}
	return nil
}
