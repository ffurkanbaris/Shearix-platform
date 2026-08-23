package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/barber-appointment/platform/tenantctx"
	"github.com/google/uuid"
)

func TestHTTPOccupancyContractAndFailures(t *testing.T) {
	tenant, barber := uuid.New(), uuid.New()
	from := time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Internal-Token") != "token" || r.Header.Get("X-Tenant-ID") != tenant.String() || r.URL.Query().Get("barber_id") != barber.String() {
			t.Fatal("occupancy request lost trusted context")
		}
		_, _ = w.Write([]byte(`[{"occupied_start_at":"2026-01-02T10:00:00Z","occupied_end_at":"2026-01-02T10:30:00Z"}]`))
	}))
	defer server.Close()
	rows, err := NewHTTPOccupancy(server.URL, "token").Occupied(context.Background(), tenantctx.Context{TenantID: tenant, AppType: "booking"}, barber, from, from.Add(time.Hour))
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows=%v err=%v", rows, err)
	}

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{}`)) }))
	defer bad.Close()
	_, err = NewHTTPOccupancy(bad.URL, "token").Occupied(context.Background(), tenantctx.Context{TenantID: tenant}, barber, from, from.Add(time.Hour))
	if !errors.Is(err, ErrOccupancyUnavailable) {
		t.Fatalf("malformed occupancy error=%v", err)
	}
	_, err = NewHTTPOccupancy("http://127.0.0.1:1", "token").Occupied(context.Background(), tenantctx.Context{TenantID: tenant}, barber, from, from.Add(time.Hour))
	if !errors.Is(err, ErrOccupancyUnavailable) {
		t.Fatalf("unavailable occupancy error=%v", err)
	}
}
