package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/barber-appointment/platform/internalauth"
	"github.com/barber-appointment/tenant-service/internal/service"
	"github.com/gofiber/fiber/v3"
)

func TestTLSAuthorizeRequiresInternalAuthAndRejectsHostnameSpoofing(t *testing.T) {
	app := fiber.New()
	New(service.TenantService{}, internalauth.NewTokenVerifier("internal-secret")).Register(app)

	request := httptest.NewRequest(http.MethodGet, "/internal/v1/domains/tls-authorize?hostname=panel.example.test", nil)
	response, err := app.Test(request)
	if err != nil || response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("missing internal auth status=%v err=%v", tlsStatus(response), err)
	}

	for _, target := range []string{
		"/internal/v1/domains/tls-authorize?hostname=https://panel.example.test",
		"/internal/v1/domains/tls-authorize?hostname=panel.example.test&domain=other.example.test",
	} {
		request = httptest.NewRequest(http.MethodGet, target, nil)
		request.Header.Set(internalauth.HeaderName, "internal-secret")
		response, err = app.Test(request)
		if err != nil || response.StatusCode != http.StatusForbidden {
			t.Fatalf("spoof target=%q status=%v err=%v", target, tlsStatus(response), err)
		}
	}
}

func tlsStatus(response *http.Response) int {
	if response == nil {
		return 0
	}
	return response.StatusCode
}
