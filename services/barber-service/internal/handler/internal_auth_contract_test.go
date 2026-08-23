package handler

import (
	"net/http"
	"testing"

	"github.com/barber-appointment/barber-service/internal/repository"
	"github.com/barber-appointment/platform/adminauth"
	"github.com/barber-appointment/platform/authcontract"
	"github.com/gofiber/fiber/v3"
)

func TestInternalAuthContract(t *testing.T) {
	app := fiber.New()
	New(repository.Repository{}, authcontract.Verifier(), adminauth.New("", "")).Register(app)
	routes := []authcontract.Route{
		authcontract.NewRoute(http.MethodGet, "/internal/v1/barbers/:id/exists", true, "booking", 401, 401, 401),
		authcontract.NewRoute(http.MethodGet, "/internal/v1/barbers/:id/scheduling-access", true, "admin", 401, 401, 401),
		authcontract.NewRoute(http.MethodGet, "/internal/v1/branches/:branch_id/barbers/:barber_id/booking-access", true, "booking", 401, 401, 401),
		authcontract.NewRoute(http.MethodGet, "/internal/v1/public/branches", true, "booking", 401, 401, 401),
		authcontract.NewRoute(http.MethodGet, "/internal/v1/public/barbers", true, "booking", 401, 401, 401),
		authcontract.NewRoute(http.MethodPost, "/internal/v1/admin/branches", true, "admin", 403, 403, 403),
		authcontract.NewRoute(http.MethodGet, "/internal/v1/admin/branches", true, "admin", 401, 401, 401),
		authcontract.NewRoute(http.MethodGet, "/internal/v1/admin/branches/:id", true, "admin", 401, 401, 401),
		authcontract.NewRoute(http.MethodPatch, "/internal/v1/admin/branches/:id", true, "admin", 403, 403, 403),
		authcontract.NewRoute(http.MethodPost, "/internal/v1/admin/barbers", true, "admin", 403, 403, 403),
		authcontract.NewRoute(http.MethodGet, "/internal/v1/admin/barbers", true, "admin", 401, 401, 401),
		authcontract.NewRoute(http.MethodGet, "/internal/v1/admin/barbers/:id", true, "admin", 401, 401, 401),
		authcontract.NewRoute(http.MethodPatch, "/internal/v1/admin/barbers/:id", true, "admin", 403, 403, 403),
		authcontract.NewRoute(http.MethodPost, "/internal/v1/admin/barbers/:barber_id/link-identity", true, "admin", 401, 401, 401),
		authcontract.NewRoute(http.MethodDelete, "/internal/v1/admin/barbers/:barber_id/link-identity", true, "admin", 401, 401, 401),
	}
	authcontract.Run(t, app, routes)
}
