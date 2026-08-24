package handler

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/barber-appointment/customer-service/internal/application"
	customerclient "github.com/barber-appointment/customer-service/internal/client"
	"github.com/barber-appointment/customer-service/internal/repository"
	"github.com/barber-appointment/platform/adminauth"
	platformemail "github.com/barber-appointment/platform/email"
	"github.com/barber-appointment/platform/internalauth"
	"github.com/barber-appointment/platform/obsmetrics"
	"github.com/barber-appointment/platform/tenantctx"
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"os"
	"strings"
	"time"
)

const cookieName = "__Host-barber_customer_session"

func sessionCookieName() string {
	if os.Getenv("APP_ENV") == "development" {
		return "barber_customer_session"
	}
	return cookieName
}

// rateLimiter mirrors auth-service's minimal limiter interface. It is
// redefined here (rather than importing auth-service's internal package)
// because services must not import each other's internal packages; the
// shape matches platform/ratelimit.Redis.Allow so ratelimit.NewRedis(...)
// satisfies it directly.
type rateLimiter interface {
	Allow(context.Context, string, int, time.Duration) (bool, error)
}

type Handler struct {
	app           application.Service
	Verify        internalauth.Verifier
	InternalToken string
	limiter       rateLimiter
	metrics       *obsmetrics.Registry
}

func New(r repository.Repository, v internalauth.Verifier, sender platformemail.Sender, token, appointmentURL string, metrics *obsmetrics.Registry, limiters ...rateLimiter) Handler {
	h := Handler{app: application.New(r, sender, customerclient.NewAppointments(appointmentURL, token)), Verify: v, InternalToken: token, metrics: metrics}
	if len(limiters) > 0 {
		h.limiter = limiters[0]
	}
	return h
}
func (h Handler) Register(a *fiber.App) {
	a.Post("/internal/v1/public/customer/auth/register", h.register)
	a.Post("/internal/v1/public/customer/auth/login", h.login)
	a.Post("/internal/v1/public/customer/auth/logout", h.logout)
	a.Get("/internal/v1/public/customer/auth/me", h.me)
	a.Post("/internal/v1/public/customer/auth/change-password", h.changePassword)
	a.Post("/internal/v1/public/customer/auth/forgot-password", h.forgot)
	a.Post("/internal/v1/customer/resolve", h.resolveGuest)
	a.Get("/internal/v1/customer/:id", h.internalCustomer)
	a.Get("/internal/v1/public/customer/profile", h.profile)
	a.Patch("/internal/v1/public/customer/profile", h.profilePatch)
	a.Get("/internal/v1/public/customer/appointments/upcoming", h.appointments)
	a.Get("/internal/v1/public/customer/appointments/history", h.appointments)
	a.Get("/internal/v1/public/customer/appointments/:id", h.appointments)
	a.Post("/internal/v1/public/customer/appointments/:id/cancel", h.appointments)
	a.Get("/internal/v1/internal/customer/session", h.internalSession)
}
func (h Handler) tenant(c fiber.Ctx) (tenantctx.Context, error) {
	t, err := adminauth.Context(c, h.Verify)
	if err != nil || t.AppType != "booking" {
		return t, application.ErrUnauthorized
	}
	return t, nil
}
func appError(c fiber.Ctx, err error) error {
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
	case errors.Is(err, application.ErrUnavailable):
		return c.SendStatus(503)
	default:
		return c.SendStatus(500)
	}
}

// limited applies a fixed-window limit of 10 requests per minute per
// tenant+operation, matching auth-service's "auth-rate:" pattern. A nil
// limiter (no limiter configured) fails closed, same as auth-service.
func (h Handler) limited(c fiber.Ctx, tenant tenantctx.Context, operation string) (bool, error) {
	if h.limiter == nil {
		return false, nil
	}
	allowed, err := h.limiter.Allow(c.Context(), "customer-rate:"+tenant.TenantID.String()+":"+operation, 10, time.Minute)
	if h.metrics != nil {
		switch {
		case err != nil:
			h.metrics.RateLimitError(operation)
		case allowed:
			h.metrics.RateLimitAllowed(operation)
		default:
			h.metrics.RateLimitBlocked(operation)
		}
	}
	return allowed, err
}

func (h Handler) register(c fiber.Ctx) error {
	t, err := h.tenant(c)
	if err != nil {
		return c.SendStatus(403)
	}
	if allowed, err := h.limited(c, t, "register"); err != nil {
		return c.SendStatus(503)
	} else if !allowed {
		return c.SendStatus(429)
	}
	var in application.Registration
	if json.Unmarshal(c.Body(), &in) != nil {
		return c.SendStatus(400)
	}
	result, err := h.app.Register(c.Context(), t.TenantID, in)
	if err != nil {
		return appError(c, err)
	}
	status := 201
	if result.Retried {
		status = 200
	}
	return c.Status(status).JSON(fiber.Map{"status": "created", "message": "Initial login credentials have been sent to your email."})
}
func (h Handler) login(c fiber.Ctx) error {
	t, err := h.tenant(c)
	if err != nil {
		return c.SendStatus(403)
	}
	if allowed, err := h.limited(c, t, "login"); err != nil {
		return c.SendStatus(503)
	} else if !allowed {
		return c.SendStatus(429)
	}
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if json.Unmarshal(c.Body(), &in) != nil {
		return c.SendStatus(400)
	}
	result, err := h.app.Login(c.Context(), t.TenantID, in.Email, in.Password)
	if err != nil {
		return appError(c, err)
	}
	c.Cookie(&fiber.Cookie{Name: sessionCookieName(), Value: result.Token, HTTPOnly: true, Secure: os.Getenv("APP_ENV") != "development", SameSite: "Lax", Path: "/", MaxAge: int(time.Until(result.Expires).Seconds())})
	return c.JSON(result.Customer)
}
func (h Handler) logout(c fiber.Ctx) error {
	t, err := h.tenant(c)
	if err != nil {
		return c.SendStatus(401)
	}
	h.app.Logout(c.Context(), t.TenantID, c.Cookies(sessionCookieName()))
	c.ClearCookie(sessionCookieName())
	return c.SendStatus(204)
}
func (h Handler) current(c fiber.Ctx) (tenantctx.Context, string, error) {
	t, err := h.tenant(c)
	if err != nil {
		return t, "", err
	}
	token := c.Cookies(sessionCookieName())
	if token == "" {
		return t, "", application.ErrUnauthorized
	}
	return t, token, nil
}
func (h Handler) me(c fiber.Ctx) error {
	t, token, err := h.current(c)
	if err != nil {
		return c.SendStatus(401)
	}
	customer, err := h.app.Current(c.Context(), t.TenantID, token)
	if err != nil {
		return c.SendStatus(401)
	}
	return c.JSON(customer)
}
func (h Handler) profile(c fiber.Ctx) error {
	t, token, err := h.current(c)
	if err != nil {
		return c.SendStatus(401)
	}
	customer, err := h.app.Profile(c.Context(), t.TenantID, token)
	if err != nil {
		return appError(c, err)
	}
	return c.JSON(customer)
}
func (h Handler) profilePatch(c fiber.Ctx) error {
	t, token, err := h.current(c)
	if err != nil {
		return c.SendStatus(401)
	}
	var in struct {
		Name string `json:"name"`
	}
	if json.Unmarshal(c.Body(), &in) != nil {
		return c.SendStatus(400)
	}
	customer, err := h.app.UpdateProfile(c.Context(), t.TenantID, token, in.Name)
	if err != nil {
		return appError(c, err)
	}
	return c.JSON(customer)
}
func (h Handler) changePassword(c fiber.Ctx) error {
	t, token, err := h.current(c)
	if err != nil {
		return c.SendStatus(401)
	}
	var in struct {
		Current string `json:"current_password"`
		Next    string `json:"new_password"`
	}
	if json.Unmarshal(c.Body(), &in) != nil {
		return c.SendStatus(400)
	}
	if err = h.app.ChangePassword(c.Context(), t.TenantID, token, in.Current, in.Next); err != nil {
		return appError(c, err)
	}
	return c.SendStatus(204)
}
func (h Handler) forgot(c fiber.Ctx) error {
	t, err := h.tenant(c)
	if err != nil {
		return c.SendStatus(403)
	}
	// Rate limiting is keyed by tenant+operation only, never by the
	// requested email, so it adds no signal that distinguishes existing
	// from non-existing accounts.
	if allowed, err := h.limited(c, t, "forgot-password"); err != nil {
		return c.SendStatus(503)
	} else if !allowed {
		return c.SendStatus(429)
	}
	var in struct {
		Email string `json:"email"`
	}
	if json.Unmarshal(c.Body(), &in) != nil {
		return c.SendStatus(204)
	}
	if err = h.app.ForgotPassword(c.Context(), t.TenantID, in.Email); err != nil {
		return appError(c, err)
	}
	return c.SendStatus(204)
}
func (h Handler) internalSession(c fiber.Ctx) error {
	t, err := h.tenant(c)
	if err != nil {
		return c.SendStatus(401)
	}
	customer, err := h.app.InternalSession(c.Context(), t.TenantID, c.Get("X-Customer-Session"))
	if err != nil {
		return appError(c, err)
	}
	return c.JSON(customer)
}
func (h Handler) appointments(c fiber.Ctx) error {
	t, token, err := h.current(c)
	if err != nil {
		return c.SendStatus(401)
	}
	path := strings.TrimPrefix(c.Path(), "/internal/v1/public/customer/appointments")
	result, err := h.app.Appointments(c.Context(), t, token, c.Method(), path, string(c.Request().URI().QueryString()))
	if err != nil {
		return appError(c, err)
	}
	return c.Status(result.Status).Send(result.Body)
}
func (h Handler) resolveGuest(c fiber.Ctx) error {
	if c.Get(internalauth.HeaderName) != h.InternalToken {
		return c.SendStatus(401)
	}
	t, err := adminauth.Context(c, h.Verify)
	if err != nil {
		return c.SendStatus(401)
	}
	var in struct {
		Name  string `json:"name"`
		Phone string `json:"phone"`
	}
	if json.Unmarshal(c.Body(), &in) != nil {
		return c.SendStatus(400)
	}
	customer, err := h.app.ResolveGuest(c.Context(), t.TenantID, in.Name, in.Phone)
	if err != nil {
		return appError(c, err)
	}
	return c.JSON(customer)
}
func (h Handler) internalCustomer(c fiber.Ctx) error {
	if c.Get(internalauth.HeaderName) != h.InternalToken {
		return c.SendStatus(401)
	}
	t, err := adminauth.Context(c, h.Verify)
	if err != nil {
		return c.SendStatus(401)
	}
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.SendStatus(400)
	}
	customer, err := h.app.ByID(c.Context(), t.TenantID, id)
	if err != nil {
		return appError(c, err)
	}
	return c.JSON(customer)
}
