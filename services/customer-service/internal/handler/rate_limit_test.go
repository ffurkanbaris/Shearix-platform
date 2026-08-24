package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/barber-appointment/customer-service/internal/repository"
	"github.com/barber-appointment/platform/authcontract"
	"github.com/barber-appointment/platform/internalauth"
	"github.com/barber-appointment/platform/tenantctx"
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
)

// fakeLimiter is a self-contained, in-memory fixed-window limiter used to
// exercise Handler.limited without depending on a real Redis instance. It
// honors the same Allow(ctx, key, limit, window) contract as
// platform/ratelimit.Redis, but ignores the caller-supplied window in favor
// of a test-controlled one so tests can use short windows without waiting on
// the handler's hardcoded 10/minute limit.
type fakeLimiter struct {
	mu      sync.Mutex
	window  time.Duration
	counts  map[string]int
	resetAt map[string]time.Time
	err     error
}

func newFakeLimiter(window time.Duration) *fakeLimiter {
	return &fakeLimiter{window: window, counts: map[string]int{}, resetAt: map[string]time.Time{}}
}

func (l *fakeLimiter) Allow(_ context.Context, key string, limit int, _ time.Duration) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.err != nil {
		return false, l.err
	}
	now := time.Now()
	if reset, ok := l.resetAt[key]; !ok || now.After(reset) {
		l.counts[key] = 0
		l.resetAt[key] = now.Add(l.window)
	}
	l.counts[key]++
	return l.counts[key] <= limit, nil
}

// limitedTestApp exposes Handler.limited directly over HTTP so the rate
// limiting decision can be exercised without routing through the real
// register/login/forgot business logic (which requires a live database).
func limitedTestApp(h Handler) *fiber.App {
	app := fiber.New()
	app.Post("/limited/:op", func(c fiber.Ctx) error {
		tenant := tenantctx.Context{TenantID: uuid.MustParse(authcontract.TenantID)}
		allowed, err := h.limited(c, tenant, c.Params("op"))
		if err != nil {
			return c.SendStatus(fiber.StatusServiceUnavailable)
		}
		if !allowed {
			return c.SendStatus(fiber.StatusTooManyRequests)
		}
		return c.SendStatus(fiber.StatusOK)
	})
	return app
}

func doLimited(t *testing.T, app *fiber.App, op string) *http.Response {
	t.Helper()
	res, err := app.Test(httptest.NewRequest(http.MethodPost, "/limited/"+op, nil))
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// 1. Allowed requests: N requests within the limit succeed normally.
func TestRateLimitAllowsRequestsWithinLimit(t *testing.T) {
	limiter := newFakeLimiter(time.Minute)
	h := New(repository.Repository{}, internalauth.NewTokenVerifier("t"), nil, "t", "http://unused", nil, limiter)
	app := limitedTestApp(h)
	for i := 0; i < 10; i++ {
		res := doLimited(t, app, "login")
		if res.StatusCode != fiber.StatusOK {
			t.Fatalf("request %d: status=%d, want 200", i+1, res.StatusCode)
		}
		_ = res.Body.Close()
	}
}

// 2. Blocked requests: the (limit+1)th request in the window is rejected
// with 429, and never reaches the real handler logic. Because the HTTP
// route below is built with a zero-value repository.Repository (a nil
// database pool), any attempt to reach the real login/register/forgot logic
// would panic — so an unpanicked 429 response is itself proof the limiter
// short-circuited before touching the database.
func TestRateLimitBlocksRequestsOverLimit(t *testing.T) {
	limiter := newFakeLimiter(time.Minute)
	h := New(repository.Repository{}, internalauth.NewTokenVerifier("t"), nil, "t", "http://unused", nil, limiter)
	app := limitedTestApp(h)
	for i := 0; i < 10; i++ {
		res := doLimited(t, app, "login")
		if res.StatusCode != fiber.StatusOK {
			t.Fatalf("warm-up request %d: status=%d, want 200", i+1, res.StatusCode)
		}
		_ = res.Body.Close()
	}
	res := doLimited(t, app, "login")
	if res.StatusCode != fiber.StatusTooManyRequests {
		t.Fatalf("11th request: status=%d, want 429", res.StatusCode)
	}
	_ = res.Body.Close()

	// Exercise the real registered login route with a separately exhausted
	// limiter bucket (filled directly, not via 10 real HTTP calls, since a
	// zero-value repository would panic if real login logic executed). A
	// single request against the pre-exhausted bucket must come back 429
	// without ever reaching the real login logic — an unpanicked 429 is
	// itself proof the limiter short-circuited before touching the database.
	realLimiter := newFakeLimiter(time.Minute)
	real := New(repository.Repository{}, internalauth.NewTokenVerifier("real-token"), nil, "real-token", "http://unused", nil, realLimiter)
	realApp := fiber.New()
	real.Register(realApp)
	tenantID := uuid.New()
	for i := 0; i < 10; i++ {
		if _, err := realLimiter.Allow(context.Background(), "customer-rate:"+tenantID.String()+":login", 10, time.Minute); err != nil {
			t.Fatal(err)
		}
	}
	blocked := request(t, realApp, "real-token", tenantID, "/internal/v1/public/customer/auth/login", `{"email":"a@example.test","password":"x"}`)
	if blocked.StatusCode != fiber.StatusTooManyRequests {
		t.Fatalf("login over limit: status=%d, want 429", blocked.StatusCode)
	}
	_ = blocked.Body.Close()
}

// 3. TTL recovery: once the window elapses, requests succeed again.
func TestRateLimitRecoversAfterWindow(t *testing.T) {
	limiter := newFakeLimiter(50 * time.Millisecond)
	h := New(repository.Repository{}, internalauth.NewTokenVerifier("t"), nil, "t", "http://unused", nil, limiter)
	app := limitedTestApp(h)
	for i := 0; i < 10; i++ {
		res := doLimited(t, app, "register")
		if res.StatusCode != fiber.StatusOK {
			t.Fatalf("warm-up request %d: status=%d, want 200", i+1, res.StatusCode)
		}
		_ = res.Body.Close()
	}
	blocked := doLimited(t, app, "register")
	if blocked.StatusCode != fiber.StatusTooManyRequests {
		t.Fatalf("request within window: status=%d, want 429", blocked.StatusCode)
	}
	_ = blocked.Body.Close()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		res := doLimited(t, app, "register")
		if res.StatusCode == fiber.StatusOK {
			_ = res.Body.Close()
			return
		}
		_ = res.Body.Close()
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("rate limit window did not expire")
}

// 4. Redis failure behavior: the endpoint fails closed with 503, never
// silently bypassing the limiter, and never reaching real business logic
// (verified the same way as the blocked-request case: a zero-value
// repository would panic if reached).
func TestRateLimitFailsClosedOnLimiterError(t *testing.T) {
	limiter := &fakeLimiter{err: errors.New("dial tcp: connection refused")}
	h := New(repository.Repository{}, internalauth.NewTokenVerifier("t"), nil, "t", "http://unused", nil, limiter)
	app := limitedTestApp(h)
	res := doLimited(t, app, "login")
	if res.StatusCode != fiber.StatusServiceUnavailable {
		t.Fatalf("status=%d, want 503", res.StatusCode)
	}
	_ = res.Body.Close()

	real := New(repository.Repository{}, internalauth.NewTokenVerifier("real-token"), nil, "real-token", "http://unused", nil, limiter)
	realApp := fiber.New()
	real.Register(realApp)
	tenantID := uuid.New()
	for _, path := range []string{
		"/internal/v1/public/customer/auth/login",
		"/internal/v1/public/customer/auth/register",
		"/internal/v1/public/customer/auth/forgot-password",
	} {
		res := request(t, realApp, "real-token", tenantID, path, `{"email":"a@example.test","password":"x"}`)
		if res.StatusCode != fiber.StatusServiceUnavailable {
			t.Fatalf("%s: status=%d, want 503", path, res.StatusCode)
		}
		_ = res.Body.Close()
	}
}

// The nil-limiter case (no limiter passed to New) fails closed too, exactly
// matching auth-service's Handler.limited behavior.
func TestRateLimitNilLimiterFailsClosed(t *testing.T) {
	h := New(repository.Repository{}, internalauth.NewTokenVerifier("t"), nil, "t", "http://unused", nil)
	app := limitedTestApp(h)
	res := doLimited(t, app, "login")
	if res.StatusCode != fiber.StatusTooManyRequests {
		t.Fatalf("status=%d, want 429", res.StatusCode)
	}
	_ = res.Body.Close()
}

// 5. No enumeration leakage: rate-limited (429) responses to
// forgot-password are byte-identical regardless of whether the supplied
// email looks like an existing account, both under and at the rate limit
// boundary. The limiter key is tenant+operation only, never the email, so
// it cannot introduce a distinguishing signal.
func TestForgotPasswordRateLimitAddsNoEnumerationSignal(t *testing.T) {
	limiter := newFakeLimiter(time.Minute)
	h := New(repository.Repository{}, internalauth.NewTokenVerifier("t"), nil, "t", "http://unused", nil, limiter)
	app := fiber.New()
	h.Register(app)
	tenantID := uuid.New()

	describe := func(res *http.Response) (int, string, string) {
		t.Helper()
		body := make([]byte, 0)
		buf := make([]byte, 512)
		for {
			n, err := res.Body.Read(buf)
			body = append(body, buf[:n]...)
			if err != nil {
				break
			}
		}
		_ = res.Body.Close()
		return res.StatusCode, string(body), res.Header.Get("Content-Type")
	}

	// Drive the shared tenant+operation bucket to its boundary directly
	// (rather than via 10 real HTTP calls, since a zero-value repository
	// would panic if the real forgot-password logic executed for an
	// allowed request). Both existing-looking and nonexistent-looking
	// requests share this same tenant+operation bucket, since the limiter
	// key never includes the email.
	for i := 0; i < 10; i++ {
		if _, err := limiter.Allow(context.Background(), "customer-rate:"+tenantID.String()+":forgot-password", 10, time.Minute); err != nil {
			t.Fatal(err)
		}
	}

	existingBlocked := request(t, app, "t", tenantID, "/internal/v1/public/customer/auth/forgot-password", `{"email":"existing-looking@example.test"}`)
	nonexistentBlocked := request(t, app, "t", tenantID, "/internal/v1/public/customer/auth/forgot-password", `{"email":"definitely-not-registered-`+uuid.NewString()+`@example.test"}`)

	existingStatus, existingBody, existingType := describe(existingBlocked)
	nonexistentStatus, nonexistentBody, nonexistentType := describe(nonexistentBlocked)

	if existingStatus != fiber.StatusTooManyRequests || nonexistentStatus != fiber.StatusTooManyRequests {
		t.Fatalf("expected both blocked responses to be 429, got existing=%d nonexistent=%d", existingStatus, nonexistentStatus)
	}
	if existingStatus != nonexistentStatus || existingBody != nonexistentBody || existingType != nonexistentType {
		t.Fatalf("rate-limited forgot-password responses diverged by email existence: existing=(%d,%q,%q) nonexistent=(%d,%q,%q)",
			existingStatus, existingBody, existingType, nonexistentStatus, nonexistentBody, nonexistentType)
	}
	if strings.Contains(existingBody, "existing-looking") || strings.Contains(nonexistentBody, "not-registered") {
		t.Fatal("rate-limited response echoed the requested email")
	}
}
