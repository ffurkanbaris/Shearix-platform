package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/barber-appointment/platform/internalauth"
	"github.com/barber-appointment/platform/otelsetup"
	"github.com/barber-appointment/platform/tenantctx"
	"github.com/barber-appointment/scheduling-service/internal/domain"
	"github.com/google/uuid"
)

// ErrOccupancyUnavailable deliberately distinguishes a dependency failure from
// an empty appointment calendar. Callers must not present stale availability.
var ErrOccupancyUnavailable = errors.New("appointment occupancy unavailable")

type HTTPOccupancy struct {
	baseURL string
	token   string
	client  *http.Client
}

func NewHTTPOccupancy(baseURL, token string) HTTPOccupancy {
	return HTTPOccupancy{baseURL: strings.TrimRight(baseURL, "/"), token: token, client: &http.Client{Timeout: 3 * time.Second, Transport: otelsetup.WrapTransport(nil)}}
}

func (p HTTPOccupancy) Occupied(ctx context.Context, tenant tenantctx.Context, barberID uuid.UUID, from, to time.Time) ([]domain.BlockedPeriod, error) {
	query := url.Values{"barber_id": {barberID.String()}, "from": {from.UTC().Format(time.RFC3339)}, "to": {to.UTC().Format(time.RFC3339)}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/internal/v1/occupancy?"+query.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("%w: build request", ErrOccupancyUnavailable)
	}
	req.Header.Set(internalauth.HeaderName, p.token)
	req.Header.Set(tenantctx.TenantIDHeader, tenant.TenantID.String())
	req.Header.Set(tenantctx.AppTypeHeader, tenant.AppType)
	req.Header.Set(tenantctx.RequestIDHeader, tenant.RequestID)
	res, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrOccupancyUnavailable, err)
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return nil, fmt.Errorf("%w: status %d", ErrOccupancyUnavailable, res.StatusCode)
	}
	var rows []struct {
		OccupiedStartAt time.Time `json:"occupied_start_at"`
		OccupiedEndAt   time.Time `json:"occupied_end_at"`
	}
	if err := json.NewDecoder(res.Body).Decode(&rows); err != nil {
		return nil, fmt.Errorf("%w: invalid response", ErrOccupancyUnavailable)
	}
	out := make([]domain.BlockedPeriod, 0, len(rows))
	for _, row := range rows {
		if row.OccupiedStartAt.Before(row.OccupiedEndAt) {
			out = append(out, domain.BlockedPeriod{StartAt: row.OccupiedStartAt, EndAt: row.OccupiedEndAt})
		}
	}
	return out, nil
}
