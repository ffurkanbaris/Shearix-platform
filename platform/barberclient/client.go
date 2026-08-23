// Package barberclient is an infrastructure adapter for the barber-service's
// private identity lookup. It keeps catalog-service out of barber-service's DB.
package barberclient

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/barber-appointment/platform/internalauth"
	"github.com/barber-appointment/platform/otelsetup"
	"github.com/barber-appointment/platform/tenantctx"
	"github.com/google/uuid"
)

type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

func New(baseURL, token string) Client {
	return Client{baseURL: strings.TrimRight(baseURL, "/"), token: token, http: &http.Client{Timeout: 3 * time.Second, Transport: otelsetup.WrapTransport(nil)}}
}

func (c Client) EnsureExists(ctx context.Context, tenant tenantctx.Context, barberID uuid.UUID) error {
	if tenant.AppType != "admin" || barberID == uuid.Nil {
		return errors.New("invalid barber lookup context")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/internal/v1/barbers/"+barberID.String()+"/exists", nil)
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
		return ErrNotFound
	}
	return errors.New("barber lookup failed")
}

var ErrNotFound = errors.New("barber not found")
