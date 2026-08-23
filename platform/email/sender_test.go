package email

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
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
