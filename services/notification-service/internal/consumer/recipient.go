package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/barber-appointment/platform/internalauth"
	"github.com/barber-appointment/platform/tenantctx"
	"github.com/google/uuid"
	"net/http"
	"strings"
	"time"
)

type RecipientResolver interface {
	Email(context.Context, uuid.UUID, uuid.UUID) (string, error)
}
type AppointmentRecipientClient struct {
	baseURL, token string
	client         *http.Client
}

func NewAppointmentRecipientClient(baseURL, token string) AppointmentRecipientClient {
	return AppointmentRecipientClient{baseURL: strings.TrimRight(baseURL, "/"), token: token, client: &http.Client{Timeout: 3 * time.Second}}
}
func (c AppointmentRecipientClient) Email(ctx context.Context, tenant, appointment uuid.UUID) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/internal/v1/appointments/"+appointment.String()+"/notification-recipient", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set(internalauth.HeaderName, c.token)
	req.Header.Set(tenantctx.TenantIDHeader, tenant.String())
	req.Header.Set(tenantctx.AppTypeHeader, "booking")
	res, err := c.client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", errors.New("appointment recipient unavailable")
	}
	var body struct {
		Email string `json:"email"`
	}
	if err = json.NewDecoder(res.Body).Decode(&body); err != nil {
		return "", err
	}
	body.Email = strings.ToLower(strings.TrimSpace(body.Email))
	if !strings.Contains(body.Email, "@") {
		return "", errors.New("invalid appointment recipient")
	}
	return body.Email, nil
}
