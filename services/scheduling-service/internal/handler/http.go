package handler

import (
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/barber-appointment/platform/adminauth"
	"github.com/barber-appointment/platform/internalauth"
	"github.com/barber-appointment/platform/tenantctx"
	"github.com/barber-appointment/scheduling-service/internal/application"
	"github.com/barber-appointment/scheduling-service/internal/domain"
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
)

type Handler struct {
	app    application.Service
	verify internalauth.Verifier
	auth   adminauth.Client
}

func New(app application.Service, verify internalauth.Verifier, auth adminauth.Client) Handler {
	return Handler{app, verify, auth}
}
func (h Handler) Register(a *fiber.App) {
	a.Get("/internal/v1/public/availability", h.availability)
	a.Get("/internal/v1/admin/availability", h.adminAvailability)
	a.Get("/internal/v1/admin/barbers/:barber_id/schedule", h.schedule)
	a.Put("/internal/v1/admin/barbers/:barber_id/working-hours", h.hours)
	a.Get("/internal/v1/admin/barbers/:barber_id/overrides", h.overrides)
	a.Post("/internal/v1/admin/barbers/:barber_id/overrides", h.saveOverride)
	a.Patch("/internal/v1/admin/barbers/:barber_id/overrides/:id", h.saveOverride)
	a.Delete("/internal/v1/admin/barbers/:barber_id/overrides/:id", h.deleteOverride)
	a.Get("/internal/v1/admin/barbers/:barber_id/blocked-periods", h.blocks)
	a.Post("/internal/v1/admin/barbers/:barber_id/blocked-periods", h.saveBlock)
	a.Delete("/internal/v1/admin/barbers/:barber_id/blocked-periods/:id", h.deleteBlock)
}
func tenant(c fiber.Ctx, v internalauth.Verifier) (tenantctx.Context, error) {
	return adminauth.Context(c, v)
}
func barberID(c fiber.Ctx) (uuid.UUID, error) { return uuid.Parse(c.Params("barber_id")) }
func id(c fiber.Ctx) (uuid.UUID, error)       { return uuid.Parse(c.Params("id")) }
func (h Handler) admin(c fiber.Ctx) (tenantctx.Context, application.Principal, uuid.UUID, error) {
	t, err := tenant(c, h.verify)
	if err != nil || t.AppType != "admin" {
		return t, application.Principal{}, uuid.Nil, application.ErrForbidden
	}
	b, err := barberID(c)
	if err != nil {
		return t, application.Principal{}, b, application.ErrInvalid
	}
	p, err := h.auth.Authenticate(c.Context(), t, c.Get("Cookie"))
	if err != nil {
		return t, application.Principal{}, b, application.ErrForbidden
	}
	return t, application.Principal{IdentityID: p.IdentityID, Role: p.Role}, b, nil
}
func sendError(c fiber.Ctx, err error, conflictStatus int) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, application.ErrInvalid):
		return c.SendStatus(400)
	case errors.Is(err, application.ErrForbidden):
		return c.SendStatus(403)
	case errors.Is(err, application.ErrNotFound):
		return c.SendStatus(404)
	case errors.Is(err, application.ErrConflict):
		return c.SendStatus(conflictStatus)
	case errors.Is(err, application.ErrOccupancyUnavailable):
		return c.Status(503).JSON(fiber.Map{"error": "occupancy_unavailable"})
	case errors.Is(err, application.ErrUnavailable):
		return c.SendStatus(502)
	default:
		return c.SendStatus(500)
	}
}
func (h Handler) schedule(c fiber.Ctx) error {
	t, p, b, err := h.admin(c)
	if err != nil {
		return sendError(c, err, 409)
	}
	v, err := h.app.Schedule(c.Context(), t, p, b)
	if err != nil {
		return sendError(c, err, 409)
	}
	return c.JSON(v)
}
func (h Handler) hours(c fiber.Ctx) error {
	t, p, b, err := h.admin(c)
	if err != nil {
		return sendError(c, err, 409)
	}
	var in domain.WorkingHoursInput
	if json.Unmarshal(c.Body(), &in) != nil {
		return c.SendStatus(400)
	}
	if err = h.app.ReplaceHours(c.Context(), t, p, b, in); err != nil {
		return sendError(c, err, 409)
	}
	return c.SendStatus(204)
}
func (h Handler) overrides(c fiber.Ctx) error {
	t, p, b, err := h.admin(c)
	if err != nil {
		return sendError(c, err, 409)
	}
	v, err := h.app.Overrides(c.Context(), t, p, b)
	if err != nil {
		return sendError(c, err, 409)
	}
	return c.JSON(v)
}
func (h Handler) saveOverride(c fiber.Ctx) error {
	t, p, b, err := h.admin(c)
	if err != nil {
		return sendError(c, err, 409)
	}
	var in domain.OverrideInput
	if json.Unmarshal(c.Body(), &in) != nil {
		return c.SendStatus(400)
	}
	oid := uuid.Nil
	if c.Method() == "PATCH" {
		oid, err = id(c)
		if err != nil {
			return c.SendStatus(400)
		}
	}
	v, err := h.app.SaveOverride(c.Context(), t, p, b, oid, in)
	if err != nil {
		return sendError(c, err, 409)
	}
	if oid == uuid.Nil {
		return c.Status(201).JSON(v)
	}
	return c.JSON(v)
}
func (h Handler) deleteOverride(c fiber.Ctx) error {
	t, p, b, err := h.admin(c)
	if err != nil {
		return sendError(c, err, 500)
	}
	oid, err := id(c)
	if err != nil {
		return c.SendStatus(400)
	}
	if err = h.app.DeleteOverride(c.Context(), t, p, b, oid); err != nil {
		return sendError(c, err, 500)
	}
	return c.SendStatus(204)
}
func (h Handler) blocks(c fiber.Ctx) error {
	t, p, b, err := h.admin(c)
	if err != nil {
		return sendError(c, err, 409)
	}
	v, err := h.app.Blocks(c.Context(), t, p, b)
	if err != nil {
		return sendError(c, err, 409)
	}
	return c.JSON(v)
}
func (h Handler) saveBlock(c fiber.Ctx) error {
	t, p, b, err := h.admin(c)
	if err != nil {
		return sendError(c, err, 409)
	}
	var in domain.BlockedPeriodInput
	if json.Unmarshal(c.Body(), &in) != nil {
		return c.SendStatus(400)
	}
	v, err := h.app.SaveBlock(c.Context(), t, p, b, in)
	if err != nil {
		return sendError(c, err, 409)
	}
	return c.Status(201).JSON(v)
}
func (h Handler) deleteBlock(c fiber.Ctx) error {
	t, p, b, err := h.admin(c)
	if err != nil {
		return sendError(c, err, 500)
	}
	bid, err := id(c)
	if err != nil {
		return c.SendStatus(400)
	}
	if err = h.app.DeleteBlock(c.Context(), t, p, b, bid); err != nil {
		return sendError(c, err, 500)
	}
	return c.SendStatus(204)
}
func availabilityInput(c fiber.Ctx, t tenantctx.Context, p *application.Principal) (application.AvailabilityQuery, error) {
	b, e := uuid.Parse(c.Query("barber_id"))
	s, x := uuid.Parse(c.Query("service_id"))
	date, y := time.Parse("2006-01-02", c.Query("date"))
	if e != nil || x != nil || y != nil {
		return application.AvailabilityQuery{}, application.ErrInvalid
	}
	return application.AvailabilityQuery{Tenant: t, Principal: p, BarberID: b, ServiceID: s, Date: date}, nil
}
func (h Handler) availability(c fiber.Ctx) error {
	t, err := tenant(c, h.verify)
	if err != nil || t.AppType != "booking" {
		return c.SendStatus(401)
	}
	q, err := availabilityInput(c, t, nil)
	if err != nil {
		return c.SendStatus(400)
	}
	v, err := h.app.Availability(c.Context(), q)
	if err != nil {
		return sendError(c, err, 500)
	}
	return c.JSON(v)
}
func (h Handler) adminAvailability(c fiber.Ctx) error {
	t, err := tenant(c, h.verify)
	if err != nil || t.AppType != "admin" {
		return c.SendStatus(401)
	}
	p, err := h.auth.Authenticate(c.Context(), t, c.Get("Cookie"))
	if err != nil {
		return c.SendStatus(403)
	}
	principal := application.Principal{IdentityID: p.IdentityID, Role: p.Role}
	q, err := availabilityInput(c, t, &principal)
	if err != nil {
		return c.SendStatus(400)
	}
	v, err := h.app.Availability(c.Context(), q)
	if err != nil {
		return sendError(c, err, 500)
	}
	return c.JSON(v)
}
func Interval(v string) int {
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return 15
	}
	return n
}
