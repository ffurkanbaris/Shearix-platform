package handler

import (
	"net/http"
	"testing"

	"github.com/barber-appointment/customer-service/internal/repository"
	"github.com/barber-appointment/platform/authcontract"
	"github.com/gofiber/fiber/v3"
)

func TestInternalAuthContract(t *testing.T) {
	app := fiber.New()
	New(repository.Repository{}, authcontract.Verifier(), nil, authcontract.ValidToken, "").Register(app)
	routes := []authcontract.Route{
		authcontract.NewRoute(http.MethodPost, "/internal/v1/public/customer/auth/register", true, "booking", 403, 403, 403),
		authcontract.NewRoute(http.MethodPost, "/internal/v1/public/customer/auth/login", true, "booking", 403, 403, 403),
		authcontract.NewRoute(http.MethodPost, "/internal/v1/public/customer/auth/logout", true, "booking", 401, 401, 401),
		authcontract.NewRoute(http.MethodGet, "/internal/v1/public/customer/auth/me", true, "booking", 401, 401, 401),
		authcontract.NewRoute(http.MethodPost, "/internal/v1/public/customer/auth/change-password", true, "booking", 401, 401, 401),
		authcontract.NewRoute(http.MethodPost, "/internal/v1/public/customer/auth/forgot-password", true, "booking", 403, 403, 403),
		authcontract.NewRoute(http.MethodPost, "/internal/v1/customer/resolve", true, "booking", 401, 401, 401),
		authcontract.NewRoute(http.MethodGet, "/internal/v1/customer/:id", true, "booking", 401, 401, 401),
		authcontract.NewRoute(http.MethodGet, "/internal/v1/public/customer/profile", true, "booking", 401, 401, 401),
		authcontract.NewRoute(http.MethodPatch, "/internal/v1/public/customer/profile", true, "booking", 401, 401, 401),
		authcontract.NewRoute(http.MethodGet, "/internal/v1/public/customer/appointments/upcoming", true, "booking", 401, 401, 401),
		authcontract.NewRoute(http.MethodGet, "/internal/v1/public/customer/appointments/history", true, "booking", 401, 401, 401),
		authcontract.NewRoute(http.MethodGet, "/internal/v1/public/customer/appointments/:id", true, "booking", 401, 401, 401),
		authcontract.NewRoute(http.MethodPost, "/internal/v1/public/customer/appointments/:id/cancel", true, "booking", 401, 401, 401),
		authcontract.NewRoute(http.MethodGet, "/internal/v1/internal/customer/session", true, "booking", 401, 401, 401),
	}
	authcontract.Run(t, app, routes)
}
