package handler

import (
	"net/http"
	"testing"

	"github.com/barber-appointment/auth-service/internal/service"
	"github.com/barber-appointment/platform/authcontract"
	"github.com/gofiber/fiber/v3"
)

func TestInternalAuthContract(t *testing.T) {
	app := fiber.New()
	New(service.Service{}, authcontract.Verifier(), "session", false).Register(app)
	routes := []authcontract.Route{
		authcontract.NewRoute(http.MethodPost, "/internal/v1/auth/login", true, "admin", 401, 401, 401),
		authcontract.NewRoute(http.MethodPost, "/internal/v1/auth/logout", true, "admin", 401, 401, 401),
		authcontract.NewRoute(http.MethodGet, "/internal/v1/auth/me", true, "admin", 401, 401, 401),
		authcontract.NewRoute(http.MethodPost, "/internal/v1/auth/register", true, "admin", 401, 401, 401),
		authcontract.NewRoute(http.MethodGet, "/internal/v1/auth/members", true, "admin", 401, 401, 401),
		authcontract.NewRoute(http.MethodGet, "/internal/v1/auth/members/:id", true, "admin", 401, 401, 401),
		authcontract.NewRoute(http.MethodPost, "/internal/v1/auth/members", true, "admin", 401, 401, 401),
		authcontract.NewRoute(http.MethodPatch, "/internal/v1/auth/members/:id/role", true, "admin", 401, 401, 401),
		authcontract.NewRoute(http.MethodPost, "/internal/v1/auth/members/:id/activate", true, "admin", 401, 401, 401),
		authcontract.NewRoute(http.MethodPost, "/internal/v1/auth/members/:id/deactivate", true, "admin", 401, 401, 401),
		authcontract.NewRoute(http.MethodGet, "/internal/v1/auth/members/:id/barber-eligibility", true, "admin", 401, 401, 401),
		authcontract.NewRoute(http.MethodPost, "/internal/v1/auth/change-password", true, "admin", 401, 401, 401),
		authcontract.NewRoute(http.MethodPost, "/internal/v1/auth/forgot-password", true, "admin", 401, 401, 401),
	}
	authcontract.Run(t, app, routes)
}
