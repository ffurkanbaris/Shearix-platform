package handler

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/barber-appointment/gateway-service/internal/service"
	"github.com/barber-appointment/platform/platformauth"
	"github.com/gofiber/fiber/v3"
)

// platformTenantsRequest builds an authorized request against the platform
// proxy route, which forwards directly to tenantURL with no other
// dependencies (no hostname resolution) - a minimal seam for exercising
// h.client's timeout behavior in isolation.
func platformTenantsRequest() *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/platform/tenants", nil)
	req.Header.Set(platformauth.HeaderName, "platform-secret")
	return req
}

func newGatewayApp(tenantURL string, outboundTimeout time.Duration) *fiber.App {
	app := fiber.New()
	New(service.Resolver{}, tenantURL, "", "", "", "", "", "", "", "internal-secret", "platform-secret", outboundTimeout).Register(app)
	return app
}

func decodeErrorBody(t *testing.T, body []byte) map[string]string {
	t.Helper()
	var decoded map[string]string
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("error body was not JSON: %s (%v)", body, err)
	}
	return decoded
}

// A backend that never responds within the configured outbound timeout must
// surface as a distinct 504 with a body identifying the failure as a timeout.
func TestSlowBackendReturnsGatewayTimeout(t *testing.T) {
	release := make(chan struct{})
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()
	defer close(release)

	app := newGatewayApp(backend.URL, 100*time.Millisecond)

	response, err := app.Test(platformTenantsRequest(), fiber.TestConfig{Timeout: 5 * time.Second, FailOnTimeout: true})
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, want %d, body=%s", response.StatusCode, http.StatusGatewayTimeout, body)
	}
	decoded := decodeErrorBody(t, body)
	if decoded["error"] != "gateway_timeout" {
		t.Fatalf("error body = %v, want error=gateway_timeout", decoded)
	}
}

// A backend that is entirely unreachable (connection refused) is a distinct
// failure mode from a timeout and must not be reported as 504.
func TestUnreachableBackendReturnsBadGatewayNotTimeout(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to reserve a port: %v", err)
	}
	closedPortURL := "http://" + listener.Addr().String()
	// Close immediately: the port stays reserved by the OS as unused, so
	// dialing it deterministically yields connection refused rather than a
	// timeout or a flaky bind to something else.
	if err := listener.Close(); err != nil {
		t.Fatalf("failed to close listener: %v", err)
	}

	app := newGatewayApp(closedPortURL, DefaultOutboundTimeout)

	response, err := app.Test(platformTenantsRequest(), fiber.TestConfig{Timeout: 5 * time.Second, FailOnTimeout: true})
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d, body=%s", response.StatusCode, http.StatusBadGateway, body)
	}
	decoded := decodeErrorBody(t, body)
	if decoded["error"] != "gateway_unavailable" {
		t.Fatalf("error body = %v, want error=gateway_unavailable", decoded)
	}
	if decoded["error"] == "gateway_timeout" {
		t.Fatal("connection-refused failure was reported as a timeout")
	}
}

// The outbound timeout is configurable per Handler: an otherwise-fast backend
// still times out when the configured timeout is shorter than its response
// latency, proving the value actually takes effect rather than being fixed.
func TestOutboundTimeoutIsConfigurable(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(30 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	// A timeout shorter than the backend's fixed 30ms latency must trip.
	shortApp := newGatewayApp(backend.URL, 5*time.Millisecond)
	response, err := shortApp.Test(platformTenantsRequest(), fiber.TestConfig{Timeout: 5 * time.Second, FailOnTimeout: true})
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusGatewayTimeout {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("short timeout: status = %d, want %d, body=%s", response.StatusCode, http.StatusGatewayTimeout, body)
	}

	// A generous timeout against the same backend must not trip.
	longApp := newGatewayApp(backend.URL, 2*time.Second)
	response, err = longApp.Test(platformTenantsRequest(), fiber.TestConfig{Timeout: 5 * time.Second, FailOnTimeout: true})
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusGatewayTimeout {
		t.Fatal("longer configured timeout still reported a timeout against a fast-enough backend")
	}
}

// A backend that responds well within the timeout proxies normally: raising
// the default outbound timeout and adding timeout/connection-failure
// handling must not change ordinary successful proxying.
func TestFastBackendProxiesSuccessfullyUnaffected(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"tenant-fast"}`))
	}))
	defer backend.Close()

	app := newGatewayApp(backend.URL, DefaultOutboundTimeout)

	response, err := app.Test(platformTenantsRequest(), fiber.TestConfig{Timeout: 5 * time.Second, FailOnTimeout: true})
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusCreated || string(body) != `{"id":"tenant-fast"}` {
		t.Fatalf("status=%d body=%s", response.StatusCode, body)
	}
}
