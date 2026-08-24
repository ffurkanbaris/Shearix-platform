package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/barber-appointment/appointment-service/internal/application"
	"github.com/barber-appointment/appointment-service/internal/domain"
	"github.com/barber-appointment/platform/internalauth"
	"github.com/barber-appointment/platform/otelsetup"
	"github.com/barber-appointment/platform/schedulingdeps"
	"github.com/barber-appointment/platform/tenantctx"
	"github.com/google/uuid"
)

type Dependencies struct {
	deps                              schedulingdeps.Client
	schedulingURL, customerURL, token string
	http                              *http.Client
}

func New(deps schedulingdeps.Client, schedulingURL, customerURL, token string) Dependencies {
	return Dependencies{deps: deps, schedulingURL: strings.TrimRight(schedulingURL, "/"), customerURL: strings.TrimRight(customerURL, "/"), token: token, http: &http.Client{Timeout: 3 * time.Second, Transport: otelsetup.WrapTransport(nil)}}
}
func (d Dependencies) BranchBarber(ctx context.Context, t tenantctx.Context, branch, barber uuid.UUID) error {
	return d.deps.BranchBarber(ctx, t, branch, barber)
}
func (d Dependencies) Barber(ctx context.Context, t tenantctx.Context, barber uuid.UUID) error {
	_, err := d.deps.Barber(ctx, t, barber)
	return err
}
func (d Dependencies) Service(ctx context.Context, t tenantctx.Context, barber, service uuid.UUID) (schedulingdeps.Service, error) {
	return d.deps.Service(ctx, t, barber, service)
}
func (d Dependencies) Settings(ctx context.Context, t tenantctx.Context) (schedulingdeps.TenantSettings, error) {
	return d.deps.Settings(ctx, t)
}
func headers(req *http.Request, t tenantctx.Context, token string) {
	req.Header.Set(internalauth.HeaderName, token)
	req.Header.Set(tenantctx.TenantIDHeader, t.TenantID.String())
	req.Header.Set(tenantctx.AppTypeHeader, t.AppType)
	req.Header.Set(tenantctx.RequestIDHeader, t.RequestID)
}
func (d Dependencies) Available(ctx context.Context, t tenantctx.Context, in domain.CreateInput, start time.Time) error {
	settings, err := d.Settings(ctx, t)
	if err != nil {
		return err
	}
	zone, err := time.LoadLocation(settings.Timezone)
	if err != nil {
		return err
	}
	query := url.Values{"barber_id": {in.BarberID.String()}, "service_id": {in.ServiceID.String()}, "date": {start.In(zone).Format("2006-01-02")}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.schedulingURL+"/internal/v1/public/availability?"+query.Encode(), nil)
	if err != nil {
		return err
	}
	headers(req, t, d.token)
	res, err := d.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return errors.New("scheduling rejected slot")
	}
	var slots []struct {
		StartAt time.Time `json:"start_at"`
	}
	if err = json.NewDecoder(res.Body).Decode(&slots); err != nil {
		return err
	}
	for _, slot := range slots {
		if slot.StartAt.Equal(start) {
			return nil
		}
	}
	return errors.New("requested slot unavailable")
}
func (d Dependencies) Customer(ctx context.Context, t tenantctx.Context, id uuid.UUID) (application.CustomerSnapshot, error) {
	var out application.CustomerSnapshot
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.customerURL+"/internal/v1/customer/"+id.String(), nil)
	if err != nil {
		return out, err
	}
	headers(req, t, d.token)
	res, err := d.http.Do(req)
	if err != nil {
		return out, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return out, errors.New("customer snapshot unavailable")
	}
	err = json.NewDecoder(res.Body).Decode(&out)
	return out, err
}
