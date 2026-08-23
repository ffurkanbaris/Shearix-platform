package handler_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/barber-appointment/appointment-service/internal/application"
	"github.com/barber-appointment/appointment-service/internal/domain"
	appointmenthandler "github.com/barber-appointment/appointment-service/internal/handler"
	"github.com/barber-appointment/appointment-service/internal/repository"
	"github.com/barber-appointment/platform/adminauth"
	"github.com/barber-appointment/platform/internalauth"
	"github.com/barber-appointment/platform/schedulingdeps"
	"github.com/barber-appointment/platform/tenantctx"
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type lifecycleDeps struct{}

func (lifecycleDeps) BranchBarber(context.Context, tenantctx.Context, uuid.UUID, uuid.UUID) error {
	return nil
}
func (lifecycleDeps) Barber(context.Context, tenantctx.Context, uuid.UUID) error { return nil }
func (lifecycleDeps) Service(context.Context, tenantctx.Context, uuid.UUID, uuid.UUID) (schedulingdeps.Service, error) {
	return schedulingdeps.Service{DurationMinutes: 30}, nil
}
func (lifecycleDeps) Settings(context.Context, tenantctx.Context) (schedulingdeps.TenantSettings, error) {
	return schedulingdeps.TenantSettings{CancellationPolicy: "allow_until_notice", CancellationNoticeMinutes: 0}, nil
}
func (lifecycleDeps) Available(context.Context, tenantctx.Context, domain.CreateInput, time.Time) error {
	return nil
}
func (lifecycleDeps) Customer(context.Context, tenantctx.Context, uuid.UUID) (application.CustomerSnapshot, error) {
	return application.CustomerSnapshot{}, nil
}

func TestAppointmentLifecycleHTTPIntegration(t *testing.T) {
	appDSN, ownerDSN := os.Getenv("APPOINTMENT_TEST_DATABASE_URL"), os.Getenv("APPOINTMENT_TEST_OWNER_DATABASE_URL")
	if appDSN == "" || ownerDSN == "" {
		t.Skip("APPOINTMENT_TEST_DATABASE_URL and APPOINTMENT_TEST_OWNER_DATABASE_URL are required")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, appDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	owner, err := pgx.Connect(ctx, ownerDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close(ctx)
	if _, err = owner.Exec(ctx, "SET ROLE appointment_db_owner"); err != nil {
		t.Fatal(err)
	}
	tenantA, tenantB := uuid.New(), uuid.New()
	defer func() {
		for _, tenant := range []uuid.UUID{tenantA, tenantB} {
			_, _ = owner.Exec(ctx, `DELETE FROM public.outbox_events WHERE tenant_id=$1; DELETE FROM public.appointment_events WHERE tenant_id=$1; DELETE FROM public.idempotency_keys WHERE tenant_id=$1; DELETE FROM public.appointments WHERE tenant_id=$1`, tenant)
		}
	}()

	auth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		role := strings.TrimPrefix(r.Header.Get("Cookie"), "role=")
		if role == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(adminauth.Principal{Role: role, TenantID: r.Header.Get(tenantctx.TenantIDHeader), IdentityID: uuid.New()})
	}))
	defer auth.Close()
	repo := repository.New(pool)
	deps := lifecycleDeps{}
	app := fiber.New()
	appointmenthandler.New(application.NewBooking(repo, deps), application.NewLifecycle(repo, deps), application.NewQuery(repo, deps), internalauth.NewTokenVerifier("internal-secret"), adminauth.New(auth.URL, "internal-secret")).Register(app)

	sequence := 0
	create := func(tenant uuid.UUID, customer *uuid.UUID, state string) domain.Appointment {
		sequence++
		start := time.Date(2035, 1, 2, 9+sequence, 0, 0, 0, time.UTC)
		in := domain.CreateInput{BranchID: uuid.New(), BarberID: uuid.New(), ServiceID: uuid.New(), CustomerID: customer, CustomerName: "Lifecycle", CustomerContact: "lifecycle@example.test", StartAt: start}
		a, createErr := repo.Create(ctx, tenant, in, start.Add(30*time.Minute), start, start.Add(30*time.Minute), "test", "test", fmt.Sprintf("lifecycle-%d", sequence), strings.Repeat("a", 64))
		if createErr != nil {
			t.Fatal(createErr)
		}
		if state == "confirmed" {
			a, createErr = repo.Transition(ctx, tenant, a.ID, "confirmed")
		} else if state != "pending" {
			a, createErr = repo.Transition(ctx, tenant, a.ID, "confirmed")
			if createErr == nil {
				a, createErr = repo.Transition(ctx, tenant, a.ID, state)
			}
		}
		if createErr != nil {
			t.Fatal(createErr)
		}
		return a
	}
	request := func(method, path string, tenant uuid.UUID, appType, role, customer, body string) *http.Response {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set(internalauth.HeaderName, "internal-secret")
		req.Header.Set(tenantctx.TenantIDHeader, tenant.String())
		req.Header.Set(tenantctx.AppTypeHeader, appType)
		if role != "" {
			req.Header.Set("Cookie", "role="+role)
		}
		if customer != "" {
			req.Header.Set("X-Customer-ID", customer)
		}
		res, requestErr := app.Test(req)
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		return res
	}
	counts := func(tenant uuid.UUID, appointment uuid.UUID) (events, outbox int) {
		if err = owner.QueryRow(ctx, `SELECT count(*) FROM public.appointment_events WHERE tenant_id=$1 AND appointment_id=$2`, tenant, appointment).Scan(&events); err != nil {
			t.Fatal(err)
		}
		if err = owner.QueryRow(ctx, `SELECT count(*) FROM public.outbox_events WHERE tenant_id=$1 AND aggregate_id=$2`, tenant, appointment).Scan(&outbox); err != nil {
			t.Fatal(err)
		}
		return
	}

	for _, tc := range []struct{ from, operation, to string }{
		{"pending", "confirm", "confirmed"}, {"pending", "cancel", "cancelled"}, {"confirmed", "cancel", "cancelled"}, {"confirmed", "complete", "completed"}, {"confirmed", "no-show", "no_show"},
	} {
		t.Run(tc.from+"_to_"+tc.to, func(t *testing.T) {
			a := create(tenantA, nil, tc.from)
			beforeEvents, beforeOutbox := counts(tenantA, a.ID)
			res := request(http.MethodPost, "/internal/v1/admin/appointments/"+a.ID.String()+"/"+tc.operation, tenantA, "admin", "OWNER", "", "")
			defer res.Body.Close()
			if res.StatusCode != http.StatusOK {
				t.Fatalf("status=%d", res.StatusCode)
			}
			got, getErr := repo.Get(ctx, tenantA, a.ID)
			afterEvents, afterOutbox := counts(tenantA, a.ID)
			if getErr != nil || got.Status != tc.to || afterEvents != beforeEvents+1 || afterOutbox != beforeOutbox+1 {
				t.Fatalf("state=%s events=%d/%d outbox=%d/%d err=%v", got.Status, beforeEvents, afterEvents, beforeOutbox, afterOutbox, getErr)
			}
		})
	}

	for _, role := range []string{"OWNER", "MANAGER", "RECEPTIONIST", "BARBER"} {
		for _, operation := range []struct{ name, source string }{{"confirm", "pending"}, {"cancel", "pending"}, {"complete", "confirmed"}, {"no-show", "confirmed"}, {"reschedule", "pending"}} {
			t.Run(role+"_"+operation.name, func(t *testing.T) {
				a := create(tenantA, nil, operation.source)
				beforeEvents, beforeOutbox := counts(tenantA, a.ID)
				body := ""
				if operation.name == "reschedule" {
					body = `{"start_at":"2038-01-01T10:00:00Z"}`
				}
				res := request(http.MethodPost, "/internal/v1/admin/appointments/"+a.ID.String()+"/"+operation.name, tenantA, "admin", role, "", body)
				defer res.Body.Close()
				want := http.StatusOK
				if role == "BARBER" {
					want = http.StatusForbidden
				}
				if res.StatusCode != want {
					t.Fatalf("status=%d want=%d", res.StatusCode, want)
				}
				if role == "BARBER" {
					got, _ := repo.Get(ctx, tenantA, a.ID)
					afterEvents, afterOutbox := counts(tenantA, a.ID)
					if got.Status != operation.source || beforeEvents != afterEvents || beforeOutbox != afterOutbox {
						t.Fatalf("forbidden mutation state=%s events=%d/%d outbox=%d/%d", got.Status, beforeEvents, afterEvents, beforeOutbox, afterOutbox)
					}
				}
			})
		}
	}
	for _, role := range []string{"OWNER", "MANAGER", "RECEPTIONIST", "BARBER"} {
		a := create(tenantA, nil, "pending")
		res := request(http.MethodGet, "/internal/v1/admin/appointments/"+a.ID.String(), tenantA, "admin", role, "", "")
		if res.StatusCode != http.StatusOK {
			t.Fatalf("read role=%s status=%d", role, res.StatusCode)
		}
		_ = res.Body.Close()
	}

	terminal := create(tenantA, nil, "completed")
	beforeEvents, beforeOutbox := counts(tenantA, terminal.ID)
	res := request(http.MethodPost, "/internal/v1/admin/appointments/"+terminal.ID.String()+"/confirm", tenantA, "admin", "OWNER", "", "")
	defer res.Body.Close()
	afterEvents, afterOutbox := counts(tenantA, terminal.ID)
	if res.StatusCode != http.StatusConflict || beforeEvents != afterEvents || beforeOutbox != afterOutbox {
		t.Fatalf("invalid transition status=%d events=%d/%d outbox=%d/%d", res.StatusCode, beforeEvents, afterEvents, beforeOutbox, afterOutbox)
	}

	other := create(tenantB, nil, "pending")
	beforeEvents, beforeOutbox = counts(tenantB, other.ID)
	for _, operation := range []string{"confirm", "cancel", "complete", "no-show", "reschedule"} {
		body := ""
		if operation == "reschedule" {
			body = `{"start_at":"2039-01-01T10:00:00Z"}`
		}
		cross := request(http.MethodPost, "/internal/v1/admin/appointments/"+other.ID.String()+"/"+operation, tenantA, "admin", "OWNER", "", body)
		if cross.StatusCode != http.StatusNotFound {
			t.Fatalf("cross-tenant %s status=%d", operation, cross.StatusCode)
		}
		_ = cross.Body.Close()
	}
	crossGet := request(http.MethodGet, "/internal/v1/admin/appointments/"+other.ID.String(), tenantA, "admin", "OWNER", "", "")
	if crossGet.StatusCode != http.StatusNotFound {
		t.Fatalf("cross-tenant get status=%d", crossGet.StatusCode)
	}
	_ = crossGet.Body.Close()
	gotOther, _ := repo.Get(ctx, tenantB, other.ID)
	afterEvents, afterOutbox = counts(tenantB, other.ID)
	if gotOther.Status != "pending" || beforeEvents != afterEvents || beforeOutbox != afterOutbox {
		t.Fatalf("cross-tenant side effect state=%s events=%d/%d outbox=%d/%d", gotOther.Status, beforeEvents, afterEvents, beforeOutbox, afterOutbox)
	}

	customerA, customerB := uuid.New(), uuid.New()
	customerAppointment := create(tenantA, &customerB, "pending")
	for _, path := range []string{"/internal/v1/customer/appointments/" + customerAppointment.ID.String(), "/internal/v1/customer/appointments/" + customerAppointment.ID.String() + "/cancel"} {
		method := http.MethodGet
		if strings.HasSuffix(path, "/cancel") {
			method = http.MethodPost
		}
		denied := request(method, path, tenantA, "booking", "", customerA.String(), "")
		if denied.StatusCode != http.StatusNotFound {
			t.Fatalf("customer ownership path=%s status=%d", path, denied.StatusCode)
		}
		_ = denied.Body.Close()
	}
	guest := request(http.MethodPost, "/internal/v1/public/appointments/"+customerAppointment.ID.String()+"/cancel", tenantA, "booking", "", "", "")
	if guest.StatusCode != http.StatusUnauthorized {
		t.Fatalf("guest cancellation status=%d", guest.StatusCode)
	}
	_ = guest.Body.Close()
}
