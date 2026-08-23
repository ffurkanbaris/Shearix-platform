// Package tenantsettings is the narrow private adapter for reading the
// tenant-service configuration that other services need at execution time.
// It deliberately has no write operation and never accesses tenant_db.
package tenantsettings

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/barber-appointment/platform/internalauth"
	"github.com/barber-appointment/platform/tenantctx"
	"github.com/google/uuid"
)

type Settings struct {
	Timezone               string `json:"business_timezone"`
	BookingIntervalMinutes int    `json:"booking_interval_minutes"`
	ReminderOffsetsMinutes []int  `json:"reminder_offsets_minutes"`
}

type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

func New(baseURL, token string) Client {
	return Client{baseURL: strings.TrimRight(baseURL, "/"), token: token, http: &http.Client{Timeout: 3 * time.Second}}
}

func (c Client) Settings(ctx context.Context, tenant tenantctx.Context) (Settings, error) {
	if c.baseURL == "" || c.token == "" || tenant.TenantID == uuid.Nil {
		return Settings{}, errors.New("tenant settings client is not configured")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/internal/v1/settings", nil)
	if err != nil {
		return Settings{}, err
	}
	req.Header.Set(internalauth.HeaderName, c.token)
	req.Header.Set(tenantctx.TenantIDHeader, tenant.TenantID.String())
	req.Header.Set(tenantctx.AppTypeHeader, tenant.AppType)
	req.Header.Set(tenantctx.RequestIDHeader, tenant.RequestID)
	res, err := c.http.Do(req)
	if err != nil {
		return Settings{}, err
	}
	defer res.Body.Close()
	if res.StatusCode < http.StatusOK || res.StatusCode >= http.StatusMultipleChoices {
		return Settings{}, errors.New("tenant settings request failed")
	}
	var settings Settings
	if err := json.NewDecoder(res.Body).Decode(&settings); err != nil {
		return Settings{}, err
	}
	return settings, nil
}

// ReminderOffsets implements notification-service's narrow dependency. The
// event itself supplies the tenant ID; notification planning explicitly uses
// a booking context because reminder offsets are booking operational data.
func (c Client) ReminderOffsets(ctx context.Context, tenantID uuid.UUID) ([]time.Duration, error) {
	settings, err := c.Settings(ctx, tenantctx.Context{TenantID: tenantID, AppType: "booking"})
	if err != nil {
		return nil, err
	}
	if len(settings.ReminderOffsetsMinutes) == 0 {
		return nil, errors.New("tenant reminder offsets are empty")
	}
	offsets := make([]time.Duration, 0, len(settings.ReminderOffsetsMinutes))
	for _, minutes := range settings.ReminderOffsetsMinutes {
		if minutes <= 0 {
			return nil, errors.New("tenant reminder offset is invalid")
		}
		offsets = append(offsets, time.Duration(minutes)*time.Minute)
	}
	return offsets, nil
}
