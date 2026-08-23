package client

import (
	"context"
	"github.com/barber-appointment/customer-service/internal/application"
	"github.com/barber-appointment/platform/internalauth"
	"github.com/barber-appointment/platform/tenantctx"
	"github.com/google/uuid"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Appointments struct {
	baseURL, token string
	http           *http.Client
}

func NewAppointments(baseURL, token string) Appointments {
	return Appointments{strings.TrimRight(baseURL, "/"), token, &http.Client{Timeout: 3 * time.Second}}
}
func (a Appointments) Forward(ctx context.Context, t tenantctx.Context, customer uuid.UUID, method, path, rawQuery string) (application.ForwardResult, error) {
	target := a.baseURL + "/internal/v1/customer/appointments" + path
	if rawQuery != "" {
		if _, err := url.ParseQuery(rawQuery); err != nil {
			return application.ForwardResult{}, err
		}
		target += "?" + rawQuery
	}
	req, err := http.NewRequestWithContext(ctx, method, target, nil)
	if err != nil {
		return application.ForwardResult{}, err
	}
	req.Header.Set(internalauth.HeaderName, a.token)
	req.Header.Set(tenantctx.TenantIDHeader, t.TenantID.String())
	req.Header.Set(tenantctx.AppTypeHeader, "booking")
	req.Header.Set(tenantctx.RequestIDHeader, t.RequestID)
	req.Header.Set("X-Customer-ID", customer.String())
	res, err := a.http.Do(req)
	if err != nil {
		return application.ForwardResult{}, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	return application.ForwardResult{Status: res.StatusCode, Body: body}, err
}
