package handler

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/barber-appointment/gateway-service/internal/service"
	"github.com/gofiber/fiber/v3"
)

func TestFrontendDispatchesByResolvedDomainType(t *testing.T) {
	resolverBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Internal-Token") != "internal" {
			t.Fatal("resolver did not receive internal authentication")
		}
		hostname := r.URL.Query().Get("hostname")
		if hostname == "missing.example" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		appType := "booking"
		if hostname == "panel.example" {
			appType = "admin"
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tenant_id":"00000000-0000-0000-0000-000000000001","app_type":"` + appType + `"}`))
	}))
	defer resolverBackend.Close()

	admin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "panel.example" {
			t.Fatalf("admin host=%q", r.Host)
		}
		_, _ = w.Write([]byte("admin-page"))
	}))
	defer admin.Close()
	booking := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "booking.example" {
			t.Fatalf("booking host=%q", r.Host)
		}
		_, _ = w.Write([]byte("booking-page"))
	}))
	defer booking.Close()

	app := fiber.New()
	New(service.NewResolver(resolverBackend.URL, "internal"), resolverBackend.URL, "", "", "", "", "", "", "", "internal", "platform", admin.URL, booking.URL).Register(app)
	for _, test := range []struct{ host, want string }{{"panel.example", "admin-page"}, {"booking.example", "booking-page"}} {
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		request.Host = test.host
		response, err := app.Test(request)
		if err != nil || response.StatusCode != http.StatusOK {
			t.Fatalf("%s status=%v err=%v", test.host, status(response), err)
		}
		body, readErr := io.ReadAll(response.Body)
		if readErr != nil || string(body) != test.want {
			t.Fatalf("%s body=%q err=%v", test.host, body, readErr)
		}
		_ = response.Body.Close()
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/not-a-route", nil)
	request.Host = "booking.example"
	response, err := app.Test(request)
	if err != nil || response.StatusCode != http.StatusNotFound {
		t.Fatalf("unmatched API status=%v err=%v", status(response), err)
	}
}
