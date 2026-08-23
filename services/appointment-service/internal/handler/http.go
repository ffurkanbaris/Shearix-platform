package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/barber-appointment/appointment-service/internal/application"
	"github.com/barber-appointment/appointment-service/internal/domain"
	"github.com/barber-appointment/platform/adminauth"
	"github.com/barber-appointment/platform/internalauth"
	"github.com/barber-appointment/platform/tenantctx"
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
)

type Handler struct {
	booking   application.BookingService
	lifecycle application.LifecycleService
	query     application.QueryService
	verify    internalauth.Verifier
	auth      adminauth.Client
}

func New(booking application.BookingService, lifecycle application.LifecycleService, query application.QueryService, verify internalauth.Verifier, auth adminauth.Client) Handler {
	return Handler{booking: booking, lifecycle: lifecycle, query: query, verify: verify, auth: auth}
}

func (h Handler) Register(app *fiber.App) {
	app.Get("/internal/v1/occupancy", h.occ)
	app.Get("/internal/v1/appointments/:id/notification-recipient", h.notificationRecipient)
	app.Post("/internal/v1/public/appointments", h.pubCreate)
	app.Get("/internal/v1/public/appointments/:id", h.pubGet)
	app.Post("/internal/v1/public/appointments/:id/cancel", h.pubCancel)
	app.Get("/internal/v1/customer/appointments/upcoming", h.customerList(true))
	app.Get("/internal/v1/customer/appointments/history", h.customerList(false))
	app.Get("/internal/v1/customer/appointments/:id", h.customerGet)
	app.Post("/internal/v1/customer/appointments/:id/cancel", h.customerCancel)
	app.Get("/internal/v1/admin/appointments", h.list)
	app.Get("/internal/v1/admin/appointments/:id", h.get)
	app.Post("/internal/v1/admin/appointments", h.admCreate)
	app.Post("/internal/v1/admin/appointments/:id/reschedule", h.reschedule)
	for _, transition := range []struct{ path, status string }{{"cancel", "cancelled"}, {"confirm", "confirmed"}, {"complete", "completed"}, {"no-show", "no_show"}} {
		path, status := transition.path, transition.status
		app.Post("/internal/v1/admin/appointments/:id/"+path, func(c fiber.Ctx) error { return h.transition(c, status) })
	}
}

func (h Handler) trusted(c fiber.Ctx, appType string) (tenantctx.Context, error) {
	t, err := adminauth.Context(c, h.verify)
	if err != nil || t.AppType != appType {
		return t, application.ErrUnauthorized
	}
	return t, nil
}

func (h Handler) admin(c fiber.Ctx) (tenantctx.Context, application.Principal, error) {
	t, err := h.trusted(c, "admin")
	if err != nil {
		return t, application.Principal{}, err
	}
	p, err := h.auth.Authenticate(c.Context(), t, c.Get("Cookie"))
	if err != nil {
		return t, application.Principal{}, application.ErrUnauthorized
	}
	return t, application.Principal{Kind: "admin", IdentityID: p.IdentityID, Role: p.Role}, nil
}

func (h Handler) customer(c fiber.Ctx) (tenantctx.Context, uuid.UUID, error) {
	t, err := h.trusted(c, "booking")
	if err != nil {
		return t, uuid.Nil, err
	}
	id, err := uuid.Parse(c.Get("X-Customer-ID"))
	if err != nil || id == uuid.Nil {
		return t, uuid.Nil, application.ErrUnauthorized
	}
	return t, id, nil
}

func parseCreate(c fiber.Ctx) (domain.CreateInput, error) {
	var in domain.CreateInput
	if err := json.Unmarshal(c.Body(), &in); err != nil {
		return in, application.ErrInvalid
	}
	return in, nil
}

func (h Handler) pubCreate(c fiber.Ctx) error {
	t, err := h.trusted(c, "booking")
	if err != nil {
		return c.SendStatus(http.StatusUnauthorized)
	}
	in, err := parseCreate(c)
	if err != nil {
		return c.SendStatus(http.StatusBadRequest)
	}
	p := application.Principal{Kind: "guest"}
	if raw := c.Get("X-Customer-ID"); raw != "" {
		id, parseErr := uuid.Parse(raw)
		if parseErr != nil || id == uuid.Nil {
			return c.SendStatus(http.StatusUnauthorized)
		}
		p = application.Principal{Kind: "customer", CustomerID: &id}
	}
	a, err := h.booking.Create(c.Context(), application.CreateCommand{Tenant: t, Principal: p, Operation: "public.create_appointment", IdempotencyKey: c.Get("Idempotency-Key"), Input: in})
	return appointmentResult(c, a, err, http.StatusCreated)
}

func (h Handler) admCreate(c fiber.Ctx) error {
	t, p, err := h.admin(c)
	if err != nil {
		return c.SendStatus(http.StatusForbidden)
	}
	in, err := parseCreate(c)
	if err != nil {
		return c.SendStatus(http.StatusBadRequest)
	}
	a, err := h.booking.Create(c.Context(), application.CreateCommand{Tenant: t, Principal: p, Operation: "admin.create_appointment", IdempotencyKey: c.Get("Idempotency-Key"), Input: in})
	return appointmentResult(c, a, err, http.StatusCreated)
}

func appointmentResult(c fiber.Ctx, a domain.Appointment, err error, success int) error {
	switch {
	case err == nil:
		return c.Status(success).JSON(a)
	case errors.Is(err, application.ErrInvalidIdempotencyKey):
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": "invalid_idempotency_key"})
	case errors.Is(err, application.ErrInvalid):
		return c.SendStatus(http.StatusBadRequest)
	case errors.Is(err, application.ErrIdempotencyConflict):
		return c.Status(http.StatusConflict).JSON(fiber.Map{"error": "idempotency_conflict"})
	case errors.Is(err, application.ErrConflict):
		return c.SendStatus(http.StatusConflict)
	case errors.Is(err, application.ErrNotFound):
		return c.SendStatus(http.StatusNotFound)
	case errors.Is(err, application.ErrForbidden):
		return c.SendStatus(http.StatusForbidden)
	case errors.Is(err, application.ErrUnauthorized):
		return c.SendStatus(http.StatusUnauthorized)
	case errors.Is(err, application.ErrUnavailable):
		return c.SendStatus(http.StatusBadGateway)
	default:
		return c.SendStatus(http.StatusInternalServerError)
	}
}

func (h Handler) customerList(upcoming bool) fiber.Handler {
	return func(c fiber.Ctx) error {
		t, id, err := h.customer(c)
		if err != nil {
			return c.SendStatus(http.StatusUnauthorized)
		}
		v, err := h.query.CustomerList(c.Context(), t, id, upcoming, time.Now())
		if errors.Is(err, application.ErrUnavailable) {
			return c.SendStatus(http.StatusBadGateway)
		}
		if err != nil {
			return c.SendStatus(http.StatusInternalServerError)
		}
		return c.JSON(v)
	}
}

func (h Handler) customerGet(c fiber.Ctx) error {
	t, customerID, err := h.customer(c)
	if err != nil {
		return c.SendStatus(http.StatusUnauthorized)
	}
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.SendStatus(http.StatusBadRequest)
	}
	a, err := h.query.CustomerGet(c.Context(), t.TenantID, customerID, id)
	return appointmentResult(c, a, err, http.StatusOK)
}
func (h Handler) customerCancel(c fiber.Ctx) error {
	t, customerID, err := h.customer(c)
	if err != nil {
		return c.SendStatus(http.StatusUnauthorized)
	}
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.SendStatus(http.StatusBadRequest)
	}
	a, err := h.lifecycle.CancelCustomer(c.Context(), t, customerID, id, time.Now())
	return appointmentResult(c, a, err, http.StatusOK)
}
func (h Handler) pubGet(c fiber.Ctx) error {
	t, err := h.trusted(c, "booking")
	if err != nil {
		return c.SendStatus(http.StatusUnauthorized)
	}
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.SendStatus(http.StatusBadRequest)
	}
	a, err := h.query.Get(c.Context(), t.TenantID, id)
	return appointmentResult(c, a, err, http.StatusOK)
}
func (h Handler) get(c fiber.Ctx) error {
	t, p, err := h.admin(c)
	if err != nil {
		return c.SendStatus(http.StatusForbidden)
	}
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.SendStatus(http.StatusBadRequest)
	}
	a, err := h.query.GetAdmin(c.Context(), t.TenantID, id, p)
	return appointmentResult(c, a, err, http.StatusOK)
}
func (h Handler) list(c fiber.Ctx) error {
	t, p, err := h.admin(c)
	if err != nil {
		return c.SendStatus(http.StatusForbidden)
	}
	v, err := h.query.List(c.Context(), t.TenantID, p)
	if errors.Is(err, application.ErrForbidden) {
		return c.SendStatus(http.StatusForbidden)
	}
	if err != nil {
		return c.SendStatus(http.StatusInternalServerError)
	}
	return c.JSON(v)
}
func (h Handler) transition(c fiber.Ctx, status string) error {
	t, p, err := h.admin(c)
	if err != nil {
		return c.SendStatus(http.StatusForbidden)
	}
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.SendStatus(http.StatusBadRequest)
	}
	a, err := h.lifecycle.Transition(c.Context(), t.TenantID, p, id, status)
	return appointmentResult(c, a, err, http.StatusOK)
}
func (h Handler) pubCancel(c fiber.Ctx) error {
	t, customerID, err := h.customer(c)
	if err != nil {
		return c.SendStatus(http.StatusUnauthorized)
	}
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.SendStatus(http.StatusBadRequest)
	}
	a, err := h.lifecycle.CancelCustomer(c.Context(), t, customerID, id, time.Now())
	return appointmentResult(c, a, err, http.StatusOK)
}
func (h Handler) reschedule(c fiber.Ctx) error {
	t, p, err := h.admin(c)
	if err != nil {
		return c.SendStatus(http.StatusForbidden)
	}
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.SendStatus(http.StatusBadRequest)
	}
	var body struct {
		StartAt time.Time `json:"start_at"`
	}
	if json.Unmarshal(c.Body(), &body) != nil || body.StartAt.IsZero() {
		return c.SendStatus(http.StatusBadRequest)
	}
	a, err := h.lifecycle.Reschedule(c.Context(), t, p, id, body.StartAt)
	return appointmentResult(c, a, err, http.StatusOK)
}
func (h Handler) occ(c fiber.Ctx) error {
	t, err := adminauth.Context(c, h.verify)
	barber, barberErr := uuid.Parse(c.Query("barber_id"))
	from, fromErr := time.Parse(time.RFC3339, c.Query("from"))
	to, toErr := time.Parse(time.RFC3339, c.Query("to"))
	if err != nil || barberErr != nil || fromErr != nil || toErr != nil || (t.AppType != "booking" && t.AppType != "admin") {
		return c.SendStatus(http.StatusBadRequest)
	}
	v, err := h.query.Occupancy(c.Context(), t.TenantID, barber, from, to)
	if err != nil {
		return c.SendStatus(http.StatusInternalServerError)
	}
	type occupied struct {
		OccupiedStartAt time.Time `json:"occupied_start_at"`
		OccupiedEndAt   time.Time `json:"occupied_end_at"`
	}
	out := make([]occupied, 0, len(v))
	for _, a := range v {
		out = append(out, occupied{a.OccupiedStartAt, a.OccupiedEndAt})
	}
	return c.JSON(out)
}
func (h Handler) notificationRecipient(c fiber.Ctx) error {
	t, err := h.trusted(c, "booking")
	if err != nil {
		return c.SendStatus(http.StatusUnauthorized)
	}
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.SendStatus(http.StatusBadRequest)
	}
	email, err := h.query.NotificationRecipient(c.Context(), t.TenantID, id)
	switch {
	case errors.Is(err, application.ErrNotFound):
		return c.SendStatus(http.StatusNotFound)
	case errors.Is(err, application.ErrInvalid):
		return c.SendStatus(http.StatusUnprocessableEntity)
	case err != nil:
		return c.SendStatus(http.StatusInternalServerError)
	}
	return c.JSON(fiber.Map{"email": email})
}
