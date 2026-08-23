package handler

import "github.com/gofiber/fiber/v3"

func (h Handler) registerCustomerRoutes(app *fiber.App) {
	app.Post("/api/v1/public/customer/auth/register", h.proxy(h.customerURL, "/internal/v1/public/customer/auth/register", "booking"))
	app.Post("/api/v1/public/customer/auth/login", h.proxy(h.customerURL, "/internal/v1/public/customer/auth/login", "booking"))
	app.Post("/api/v1/public/customer/auth/logout", h.proxy(h.customerURL, "/internal/v1/public/customer/auth/logout", "booking"))
	app.Get("/api/v1/public/customer/auth/me", h.proxy(h.customerURL, "/internal/v1/public/customer/auth/me", "booking"))
	app.Post("/api/v1/public/customer/auth/change-password", h.proxy(h.customerURL, "/internal/v1/public/customer/auth/change-password", "booking"))
	app.Post("/api/v1/public/customer/auth/forgot-password", h.proxy(h.customerURL, "/internal/v1/public/customer/auth/forgot-password", "booking"))
	app.Get("/api/v1/public/customer/profile", h.proxy(h.customerURL, "/internal/v1/public/customer/profile", "booking"))
	app.Patch("/api/v1/public/customer/profile", h.proxy(h.customerURL, "/internal/v1/public/customer/profile", "booking"))
	app.Get("/api/v1/public/customer/appointments/upcoming", h.proxy(h.customerURL, "/internal/v1/public/customer/appointments/upcoming", "booking"))
	app.Get("/api/v1/public/customer/appointments/history", h.proxy(h.customerURL, "/internal/v1/public/customer/appointments/history", "booking"))
	app.Get("/api/v1/public/customer/appointments/:id", h.proxy(h.customerURL, "/internal/v1/public/customer/appointments/:id", "booking"))
	app.Post("/api/v1/public/customer/appointments/:id/cancel", h.proxy(h.customerURL, "/internal/v1/public/customer/appointments/:id/cancel", "booking"))
}

func (h Handler) registerAuthRoutes(app *fiber.App) {
	app.Post("/api/v1/admin/auth/login", h.auth("/login"))
	app.Post("/api/v1/admin/auth/logout", h.auth("/logout"))
	app.Get("/api/v1/admin/auth/me", h.auth("/me"))
	app.Post("/api/v1/admin/auth/register", h.auth("/register"))
	app.Get("/api/v1/admin/members", h.auth("/members"))
	app.Get("/api/v1/admin/members/:id", h.auth("/members/:id"))
	app.Post("/api/v1/admin/members", h.auth("/members"))
	app.Patch("/api/v1/admin/members/:id/role", h.auth("/members/:id/role"))
	app.Post("/api/v1/admin/members/:id/activate", h.auth("/members/:id/activate"))
	app.Post("/api/v1/admin/members/:id/deactivate", h.auth("/members/:id/deactivate"))
	app.Post("/api/v1/admin/auth/change-password", h.auth("/change-password"))
	app.Post("/api/v1/admin/auth/forgot-password", h.auth("/forgot-password"))
}

func (h Handler) registerTenantRoutes(app *fiber.App) {
	app.Get("/api/v1/admin/settings", h.proxy(h.tenantURL, "/internal/v1/admin/settings", "admin"))
	app.Patch("/api/v1/admin/settings", h.proxy(h.tenantURL, "/internal/v1/admin/settings", "admin"))
	app.Get("/api/v1/public/branches", h.proxy(h.barberURL, "/internal/v1/public/branches", "booking"))
	app.Get("/api/v1/public/barbers", h.proxy(h.barberURL, "/internal/v1/public/barbers", "booking"))
	app.Get("/api/v1/public/services", h.proxy(h.catalogURL, "/internal/v1/public/services", "booking"))
	app.Get("/api/v1/public/barber-services", h.proxy(h.catalogURL, "/internal/v1/public/barber-services", "booking"))
	app.Get("/api/v1/public/availability", h.proxy(h.schedulingURL, "/internal/v1/public/availability", "booking"))
	app.Get("/api/v1/admin/availability", h.proxy(h.schedulingURL, "/internal/v1/admin/availability", "admin"))
}
func (h Handler) registerAppointmentRoutes(app *fiber.App) {
	app.Post("/api/v1/public/appointments", h.proxy(h.appointmentURL, "/internal/v1/public/appointments", "booking"))
	app.Get("/api/v1/public/appointments/:id", h.proxy(h.appointmentURL, "/internal/v1/public/appointments/:id", "booking"))
	app.Post("/api/v1/public/appointments/:id/cancel", h.proxy(h.appointmentURL, "/internal/v1/public/appointments/:id/cancel", "booking"))
	app.Get("/api/v1/admin/appointments", h.proxy(h.appointmentURL, "/internal/v1/admin/appointments", "admin"))
	app.Get("/api/v1/admin/appointments/:id", h.proxy(h.appointmentURL, "/internal/v1/admin/appointments/:id", "admin"))
	app.Post("/api/v1/admin/appointments", h.proxy(h.appointmentURL, "/internal/v1/admin/appointments", "admin"))
	for _, operation := range []string{"cancel", "confirm", "complete", "no-show", "reschedule"} {
		app.Post("/api/v1/admin/appointments/:id/"+operation, h.proxy(h.appointmentURL, "/internal/v1/admin/appointments/:id/"+operation, "admin"))
	}
}
func (h Handler) registerBarberRoutes(app *fiber.App) {
	app.Post("/api/v1/admin/branches", h.proxy(h.barberURL, "/internal/v1/admin/branches", "admin"))
	app.Get("/api/v1/admin/branches", h.proxy(h.barberURL, "/internal/v1/admin/branches", "admin"))
	app.Get("/api/v1/admin/branches/:id", h.proxy(h.barberURL, "/internal/v1/admin/branches/:id", "admin"))
	app.Patch("/api/v1/admin/branches/:id", h.proxy(h.barberURL, "/internal/v1/admin/branches/:id", "admin"))
	app.Post("/api/v1/admin/barbers", h.proxy(h.barberURL, "/internal/v1/admin/barbers", "admin"))
	app.Get("/api/v1/admin/barbers", h.proxy(h.barberURL, "/internal/v1/admin/barbers", "admin"))
	app.Get("/api/v1/admin/barbers/:id", h.proxy(h.barberURL, "/internal/v1/admin/barbers/:id", "admin"))
	app.Patch("/api/v1/admin/barbers/:id", h.proxy(h.barberURL, "/internal/v1/admin/barbers/:id", "admin"))
	app.Post("/api/v1/admin/barbers/:barber_id/link-identity", h.proxy(h.barberURL, "/internal/v1/admin/barbers/:barber_id/link-identity", "admin"))
	app.Delete("/api/v1/admin/barbers/:barber_id/link-identity", h.proxy(h.barberURL, "/internal/v1/admin/barbers/:barber_id/link-identity", "admin"))
}
func (h Handler) registerCatalogRoutes(app *fiber.App) {
	app.Post("/api/v1/admin/services", h.proxy(h.catalogURL, "/internal/v1/admin/services", "admin"))
	app.Get("/api/v1/admin/services", h.proxy(h.catalogURL, "/internal/v1/admin/services", "admin"))
	app.Get("/api/v1/admin/services/:id", h.proxy(h.catalogURL, "/internal/v1/admin/services/:id", "admin"))
	app.Patch("/api/v1/admin/services/:id", h.proxy(h.catalogURL, "/internal/v1/admin/services/:id", "admin"))
	app.Post("/api/v1/admin/barbers/:barber_id/services", h.proxy(h.catalogURL, "/internal/v1/admin/barbers/:barber_id/services", "admin"))
	app.Get("/api/v1/admin/barbers/:barber_id/services", h.proxy(h.catalogURL, "/internal/v1/admin/barbers/:barber_id/services", "admin"))
	app.Delete("/api/v1/admin/barbers/:barber_id/services/:service_id", h.proxy(h.catalogURL, "/internal/v1/admin/barbers/:barber_id/services/:service_id", "admin"))
}
func (h Handler) registerSchedulingRoutes(app *fiber.App) {
	app.Get("/api/v1/admin/barbers/:barber_id/schedule", h.proxy(h.schedulingURL, "/internal/v1/admin/barbers/:barber_id/schedule", "admin"))
	app.Put("/api/v1/admin/barbers/:barber_id/working-hours", h.proxy(h.schedulingURL, "/internal/v1/admin/barbers/:barber_id/working-hours", "admin"))
	app.Get("/api/v1/admin/barbers/:barber_id/overrides", h.proxy(h.schedulingURL, "/internal/v1/admin/barbers/:barber_id/overrides", "admin"))
	app.Post("/api/v1/admin/barbers/:barber_id/overrides", h.proxy(h.schedulingURL, "/internal/v1/admin/barbers/:barber_id/overrides", "admin"))
	app.Patch("/api/v1/admin/barbers/:barber_id/overrides/:id", h.proxy(h.schedulingURL, "/internal/v1/admin/barbers/:barber_id/overrides/:id", "admin"))
	app.Delete("/api/v1/admin/barbers/:barber_id/overrides/:id", h.proxy(h.schedulingURL, "/internal/v1/admin/barbers/:barber_id/overrides/:id", "admin"))
	app.Get("/api/v1/admin/barbers/:barber_id/blocked-periods", h.proxy(h.schedulingURL, "/internal/v1/admin/barbers/:barber_id/blocked-periods", "admin"))
	app.Post("/api/v1/admin/barbers/:barber_id/blocked-periods", h.proxy(h.schedulingURL, "/internal/v1/admin/barbers/:barber_id/blocked-periods", "admin"))
	app.Delete("/api/v1/admin/barbers/:barber_id/blocked-periods/:id", h.proxy(h.schedulingURL, "/internal/v1/admin/barbers/:barber_id/blocked-periods/:id", "admin"))
}
