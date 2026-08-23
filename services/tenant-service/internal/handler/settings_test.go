package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/barber-appointment/platform/adminauth"
	"github.com/barber-appointment/platform/internalauth"
	"github.com/barber-appointment/platform/tenantctx"
	"github.com/barber-appointment/tenant-service/internal/domain"
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
)

type settingsStub struct {
	settings domain.Settings
	updated  domain.UpdateSettingsInput
	calls    int
	err      error
}

func (s *settingsStub) Settings(_ context.Context, _ string) (domain.Settings, error) {
	return s.settings, s.err
}
func (s *settingsStub) UpdateSettings(_ context.Context, _ string, input domain.UpdateSettingsInput) (domain.Settings, error) {
	s.calls++
	s.updated = input
	return s.settings, s.err
}

type adminStub struct {
	principal adminauth.Principal
	err       error
}

func (a adminStub) Authenticate(_ context.Context, _ tenantctx.Context, _ string) (adminauth.Principal, error) {
	return a.principal, a.err
}

func TestAdminSettingsAuthorizationAndUpdate(t *testing.T) {
	tenantID := uuid.New()
	base := domain.Settings{TenantID: tenantID, BusinessTimezone: "Europe/Istanbul", BookingIntervalMinutes: 15, ReminderOffsetsMinutes: []int{1440, 120}, CancellationPolicy: "allow_until_notice", CancellationNoticeMinutes: 120, BookingHorizonDays: 60, MinimumBookingNoticeMinutes: 60}
	for _, test := range []struct {
		name       string
		role       string
		method     string
		wantStatus int
		wantCalls  int
	}{
		{name: "owner updates", role: "OWNER", method: http.MethodPatch, wantStatus: http.StatusOK, wantCalls: 1},
		{name: "manager updates operational settings", role: "MANAGER", method: http.MethodPatch, wantStatus: http.StatusOK, wantCalls: 1},
		{name: "barber cannot update", role: "BARBER", method: http.MethodPatch, wantStatus: http.StatusForbidden},
		{name: "receptionist cannot update", role: "RECEPTIONIST", method: http.MethodPatch, wantStatus: http.StatusForbidden},
		{name: "barber can read", role: "BARBER", method: http.MethodGet, wantStatus: http.StatusOK},
	} {
		t.Run(test.name, func(t *testing.T) {
			stub := &settingsStub{settings: base}
			app := fiber.New()
			Handler{settings: stub, auth: internalauth.NewTokenVerifier("secret"), admin: adminStub{principal: adminauth.Principal{Role: test.role, TenantID: tenantID.String(), IdentityID: uuid.New()}}}.Register(app)
			body := strings.NewReader(`{"booking_interval_minutes":30}`)
			request := httptest.NewRequest(test.method, "/internal/v1/admin/settings", body)
			request.Header.Set(internalauth.HeaderName, "secret")
			request.Header.Set(tenantctx.TenantIDHeader, tenantID.String())
			request.Header.Set(tenantctx.AppTypeHeader, "admin")
			request.Header.Set("Cookie", "barber_session=valid")
			request.Header.Set("Content-Type", "application/json")
			response, err := app.Test(request)
			if err != nil || response.StatusCode != test.wantStatus {
				t.Fatalf("status=%v err=%v", responseStatus(response), err)
			}
			if stub.calls != test.wantCalls {
				t.Fatalf("update calls=%d want=%d", stub.calls, test.wantCalls)
			}
			if test.wantCalls == 1 && (stub.updated.BookingIntervalMinutes == nil || *stub.updated.BookingIntervalMinutes != 30) {
				t.Fatalf("unexpected update input: %+v", stub.updated)
			}
		})
	}
}

func TestAdminSettingsRejectsUntrustedContextAndInvalidInput(t *testing.T) {
	tenantID := uuid.New()
	stub := &settingsStub{settings: domain.Settings{TenantID: tenantID}}
	app := fiber.New()
	Handler{settings: stub, auth: internalauth.NewTokenVerifier("secret"), admin: adminStub{principal: adminauth.Principal{Role: "OWNER", TenantID: tenantID.String()}}}.Register(app)

	request := httptest.NewRequest(http.MethodPatch, "/internal/v1/admin/settings", strings.NewReader(`{`))
	request.Header.Set(internalauth.HeaderName, "secret")
	request.Header.Set(tenantctx.TenantIDHeader, tenantID.String())
	request.Header.Set(tenantctx.AppTypeHeader, "admin")
	response, err := app.Test(request)
	if err != nil || response.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid JSON status=%v err=%v", responseStatus(response), err)
	}

	request = httptest.NewRequest(http.MethodGet, "/internal/v1/admin/settings", nil)
	request.Header.Set(tenantctx.TenantIDHeader, tenantID.String())
	request.Header.Set(tenantctx.AppTypeHeader, "admin")
	response, err = app.Test(request)
	if err != nil || response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("missing internal auth status=%v err=%v", responseStatus(response), err)
	}
}

func responseStatus(response *http.Response) int {
	if response == nil {
		return 0
	}
	return response.StatusCode
}
