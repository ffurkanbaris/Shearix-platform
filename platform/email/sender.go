package email

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"net"
	"net/smtp"
	"os"
	"strings"
	"time"
)

type Message struct {
	To             string
	Subject        string
	Text           string
	Template       string
	IdempotencyKey string
}

type Result struct{ ProviderMessageID string }

type Sender interface {
	Send(context.Context, Message) (Result, error)
}

// Backoff parameters for SendWithRetry's retry loop. Kept as unexported
// constants rather than a configurable policy: callers all retry a
// synchronous, in-request SMTP send with the same small attempt count (3),
// so a single bounded default keeps the change minimal. Worst case across 3
// attempts is retryBaseDelay + 2*retryBaseDelay = 600ms of added latency
// (well under the ~5-6s budget), since the last attempt never sleeps.
const (
	retryBaseDelay = 200 * time.Millisecond
	retryMaxDelay  = 2 * time.Second
)

// SendWithRetry retries sender.Send while the error is temporary, waiting a
// bounded, jittered exponential backoff between attempts so a flaky
// downstream provider isn't hammered in a tight loop. It remains a
// synchronous, in-request call: this is not a background/async retry
// mechanism, so the total added delay is deliberately kept small.
func SendWithRetry(ctx context.Context, sender Sender, message Message, attempts int) (Result, error) {
	if attempts < 1 {
		attempts = 1
	}
	var result Result
	var err error
	for attempt := 0; attempt < attempts; attempt++ {
		result, err = sender.Send(ctx, message)
		if err == nil || !IsTemporary(err) {
			return result, err
		}
		if ctx.Err() != nil {
			return Result{}, ctx.Err()
		}
		if attempt == attempts-1 {
			break
		}
		if waitErr := sleepBackoff(ctx, attempt); waitErr != nil {
			return Result{}, waitErr
		}
	}
	return result, err
}

// sleepBackoff blocks for a bounded, full-jitter exponential backoff before
// the next retry attempt (attempt is the 0-indexed attempt that just
// failed). It returns promptly with ctx.Err() if the context is cancelled
// during the wait, so a cancelled request never blocks on the sleep.
func sleepBackoff(ctx context.Context, attempt int) error {
	delay := retryBaseDelay << uint(attempt)
	if delay > retryMaxDelay || delay <= 0 {
		delay = retryMaxDelay
	}
	jittered := time.Duration(rand.Int63n(int64(delay) + 1)) // full jitter: [0, delay]
	return backoffWait(ctx, jittered)
}

// backoffWait actually performs the wait for sleepBackoff. It is a package
// variable so tests can substitute a fast, deterministic stand-in instead of
// sleeping in real time; production code always uses realBackoffWait.
var backoffWait = realBackoffWait

func realBackoffWait(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type TemporaryError struct{ Err error }

func (e TemporaryError) Error() string { return e.Err.Error() }
func (e TemporaryError) Unwrap() error { return e.Err }
func IsTemporary(err error) bool       { var target TemporaryError; return errors.As(err, &target) }

// LoggingSender is safe for development: it records delivery metadata but
// deliberately never records the message subject or body, which may contain a credential.
type LoggingSender struct{ Logger *slog.Logger }

func (s LoggingSender) Send(_ context.Context, m Message) (Result, error) {
	if strings.TrimSpace(m.To) == "" || strings.TrimSpace(m.Template) == "" {
		return Result{}, errors.New("invalid email message")
	}
	id := "dev-" + m.IdempotencyKey
	if s.Logger != nil {
		s.Logger.Info("email accepted", "recipient", m.To, "template", m.Template, "message_id", id)
	}
	return Result{ProviderMessageID: id}, nil
}

type SMTPConfig struct {
	Address, Username, Password, From string
	ImplicitTLS                       bool
	Timeout                           time.Duration
}
type SMTPSender struct{ Config SMTPConfig }

func (s SMTPSender) Send(ctx context.Context, m Message) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	host, _, err := net.SplitHostPort(s.Config.Address)
	if err != nil || host == "" || s.Config.From == "" {
		return Result{}, errors.New("invalid SMTP configuration")
	}
	var auth smtp.Auth
	if s.Config.Username != "" {
		auth = smtp.PlainAuth("", s.Config.Username, s.Config.Password, host)
	}
	body := "From: " + s.Config.From + "\r\nTo: " + m.To + "\r\nSubject: " + sanitizeHeader(m.Subject) + "\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n" + m.Text
	timeout := s.Config.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	dialCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	connection, err := (&net.Dialer{Timeout: timeout}).DialContext(dialCtx, "tcp", s.Config.Address)
	if err != nil {
		return Result{}, TemporaryError{err}
	}
	_ = connection.SetDeadline(time.Now().Add(timeout))
	if s.Config.ImplicitTLS {
		connection = tls.Client(connection, &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12})
	}
	client, err := smtp.NewClient(connection, host)
	if err != nil {
		_ = connection.Close()
		return Result{}, TemporaryError{err}
	}
	defer client.Close()
	if !s.Config.ImplicitTLS {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err = client.StartTLS(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}); err != nil {
				return Result{}, TemporaryError{err}
			}
		}
	}
	if auth != nil {
		if err = client.Auth(auth); err != nil {
			return Result{}, err
		}
	}
	if err = client.Mail(s.Config.From); err == nil {
		err = client.Rcpt(m.To)
	}
	var writer interface {
		Write([]byte) (int, error)
		Close() error
	}
	if err == nil {
		writer, err = client.Data()
	}
	if err == nil {
		_, err = writer.Write([]byte(body))
		if closeErr := writer.Close(); err == nil {
			err = closeErr
		}
	}
	if err != nil {
		return Result{}, TemporaryError{err}
	}
	return Result{ProviderMessageID: fmt.Sprintf("smtp-%s", m.IdempotencyKey)}, nil
}

func sanitizeHeader(v string) string { return strings.NewReplacer("\r", "", "\n", "").Replace(v) }

func FromEnvironment(logger *slog.Logger) (Sender, error) {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("EMAIL_PROVIDER")))
	if mode == "" {
		mode = "log"
	}
	if mode == "log" || mode == "mock" {
		if strings.EqualFold(os.Getenv("APP_ENV"), "production") {
			return nil, errors.New("logging email provider is forbidden in production")
		}
		return LoggingSender{Logger: logger}, nil
	}
	if mode != "smtp" {
		return nil, fmt.Errorf("unsupported EMAIL_PROVIDER %q", mode)
	}
	timeout := 10 * time.Second
	if raw := os.Getenv("SMTP_TIMEOUT"); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil || parsed <= 0 {
			return nil, errors.New("SMTP_TIMEOUT must be a positive duration")
		}
		timeout = parsed
	}
	config := SMTPConfig{Address: os.Getenv("SMTP_ADDRESS"), Username: os.Getenv("SMTP_USERNAME"), Password: os.Getenv("SMTP_PASSWORD"), From: os.Getenv("EMAIL_FROM"), ImplicitTLS: strings.EqualFold(os.Getenv("SMTP_IMPLICIT_TLS"), "true"), Timeout: timeout}
	if config.Address == "" || config.From == "" {
		return nil, errors.New("SMTP_ADDRESS and EMAIL_FROM are required")
	}
	if config.Username != "" && config.Password == "" {
		return nil, errors.New("SMTP_PASSWORD is required when SMTP_USERNAME is configured")
	}
	return SMTPSender{Config: config}, nil
}
