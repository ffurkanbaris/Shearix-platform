# AGENTS.md

## Project

Multi-tenant barber appointment SaaS using a **microservices architecture**.

Each barber business uses its own domains:

- `randevu.ahmetkuafor.com` → customer booking application
- `panel.ahmetkuafor.com` → barber/admin panel

All tenant domains are served by the same platform infrastructure.

## Technology Stack

- Go
- Fiber v3
- PostgreSQL
- pgx
- sqlc
- Redis
- NATS JetStream
- Docker
- Docker Compose
- REST for synchronous service communication
- Events for asynchronous workflows

Do not build a modular monolith.

Do not introduce Kubernetes, Kafka, service mesh, CQRS, or event sourcing unless explicitly required.

## Microservices

Initial services:

```text
gateway-service
tenant-service
auth-service
barber-service
catalog-service
scheduling-service
appointment-service
customer-service
notification-service
```

Do not create a service per table. Services represent business bounded contexts.

## High-Level Architecture

```text
                 Internet
                    |
             Cloudflare / WAF
                    |
             Reverse Proxy
                    |
             gateway-service
                    |
             Tenant Resolution
                    |
       +------------+-------------+
       |            |             |
       v            v             v
 auth-service  barber-service  catalog-service
                               
       +------------+-------------+
       |            |             |
       v            v             v
 scheduling    appointment    customer
  service        service       service
                    |
                    v
             NATS JetStream
                    |
                    v
          notification-service
```

## Custom Domains

Both booking and admin applications use tenant-owned domains.

Examples:

```text
randevu.ahmetkuafor.com
panel.ahmetkuafor.com

booking.mehmetbarber.com
admin.mehmetbarber.com
```

Domain names are not required to use specific prefixes.

`tenant-service` stores:

```text
tenants
tenant_domains
tenant_settings
```

A tenant domain contains at minimum:

```text
id
tenant_id
hostname
domain_type
verified
ssl_status
created_at
```

Supported domain types:

```text
booking
admin
```

## Gateway and Tenant Resolution

External traffic enters through `gateway-service`.

The gateway resolves:

```text
hostname
    ↓
tenant
    ↓
tenant_id
    +
application type
```

Example:

```text
panel.ahmetkuafor.com
→ tenant_id = <uuid>
→ app_type = admin
```

```text
randevu.ahmetkuafor.com
→ tenant_id = <uuid>
→ app_type = booking
```

Never trust a client-supplied `tenant_id` when tenant identity can be derived from the hostname.

Trusted tenant context may be propagated internally using metadata such as:

```text
X-Tenant-ID
X-App-Type
X-Request-ID
```

External clients must not be able to spoof trusted internal headers.

Internal services must validate that requests originate from trusted infrastructure.

## Database Architecture

Use **database ownership per microservice**.

Each stateful microservice owns its database.

Example:

```text
PostgreSQL Cluster
│
├── tenant_db
├── auth_db
├── barber_db
├── catalog_db
├── scheduling_db
├── appointment_db
└── customer_db
```

These databases may initially run on the same PostgreSQL cluster.

Each service may access only its own database.

Example:

```text
appointment-service
        ↓
appointment_db
```

```text
scheduling-service
        ↓
scheduling_db
```

A service must never directly query another service's database.

Forbidden:

```text
appointment-service
        ↓ SQL
scheduling_db
```

Correct:

```text
appointment-service
        ↓ REST
scheduling-service
```

or asynchronous event communication.

## Multi-Tenant Data Model

All tenants inside a service use:

**Shared Database + Shared Schema + tenant_id**

Do not implement:

- schema-per-tenant
- database-per-tenant
- tenant-selectable storage strategies
- StorageResolver
- dedicated tenant databases

Tenant-owned tables must contain:

```sql
tenant_id UUID NOT NULL
```

Example:

```text
appointment_db

appointments
├── tenant A
├── tenant A
├── tenant B
├── tenant C
└── tenant C
```

## Tenant Isolation

Tenant isolation is mandatory.

Use three layers:

```text
tenant-aware queries
+
tenant_id
+
PostgreSQL Row-Level Security
```

Every tenant-owned repository operation must require tenant context.

Correct:

```sql
SELECT *
FROM appointments
WHERE tenant_id = $1
AND id = $2;
```

Forbidden:

```sql
SELECT *
FROM appointments
WHERE id = $1;
```

Cross-tenant access is forbidden except explicitly implemented platform administration operations.

## PostgreSQL Row-Level Security

Use PostgreSQL RLS on tenant-owned tables where applicable.

Example:

```sql
ALTER TABLE appointments
ENABLE ROW LEVEL SECURITY;
```

Tenant policies should restrict rows using request/transaction tenant context.

Conceptually:

```sql
tenant_id = current_setting('app.tenant_id')::uuid
```

Set tenant context transaction-locally where possible.

Application-level tenant filtering must still be used even when RLS exists.

RLS is defense in depth, not a replacement for tenant-aware repositories.

## Tenant-Aware Constraints

Indexes and constraints must account for tenant scope where appropriate.

Example:

```sql
CREATE INDEX idx_appointments_tenant_start
ON appointments (tenant_id, start_at);
```

Tenant-scoped uniqueness:

```sql
UNIQUE (tenant_id, phone)
```

instead of:

```sql
UNIQUE (phone)
```

unless the value is intentionally globally unique.

## Tenant Service

Responsibilities:

- tenant creation
- tenant configuration
- custom-domain registration
- domain verification
- domain → tenant resolution
- tenant status management

Owns:

```text
tenants
tenant_domains
tenant_settings
```

## Authentication Service

Owns authentication and authorization data.

Model identity separately from tenant membership:

```text
identity
    ↓
tenant_membership
    ↓
tenant
```

A single identity may belong to multiple tenants.

Initial roles:

```text
OWNER
MANAGER
BARBER
RECEPTIONIST
```

Tenant is derived from the admin hostname during login.

Do not require:

```json
{
  "tenant_id": "..."
}
```

from the login client.

Prefer secure cookie-based authentication for the admin panel:

```text
HttpOnly
Secure
SameSite
```

Never store passwords, session tokens, or refresh tokens in plaintext.

## Barber Service

Owns:

```text
branches
barbers
barber_profiles
barber_branch_assignments
```

Responsibilities:

- branch management
- barber management
- barber profile management
- barber/branch assignments

## Catalog Service

Owns:

```text
services
barber_services
pricing
```

A service definition should support:

```text
name
duration
price
currency
buffer_before
buffer_after
status
```

Responsibilities:

- services offered by tenant
- barber-service relationships
- service duration
- pricing
- booking buffers

## Scheduling Service

Owns:

```text
barber_working_hours
schedule_overrides
blocked_periods
```

Schedule overrides must support cases such as:

```text
vacation
unavailable
custom_hours
```

Availability is calculated from:

```text
working hours
- schedule overrides
- blocked periods
- existing appointments
- service duration
- service buffers
= available slots
```

Redis may cache availability results but must never be considered authoritative.

## Appointment Service

Owns:

```text
appointments
appointment_events
idempotency_keys
outbox_events
```

Responsibilities:

- appointment creation
- cancellation
- rescheduling
- confirmation
- completion
- appointment state transitions
- double-booking prevention

Appointment creation must validate:

1. tenant
2. branch
3. barber
4. service
5. barber-service relationship
6. working hours
7. schedule overrides
8. service duration and buffers
9. conflicting appointments

## Double-Booking Prevention

Never rely only on Go application checks.

PostgreSQL must enforce the final consistency guarantee.

Use:

```text
barber_id
+
time range
+
GiST exclusion constraint
```

with PostgreSQL range types such as:

```text
tstzrange(start_at, end_at)
```

The constraint must also respect tenant scope.

Concurrent attempts to reserve overlapping active slots for the same barber must result in only one successful appointment.

## Time Handling

Use:

```text
TIMESTAMPTZ
```

for persisted appointment timestamps.

Store business timezone separately.

Do not rely on server-local timezone.

Perform timezone conversion explicitly at system boundaries.

## Appointment Transactions

Appointment creation must use a database transaction.

Conceptually:

```text
BEGIN

validate request
create appointment
create appointment event
create outbox event

COMMIT
```

Do not publish an event before the transaction commits.

## Idempotency

Public appointment creation must support idempotency.

Example:

```text
Idempotency-Key: <uuid>
```

Idempotency must be tenant-scoped.

Retries with the same valid idempotency key must not create duplicate appointments.

## Customer Service

Owns:

```text
customers
customer_contacts
customer_preferences
```

Customer data is tenant-scoped.

The same phone number may belong to customers of different tenants.

Therefore prefer constraints such as:

```sql
UNIQUE (tenant_id, phone)
```

Guest booking should be supported.

Customer accounts must not be mandatory for basic appointment creation.

## Service Communication

Use REST for synchronous operations that require an immediate result.

Examples:

```text
gateway → auth
gateway → appointment
appointment → scheduling
appointment → catalog
```

Use events for asynchronous side effects and state propagation.

Examples:

```text
AppointmentCreated
AppointmentCancelled
AppointmentRescheduled
BarberCreated
TenantCreated
```

Avoid distributed transactions.

Use timeouts for all service-to-service network calls.

Retries must only be used when the operation is safe to retry.

## Event Bus

Use NATS JetStream for asynchronous communication.

Events must contain sufficient metadata:

```text
event_id
event_type
tenant_id
aggregate_id
occurred_at
version
payload
```

Event consumers must be idempotent.

Assume events may be delivered more than once.

## Transactional Outbox

Services publishing domain events after database changes must use the transactional outbox pattern.

Example:

```text
BEGIN

INSERT appointment

INSERT outbox_event

COMMIT
```

A background publisher reads pending outbox records and publishes them to NATS.

After successful publication, mark the event as published.

Do not rely on:

```text
database commit
↓
direct event publish
```

because a process crash between these operations can lose events.

## Notification Service

Consumes appointment events.

Responsibilities:

- email
- appointment confirmations
- cancellations
- reminders

External notification providers must not be called synchronously during appointment creation.

Example:

```text
appointment-service
       ↓
AppointmentCreated
       ↓
NATS
       ↓
notification-service
       ↓
Email
```

## Redis

Use Redis for:

- tenant/domain cache
- rate limiting
- OTP
- short-lived cache
- temporary distributed coordination where justified

Redis must not be the authoritative source for:

- appointments
- availability
- tenant data
- customer data

PostgreSQL remains the source of truth.

## Public API

Example external routes:

```text
GET  /api/v1/public/config
GET  /api/v1/public/branches
GET  /api/v1/public/services
GET  /api/v1/public/barbers
GET  /api/v1/public/availability

POST /api/v1/public/appointments
POST /api/v1/public/otp
```

## Admin API

Example routes:

```text
GET    /api/v1/admin/appointments
POST   /api/v1/admin/appointments
PATCH  /api/v1/admin/appointments/:id

GET    /api/v1/admin/barbers
POST   /api/v1/admin/barbers

GET    /api/v1/admin/services
POST   /api/v1/admin/services

GET    /api/v1/admin/customers

GET    /api/v1/admin/schedules
POST   /api/v1/admin/schedules
```

Admin operations require:

```text
admin tenant domain
+
authentication
+
tenant membership
+
role authorization
```

## Go Service Structure

Each service should follow a consistent structure:

```text
services/<service-name>/
├── cmd/
│   └── api/
│       └── main.go
├── internal/
│   ├── domain/
│   ├── handler/
│   ├── service/
│   ├── repository/
│   ├── event/
│   └── config/
├── migrations/
├── sql/
├── Dockerfile
└── go.mod
```

Maintain separation:

```text
HTTP handler
     ↓
application/service
     ↓
repository
     ↓
database
```

Fiber-specific types must remain in the HTTP transport layer.

Business logic must not depend directly on Fiber.

## Database Access

Use:

```text
pgx
+
sqlc
```

Prefer explicit SQL over ORM abstractions.

Repositories own database interaction.

Handlers must never contain SQL.

Services must never directly access another microservice's database.

Every database schema change requires a migration.

Never modify migrations that have already been applied.

## Docker

Every microservice must have its own Dockerfile.

Use Docker Compose for local development and initial deployment.

Example:

```text
Docker Compose
│
├── gateway-service
├── tenant-service
├── auth-service
├── barber-service
├── catalog-service
├── scheduling-service
├── appointment-service
├── customer-service
├── notification-service
├── postgres
├── redis
└── nats
```

Services must communicate using Docker network service names rather than hardcoded IP addresses.

Configuration must come from environment variables or configuration files.

Never commit production secrets.

## Reliability

Services must implement:

- health checks
- readiness checks
- graceful shutdown
- request IDs
- structured logging
- service call timeouts
- controlled retries
- connection pooling

Do not retry non-idempotent operations blindly.

## Observability

Design services to support:

```text
OpenTelemetry
Prometheus
Grafana
structured logs
distributed tracing
```

Propagate trace and request context between services.

## Security

Never trust:

- tenant IDs from external clients
- internal routing headers from the public internet
- client-provided ownership information

Validate authorization server-side.

Never log:

- passwords
- OTP codes
- access tokens
- refresh tokens
- session secrets
- database credentials

Use parameterized SQL.

Apply rate limiting to sensitive public endpoints.

## Testing Priorities

Prioritize tests for:

- tenant isolation
- RLS policies
- domain resolution
- authentication
- authorization
- role permissions
- availability calculation
- schedule overrides
- concurrent appointment creation
- double-booking prevention
- idempotency
- transactional outbox
- duplicate event delivery
- service contracts

Use real PostgreSQL instances for integration tests involving:

```text
transactions
RLS
constraints
GiST exclusion constraints
concurrency
```

## Development Principles

This project intentionally uses microservices.

Maintain:

```text
clear bounded contexts
independent service ownership
database-per-service ownership
shared-schema multi-tenancy
tenant isolation
explicit service contracts
event-driven asynchronous workflows
```

Do not create unnecessary infrastructure or abstractions.

Current architecture is explicitly:

```text
Microservices
+
Database ownership per service
+
Shared schema per service
+
tenant_id
+
PostgreSQL RLS
+
REST
+
NATS JetStream
+
Transactional Outbox
+
Redis
+
Docker Compose
```

Do not implement tenant-selectable database strategies or schema-per-tenant architecture unless the project requirements are explicitly changed.