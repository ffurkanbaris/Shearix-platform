package consumer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

func TestAppointmentRecipientClientContract(t *testing.T) {
	tenant, appointment := uuid.New(), uuid.New()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/v1/appointments/"+appointment.String()+"/notification-recipient" || r.Header.Get("X-Internal-Token") != "token" || r.Header.Get("X-Tenant-ID") != tenant.String() {
			t.Fatal("recipient request lost its trusted contract")
		}
		_, _ = w.Write([]byte(`{"email":" Customer@Example.COM "}`))
	}))
	defer server.Close()
	email, err := NewAppointmentRecipientClient(server.URL, "token").Email(context.Background(), tenant, appointment)
	if err != nil || email != "customer@example.com" {
		t.Fatalf("email=%q err=%v", email, err)
	}
}

func TestAppointmentRecipientClientRejectsInvalidOrUnavailableResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusUnauthorized) }))
	defer server.Close()
	if _, err := NewAppointmentRecipientClient(server.URL, "token").Email(context.Background(), uuid.New(), uuid.New()); err == nil {
		t.Fatal("non-200 recipient response accepted")
	}
	if _, err := NewAppointmentRecipientClient("http://127.0.0.1:1", "token").Email(context.Background(), uuid.New(), uuid.New()); err == nil {
		t.Fatal("unavailable recipient service accepted")
	}
}
