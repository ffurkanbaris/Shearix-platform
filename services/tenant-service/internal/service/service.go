package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"net"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/barber-appointment/tenant-service/internal/domain"
	"github.com/barber-appointment/tenant-service/internal/repository"
	"github.com/google/uuid"
)

var (
	ErrInvalidInput            = errors.New("invalid tenant control-plane input")
	ErrForbidden               = errors.New("tenant settings update forbidden")
	ErrVerificationFailed      = errors.New("domain verification failed")
	ErrVerificationRequired    = errors.New("domain verification required before activation")
	ErrVerificationUnavailable = errors.New("domain verification token unavailable")
)

type TXTResolver interface {
	LookupTXT(ctx context.Context, name string) ([]string, error)
}

type netTXTResolver struct{ resolver *net.Resolver }

func (r netTXTResolver) LookupTXT(ctx context.Context, name string) ([]string, error) {
	return r.resolver.LookupTXT(ctx, name)
}

type Dependencies struct {
	DNS                   TXTResolver
	AllowLocalhostDomains bool
}

type TenantService struct {
	repository            repository.TenantRepository
	dns                   TXTResolver
	allowLocalhostDomains bool
}

// New accepts optional explicit dependencies so production can disable
// localhost domains while focused tests can supply a deterministic TXT lookup.
func New(repository repository.TenantRepository, dependencies ...Dependencies) TenantService {
	dep := Dependencies{DNS: netTXTResolver{resolver: net.DefaultResolver}, AllowLocalhostDomains: true}
	if len(dependencies) > 0 {
		if dependencies[0].DNS != nil {
			dep.DNS = dependencies[0].DNS
		}
		dep.AllowLocalhostDomains = dependencies[0].AllowLocalhostDomains
	}
	return TenantService{repository: repository, dns: dep.DNS, allowLocalhostDomains: dep.AllowLocalhostDomains}
}

func (s TenantService) Resolve(ctx context.Context, hostname string) (domain.DomainResolution, error) {
	normalized, err := NormalizeHostname(hostname, s.allowLocalhostDomains)
	if err != nil {
		return domain.DomainResolution{}, ErrInvalidInput
	}
	return s.repository.ResolveDomain(ctx, normalized)
}

// TLSAuthorized is intentionally narrower than Resolve: Caddy needs only an
// allow/deny decision before an on-demand certificate operation. The existing
// privileged resolver remains the single source of truth for verified, active
// domains on active tenants, including its cache invalidation semantics.
func (s TenantService) TLSAuthorized(ctx context.Context, hostname string) (bool, error) {
	normalized, err := NormalizeHostname(hostname, s.allowLocalhostDomains)
	if err != nil {
		return false, nil
	}
	_, err = s.repository.ResolveDomain(ctx, normalized)
	if errors.Is(err, repository.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (s TenantService) Settings(ctx context.Context, tenantID string) (domain.Settings, error) {
	id, err := parseTenantID(tenantID)
	if err != nil {
		return domain.Settings{}, err
	}
	return s.repository.Settings(ctx, id)
}

// UpdateSettings merges an explicitly supplied PATCH with the tenant's current
// persisted settings. The tenant ID is received from trusted infrastructure,
// never the browser payload.
func (s TenantService) UpdateSettings(ctx context.Context, tenantID string, input domain.UpdateSettingsInput) (domain.Settings, error) {
	id, err := parseTenantID(tenantID)
	if err != nil {
		return domain.Settings{}, ErrInvalidInput
	}
	current, err := s.repository.Settings(ctx, id)
	if err != nil {
		return domain.Settings{}, err
	}
	updated, err := mergeSettings(current, input)
	if err != nil {
		return domain.Settings{}, err
	}
	return s.repository.UpdateSettings(ctx, id, updated)
}

func (s TenantService) CreateTenant(ctx context.Context, input domain.CreateTenantInput) (domain.Tenant, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" || utf8.RuneCountInString(name) > 160 {
		return domain.Tenant{}, ErrInvalidInput
	}
	settings, err := normalizeSettings(input.Settings)
	if err != nil {
		return domain.Tenant{}, err
	}
	return s.repository.CreateTenant(ctx, name, settings)
}

func (s TenantService) Tenant(ctx context.Context, tenantID string) (domain.Tenant, error) {
	id, err := parseTenantID(tenantID)
	if err != nil {
		return domain.Tenant{}, ErrInvalidInput
	}
	return s.repository.Tenant(ctx, id)
}

func (s TenantService) Tenants(ctx context.Context) ([]domain.Tenant, error) {
	return s.repository.Tenants(ctx)
}

func (s TenantService) SetTenantStatus(ctx context.Context, tenantID, status, actor, requestID string) (domain.Tenant, error) {
	id, err := parseTenantID(tenantID)
	if err != nil || (status != "active" && status != "suspended") || strings.TrimSpace(actor) == "" || strings.TrimSpace(requestID) == "" {
		return domain.Tenant{}, ErrInvalidInput
	}
	return s.repository.SetTenantStatus(ctx, id, status, strings.TrimSpace(actor), strings.TrimSpace(requestID))
}

func (s TenantService) PlatformAudit(ctx context.Context, tenantID string) ([]domain.PlatformAudit, error) {
	if tenantID == "" {
		return s.repository.PlatformAudit(ctx, nil)
	}
	id, err := parseTenantID(tenantID)
	if err != nil {
		return nil, ErrInvalidInput
	}
	return s.repository.PlatformAudit(ctx, &id)
}

func (s TenantService) CreateDomain(ctx context.Context, tenantID string, input domain.CreateDomainInput) (domain.TenantDomain, error) {
	id, err := parseTenantID(tenantID)
	if err != nil {
		return domain.TenantDomain{}, ErrInvalidInput
	}
	hostname, err := NormalizeHostname(input.Hostname, s.allowLocalhostDomains)
	if err != nil || !validDomainType(input.DomainType) {
		return domain.TenantDomain{}, ErrInvalidInput
	}
	token, err := verificationToken()
	if err != nil {
		return domain.TenantDomain{}, err
	}
	created, err := s.repository.CreateDomain(ctx, id, hostname, input.DomainType, verificationHash(verificationValue(token)))
	if err != nil {
		return domain.TenantDomain{}, err
	}
	created.VerificationToken = token
	created.VerificationValue = verificationValue(token)
	return created, nil
}

func (s TenantService) Domains(ctx context.Context, tenantID string) ([]domain.TenantDomain, error) {
	id, err := parseTenantID(tenantID)
	if err != nil {
		return nil, ErrInvalidInput
	}
	return s.repository.Domains(ctx, id)
}

func (s TenantService) VerifyDomain(ctx context.Context, tenantID, domainID string) (domain.TenantDomain, error) {
	tenant, domainUUID, err := parseIDs(tenantID, domainID)
	if err != nil {
		return domain.TenantDomain{}, ErrInvalidInput
	}
	// Loading first makes a verification retry idempotent and avoids a DNS
	// dependency for a domain that is already verified.
	candidate, err := s.repository.DomainForVerification(ctx, tenant, domainUUID)
	if err != nil {
		return domain.TenantDomain{}, err
	}
	if candidate.Domain.Verified {
		return candidate.Domain, nil
	}
	if len(candidate.TokenHash) == 0 {
		return candidate.Domain, ErrVerificationUnavailable
	}
	answers, lookupErr := s.dns.LookupTXT(ctx, candidate.Domain.VerificationRecord)
	if lookupErr == nil {
		for _, answer := range answers {
			if subtle.ConstantTimeCompare(verificationHash(answer), candidate.TokenHash) == 1 {
				return s.repository.MarkVerificationSucceeded(ctx, tenant, domainUUID)
			}
		}
	}
	failed, updateErr := s.repository.MarkVerificationFailed(ctx, tenant, domainUUID)
	if errors.Is(updateErr, repository.ErrNotFound) {
		// A concurrent successful verifier cleared the token hash between our
		// read and failed-state update. Treat that retry as success rather than
		// overwriting or reporting a misleading missing resource.
		current, currentErr := s.repository.Domain(ctx, tenant, domainUUID)
		if currentErr == nil && current.Verified {
			return current, nil
		}
	}
	if updateErr != nil {
		return domain.TenantDomain{}, updateErr
	}
	return failed, ErrVerificationFailed
}

func (s TenantService) ActivateDomain(ctx context.Context, tenantID, domainID string) (domain.TenantDomain, error) {
	tenant, domainUUID, err := parseIDs(tenantID, domainID)
	if err != nil {
		return domain.TenantDomain{}, ErrInvalidInput
	}
	current, err := s.repository.Domain(ctx, tenant, domainUUID)
	if err != nil {
		return domain.TenantDomain{}, err
	}
	if !current.Verified {
		return current, ErrVerificationRequired
	}
	return s.repository.ActivateDomain(ctx, tenant, domainUUID)
}

func (s TenantService) DeactivateDomain(ctx context.Context, tenantID, domainID string) (domain.TenantDomain, error) {
	tenant, domainUUID, err := parseIDs(tenantID, domainID)
	if err != nil {
		return domain.TenantDomain{}, ErrInvalidInput
	}
	return s.repository.DeactivateDomain(ctx, tenant, domainUUID)
}

func DefaultSettings() domain.Settings {
	return domain.Settings{
		BusinessTimezone:            "Europe/Istanbul",
		BookingIntervalMinutes:      15,
		ReminderOffsetsMinutes:      []int{1440, 120},
		CancellationPolicy:          "allow_until_notice",
		CancellationNoticeMinutes:   120,
		BookingHorizonDays:          60,
		MinimumBookingNoticeMinutes: 60,
	}
}

func normalizeSettings(input domain.Settings) (domain.Settings, error) {
	defaults := DefaultSettings()
	result := input
	if result.BusinessTimezone == "" {
		result.BusinessTimezone = defaults.BusinessTimezone
	}
	if result.BookingIntervalMinutes == 0 {
		result.BookingIntervalMinutes = defaults.BookingIntervalMinutes
	}
	if result.ReminderOffsetsMinutes == nil {
		result.ReminderOffsetsMinutes = defaults.ReminderOffsetsMinutes
	}
	if result.CancellationPolicy == "" {
		result.CancellationPolicy = defaults.CancellationPolicy
	}
	if result.CancellationNoticeMinutes == 0 {
		result.CancellationNoticeMinutes = defaults.CancellationNoticeMinutes
	}
	if result.BookingHorizonDays == 0 {
		result.BookingHorizonDays = defaults.BookingHorizonDays
	}
	if result.MinimumBookingNoticeMinutes == 0 {
		result.MinimumBookingNoticeMinutes = defaults.MinimumBookingNoticeMinutes
	}
	return validateSettings(result)
}

func mergeSettings(current domain.Settings, input domain.UpdateSettingsInput) (domain.Settings, error) {
	if input.Timezone == nil && input.BookingIntervalMinutes == nil && input.ReminderOffsetsMinutes == nil && input.CancellationPolicy == nil && input.BookingHorizonDays == nil && input.MinimumBookingNoticeMinutes == nil {
		return domain.Settings{}, ErrInvalidInput
	}
	updated := current
	if input.Timezone != nil {
		updated.BusinessTimezone = strings.TrimSpace(*input.Timezone)
	}
	if input.BookingIntervalMinutes != nil {
		updated.BookingIntervalMinutes = *input.BookingIntervalMinutes
	}
	if input.ReminderOffsetsMinutes != nil {
		updated.ReminderOffsetsMinutes = append([]int(nil), (*input.ReminderOffsetsMinutes)...)
	}
	if input.CancellationPolicy != nil {
		updated.CancellationPolicy = strings.TrimSpace(*input.CancellationPolicy)
	}
	if input.BookingHorizonDays != nil {
		updated.BookingHorizonDays = *input.BookingHorizonDays
	}
	if input.MinimumBookingNoticeMinutes != nil {
		updated.MinimumBookingNoticeMinutes = *input.MinimumBookingNoticeMinutes
	}
	return validateSettings(updated)
}

func validateSettings(settings domain.Settings) (domain.Settings, error) {
	settings.BusinessTimezone = strings.TrimSpace(settings.BusinessTimezone)
	if settings.BusinessTimezone == "" || settings.BusinessTimezone == "Local" {
		return domain.Settings{}, ErrInvalidInput
	}
	if _, err := time.LoadLocation(settings.BusinessTimezone); err != nil {
		return domain.Settings{}, ErrInvalidInput
	}
	if settings.BookingIntervalMinutes < 1 || settings.BookingIntervalMinutes > 120 {
		return domain.Settings{}, ErrInvalidInput
	}
	if len(settings.ReminderOffsetsMinutes) == 0 {
		return domain.Settings{}, ErrInvalidInput
	}
	seenOffsets := make(map[int]struct{}, len(settings.ReminderOffsetsMinutes))
	for _, offset := range settings.ReminderOffsetsMinutes {
		if offset < 1 || offset > 10080 {
			return domain.Settings{}, ErrInvalidInput
		}
		if _, exists := seenOffsets[offset]; exists {
			return domain.Settings{}, ErrInvalidInput
		}
		seenOffsets[offset] = struct{}{}
	}
	if settings.CancellationPolicy != "allow_until_notice" && settings.CancellationPolicy != "no_cancellation" {
		return domain.Settings{}, ErrInvalidInput
	}
	if settings.CancellationNoticeMinutes < 0 || settings.CancellationNoticeMinutes > 10080 {
		return domain.Settings{}, ErrInvalidInput
	}
	if settings.BookingHorizonDays < 1 || settings.BookingHorizonDays > 365 {
		return domain.Settings{}, ErrInvalidInput
	}
	if settings.MinimumBookingNoticeMinutes < 0 || settings.MinimumBookingNoticeMinutes > 10080 {
		return domain.Settings{}, ErrInvalidInput
	}
	return settings, nil
}

// NormalizeHostname rejects URL-shaped and wildcard input. Internationalized
// domains must be supplied in their ASCII/Punycode form so the DNS name stored
// and looked up is unambiguous.
func NormalizeHostname(input string, allowLocalhost bool) (string, error) {
	hostname := strings.ToLower(strings.TrimSpace(input))
	hostname = strings.TrimSuffix(hostname, ".")
	if hostname == "" || len(hostname) > 253 || strings.ContainsAny(hostname, ":/?#@*\\\\") || net.ParseIP(hostname) != nil {
		return "", ErrInvalidInput
	}
	if !allowLocalhost && (hostname == "localhost" || strings.HasSuffix(hostname, ".localhost")) {
		return "", ErrInvalidInput
	}
	labels := strings.Split(hostname, ".")
	if len(labels) < 2 && hostname != "localhost" {
		return "", ErrInvalidInput
	}
	for _, label := range labels {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", ErrInvalidInput
		}
		for _, char := range label {
			if !(char >= 'a' && char <= 'z') && !(char >= '0' && char <= '9') && char != '-' {
				return "", ErrInvalidInput
			}
		}
	}
	return hostname, nil
}

func verificationToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

func verificationValue(token string) string { return "barber-verify=" + token }
func verificationHash(value string) []byte {
	hash := sha256.Sum256([]byte(value))
	return hash[:]
}
func validDomainType(value string) bool {
	return value == domain.DomainTypeAdmin || value == domain.DomainTypeBooking
}
func parseIDs(tenantID, domainID string) (uuid.UUID, uuid.UUID, error) {
	tenant, err := parseTenantID(tenantID)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	domain, err := uuid.Parse(domainID)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	return tenant, domain, nil
}
