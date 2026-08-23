package adminauth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/barber-appointment/platform/internalauth"
	"github.com/barber-appointment/platform/tenantctx"
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
)

type Principal struct {
	Role               string    `json:"role"`
	TenantID           string    `json:"tenant_id"`
	IdentityID         uuid.UUID `json:"identity_id"`
	MustChangePassword bool      `json:"must_change_password"`
}
type Client struct {
	baseURL, token string
	http           *http.Client
}

func New(baseURL, token string) Client {
	return Client{baseURL: strings.TrimRight(baseURL, "/"), token: token, http: &http.Client{Timeout: 3 * time.Second}}
}

// Context validates only infrastructure trust and is reusable by public and
// authenticated internal handlers. The domain services never inspect auth DBs.
func Context(c fiber.Ctx, verifier internalauth.Verifier) (tenantctx.Context, error) {
	if err := verifier.Verify(c.Get(internalauth.HeaderName)); err != nil {
		return tenantctx.Context{}, err
	}
	return tenantctx.New(c.Get(tenantctx.TenantIDHeader), c.Get(tenantctx.AppTypeHeader), c.Get(tenantctx.RequestIDHeader))
}
func (c Client) Authenticate(ctx context.Context, tenant tenantctx.Context, cookie string) (Principal, error) {
	if tenant.AppType != "admin" || cookie == "" {
		return Principal{}, errors.New("admin session required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/internal/v1/auth/me", nil)
	if err != nil {
		return Principal{}, err
	}
	req.Header.Set(internalauth.HeaderName, c.token)
	req.Header.Set(tenantctx.TenantIDHeader, tenant.TenantID.String())
	req.Header.Set(tenantctx.AppTypeHeader, tenant.AppType)
	req.Header.Set(tenantctx.RequestIDHeader, tenant.RequestID)
	req.Header.Set("Cookie", cookie)
	response, err := c.http.Do(req)
	if err != nil {
		return Principal{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Principal{}, errors.New("invalid admin session")
	}
	var principal Principal
	if err := json.NewDecoder(response.Body).Decode(&principal); err != nil {
		return Principal{}, err
	}
	if principal.TenantID != tenant.TenantID.String() {
		return Principal{}, errors.New("tenant mismatch")
	}
	if principal.MustChangePassword {
		return Principal{}, errors.New("password change required")
	}
	return principal, nil
}

// EnsureBarberMembership is a private, tenant-scoped authorization lookup for
// barber-service. It deliberately returns no identity profile and never reads
// auth_db outside auth-service.
func (c Client) EnsureBarberMembership(ctx context.Context, tenant tenantctx.Context, identityID uuid.UUID) error {
	if tenant.AppType != "admin" || identityID == uuid.Nil {
		return errors.New("invalid barber membership lookup")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/internal/v1/auth/members/"+identityID.String()+"/barber-eligibility", nil)
	if err != nil {
		return err
	}
	req.Header.Set(internalauth.HeaderName, c.token)
	req.Header.Set(tenantctx.TenantIDHeader, tenant.TenantID.String())
	req.Header.Set(tenantctx.AppTypeHeader, tenant.AppType)
	req.Header.Set(tenantctx.RequestIDHeader, tenant.RequestID)
	response, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNoContent {
		return nil
	}
	if response.StatusCode == http.StatusNotFound {
		return ErrBarberMembershipIneligible
	}
	return errors.New("barber membership lookup failed")
}

var ErrBarberMembershipIneligible = errors.New("barber membership is not active in tenant")

// Member administration is intentionally OWNER-only. Managers retain their
// explicit business-resource permissions but cannot create or promote staff.
func CanManageMembers(role string) bool { return role == "OWNER" }

// CanManageTenantSettings is intentionally separate from CanWrite. Both
// OWNER and MANAGER may change the operational booking configuration; BARBER
// and RECEPTIONIST remain read-only even though they may read the settings.
func CanManageTenantSettings(role string) bool { return role == "OWNER" || role == "MANAGER" }

func CanWrite(role string) bool { return role == "OWNER" || role == "MANAGER" }
func CanRead(role string) bool {
	return role == "OWNER" || role == "MANAGER" || role == "BARBER" || role == "RECEPTIONIST"
}
