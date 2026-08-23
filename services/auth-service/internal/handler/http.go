package handler

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/barber-appointment/auth-service/internal/domain"
	"github.com/barber-appointment/auth-service/internal/repository"
	"github.com/barber-appointment/auth-service/internal/service"
	"github.com/barber-appointment/platform/internalauth"
	"github.com/barber-appointment/platform/tenantctx"
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
)

const principalKey = "auth.principal"

type rateLimiter interface {
	Allow(context.Context, string, int, time.Duration) (bool, error)
}

// authService is the subset of service.Service the handler depends on. It
// exists so tests can substitute a fake implementation instead of a live
// database-backed service.Service; production wiring passes a real
// service.Service, which satisfies this interface implicitly.
type authService interface {
	Login(context.Context, uuid.UUID, domain.LoginInput) (domain.Session, error)
	Register(context.Context, uuid.UUID, domain.Principal, domain.RegisterInput) (domain.Registration, error)
	Members(context.Context, uuid.UUID) ([]domain.Member, error)
	Member(context.Context, uuid.UUID, uuid.UUID) (domain.Member, error)
	ChangeMemberRole(context.Context, uuid.UUID, domain.Principal, uuid.UUID, domain.Role) (domain.Member, error)
	SetMemberStatus(context.Context, uuid.UUID, domain.Principal, uuid.UUID, bool) (domain.Member, error)
	BarberEligible(context.Context, uuid.UUID, uuid.UUID) (bool, error)
	ChangePassword(context.Context, uuid.UUID, domain.Principal, domain.ChangePasswordInput) error
	ForgotPassword(context.Context, uuid.UUID, domain.ForgotPasswordInput) error
	Current(context.Context, uuid.UUID, string) (domain.Principal, error)
	Logout(context.Context, uuid.UUID, string) error
}

type Handler struct {
	service      authService
	auth         internalauth.Verifier
	cookieName   string
	cookieSecure bool
	limiter      rateLimiter
}

func New(service authService, auth internalauth.Verifier, cookieName string, cookieSecure bool, limiters ...rateLimiter) Handler {
	handler := Handler{service: service, auth: auth, cookieName: cookieName, cookieSecure: cookieSecure}
	if len(limiters) > 0 {
		handler.limiter = limiters[0]
	}
	return handler
}

func (h Handler) Register(app *fiber.App) {
	app.Post("/internal/v1/auth/login", h.login)
	app.Post("/internal/v1/auth/logout", h.logout)
	app.Get("/internal/v1/auth/me", h.Authenticate(), h.me)
	// OWNER alone administers tenant memberships. MANAGER has no implicit
	// identity/role-management authority.
	//
	// RequireCurrentPassword is inserted after Authenticate on every
	// privileged member-management route so an OWNER mid forced-password-reset
	// cannot create staff or grant roles before completing the reset. It is
	// deliberately absent from /me, /change-password (the escape hatch), and
	// /logout, which must stay reachable to a pending-reset session.
	app.Post("/internal/v1/auth/register", h.Authenticate(), RequireCurrentPassword(), RequireRoles(domain.RoleOwner), h.register)
	app.Get("/internal/v1/auth/members", h.Authenticate(), RequireCurrentPassword(), RequireRoles(domain.RoleOwner), h.members)
	app.Get("/internal/v1/auth/members/:id", h.Authenticate(), RequireCurrentPassword(), RequireRoles(domain.RoleOwner), h.member)
	app.Post("/internal/v1/auth/members", h.Authenticate(), RequireCurrentPassword(), RequireRoles(domain.RoleOwner), h.register)
	app.Patch("/internal/v1/auth/members/:id/role", h.Authenticate(), RequireCurrentPassword(), RequireRoles(domain.RoleOwner), h.changeMemberRole)
	app.Post("/internal/v1/auth/members/:id/activate", h.Authenticate(), RequireCurrentPassword(), RequireRoles(domain.RoleOwner), h.activateMember)
	app.Post("/internal/v1/auth/members/:id/deactivate", h.Authenticate(), RequireCurrentPassword(), RequireRoles(domain.RoleOwner), h.deactivateMember)
	app.Get("/internal/v1/auth/members/:id/barber-eligibility", h.barberEligibility)
	app.Post("/internal/v1/auth/change-password", h.Authenticate(), h.changePassword)
	app.Post("/internal/v1/auth/forgot-password", h.forgotPassword)
}

func (h Handler) trustedContext(c fiber.Ctx) (tenantctx.Context, error) {
	if err := h.auth.Verify(c.Get(internalauth.HeaderName)); err != nil {
		return tenantctx.Context{}, err
	}
	context, err := tenantctx.New(c.Get(tenantctx.TenantIDHeader), c.Get(tenantctx.AppTypeHeader), c.Get(tenantctx.RequestIDHeader))
	if err != nil {
		return tenantctx.Context{}, err
	}
	if context.AppType != "admin" {
		return tenantctx.Context{}, errors.New("admin application required")
	}
	return context, nil
}

func (h Handler) login(c fiber.Ctx) error {
	tenant, err := h.trustedContext(c)
	if err != nil {
		return c.SendStatus(fiber.StatusUnauthorized)
	}
	if allowed, err := h.limited(c, tenant, "login"); err != nil {
		return c.SendStatus(fiber.StatusServiceUnavailable)
	} else if !allowed {
		return c.SendStatus(fiber.StatusTooManyRequests)
	}
	var input domain.LoginInput
	if err := json.Unmarshal(c.Body(), &input); err != nil || input.Email == "" || input.Password == "" {
		return c.SendStatus(fiber.StatusBadRequest)
	}
	session, err := h.service.Login(c.Context(), tenant.TenantID, input)
	if errors.Is(err, service.ErrUnauthorized) {
		return c.SendStatus(fiber.StatusUnauthorized)
	}
	if err != nil {
		return c.SendStatus(fiber.StatusInternalServerError)
	}
	h.setCookie(c, session.Token, session.ExpiresAt)
	return c.JSON(session.Principal)
}

func (h Handler) register(c fiber.Ctx) error {
	tenant, err := h.trustedContext(c)
	if err != nil {
		return c.SendStatus(fiber.StatusUnauthorized)
	}
	if allowed, err := h.limited(c, tenant, "register"); err != nil {
		return c.SendStatus(fiber.StatusServiceUnavailable)
	} else if !allowed {
		return c.SendStatus(fiber.StatusTooManyRequests)
	}
	var input domain.RegisterInput
	if err := json.Unmarshal(c.Body(), &input); err != nil {
		return c.SendStatus(fiber.StatusBadRequest)
	}
	principal := c.Locals(principalKey).(domain.Principal)
	registration, err := h.service.Register(c.Context(), tenant.TenantID, principal, input)
	if errors.Is(err, service.ErrInvalidInput) {
		return c.SendStatus(fiber.StatusBadRequest)
	}
	if errors.Is(err, service.ErrForbidden) {
		return c.SendStatus(fiber.StatusForbidden)
	}
	if errors.Is(err, service.ErrDeliveryUnavailable) {
		return c.SendStatus(fiber.StatusServiceUnavailable)
	}
	if errors.Is(err, service.ErrDeliveryInProgress) {
		return c.SendStatus(fiber.StatusConflict)
	}
	if err != nil {
		return c.SendStatus(fiber.StatusInternalServerError)
	}
	status := fiber.StatusOK
	if registration.MembershipCreated {
		status = fiber.StatusCreated
	}
	message := "Membership created for the existing identity."
	if registration.CredentialScheduled {
		message = "Initial login credentials have been sent to the user's email."
	}
	return c.Status(status).JSON(fiber.Map{"status": "created", "message": message, "identity_id": registration.IdentityID, "membership_created": registration.MembershipCreated, "credential_scheduled": registration.CredentialScheduled, "role": registration.Role})
}

func (h Handler) members(c fiber.Ctx) error {
	tenant, err := h.trustedContext(c)
	if err != nil {
		return c.SendStatus(fiber.StatusUnauthorized)
	}
	members, err := h.service.Members(c.Context(), tenant.TenantID)
	if err != nil {
		return c.SendStatus(fiber.StatusInternalServerError)
	}
	return c.JSON(members)
}

func memberID(c fiber.Ctx) (uuid.UUID, error) { return uuid.Parse(c.Params("id")) }

func (h Handler) member(c fiber.Ctx) error {
	tenant, err := h.trustedContext(c)
	id, idErr := memberID(c)
	if err != nil {
		return c.SendStatus(fiber.StatusUnauthorized)
	}
	if idErr != nil {
		return c.SendStatus(fiber.StatusBadRequest)
	}
	member, err := h.service.Member(c.Context(), tenant.TenantID, id)
	if errors.Is(err, repository.ErrNotFound) {
		return c.SendStatus(fiber.StatusNotFound)
	}
	if err != nil {
		return c.SendStatus(fiber.StatusInternalServerError)
	}
	return c.JSON(member)
}

func (h Handler) changeMemberRole(c fiber.Ctx) error {
	tenant, err := h.trustedContext(c)
	id, idErr := memberID(c)
	if err != nil {
		return c.SendStatus(fiber.StatusUnauthorized)
	}
	if idErr != nil {
		return c.SendStatus(fiber.StatusBadRequest)
	}
	principal := c.Locals(principalKey).(domain.Principal)
	var input domain.ChangeMemberRoleInput
	if err := json.Unmarshal(c.Body(), &input); err != nil {
		return c.SendStatus(fiber.StatusBadRequest)
	}
	member, err := h.service.ChangeMemberRole(c.Context(), tenant.TenantID, principal, id, input.Role)
	if errors.Is(err, service.ErrForbidden) {
		return c.SendStatus(fiber.StatusForbidden)
	}
	if errors.Is(err, repository.ErrNotFound) {
		return c.SendStatus(fiber.StatusNotFound)
	}
	if err != nil {
		return c.SendStatus(fiber.StatusInternalServerError)
	}
	return c.JSON(member)
}

func (h Handler) activateMember(c fiber.Ctx) error   { return h.setMemberStatus(c, true) }
func (h Handler) deactivateMember(c fiber.Ctx) error { return h.setMemberStatus(c, false) }

func (h Handler) setMemberStatus(c fiber.Ctx, active bool) error {
	tenant, err := h.trustedContext(c)
	id, idErr := memberID(c)
	if err != nil {
		return c.SendStatus(fiber.StatusUnauthorized)
	}
	if idErr != nil {
		return c.SendStatus(fiber.StatusBadRequest)
	}
	principal := c.Locals(principalKey).(domain.Principal)
	member, err := h.service.SetMemberStatus(c.Context(), tenant.TenantID, principal, id, active)
	if errors.Is(err, service.ErrForbidden) {
		return c.SendStatus(fiber.StatusForbidden)
	}
	if errors.Is(err, repository.ErrNotFound) {
		return c.SendStatus(fiber.StatusNotFound)
	}
	if err != nil {
		return c.SendStatus(fiber.StatusInternalServerError)
	}
	return c.JSON(member)
}

// barberEligibility is private infrastructure validation for barber-service.
// It returns no identity profile and never accepts a browser cookie or caller
// tenant input; the trusted gateway/service context supplies the tenant.
func (h Handler) barberEligibility(c fiber.Ctx) error {
	tenant, err := h.trustedContext(c)
	id, idErr := memberID(c)
	if err != nil || idErr != nil {
		return c.SendStatus(fiber.StatusUnauthorized)
	}
	eligible, err := h.service.BarberEligible(c.Context(), tenant.TenantID, id)
	if err != nil {
		return c.SendStatus(fiber.StatusInternalServerError)
	}
	if !eligible {
		return c.SendStatus(fiber.StatusNotFound)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h Handler) changePassword(c fiber.Ctx) error {
	tenant, err := h.trustedContext(c)
	if err != nil {
		return c.SendStatus(fiber.StatusUnauthorized)
	}
	principal := c.Locals(principalKey).(domain.Principal)
	var input domain.ChangePasswordInput
	if err := json.Unmarshal(c.Body(), &input); err != nil {
		return c.SendStatus(fiber.StatusBadRequest)
	}
	err = h.service.ChangePassword(c.Context(), tenant.TenantID, principal, input)
	if errors.Is(err, service.ErrInvalidInput) {
		return c.SendStatus(fiber.StatusBadRequest)
	}
	if errors.Is(err, service.ErrUnauthorized) {
		return c.SendStatus(fiber.StatusUnauthorized)
	}
	if err != nil {
		return c.SendStatus(fiber.StatusInternalServerError)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h Handler) forgotPassword(c fiber.Ctx) error {
	tenant, err := h.trustedContext(c)
	if err != nil {
		return c.SendStatus(fiber.StatusUnauthorized)
	}
	if allowed, err := h.limited(c, tenant, "forgot-password"); err != nil {
		return c.SendStatus(fiber.StatusServiceUnavailable)
	} else if !allowed {
		return c.SendStatus(fiber.StatusTooManyRequests)
	}
	var input domain.ForgotPasswordInput
	if err := json.Unmarshal(c.Body(), &input); err != nil || input.Email == "" {
		return c.SendStatus(fiber.StatusBadRequest)
	}
	if err := h.service.ForgotPassword(c.Context(), tenant.TenantID, input); err != nil {
		return c.SendStatus(fiber.StatusServiceUnavailable)
	}
	// Deliberately identical for unknown identities, wrong tenants, and success.
	return c.SendStatus(fiber.StatusNoContent)
}

func (h Handler) logout(c fiber.Ctx) error {
	tenant, err := h.trustedContext(c)
	if err != nil {
		return c.SendStatus(fiber.StatusUnauthorized)
	}
	if err := h.service.Logout(c.Context(), tenant.TenantID, c.Cookies(h.cookieName)); err != nil {
		return c.SendStatus(fiber.StatusInternalServerError)
	}
	h.clearCookie(c)
	return c.SendStatus(fiber.StatusNoContent)
}

func (h Handler) me(c fiber.Ctx) error { return c.JSON(c.Locals(principalKey).(domain.Principal)) }

// Authenticate validates trusted infrastructure context, app type, and the
// browser session before storing the principal for authorization middleware.
func (h Handler) Authenticate() fiber.Handler {
	return func(c fiber.Ctx) error {
		tenant, err := h.trustedContext(c)
		if err != nil {
			return c.SendStatus(fiber.StatusUnauthorized)
		}
		principal, err := h.service.Current(c.Context(), tenant.TenantID, c.Cookies(h.cookieName))
		if errors.Is(err, service.ErrUnauthorized) {
			return c.SendStatus(fiber.StatusUnauthorized)
		}
		if err != nil {
			return c.SendStatus(fiber.StatusInternalServerError)
		}
		c.Locals(principalKey, principal)
		return c.Next()
	}
}

func (h Handler) limited(c fiber.Ctx, tenant tenantctx.Context, operation string) (bool, error) {
	if h.limiter == nil {
		return false, nil
	}
	// The gateway is the trust boundary. Tenant+operation limits avoid trusting
	// arbitrary forwarded addresses while still bounding password abuse.
	return h.limiter.Allow(c.Context(), "auth-rate:"+tenant.TenantID.String()+":"+operation, 10, time.Minute)
}

func (h Handler) setCookie(c fiber.Ctx, value string, expiresAt time.Time) {
	c.Cookie(&fiber.Cookie{Name: h.cookieName, Value: value, Path: "/", HTTPOnly: true, Secure: h.cookieSecure, SameSite: "Strict", Expires: expiresAt, MaxAge: int(time.Until(expiresAt).Seconds())})
}
func (h Handler) clearCookie(c fiber.Ctx) {
	c.Cookie(&fiber.Cookie{Name: h.cookieName, Value: "", Path: "/", HTTPOnly: true, Secure: h.cookieSecure, SameSite: "Strict", Expires: time.Unix(0, 0), MaxAge: -1})
}

// RequireCurrentPassword blocks privileged operations for a principal whose
// password change is still pending, mirroring the must_change_password gate
// platform/adminauth.Client.Authenticate enforces for every other service.
// It must run after Authenticate (which populates principalKey) and must be
// omitted from routes that need to remain reachable during a forced reset:
// /me, /change-password, and /logout.
func RequireCurrentPassword() fiber.Handler {
	return func(c fiber.Ctx) error {
		principal, ok := c.Locals(principalKey).(domain.Principal)
		if !ok {
			return c.SendStatus(fiber.StatusUnauthorized)
		}
		if principal.MustChangePassword {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "password change required"})
		}
		return c.Next()
	}
}

func RequireRoles(allowed ...domain.Role) fiber.Handler {
	return func(c fiber.Ctx) error {
		principal, ok := c.Locals(principalKey).(domain.Principal)
		if !ok {
			return c.SendStatus(fiber.StatusUnauthorized)
		}
		for _, role := range allowed {
			if principal.Role == role {
				return c.Next()
			}
		}
		return c.SendStatus(fiber.StatusForbidden)
	}
}
