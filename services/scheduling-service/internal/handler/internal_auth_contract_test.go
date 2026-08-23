package handler

import (
	"net/http"
	"testing"

	"github.com/barber-appointment/platform/adminauth"
	"github.com/barber-appointment/platform/authcontract"
	"github.com/barber-appointment/scheduling-service/internal/application"
	"github.com/gofiber/fiber/v3"
)

func TestInternalAuthContract(t *testing.T) {
	app := fiber.New()
	New(application.Service{}, authcontract.Verifier(), adminauth.New("", "")).Register(app)
	routes := []authcontract.Route{
		authcontract.NewRoute(http.MethodGet, "/internal/v1/public/availability", true, "booking", 401, 401, 401),
		authcontract.NewRoute(http.MethodGet, "/internal/v1/admin/availability", true, "admin", 401, 401, 401),
		authcontract.NewRoute(http.MethodGet, "/internal/v1/admin/barbers/:barber_id/schedule", true, "admin", 403, 403, 403),
		authcontract.NewRoute(http.MethodPut, "/internal/v1/admin/barbers/:barber_id/working-hours", true, "admin", 403, 403, 403),
		authcontract.NewRoute(http.MethodGet, "/internal/v1/admin/barbers/:barber_id/overrides", true, "admin", 403, 403, 403),
		authcontract.NewRoute(http.MethodPost, "/internal/v1/admin/barbers/:barber_id/overrides", true, "admin", 403, 403, 403),
		authcontract.NewRoute(http.MethodPatch, "/internal/v1/admin/barbers/:barber_id/overrides/:id", true, "admin", 403, 403, 403),
		authcontract.NewRoute(http.MethodDelete, "/internal/v1/admin/barbers/:barber_id/overrides/:id", true, "admin", 403, 403, 403),
		authcontract.NewRoute(http.MethodGet, "/internal/v1/admin/barbers/:barber_id/blocked-periods", true, "admin", 403, 403, 403),
		authcontract.NewRoute(http.MethodPost, "/internal/v1/admin/barbers/:barber_id/blocked-periods", true, "admin", 403, 403, 403),
		authcontract.NewRoute(http.MethodDelete, "/internal/v1/admin/barbers/:barber_id/blocked-periods/:id", true, "admin", 403, 403, 403),
	}
	authcontract.Run(t, app, routes)
}
