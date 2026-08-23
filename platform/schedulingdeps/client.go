// Package schedulingdeps contains private REST adapters used by scheduling.
package schedulingdeps

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/barber-appointment/platform/internalauth"
	"github.com/barber-appointment/platform/otelsetup"
	"github.com/barber-appointment/platform/tenantctx"
	"github.com/google/uuid"
	"net/http"
	"strings"
	"time"
)

type Service struct {
	DurationMinutes     int `json:"duration_minutes"`
	BufferBeforeMinutes int `json:"buffer_before_minutes"`
	BufferAfterMinutes  int `json:"buffer_after_minutes"`
}
type Barber struct {
	IdentityID *uuid.UUID `json:"identity_id"`
}
type Client struct {
	barberURL, catalogURL, tenantURL, token string
	http                                    *http.Client
}

func New(barberURL, catalogURL, tenantURL, token string) Client {
	return Client{strings.TrimRight(barberURL, "/"), strings.TrimRight(catalogURL, "/"), strings.TrimRight(tenantURL, "/"), token, &http.Client{Timeout: 3 * time.Second, Transport: otelsetup.WrapTransport(nil)}}
}

// InternalToken is used only by another private HTTP adapter. It is sourced
// from service configuration, never from an inbound request.
func (c Client) InternalToken() string { return c.token }
func (c Client) request(ctx context.Context, tenant tenantctx.Context, url string, out any) error {
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if e != nil {
		return e
	}
	req.Header.Set(internalauth.HeaderName, c.token)
	req.Header.Set(tenantctx.TenantIDHeader, tenant.TenantID.String())
	req.Header.Set(tenantctx.AppTypeHeader, tenant.AppType)
	req.Header.Set(tenantctx.RequestIDHeader, tenant.RequestID)
	res, e := c.http.Do(req)
	if e != nil {
		return e
	}
	defer res.Body.Close()
	if res.StatusCode == 404 {
		return ErrNotFound
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return errors.New("private dependency request failed")
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(res.Body).Decode(out)
}
func (c Client) Barber(ctx context.Context, t tenantctx.Context, id uuid.UUID) (Barber, error) {
	var b Barber
	e := c.request(ctx, t, c.barberURL+"/internal/v1/barbers/"+id.String()+"/scheduling-access", &b)
	return b, e
}

// BranchBarber verifies the active branch assignment without exposing barber
// storage to callers in other services.
func (c Client) BranchBarber(ctx context.Context, t tenantctx.Context, branchID, barberID uuid.UUID) error {
	return c.request(ctx, t, c.barberURL+"/internal/v1/branches/"+branchID.String()+"/barbers/"+barberID.String()+"/booking-access", nil)
}
func (c Client) Service(ctx context.Context, t tenantctx.Context, b, s uuid.UUID) (Service, error) {
	var v Service
	e := c.request(ctx, t, c.catalogURL+"/internal/v1/barbers/"+b.String()+"/services/"+s.String()+"/availability", &v)
	return v, e
}

type TenantSettings struct {
	Timezone                  string
	BookingIntervalMinutes    int
	CancellationPolicy        string
	CancellationNoticeMinutes int
}

func (c Client) Settings(ctx context.Context, t tenantctx.Context) (TenantSettings, error) {
	var v struct {
		BusinessTimezone       string `json:"business_timezone"`
		BookingIntervalMinutes int    `json:"booking_interval_minutes"`
		Settings               struct {
			CancellationPolicy        string `json:"cancellation_policy"`
			CancellationNoticeMinutes int    `json:"cancellation_notice_minutes"`
		} `json:"settings"`
	}
	e := c.request(ctx, t, c.tenantURL+"/internal/v1/public/config", &v)
	return TenantSettings{Timezone: v.BusinessTimezone, BookingIntervalMinutes: v.BookingIntervalMinutes, CancellationPolicy: v.Settings.CancellationPolicy, CancellationNoticeMinutes: v.Settings.CancellationNoticeMinutes}, e
}

var ErrNotFound = errors.New("dependency entity not found")
