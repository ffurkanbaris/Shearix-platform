package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/barber-appointment/customer-service/internal/repository"
	platformemail "github.com/barber-appointment/platform/email"
	"github.com/barber-appointment/platform/internalauth"
	"github.com/barber-appointment/platform/tenantctx"
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

type testSender struct {
	mu       sync.Mutex
	calls    int
	messages []platformemail.Message
	err      error
	entered  chan struct{}
	release  chan struct{}
}

func (s *testSender) Send(_ context.Context, message platformemail.Message) (platformemail.Result, error) {
	s.mu.Lock()
	s.calls++
	s.messages = append(s.messages, message)
	err := s.err
	s.mu.Unlock()
	if s.entered != nil {
		s.entered <- struct{}{}
	}
	if s.release != nil {
		<-s.release
	}
	return platformemail.Result{ProviderMessageID: "test-message"}, err
}

func (s *testSender) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func (s *testSender) lastMessage() platformemail.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.messages[len(s.messages)-1]
}

func TestCredentialDeliveryFailureIntegration(t *testing.T) {
	appDSN, ownerDSN := os.Getenv("CUSTOMER_TEST_DATABASE_URL"), os.Getenv("CUSTOMER_TEST_OWNER_DATABASE_URL")
	if appDSN == "" || ownerDSN == "" {
		t.Skip("CUSTOMER_TEST_DATABASE_URL and CUSTOMER_TEST_OWNER_DATABASE_URL are required")
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
	if _, err = owner.Exec(ctx, "SET ROLE customer_db_owner"); err != nil {
		t.Fatal(err)
	}
	tenantID := uuid.New()
	defer func() { _, _ = owner.Exec(ctx, `DELETE FROM public.customers WHERE tenant_id=$1`, tenantID) }()
	repo := repository.New(pool)
	const token = "integration-internal-token"

	failing := &testSender{err: errors.New("provider unavailable")}
	app := fiber.New()
	New(repo, internalauth.NewTokenVerifier(token), failing, token, "http://unused", newFakeLimiter(time.Minute)).Register(app)
	response := request(t, app, token, tenantID, "/internal/v1/public/customer/auth/register", `{"name":"Delivery Failure","email":"failed@example.test"}`)
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("failed registration status = %d", response.StatusCode)
	}
	body, _ := io.ReadAll(response.Body)
	if strings.Contains(string(body), "password") {
		t.Fatal("registration response exposed a credential")
	}
	failed, failedHash, err := repo.FindByEmail(ctx, tenantID, "failed@example.test")
	if err != nil {
		t.Fatalf("failed delivery deleted account: %v", err)
	}
	if failed.InitialDeliveryStatus != "failed" || !failed.MustChangePassword || failedHash == "" {
		t.Fatalf("unexpected failed delivery state: status=%q must_change=%v", failed.InitialDeliveryStatus, failed.MustChangePassword)
	}
	if failing.callCount() != 1 {
		t.Fatalf("non-temporary provider failure calls = %d", failing.callCount())
	}

	blocking := &testSender{entered: make(chan struct{}, 1), release: make(chan struct{})}
	concurrentApp := fiber.New()
	New(repo, internalauth.NewTokenVerifier(token), blocking, token, "http://unused", newFakeLimiter(time.Minute)).Register(concurrentApp)
	firstDone := make(chan *http.Response, 1)
	go func() {
		firstDone <- request(t, concurrentApp, token, tenantID, "/internal/v1/public/customer/auth/register", `{"name":"Delivery Failure","email":"failed@example.test"}`)
	}()
	<-blocking.entered
	concurrent := request(t, concurrentApp, token, tenantID, "/internal/v1/public/customer/auth/register", `{"name":"Delivery Failure","email":"failed@example.test"}`)
	if concurrent.StatusCode != http.StatusConflict {
		t.Fatalf("concurrent retry status = %d", concurrent.StatusCode)
	}
	close(blocking.release)
	if completed := <-firstDone; completed.StatusCode != http.StatusOK {
		t.Fatalf("claimed retry status = %d", completed.StatusCode)
	}
	if blocking.callCount() != 1 {
		t.Fatalf("concurrent provider calls = %d", blocking.callCount())
	}

	success := &testSender{}
	successApp := fiber.New()
	New(repo, internalauth.NewTokenVerifier(token), success, token, "http://unused", newFakeLimiter(time.Minute)).Register(successApp)
	response = request(t, successApp, token, tenantID, "/internal/v1/public/customer/auth/register", `{"name":"Reset Failure","email":"reset@example.test"}`)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("setup registration status = %d", response.StatusCode)
	}
	_, oldHash, err := repo.FindByEmail(ctx, tenantID, "reset@example.test")
	if err != nil {
		t.Fatal(err)
	}
	response = request(t, successApp, token, tenantID, "/internal/v1/public/customer/auth/register", `{"name":"Duplicate","email":"reset@example.test"}`)
	if response.StatusCode != http.StatusConflict || success.callCount() != 1 {
		t.Fatalf("duplicate registration status=%d provider_calls=%d", response.StatusCode, success.callCount())
	}

	// The controlled sender retains the credential only in this test process.
	// Never include the body or extracted password in test output.
	message := success.lastMessage()
	const prefix = "Your initial password is: "
	line := strings.SplitN(message.Text, "\n", 2)[0]
	if !strings.HasPrefix(line, prefix) {
		t.Fatal("initial credential message did not use the expected template")
	}
	initialPassword := strings.TrimPrefix(line, prefix)
	login := request(t, successApp, token, tenantID, "/internal/v1/public/customer/auth/login", `{"email":"reset@example.test","password":"`+initialPassword+`"}`)
	if login.StatusCode != http.StatusOK {
		t.Fatalf("initial credential login status = %d", login.StatusCode)
	}
	var loggedIn struct {
		MustChangePassword bool `json:"must_change_password"`
	}
	if err = json.NewDecoder(login.Body).Decode(&loggedIn); err != nil || !loggedIn.MustChangePassword {
		t.Fatal("initial login did not require password replacement")
	}
	cookies := login.Cookies()
	if len(cookies) == 0 {
		t.Fatal("initial login did not establish a session")
	}
	changeReq, err := http.NewRequest(http.MethodPost, "/internal/v1/public/customer/auth/change-password", strings.NewReader(`{"current_password":"`+initialPassword+`","new_password":"replacement-password-123"}`))
	if err != nil {
		t.Fatal(err)
	}
	changeReq.Header.Set("Content-Type", "application/json")
	changeReq.Header.Set(internalauth.HeaderName, token)
	changeReq.Header.Set(tenantctx.TenantIDHeader, tenantID.String())
	changeReq.Header.Set(tenantctx.AppTypeHeader, "booking")
	changeReq.AddCookie(cookies[0])
	changed, err := successApp.Test(changeReq)
	if err != nil || changed.StatusCode != http.StatusNoContent {
		t.Fatalf("password replacement status = %d", changed.StatusCode)
	}
	updated, activeHash, err := repo.FindByEmail(ctx, tenantID, "reset@example.test")
	if err != nil || updated.MustChangePassword {
		t.Fatal("successful password replacement did not clear must_change_password")
	}
	oldHash = activeHash
	oldTemporaryLogin := request(t, successApp, token, tenantID, "/internal/v1/public/customer/auth/login", `{"email":"reset@example.test","password":"`+initialPassword+`"}`)
	if oldTemporaryLogin.StatusCode != http.StatusUnauthorized {
		t.Fatalf("old temporary credential remained valid status=%d", oldTemporaryLogin.StatusCode)
	}
	activeLogin := request(t, successApp, token, tenantID, "/internal/v1/public/customer/auth/login", `{"email":"reset@example.test","password":"replacement-password-123"}`)
	if activeLogin.StatusCode != http.StatusOK {
		t.Fatalf("replacement credential login status=%d", activeLogin.StatusCode)
	}
	_ = activeLogin.Body.Close()

	resetFailure := &testSender{err: errors.New("provider unavailable")}
	resetApp := fiber.New()
	New(repo, internalauth.NewTokenVerifier(token), resetFailure, token, "http://unused", newFakeLimiter(time.Minute)).Register(resetApp)
	response = request(t, resetApp, token, tenantID, "/internal/v1/public/customer/auth/forgot-password", `{"email":"reset@example.test"}`)
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("failed reset status = %d", response.StatusCode)
	}
	_, preservedHash, err := repo.FindByEmail(ctx, tenantID, "reset@example.test")
	if err != nil {
		t.Fatal(err)
	}
	if preservedHash != oldHash || bcrypt.CompareHashAndPassword([]byte(preservedHash), []byte("definitely-not-the-password")) == nil {
		t.Fatal("failed reset changed the previous password hash")
	}
	meAfterFailure := authenticatedRequest(t, resetApp, token, tenantID, cookies[0], http.MethodGet, "/internal/v1/public/customer/auth/me", "")
	if meAfterFailure.StatusCode != http.StatusOK {
		t.Fatalf("failed reset revoked the existing session status=%d", meAfterFailure.StatusCode)
	}
	_ = meAfterFailure.Body.Close()

	response = request(t, successApp, token, tenantID, "/internal/v1/public/customer/auth/forgot-password", `{"email":"reset@example.test"}`)
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("successful reset status=%d", response.StatusCode)
	}
	resetMessage := success.lastMessage()
	const resetPrefix = "Your temporary password is: "
	resetLine := strings.SplitN(resetMessage.Text, "\n", 2)[0]
	if !strings.HasPrefix(resetLine, resetPrefix) {
		t.Fatal("reset credential message did not use the expected template")
	}
	resetPassword := strings.TrimPrefix(resetLine, resetPrefix)
	oldPasswordLogin := request(t, successApp, token, tenantID, "/internal/v1/public/customer/auth/login", `{"email":"reset@example.test","password":"replacement-password-123"}`)
	if oldPasswordLogin.StatusCode != http.StatusUnauthorized {
		t.Fatalf("old password remained valid after reset status=%d", oldPasswordLogin.StatusCode)
	}
	resetLogin := request(t, successApp, token, tenantID, "/internal/v1/public/customer/auth/login", `{"email":"reset@example.test","password":"`+resetPassword+`"}`)
	if resetLogin.StatusCode != http.StatusOK {
		t.Fatalf("reset credential login status=%d", resetLogin.StatusCode)
	}
	var resetCustomer struct {
		MustChangePassword bool `json:"must_change_password"`
	}
	if err = json.NewDecoder(resetLogin.Body).Decode(&resetCustomer); err != nil || !resetCustomer.MustChangePassword {
		t.Fatal("reset login did not require password replacement")
	}
	oldSession := authenticatedRequest(t, successApp, token, tenantID, cookies[0], http.MethodGet, "/internal/v1/public/customer/auth/me", "")
	if oldSession.StatusCode != http.StatusUnauthorized {
		t.Fatalf("successful reset did not revoke existing session status=%d", oldSession.StatusCode)
	}
}

func authenticatedRequest(t *testing.T, app *fiber.App, token string, tenantID uuid.UUID, cookie *http.Cookie, method, path, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(internalauth.HeaderName, token)
	req.Header.Set(tenantctx.TenantIDHeader, tenantID.String())
	req.Header.Set(tenantctx.AppTypeHeader, "booking")
	req.AddCookie(cookie)
	response, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func request(t *testing.T, app *fiber.App, token string, tenantID uuid.UUID, path, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(internalauth.HeaderName, token)
	req.Header.Set(tenantctx.TenantIDHeader, tenantID.String())
	req.Header.Set(tenantctx.AppTypeHeader, "booking")
	response, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	return response
}
