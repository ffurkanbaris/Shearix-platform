package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestResolverRejectsUnavailableAndMalformedTenantResponses(t *testing.T) {
	for _, tc := range []struct {
		name    string
		handler http.HandlerFunc
	}{
		{name: "unavailable", handler: func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }},
		{name: "malformed", handler: func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"tenant_id":`)) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(tc.handler)
			defer server.Close()
			if _, err := NewResolver(server.URL, "internal-secret").Resolve(context.Background(), "booking.localhost"); err == nil {
				t.Fatal("resolver accepted invalid tenant-service response")
			}
		})
	}

	if _, err := NewResolver("http://127.0.0.1:1", "internal-secret").Resolve(context.Background(), "booking.localhost"); err == nil {
		t.Fatal("resolver accepted unavailable tenant-service")
	}
}
