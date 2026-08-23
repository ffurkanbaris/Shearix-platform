package email

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

type countingSender struct {
	calls    int
	failures int
}

func (s *countingSender) Send(context.Context, Message) (Result, error) {
	s.calls++
	if s.calls <= s.failures {
		return Result{}, TemporaryError{Err: context.DeadlineExceeded}
	}
	return Result{ProviderMessageID: "ok"}, nil
}

func TestLoggingSenderNeverLogsMessageBody(t *testing.T) {
	var output bytes.Buffer
	sender := LoggingSender{Logger: slog.New(slog.NewTextHandler(&output, nil))}
	secret := "plain-secret-must-not-leak"
	result, err := sender.Send(context.Background(), Message{To: "user@example.com", Subject: "secret", Text: secret, Template: "user_initial_password", IdempotencyKey: "delivery"})
	if err != nil || result.ProviderMessageID == "" {
		t.Fatalf("send result=%+v err=%v", result, err)
	}
	logged := output.String()
	if strings.Contains(logged, secret) || strings.Contains(logged, "Subject=secret") {
		t.Fatalf("sensitive message was logged: %s", logged)
	}
	if !strings.Contains(logged, "user@example.com") || !strings.Contains(logged, "user_initial_password") {
		t.Fatalf("delivery metadata missing: %s", logged)
	}
}
func TestSendWithRetryIsOnceOnSuccessAndRetriesTemporaryFailures(t *testing.T) {
	success := &countingSender{}
	if _, err := SendWithRetry(context.Background(), success, Message{}, 3); err != nil || success.calls != 1 {
		t.Fatalf("success calls=%d err=%v", success.calls, err)
	}
	retry := &countingSender{failures: 2}
	if _, err := SendWithRetry(context.Background(), retry, Message{}, 3); err != nil || retry.calls != 3 {
		t.Fatalf("retry calls=%d err=%v", retry.calls, err)
	}
}

type permanentSender struct{ calls int }

func (s *permanentSender) Send(context.Context, Message) (Result, error) {
	s.calls++
	return Result{}, errors.New("permanent failure")
}

// TestSendWithRetryBacksOffBetweenAttempts verifies that a backoff wait is
// actually invoked between retries (attempts-1 times, once per retried
// failure, never after the final attempt), using the backoffWait seam so the
// test is fast and deterministic rather than relying on real elapsed time.
func TestSendWithRetryBacksOffBetweenAttempts(t *testing.T) {
	original := backoffWait
	defer func() { backoffWait = original }()

	var waits []time.Duration
	backoffWait = func(ctx context.Context, d time.Duration) error {
		waits = append(waits, d)
		return nil
	}

	retry := &countingSender{failures: 2}
	start := time.Now()
	if _, err := SendWithRetry(context.Background(), retry, Message{}, 3); err != nil || retry.calls != 3 {
		t.Fatalf("retry calls=%d err=%v", retry.calls, err)
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("expected no real sleeping with backoffWait stubbed, elapsed=%v", elapsed)
	}
	if len(waits) != 2 {
		t.Fatalf("expected 2 backoff waits (one between each of 3 attempts), got %d: %v", len(waits), waits)
	}
	for i, d := range waits {
		if d < 0 || d > retryMaxDelay {
			t.Fatalf("wait[%d]=%v outside bounds [0, %v]", i, d, retryMaxDelay)
		}
	}
}

// TestSendWithRetryCancelledContextReturnsPromptly verifies that cancelling
// the context during a backoff sleep returns quickly rather than waiting out
// the full backoff window, so a disconnected client doesn't hold a
// connection open through a sleep.
func TestSendWithRetryCancelledContextReturnsPromptly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	alwaysTemporary := &countingSender{failures: 100}
	start := time.Now()
	_, err := SendWithRetry(ctx, alwaysTemporary, Message{}, 5)
	elapsed := time.Since(start)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	// Generous tolerance: worst case unbounded backoff across 5 attempts
	// would be well over a second; returning within well under that
	// confirms cancellation interrupts the sleep rather than waiting it out.
	if elapsed > 500*time.Millisecond {
		t.Fatalf("cancellation did not return promptly, elapsed=%v", elapsed)
	}
}

// TestSendWithRetryPermanentErrorFailsFastWithoutBackoff verifies that a
// non-temporary error still fails on the first attempt with no retry and no
// backoff delay.
func TestSendWithRetryPermanentErrorFailsFastWithoutBackoff(t *testing.T) {
	sender := &permanentSender{}
	start := time.Now()
	if _, err := SendWithRetry(context.Background(), sender, Message{}, 3); err == nil {
		t.Fatal("expected permanent error to be returned")
	}
	if sender.calls != 1 {
		t.Fatalf("expected exactly 1 call for a permanent error, got %d", sender.calls)
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("expected no backoff delay for a permanent error, elapsed=%v", elapsed)
	}
}

func TestSMTPConfigurationRequiresBoundedValidSettings(t *testing.T) {
	t.Setenv("EMAIL_PROVIDER", "smtp")
	t.Setenv("SMTP_ADDRESS", "smtp.example.test:587")
	t.Setenv("EMAIL_FROM", "sender@example.test")
	t.Setenv("SMTP_TIMEOUT", "-1s")
	if _, err := FromEnvironment(nil); err == nil {
		t.Fatal("negative SMTP timeout was accepted")
	}
	t.Setenv("SMTP_TIMEOUT", "5s")
	t.Setenv("SMTP_USERNAME", "user")
	t.Setenv("SMTP_PASSWORD", "")
	if _, err := FromEnvironment(nil); err == nil {
		t.Fatal("authenticated SMTP without password was accepted")
	}
}
