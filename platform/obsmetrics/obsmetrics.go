// Package obsmetrics provides shared Prometheus instrumentation. Every
// metric label set here is deliberately bounded: service name, HTTP method,
// the fiber *route pattern* (never the raw path, which could contain a
// tenant subdomain-derived value or an ID), and status code/class. Nothing
// in this package accepts a tenant ID, email, appointment ID, request ID, or
// trace ID as a label — those are unbounded-cardinality values that belong
// in logs/traces, not metrics.
package obsmetrics

import (
	"strconv"
	"time"

	"github.com/barber-appointment/platform/httpx"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/adaptor"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"
)

// Registry wraps a private prometheus.Registry (not the global default) so
// each service's metric set is self-contained and testable in isolation.
type Registry struct {
	reg     *prometheus.Registry
	service string

	httpRequests *prometheus.CounterVec
	httpDuration *prometheus.HistogramVec

	dependencyUp *prometheus.GaugeVec

	rateLimitAllowed *prometheus.CounterVec
	rateLimitBlocked *prometheus.CounterVec
	rateLimitErrors  *prometheus.CounterVec
}

// New creates a private registry for serviceName, pre-registered with the
// HTTP request metrics, a generic dependency-health gauge, and rate-limit
// counters every service can use. Domain-specific metrics (outbox, NATS,
// notification delivery) are registered separately by the owning service
// via Registry.Registerer().
func New(serviceName string) *Registry {
	reg := prometheus.NewRegistry()
	r := &Registry{reg: reg, service: serviceName}

	r.httpRequests = promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
		Name: "http_requests_total",
		Help: "Total HTTP requests handled, by method, route pattern and status class.",
	}, []string{"service", "method", "route", "status"})

	r.httpDuration = promauto.With(reg).NewHistogramVec(prometheus.HistogramOpts{
		Name:    "http_request_duration_seconds",
		Help:    "HTTP request handling latency in seconds, by method and route pattern.",
		Buckets: prometheus.DefBuckets,
	}, []string{"service", "method", "route"})

	r.dependencyUp = promauto.With(reg).NewGaugeVec(prometheus.GaugeOpts{
		Name: "dependency_up",
		Help: "1 if the named dependency (postgres, redis, nats) is currently reachable, else 0.",
	}, []string{"service", "dependency"})

	r.rateLimitAllowed = promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
		Name: "rate_limit_allowed_total",
		Help: "Requests allowed by a rate limiter, by operation.",
	}, []string{"service", "operation"})
	r.rateLimitBlocked = promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
		Name: "rate_limit_blocked_total",
		Help: "Requests blocked by a rate limiter for exceeding the limit, by operation.",
	}, []string{"service", "operation"})
	r.rateLimitErrors = promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
		Name: "rate_limit_errors_total",
		Help: "Requests that failed closed because the rate limiter backend errored, by operation.",
	}, []string{"service", "operation"})

	return r
}

// Registerer exposes the underlying prometheus.Registerer so a service can
// register additional, domain-specific collectors (outbox backlog, NATS
// redelivery, notification delivery, pgx pool gauges) on the same private
// registry that /metrics serves.
func (r *Registry) Registerer() prometheus.Registerer { return r.reg }

// Handler returns the fiber handler for a private /metrics endpoint. Callers
// are responsible for never routing this publicly — see
// services/gateway-service for the regression test asserting no gateway
// route ever forwards to a backend's /metrics.
func (r *Registry) Handler() fiber.Handler {
	return adaptor.HTTPHandler(promhttp.HandlerFor(r.reg, promhttp.HandlerOpts{}))
}

// HTTPMiddleware records one request-count and one latency observation per
// completed request, labeled only by method, the fiber *route pattern* and
// status. It never reads or logs the request body, headers, or query
// string.
func (r *Registry) HTTPMiddleware() fiber.Handler {
	return func(c fiber.Ctx) error {
		start := time.Now()
		err := c.Next()

		route := httpx.RoutePattern(c, err)
		status := strconv.Itoa(httpx.ResolveStatus(c, err))
		r.httpRequests.WithLabelValues(r.service, c.Method(), route, status).Inc()
		r.httpDuration.WithLabelValues(r.service, c.Method(), route).Observe(time.Since(start).Seconds())
		return err
	}
}

// SetDependencyUp records the reachability of a named dependency
// ("postgres", "redis", "nats"). Call this from the same readiness checks
// that already back /ready so the metric and the health endpoint never
// disagree.
func (r *Registry) SetDependencyUp(dependency string, up bool) {
	value := 0.0
	if up {
		value = 1.0
	}
	r.dependencyUp.WithLabelValues(r.service, dependency).Set(value)
}

// RateLimitAllowed/RateLimitBlocked/RateLimitError record the outcome of a
// rate-limiter decision for a bounded operation name (e.g. "login",
// "register", "forgot-password") — never a tenant ID or key.
func (r *Registry) RateLimitAllowed(operation string) {
	r.rateLimitAllowed.WithLabelValues(r.service, operation).Inc()
}
func (r *Registry) RateLimitBlocked(operation string) {
	r.rateLimitBlocked.WithLabelValues(r.service, operation).Inc()
}
func (r *Registry) RateLimitError(operation string) {
	r.rateLimitErrors.WithLabelValues(r.service, operation).Inc()
}

// RegisterPgxPool registers gauges reporting the pool's live connection
// usage (total/idle/acquired/max), refreshed on each /metrics scrape via a
// GaugeFunc collector — no background goroutine required.
func (r *Registry) RegisterPgxPool(pool *pgxpool.Pool) {
	labels := prometheus.Labels{"service": r.service}
	promauto.With(r.reg).NewGaugeFunc(prometheus.GaugeOpts{Name: "pgx_pool_total_conns", Help: "Total connections currently held by the pgx pool.", ConstLabels: labels}, func() float64 { return float64(pool.Stat().TotalConns()) })
	promauto.With(r.reg).NewGaugeFunc(prometheus.GaugeOpts{Name: "pgx_pool_idle_conns", Help: "Idle connections currently held by the pgx pool.", ConstLabels: labels}, func() float64 { return float64(pool.Stat().IdleConns()) })
	promauto.With(r.reg).NewGaugeFunc(prometheus.GaugeOpts{Name: "pgx_pool_acquired_conns", Help: "Connections currently acquired (in use) from the pgx pool.", ConstLabels: labels}, func() float64 { return float64(pool.Stat().AcquiredConns()) })
	promauto.With(r.reg).NewGaugeFunc(prometheus.GaugeOpts{Name: "pgx_pool_max_conns", Help: "Configured maximum size of the pgx pool.", ConstLabels: labels}, func() float64 { return float64(pool.Stat().MaxConns()) })
}

// RegisterRedis registers a gauge reporting go-redis's internal pool stats
// (idle vs total connections), refreshed on scrape.
func (r *Registry) RegisterRedis(client *redis.Client) {
	if client == nil {
		return
	}
	labels := prometheus.Labels{"service": r.service}
	promauto.With(r.reg).NewGaugeFunc(prometheus.GaugeOpts{Name: "redis_pool_total_conns", Help: "Total connections currently held by the redis client pool.", ConstLabels: labels}, func() float64 { return float64(client.PoolStats().TotalConns) })
	promauto.With(r.reg).NewGaugeFunc(prometheus.GaugeOpts{Name: "redis_pool_idle_conns", Help: "Idle connections currently held by the redis client pool.", ConstLabels: labels}, func() float64 { return float64(client.PoolStats().IdleConns) })
}
