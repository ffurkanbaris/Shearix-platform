package handler

import (
	"net/http"
	"testing"

	"github.com/barber-appointment/appointment-service/internal/application"
	"github.com/barber-appointment/platform/adminauth"
	"github.com/barber-appointment/platform/authcontract"
	"github.com/gofiber/fiber/v3"
)

func TestInternalAuthContract(t *testing.T) {
	app := fiber.New()
	New(application.BookingService{}, application.LifecycleService{}, application.QueryService{}, authcontract.Verifier(), adminauth.New("", "")).Register(app)
	routes := []authcontract.Route{
		authcontract.NewRoute(http.MethodGet, "/internal/v1/occupancy", true, "booking", 400, 400, 400),
		authcontract.NewRoute(http.MethodGet, "/internal/v1/appointments/:id/notification-recipient", true, "booking", 401, 401, 401),
		authcontract.NewRoute(http.MethodPost, "/internal/v1/public/appointments", true, "booking", 401, 401, 401),
		authcontract.NewRoute(http.MethodGet, "/internal/v1/public/appointments/:id", true, "booking", 401, 401, 401),
		authcontract.NewRoute(http.MethodPost, "/internal/v1/public/appointments/:id/cancel", true, "booking", 401, 401, 401),
		authcontract.NewRoute(http.MethodGet, "/internal/v1/customer/appointments/upcoming", true, "booking", 401, 401, 401),
		authcontract.NewRoute(http.MethodGet, "/internal/v1/customer/appointments/history", true, "booking", 401, 401, 401),
		authcontract.NewRoute(http.MethodGet, "/internal/v1/customer/appointments/:id", true, "booking", 401, 401, 401),
		authcontract.NewRoute(http.MethodPost, "/internal/v1/customer/appointments/:id/cancel", true, "booking", 401, 401, 401),
		authcontract.NewRoute(http.MethodGet, "/internal/v1/admin/appointments", true, "admin", 403, 403, 403),
		authcontract.NewRoute(http.MethodGet, "/internal/v1/admin/appointments/:id", true, "admin", 403, 403, 403),
		authcontract.NewRoute(http.MethodPost, "/internal/v1/admin/appointments", true, "admin", 403, 403, 403),
		authcontract.NewRoute(http.MethodPost, "/internal/v1/admin/appointments/:id/reschedule", true, "admin", 403, 403, 403),
		authcontract.NewRoute(http.MethodPost, "/internal/v1/admin/appointments/:id/cancel", true, "admin", 403, 403, 403),
		authcontract.NewRoute(http.MethodPost, "/internal/v1/admin/appointments/:id/confirm", true, "admin", 403, 403, 403),
		authcontract.NewRoute(http.MethodPost, "/internal/v1/admin/appointments/:id/complete", true, "admin", 403, 403, 403),
		authcontract.NewRoute(http.MethodPost, "/internal/v1/admin/appointments/:id/no-show", true, "admin", 403, 403, 403),
	}
	authcontract.Run(t, app, routes)
}
