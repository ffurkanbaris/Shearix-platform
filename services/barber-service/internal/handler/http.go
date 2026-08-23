package handler

import (
	"encoding/json"
	"errors"

	"github.com/barber-appointment/barber-service/internal/application"
	"github.com/barber-appointment/barber-service/internal/domain"
	"github.com/barber-appointment/barber-service/internal/repository"
	"github.com/barber-appointment/platform/adminauth"
	"github.com/barber-appointment/platform/internalauth"
	"github.com/barber-appointment/platform/tenantctx"
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
)

type Handler struct {
	app      application.Service
	verifier internalauth.Verifier
}

func New(repo repository.Repository, verifier internalauth.Verifier, auth adminauth.Client) Handler {
	return Handler{application.New(repo, auth), verifier}
}
func (h Handler) Register(app *fiber.App) {
	app.Get("/internal/v1/barbers/:id/exists", h.barberExists)
	app.Get("/internal/v1/barbers/:id/scheduling-access", h.schedulingAccess)
	app.Get("/internal/v1/branches/:branch_id/barbers/:barber_id/booking-access", h.branchBarberAccess)
	app.Get("/internal/v1/public/branches", h.publicBranches)
	app.Get("/internal/v1/public/barbers", h.publicBarbers)
	app.Post("/internal/v1/admin/branches", h.branchSave)
	app.Get("/internal/v1/admin/branches", h.branchList)
	app.Get("/internal/v1/admin/branches/:id", h.branchGet)
	app.Patch("/internal/v1/admin/branches/:id", h.branchSave)
	app.Post("/internal/v1/admin/barbers", h.barberSave)
	app.Get("/internal/v1/admin/barbers", h.barberList)
	app.Get("/internal/v1/admin/barbers/:id", h.barberGet)
	app.Patch("/internal/v1/admin/barbers/:id", h.barberSave)
	app.Post("/internal/v1/admin/barbers/:barber_id/link-identity", h.linkIdentity)
	app.Delete("/internal/v1/admin/barbers/:barber_id/link-identity", h.unlinkIdentity)
}
func (h Handler) tenant(c fiber.Ctx) (tenantctx.Context, error) {
	return adminauth.Context(c, h.verifier)
}
func id(c fiber.Ctx, key string) (uuid.UUID, error) { return uuid.Parse(c.Params(key)) }
func respond(c fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, application.ErrInvalid):
		return c.SendStatus(400)
	case errors.Is(err, application.ErrUnauthorized):
		return c.SendStatus(401)
	case errors.Is(err, application.ErrForbidden):
		return c.SendStatus(403)
	case errors.Is(err, application.ErrNotFound):
		return c.SendStatus(404)
	case errors.Is(err, application.ErrConflict):
		return c.SendStatus(409)
	case errors.Is(err, application.ErrUnprocessable):
		return c.SendStatus(422)
	default:
		return c.SendStatus(500)
	}
}
func (h Handler) publicBranches(c fiber.Ctx) error {
	t, e := h.tenant(c)
	if e != nil {
		return c.SendStatus(401)
	}
	v, e := h.app.PublicBranches(c.Context(), t)
	if e != nil {
		return respond(c, e)
	}
	return c.JSON(v)
}
func (h Handler) publicBarbers(c fiber.Ctx) error {
	t, e := h.tenant(c)
	if e != nil {
		return c.SendStatus(401)
	}
	v, e := h.app.PublicBarbers(c.Context(), t)
	if e != nil {
		return respond(c, e)
	}
	return c.JSON(v)
}
func (h Handler) branchList(c fiber.Ctx) error {
	t, e := h.tenant(c)
	if e != nil {
		return c.SendStatus(401)
	}
	v, e := h.app.Branches(c.Context(), t, c.Get("Cookie"))
	if e != nil {
		return respond(c, e)
	}
	return c.JSON(v)
}
func (h Handler) branchGet(c fiber.Ctx) error {
	t, e := h.tenant(c)
	if e != nil {
		return c.SendStatus(401)
	}
	i, e := id(c, "id")
	if e != nil {
		return c.SendStatus(400)
	}
	v, e := h.app.Branch(c.Context(), t, c.Get("Cookie"), i)
	if e != nil {
		return respond(c, e)
	}
	return c.JSON(v)
}
func (h Handler) branchSave(c fiber.Ctx) error {
	t, e := h.tenant(c)
	if e != nil {
		return c.SendStatus(403)
	}
	var in domain.BranchInput
	if json.Unmarshal(c.Body(), &in) != nil {
		return c.SendStatus(400)
	}
	i := uuid.Nil
	if c.Method() == "PATCH" {
		i, e = id(c, "id")
		if e != nil {
			return c.SendStatus(400)
		}
	}
	v, e := h.app.SaveBranch(c.Context(), t, c.Get("Cookie"), i, in)
	if e != nil {
		return respond(c, e)
	}
	if i == uuid.Nil {
		return c.Status(201).JSON(v)
	}
	return c.JSON(v)
}
func (h Handler) barberList(c fiber.Ctx) error {
	t, e := h.tenant(c)
	if e != nil {
		return c.SendStatus(401)
	}
	v, e := h.app.Barbers(c.Context(), t, c.Get("Cookie"))
	if e != nil {
		return respond(c, e)
	}
	return c.JSON(v)
}
func (h Handler) barberGet(c fiber.Ctx) error {
	t, e := h.tenant(c)
	if e != nil {
		return c.SendStatus(401)
	}
	i, e := id(c, "id")
	if e != nil {
		return c.SendStatus(400)
	}
	v, e := h.app.Barber(c.Context(), t, c.Get("Cookie"), i)
	if e != nil {
		return respond(c, e)
	}
	return c.JSON(v)
}
func (h Handler) barberSave(c fiber.Ctx) error {
	t, e := h.tenant(c)
	if e != nil {
		return c.SendStatus(403)
	}
	var raw map[string]json.RawMessage
	var in domain.BarberInput
	if json.Unmarshal(c.Body(), &raw) != nil || raw["identity_id"] != nil || json.Unmarshal(c.Body(), &in) != nil {
		return c.SendStatus(400)
	}
	i := uuid.Nil
	if c.Method() == "PATCH" {
		i, e = id(c, "id")
		if e != nil {
			return c.SendStatus(400)
		}
	}
	v, e := h.app.SaveBarber(c.Context(), t, c.Get("Cookie"), i, in)
	if e != nil {
		return respond(c, e)
	}
	if i == uuid.Nil {
		return c.Status(201).JSON(v)
	}
	return c.JSON(v)
}
func (h Handler) branchBarberAccess(c fiber.Ctx) error {
	t, e := h.tenant(c)
	if e != nil {
		return c.SendStatus(401)
	}
	b, e1 := id(c, "branch_id")
	r, e2 := id(c, "barber_id")
	if e1 != nil || e2 != nil {
		return c.SendStatus(401)
	}
	if e = h.app.BookingAccess(c.Context(), t, b, r); e != nil {
		return respond(c, e)
	}
	return c.SendStatus(204)
}
func (h Handler) schedulingAccess(c fiber.Ctx) error {
	t, e := h.tenant(c)
	if e != nil {
		return c.SendStatus(401)
	}
	i, e := id(c, "id")
	if e != nil {
		return c.SendStatus(401)
	}
	identity, e := h.app.SchedulingAccess(c.Context(), t, i)
	if e != nil {
		return respond(c, e)
	}
	return c.JSON(fiber.Map{"identity_id": identity})
}
func (h Handler) barberExists(c fiber.Ctx) error {
	t, e := h.tenant(c)
	if e != nil {
		return c.SendStatus(401)
	}
	i, e := id(c, "id")
	if e != nil {
		return c.SendStatus(401)
	}
	if e = h.app.Exists(c.Context(), t, i); e != nil {
		return respond(c, e)
	}
	return c.SendStatus(204)
}
func (h Handler) linkIdentity(c fiber.Ctx) error {
	t, e := h.tenant(c)
	if e != nil {
		return c.SendStatus(401)
	}
	bid, e := id(c, "barber_id")
	if e != nil {
		return c.SendStatus(400)
	}
	var in domain.LinkIdentityInput
	if json.Unmarshal(c.Body(), &in) != nil {
		return c.SendStatus(400)
	}
	if e = h.app.LinkIdentity(c.Context(), t, c.Get("Cookie"), bid, in.IdentityID); e != nil {
		return respond(c, e)
	}
	return c.SendStatus(204)
}
func (h Handler) unlinkIdentity(c fiber.Ctx) error {
	t, e := h.tenant(c)
	if e != nil {
		return c.SendStatus(401)
	}
	bid, e := id(c, "barber_id")
	if e != nil {
		return c.SendStatus(400)
	}
	if e = h.app.UnlinkIdentity(c.Context(), t, c.Get("Cookie"), bid); e != nil {
		return respond(c, e)
	}
	return c.SendStatus(204)
}
