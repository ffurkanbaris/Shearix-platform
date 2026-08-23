package email

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
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
	}
	return result, err
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
