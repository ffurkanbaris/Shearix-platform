package tenantctx

import "testing"

func TestNewRejectsInvalidContext(t *testing.T) {
	if _, err := New("not-a-uuid", "booking", "request"); err == nil {
		t.Fatal("expected invalid UUID rejection")
	}
	if _, err := New("00000000-0000-0000-0000-000000000001", "external", "request"); err == nil {
		t.Fatal("expected invalid app type rejection")
	}
}
