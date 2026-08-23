package handler

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/barber-appointment/platform/adminauth"
	"github.com/barber-appointment/platform/internalauth"
	"github.com/barber-appointment/platform/tenantctx"
	"github.com/barber-appointment/tenant-service/internal/domain"
	"github.com/barber-appointment/tenant-service/internal/repository"
	"github.com/barber-appointment/tenant-service/internal/service"
	"github.com/gofiber/fiber/v3"
)

type Handler struct {
	service  service.TenantService
	settings settingsService
	auth     internalauth.Verifier
	admin    adminSessionAuthenticator
}

type settingsService interface {
	Settings(context.Context, string) (domain.Settings, error)
	UpdateSettings(context.Context, string, domain.UpdateSettingsInput) (domain.Settings, error)
}

type adminSessionAuthenticator interface {
	Authenticate(context.Context, tenantctx.Context, string) (adminauth.Principal, error)
}

// New accepts an optional auth-service client to keep the control-plane-only
// construction used by existing focused tests intact. Admin settings routes
// always receive the configured client in normal service startup.
func New(tenantService service.TenantService, auth internalauth.Verifier, adminClients ...adminauth.Client) Handler {
	var admin adminSessionAuthenticator = adminauth.New("", "")
	if len(adminClients) > 0 {
		admin = adminClients[0]
	}
	return Handler{service: tenantService, settings: tenantService, auth: auth, admin: admin}
}

func (h Handler) Register(app *fiber.App) {
	app.Get("/internal/v1/domains/resolve", h.resolve)
	app.Get("/internal/v1/domains/tls-authorize", h.tlsAuthorize)
	app.Get("/internal/v1/public/config", h.publicConfig)
	app.Get("/internal/v1/settings", h.internalSettings)
	app.Get("/internal/v1/admin/settings", h.adminSettings)
	app.Patch("/internal/v1/admin/settings", h.updateAdminSettings)

	// These control-plane endpoints are private-network only. Gateway verifies
	// a separate platform-admin credential before forwarding with internal auth.
	app.Post("/internal/v1/platform/tenants", h.createTenant)
	app.Get("/internal/v1/platform/tenants/:id", h.tenant)
	app.Post("/internal/v1/platform/tenants/:id/domains", h.createDomain)
	app.Get("/internal/v1/platform/tenants/:id/domains", h.domains)
	app.Post("/internal/v1/platform/tenants/:id/domains/:domain_id/verify", h.verifyDomain)
	app.Post("/internal/v1/platform/tenants/:id/domains/:domain_id/activate", h.activateDomain)
	app.Post("/internal/v1/platform/tenants/:id/domains/:domain_id/deactivate", h.deactivateDomain)
}

// internalSettings is for private service-to-service consumers such as
// notification planning. It returns tenant settings only after both internal
// authentication and trusted tenant context validation.
func (h Handler) internalSettings(c fiber.Ctx) error {
	if !h.internal(c) {
		return c.SendStatus(fiber.StatusUnauthorized)
	}
	context, err := tenantctx.New(c.Get(tenantctx.TenantIDHeader), c.Get(tenantctx.AppTypeHeader), c.Get(tenantctx.RequestIDHeader))
	if err != nil {
		return c.SendStatus(fiber.StatusBadRequest)
	}
	settings, err := h.settings.Settings(c.Context(), context.TenantID.String())
	if errors.Is(err, repository.ErrNotFound) {
		return c.SendStatus(fiber.StatusNotFound)
	}
	if err != nil {
		return c.SendStatus(fiber.StatusInternalServerError)
	}
	return c.JSON(settings)
}

func (h Handler) adminContext(c fiber.Ctx, write bool) (tenantctx.Context, error) {
	context, err := adminauth.Context(c, h.auth)
	if err != nil || context.AppType != "admin" {
		return tenantctx.Context{}, errors.New("untrusted admin context")
	}
	principal, err := h.admin.Authenticate(c.Context(), context, c.Get("Cookie"))
	if err != nil || principal.TenantID != context.TenantID.String() || !adminauth.CanRead(principal.Role) {
		return tenantctx.Context{}, errors.New("admin session required")
	}
	if write && !adminauth.CanManageTenantSettings(principal.Role) {
		return tenantctx.Context{}, service.ErrForbidden
	}
	return context, nil
}

func (h Handler) adminSettings(c fiber.Ctx) error {
	context, err := h.adminContext(c, false)
	if err != nil {
		return c.SendStatus(fiber.StatusUnauthorized)
	}
	settings, err := h.settings.Settings(c.Context(), context.TenantID.String())
	if errors.Is(err, repository.ErrNotFound) {
		return c.SendStatus(fiber.StatusNotFound)
	}
	if err != nil {
		return c.SendStatus(fiber.StatusInternalServerError)
	}
	return c.JSON(settings)
}

func (h Handler) updateAdminSettings(c fiber.Ctx) error {
	context, err := h.adminContext(c, true)
	if errors.Is(err, service.ErrForbidden) {
		return c.SendStatus(fiber.StatusForbidden)
	}
	if err != nil {
		return c.SendStatus(fiber.StatusUnauthorized)
	}
	var input domain.UpdateSettingsInput
	if err := json.Unmarshal(c.Body(), &input); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid settings payload"})
	}
	settings, err := h.settings.UpdateSettings(c.Context(), context.TenantID.String(), input)
	if errors.Is(err, service.ErrInvalidInput) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid settings"})
	}
	if errors.Is(err, repository.ErrNotFound) {
		return c.SendStatus(fiber.StatusNotFound)
	}
	if err != nil {
		return c.SendStatus(fiber.StatusInternalServerError)
	}
	return c.JSON(settings)
}

// tlsAuthorize is the narrowly scoped Caddy on-demand TLS permission check.
// Caddy's ask protocol supplies `domain`; `hostname` is retained for internal
// callers. A conflicting pair is rejected to avoid ambiguous authorization.
func (h Handler) tlsAuthorize(c fiber.Ctx) error {
	if !h.internal(c) {
		return c.SendStatus(fiber.StatusUnauthorized)
	}
	hostname, caddyDomain := c.Query("hostname"), c.Query("domain")
	if hostname == "" {
		hostname = caddyDomain
	}
	if hostname == "" || (caddyDomain != "" && c.Query("hostname") != "" && caddyDomain != c.Query("hostname")) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"eligible": false})
	}
	eligible, err := h.service.TLSAuthorized(c.Context(), hostname)
	if err != nil {
		return c.SendStatus(fiber.StatusServiceUnavailable)
	}
	if !eligible {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"eligible": false})
	}
	return c.JSON(fiber.Map{"eligible": true})
}

func (h Handler) resolve(c fiber.Ctx) error {
	if !h.internal(c) {
		return c.SendStatus(fiber.StatusUnauthorized)
	}
	result, err := h.service.Resolve(c.Context(), c.Query("hostname"))
	if errors.Is(err, repository.ErrNotFound) {
		return c.SendStatus(fiber.StatusNotFound)
	}
	if errors.Is(err, service.ErrInvalidInput) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid hostname"})
	}
	if err != nil {
		return c.SendStatus(fiber.StatusInternalServerError)
	}
	return c.JSON(result)
}

func (h Handler) publicConfig(c fiber.Ctx) error {
	if !h.internal(c) {
		return c.SendStatus(fiber.StatusUnauthorized)
	}
	context, err := tenantctx.New(c.Get(tenantctx.TenantIDHeader), c.Get(tenantctx.AppTypeHeader), c.Get(tenantctx.RequestIDHeader))
	if err != nil {
		return c.SendStatus(fiber.StatusBadRequest)
	}
	settings, err := h.service.Settings(c.Context(), context.TenantID.String())
	if errors.Is(err, repository.ErrNotFound) {
		return c.SendStatus(fiber.StatusNotFound)
	}
	if err != nil {
		return c.SendStatus(fiber.StatusInternalServerError)
	}
	tenant, err := h.service.Tenant(c.Context(), context.TenantID.String())
	if errors.Is(err, repository.ErrNotFound) {
		return c.SendStatus(fiber.StatusNotFound)
	}
	if err != nil {
		return c.SendStatus(fiber.StatusInternalServerError)
	}
	return c.JSON(fiber.Map{
		"tenant_id":                context.TenantID,
		"app_type":                 context.AppType,
		"business_name":            tenant.Name,
		"business_timezone":        settings.BusinessTimezone,
		"booking_interval_minutes": settings.BookingIntervalMinutes,
		"settings":                 settings,
	})
}

func (h Handler) createTenant(c fiber.Ctx) error {
	if !h.internal(c) {
		return c.SendStatus(fiber.StatusUnauthorized)
	}
	var input domain.CreateTenantInput
	if err := json.Unmarshal(c.Body(), &input); err != nil {
		return c.SendStatus(fiber.StatusBadRequest)
	}
	created, err := h.service.CreateTenant(c.Context(), input)
	if errors.Is(err, service.ErrInvalidInput) {
		return c.SendStatus(fiber.StatusBadRequest)
	}
	if err != nil {
		return c.SendStatus(fiber.StatusInternalServerError)
	}
	return c.Status(fiber.StatusCreated).JSON(created)
}

func (h Handler) tenant(c fiber.Ctx) error {
	if !h.internal(c) {
		return c.SendStatus(fiber.StatusUnauthorized)
	}
	result, err := h.service.Tenant(c.Context(), c.Params("id"))
	if errors.Is(err, service.ErrInvalidInput) {
		return c.SendStatus(fiber.StatusBadRequest)
	}
	if errors.Is(err, repository.ErrNotFound) {
		return c.SendStatus(fiber.StatusNotFound)
	}
	if err != nil {
		return c.SendStatus(fiber.StatusInternalServerError)
	}
	return c.JSON(result)
}

func (h Handler) createDomain(c fiber.Ctx) error {
	if !h.internal(c) {
		return c.SendStatus(fiber.StatusUnauthorized)
	}
	var input domain.CreateDomainInput
	if err := json.Unmarshal(c.Body(), &input); err != nil {
		return c.SendStatus(fiber.StatusBadRequest)
	}
	result, err := h.service.CreateDomain(c.Context(), c.Params("id"), input)
	if errors.Is(err, service.ErrInvalidInput) {
		return c.SendStatus(fiber.StatusBadRequest)
	}
	return h.domainResponse(c, result, err, fiber.StatusCreated)
}

func (h Handler) domains(c fiber.Ctx) error {
	if !h.internal(c) {
		return c.SendStatus(fiber.StatusUnauthorized)
	}
	result, err := h.service.Domains(c.Context(), c.Params("id"))
	if errors.Is(err, service.ErrInvalidInput) {
		return c.SendStatus(fiber.StatusBadRequest)
	}
	if errors.Is(err, repository.ErrNotFound) {
		return c.SendStatus(fiber.StatusNotFound)
	}
	if err != nil {
		return c.SendStatus(fiber.StatusInternalServerError)
	}
	return c.JSON(result)
}

func (h Handler) verifyDomain(c fiber.Ctx) error {
	if !h.internal(c) {
		return c.SendStatus(fiber.StatusUnauthorized)
	}
	result, err := h.service.VerifyDomain(c.Context(), c.Params("id"), c.Params("domain_id"))
	return h.domainResponse(c, result, err, fiber.StatusOK)
}

func (h Handler) activateDomain(c fiber.Ctx) error {
	if !h.internal(c) {
		return c.SendStatus(fiber.StatusUnauthorized)
	}
	result, err := h.service.ActivateDomain(c.Context(), c.Params("id"), c.Params("domain_id"))
	return h.domainResponse(c, result, err, fiber.StatusOK)
}

func (h Handler) deactivateDomain(c fiber.Ctx) error {
	if !h.internal(c) {
		return c.SendStatus(fiber.StatusUnauthorized)
	}
	result, err := h.service.DeactivateDomain(c.Context(), c.Params("id"), c.Params("domain_id"))
	return h.domainResponse(c, result, err, fiber.StatusOK)
}

func (h Handler) domainResponse(c fiber.Ctx, result domain.TenantDomain, err error, success int) error {
	if err == nil {
		return c.Status(success).JSON(result)
	}
	switch {
	case errors.Is(err, service.ErrInvalidInput):
		return c.SendStatus(fiber.StatusBadRequest)
	case errors.Is(err, repository.ErrNotFound):
		return c.SendStatus(fiber.StatusNotFound)
	case errors.Is(err, repository.ErrConflict):
		return c.SendStatus(fiber.StatusConflict)
	case errors.Is(err, repository.ErrTenantInactive), errors.Is(err, service.ErrVerificationRequired), errors.Is(err, service.ErrVerificationUnavailable):
		return c.Status(fiber.StatusConflict).JSON(result)
	case errors.Is(err, service.ErrVerificationFailed):
		return c.Status(fiber.StatusUnprocessableEntity).JSON(result)
	default:
		return c.SendStatus(fiber.StatusInternalServerError)
	}
}

func (h Handler) internal(c fiber.Ctx) bool {
	return h.auth.Verify(c.Get(internalauth.HeaderName)) == nil
}
