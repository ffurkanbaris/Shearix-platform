package handler

import (
	"net/http"
	"testing"

	"github.com/barber-appointment/catalog-service/internal/repository"
	"github.com/barber-appointment/platform/adminauth"
	"github.com/barber-appointment/platform/authcontract"
	"github.com/barber-appointment/platform/barberclient"
	"github.com/gofiber/fiber/v3"
)

func TestInternalAuthContract(t *testing.T) {
	app := fiber.New()
	New(repository.Repository{}, authcontract.Verifier(), adminauth.New("", ""), barberclient.New("", "")).Register(app)
	routes := []authcontract.Route{
		authcontract.NewRoute(http.MethodGet, "/internal/v1/public/services", true, "booking", 401, 401, 401),
		authcontract.NewRoute(http.MethodGet, "/internal/v1/public/barber-services", true, "booking", 401, 401, 401),
		authcontract.NewRoute(http.MethodGet, "/internal/v1/barbers/:barber_id/services/:service_id/availability", true, "booking", 401, 401, 401),
		authcontract.NewRoute(http.MethodPost, "/internal/v1/admin/services", true, "admin", 403, 403, 403),
		authcontract.NewRoute(http.MethodGet, "/internal/v1/admin/services", true, "admin", 401, 401, 401),
		authcontract.NewRoute(http.MethodGet, "/internal/v1/admin/services/:id", true, "admin", 401, 401, 401),
		authcontract.NewRoute(http.MethodPatch, "/internal/v1/admin/services/:id", true, "admin", 403, 403, 403),
		authcontract.NewRoute(http.MethodPost, "/internal/v1/admin/barbers/:barber_id/services", true, "admin", 403, 403, 403),
		authcontract.NewRoute(http.MethodGet, "/internal/v1/admin/barbers/:barber_id/services", true, "admin", 401, 401, 401),
		authcontract.NewRoute(http.MethodDelete, "/internal/v1/admin/barbers/:barber_id/services/:service_id", true, "admin", 403, 403, 403),
	}
	authcontract.Run(t, app, routes)
}
