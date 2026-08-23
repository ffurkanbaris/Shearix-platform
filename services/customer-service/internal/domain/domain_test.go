package domain

import "testing"

func TestNormalizePhone(t *testing.T) {
	got, err := NormalizePhone(" +90 (555) 111-2233 ")
	if err != nil || got != "+905551112233" {
		t.Fatalf("got=%q err=%v", got, err)
	}
	for _, value := range []string{"5551112233", "+90abc", "+1"} {
		if _, err := NormalizePhone(value); err == nil {
			t.Fatalf("expected invalid phone %q", value)
		}
	}
}
