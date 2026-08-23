package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/barber-appointment/appointment-service/internal/domain"
	"github.com/barber-appointment/platform/schedulingdeps"
	"github.com/barber-appointment/platform/tenantctx"
	"github.com/google/uuid"
)

func TestDependenciesAvailabilityContract(t *testing.T) {
	tenant, barber, service := uuid.New(), uuid.New(), uuid.New()
	start := time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/internal/v1/public/config" {
			_, _ = w.Write([]byte(`{"business_timezone":"UTC"}`))
			return
		}
		if r.URL.Path != "/internal/v1/public/availability" || r.URL.Query().Get("barber_id") != barber.String() || r.URL.Query().Get("service_id") != service.String() || r.Header.Get("X-Internal-Token") != "token" || r.Header.Get("X-Tenant-ID") != tenant.String() {
			t.Fatalf("unexpected availability request: %s %s", r.URL, r.Header)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"start_at":"2026-01-02T10:00:00Z"}]`))
	}))
	defer server.Close()
	d := New(schedulingdeps.New(server.URL, server.URL, server.URL, "token"), server.URL, server.URL, "token")
	err := d.Available(context.Background(), tenantctx.Context{TenantID: tenant, AppType: "booking", RequestID: "request"}, domain.CreateInput{BarberID: barber, ServiceID: service}, start)
	if err != nil {
		t.Fatal(err)
	}
}

func TestDependenciesRejectUnavailableAndMalformedResponses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/internal/v1/public/config" {
			_, _ = w.Write([]byte(`{"business_timezone":"UTC"}`))
			return
		}
		_, _ = w.Write([]byte(`not-json`))
	}))
	defer server.Close()
	d := New(schedulingdeps.New(server.URL, server.URL, server.URL, "token"), server.URL, server.URL, "token")
	err := d.Available(context.Background(), tenantctx.Context{TenantID: uuid.New(), AppType: "booking"}, domain.CreateInput{BarberID: uuid.New(), ServiceID: uuid.New()}, time.Now().UTC())
	if err == nil {
		t.Fatal("malformed scheduling response was accepted")
	}
	if _, err = New(schedulingdeps.New("http://127.0.0.1:1", "http://127.0.0.1:1", "http://127.0.0.1:1", "token"), "http://127.0.0.1:1", "http://127.0.0.1:1", "token").Customer(context.Background(), tenantctx.Context{TenantID: uuid.New()}, uuid.New()); err == nil {
		t.Fatal("unavailable customer dependency was accepted")
	}
}
