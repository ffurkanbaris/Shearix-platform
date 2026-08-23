package tenantsettings

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/barber-appointment/platform/internalauth"
	"github.com/barber-appointment/platform/tenantctx"
	"github.com/google/uuid"
)

func TestClientReadsTenantScopedReminderOffsets(t *testing.T) {
	tenantID := uuid.New()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/v1/settings" || r.Header.Get(internalauth.HeaderName) != "secret" || r.Header.Get(tenantctx.TenantIDHeader) != tenantID.String() || r.Header.Get(tenantctx.AppTypeHeader) != "booking" {
			t.Fatalf("unexpected private settings request: path=%s headers=%v", r.URL.Path, r.Header)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"business_timezone":"Europe/Istanbul","booking_interval_minutes":30,"reminder_offsets_minutes":[1440,120]}`))
	}))
	defer server.Close()

	offsets, err := New(server.URL, "secret").ReminderOffsets(context.Background(), tenantID)
	if err != nil || len(offsets) != 2 || offsets[0].Minutes() != 1440 || offsets[1].Minutes() != 120 {
		t.Fatalf("offsets=%v err=%v", offsets, err)
	}
}
