package handler

import (
	"encoding/json"
	"errors"
	"github.com/barber-appointment/catalog-service/internal/application"
	"github.com/barber-appointment/catalog-service/internal/domain"
	"github.com/barber-appointment/catalog-service/internal/repository"
	"github.com/barber-appointment/platform/adminauth"
	"github.com/barber-appointment/platform/barberclient"
	"github.com/barber-appointment/platform/internalauth"
	"github.com/barber-appointment/platform/tenantctx"
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
)

type Handler struct {
	app    application.Service
	verify internalauth.Verifier
}

func New(r repository.Repository, v internalauth.Verifier, a adminauth.Client, b barberclient.Client) Handler {
	return Handler{application.New(r, a, b), v}
}
func (h Handler) Register(a *fiber.App) {
	a.Get("/internal/v1/public/services", h.public)
	a.Get("/internal/v1/public/barber-services", h.publicBarberServices)
	a.Get("/internal/v1/barbers/:barber_id/services/:service_id/availability", h.availabilityService)
	a.Post("/internal/v1/admin/services", h.save)
	a.Get("/internal/v1/admin/services", h.list)
	a.Get("/internal/v1/admin/services/:id", h.get)
	a.Patch("/internal/v1/admin/services/:id", h.save)
	a.Post("/internal/v1/admin/barbers/:barber_id/services", h.assign)
	a.Get("/internal/v1/admin/barbers/:barber_id/services", h.assignments)
	a.Delete("/internal/v1/admin/barbers/:barber_id/services/:service_id", h.unassign)
}
func (h Handler) tenant(c fiber.Ctx) (tenantctx.Context, error) {
	return adminauth.Context(c, h.verify)
}
func sid(c fiber.Ctx, key string) (uuid.UUID, error) { return uuid.Parse(c.Params(key)) }
func result(c fiber.Ctx, e error) error {
	switch {
	case errors.Is(e, application.ErrInvalid):
		return c.SendStatus(400)
	case errors.Is(e, application.ErrUnauthorized):
		return c.SendStatus(401)
	case errors.Is(e, application.ErrForbidden):
		return c.SendStatus(403)
	case errors.Is(e, application.ErrNotFound):
		return c.SendStatus(404)
	case errors.Is(e, application.ErrConflict):
		return c.SendStatus(409)
	case errors.Is(e, application.ErrUnavailable):
		return c.SendStatus(502)
	default:
		return c.SendStatus(500)
	}
}
func (h Handler) public(c fiber.Ctx) error {
	t, e := h.tenant(c)
	if e != nil {
		return c.SendStatus(401)
	}
	v, e := h.app.Public(c.Context(), t)
	if e != nil {
		return result(c, e)
	}
	return c.JSON(v)
}
func (h Handler) publicBarberServices(c fiber.Ctx) error {
	t, e := h.tenant(c)
	if e != nil {
		return c.SendStatus(401)
	}
	v, e := h.app.PublicBarberServices(c.Context(), t)
	if e != nil {
		return result(c, e)
	}
	return c.JSON(v)
}
func (h Handler) availabilityService(c fiber.Ctx) error {
	t, e := h.tenant(c)
	b, x := sid(c, "barber_id")
	s, y := sid(c, "service_id")
	if e != nil || x != nil || y != nil {
		return c.SendStatus(401)
	}
	v, e := h.app.Available(c.Context(), t, b, s)
	if e != nil {
		return result(c, e)
	}
	return c.JSON(v)
}
func (h Handler) list(c fiber.Ctx) error {
	t, e := h.tenant(c)
	if e != nil {
		return c.SendStatus(401)
	}
	v, e := h.app.List(c.Context(), t, c.Get("Cookie"))
	if e != nil {
		return result(c, e)
	}
	return c.JSON(v)
}
func (h Handler) get(c fiber.Ctx) error {
	t, e := h.tenant(c)
	if e != nil {
		return c.SendStatus(401)
	}
	i, e := sid(c, "id")
	if e != nil {
		return c.SendStatus(400)
	}
	v, e := h.app.Get(c.Context(), t, c.Get("Cookie"), i)
	if e != nil {
		return result(c, e)
	}
	return c.JSON(v)
}
func (h Handler) save(c fiber.Ctx) error {
	t, e := h.tenant(c)
	if e != nil {
		return c.SendStatus(403)
	}
	var in domain.ServiceInput
	if json.Unmarshal(c.Body(), &in) != nil {
		return c.SendStatus(400)
	}
	i := uuid.Nil
	if c.Method() == "PATCH" {
		i, e = sid(c, "id")
		if e != nil {
			return c.SendStatus(400)
		}
	}
	v, e := h.app.Save(c.Context(), t, c.Get("Cookie"), i, in)
	if e != nil {
		return result(c, e)
	}
	if i == uuid.Nil {
		return c.Status(201).JSON(v)
	}
	return c.JSON(v)
}
func (h Handler) assign(c fiber.Ctx) error {
	t, e := h.tenant(c)
	if e != nil {
		return c.SendStatus(403)
	}
	b, e := sid(c, "barber_id")
	if e != nil {
		return c.SendStatus(400)
	}
	var in domain.AssignmentInput
	if json.Unmarshal(c.Body(), &in) != nil {
		return c.SendStatus(400)
	}
	if e = h.app.Assign(c.Context(), t, c.Get("Cookie"), b, in.ServiceID); e != nil {
		return result(c, e)
	}
	return c.SendStatus(201)
}
func (h Handler) assignments(c fiber.Ctx) error {
	t, e := h.tenant(c)
	if e != nil {
		return c.SendStatus(401)
	}
	b, e := sid(c, "barber_id")
	if e != nil {
		return c.SendStatus(400)
	}
	v, e := h.app.Assignments(c.Context(), t, c.Get("Cookie"), b)
	if e != nil {
		return result(c, e)
	}
	return c.JSON(v)
}
func (h Handler) unassign(c fiber.Ctx) error {
	t, e := h.tenant(c)
	if e != nil {
		return c.SendStatus(403)
	}
	b, x := sid(c, "barber_id")
	s, y := sid(c, "service_id")
	if x != nil || y != nil {
		return c.SendStatus(400)
	}
	if e = h.app.Unassign(c.Context(), t, c.Get("Cookie"), b, s); e != nil {
		return result(c, e)
	}
	return c.SendStatus(204)
}
