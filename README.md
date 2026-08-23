# Barber Appointment Platform Foundation

## Run locally

```sh
cp .env.example .env
docker compose up --build
```

The gateway is the public API boundary at `http://localhost:8080`. The two
development browser applications are also host-published on ports 3000 and
3001; production exposes only Caddy. Verify tenant resolution with:

```sh
curl -H 'Host: booking.localhost' http://localhost:8080/api/v1/public/config
curl -H 'Host: admin.localhost' http://localhost:8080/api/v1/public/config
```

Development-only admin login: `demo-owner@booking.local` / `password` on
`admin.localhost`. It is seeded separately from migrations and must not be used
outside local development.

Development-only booking login: `demo-customer@booking.local` / `password` on
`booking.localhost`. It is also seeded separately from migrations and must not
be used outside local development.

## Tenant browser applications

The platform has separate Next.js applications for tenant administration and
customer booking. Both proxy browser API requests to gateway-service while
retaining the original hostname, so tenant context remains at the existing
gateway boundary. Neither application accepts or sends a tenant ID, and neither
stores an access token in browser storage.

```sh
docker compose up --build
# tenant administration: http://admin.localhost:3000
# customer booking:      http://booking.localhost:3001
```

`booking-web` loads public tenant configuration from its booking hostname,
shows active services, branches, and branch-assigned barbers, and uses the
authoritative availability endpoint for timezone-aware slots. It sends a UUID
`Idempotency-Key` for a logical booking request and reuses it for retries. A
`409` clears the stale selection and refreshes availability. Appointment-service
remains authoritative for pricing, timing, compatibility, and double-booking.

Local Compose intentionally uses an HttpOnly `barber_session` cookie without
the Secure flag because it is plain HTTP. The production Caddy overlay switches
to the HTTPS-only `__Host-barber_session` cookie automatically; do not use the
development cookie settings in production.

Run frontend verification locally with:

```sh
cd apps/admin-web
npm ci
npm run lint
npm run typecheck
npm test
npm run build
cd ../booking-web
npm ci
npm run lint
npm run typecheck
npm test
npm run build
```

All non-gateway services are private Compose-network services. Development seeds
are separate from immutable migrations and can be safely rerun.

## Platform tenant onboarding and domains

Tenant provisioning is a platform-control-plane operation, not a tenant-admin
capability. The gateway exposes `/api/v1/platform/...` only with the separate
`X-Platform-Admin-Token` from `PLATFORM_ADMIN_TOKEN`; it does not resolve the
caller hostname or forward client tenant-context headers for these routes.

```sh
platform_token='replace-with-a-separate-platform-development-secret'
tenant=$(curl -sS -X POST http://localhost:8080/api/v1/platform/tenants \
  -H "X-Platform-Admin-Token: $platform_token" \
  -H 'Content-Type: application/json' \
  --data '{"name":"Example Barber"}')
```

Creation persists explicit defaults for timezone (`Europe/Istanbul`), a
15-minute booking interval, 24-hour and 2-hour reminders, a 120-minute
cancellation notice, a 60-day booking horizon, and a 60-minute booking notice.
It may accept an explicit `settings` object with those same fields.

Register `admin` or `booking` domains through
`POST /api/v1/platform/tenants/:id/domains`. The response returns a one-time
token and the TXT record name. Publish the returned `verification_value` at
`_barber-verify.<hostname>`, then call `/verify` and `/activate`. Only a
verified, active domain for an active tenant resolves at the gateway; deactivating
it invalidates the resolver cache immediately. Tokens are stored only as hashes
and are removed when verification succeeds. TLS issuance remains an edge/proxy
concern.

## Tenant operational settings

Authenticated admin-domain users read settings through
`GET /api/v1/admin/settings`. `OWNER` and `MANAGER` may update operational
booking settings with `PATCH /api/v1/admin/settings`; `BARBER` and
`RECEPTIONIST` are read-only. The request accepts `timezone`,
`booking_interval_minutes`, `reminder_offsets_minutes`,
`cancellation_policy`, `booking_horizon_days`, and
`minimum_booking_notice_minutes`. The tenant is always resolved from the
hostname—no request body can select one.

Timezone values are validated against the Go/IANA timezone database. Reminder
offsets are positive, unique minute values. Updated settings are written through
to the tenant settings cache immediately. Scheduling reads the booking interval
per availability request, and notification-service reads reminder offsets from
tenant-service when planning each new appointment event.

## Production edge TLS with Caddy

Production uses Caddy as the only host-published application edge. Start the
base stack with the edge overlay after setting a real `INTERNAL_AUTH_TOKEN`,
`PLATFORM_ADMIN_TOKEN`, `APP_ENV=production`, and `CADDY_ACME_EMAIL`:

```sh
docker compose -f docker-compose.yml \
  -f infrastructure/caddy/docker-compose.production.yml up -d
```

The overlay removes the gateway, admin-web, and booking-web host ports and
publishes Caddy on ports 80 and 443 (plus UDP 443 for HTTP/3). Caddy forwards
both `/api/*`/webhook traffic and page/static traffic to gateway-service with
the original `Host` header. For page traffic gateway resolves that hostname and
dispatches `admin` domains only to `admin-web` and `booking` domains only to
`booking-web`. This dynamic dispatch is necessary because stock Caddy can
authorize TLS dynamically but cannot choose an upstream from a tenant-service
lookup response. Caddy remains infrastructure-only and does not own tenant or
domain state. It drops client-supplied internal tenant headers on the gateway
path; gateway itself does not use forwarding headers for tenant identity.

For a custom domain, point its DNS `A`/`AAAA` record (or a provider-supported
`CNAME`) to the Caddy edge. First publish the onboarding TXT value at
`_barber-verify.<hostname>`, call the platform verification endpoint, and then
activate the domain. Caddy uses on-demand TLS but asks a loopback-only Caddy
route before each new certificate operation. That route adds the internal token
and calls tenant-service's private TLS eligibility endpoint. Caddy receives a
2xx response only for verified, active domains on active tenants; unknown,
pending, failed, and inactive domains cannot trigger certificate issuance.

Caddy stores certificate and renewal state in the `caddy_data` volume. A domain
deactivated after issuance may retain its already-issued certificate until it
expires, but gateway routing is denied immediately and Caddy denies every
future issuance or renewal authorization. Caddy automatically renews allowed
certificates. For certificate failures, verify ports 80/443 reach Caddy, DNS
has propagated to the edge, the TXT record has passed tenant verification, and
the configured ACME email/token are present. Do not use the production overlay
for `*.localhost`; normal development remains `docker compose up --build` with
HTTP on `localhost:8080`.

## Appointment flow

Public booking requests go through the gateway using `booking.localhost` and
must include an `Idempotency-Key`. Keys are 1–128 ASCII characters from
`A-Z`, `a-z`, `0-9`, `.`, `_`, `:`, and `-`; malformed keys receive
`400 invalid_idempotency_key` before database access. Keys are scoped by the
resolved tenant, trusted authenticated principal (or a hash derived from the
normalized guest email),
and operation. Reusing a key with the same semantic request replays the
original appointment; reusing it in that scope with different booking values
returns `409`. The client sends only `branch_id`,
`barber_id`, `service_id`, guest contact details, and `start_at`. Appointment
service verifies the active branch/barber assignment with barber-service,
validates the assigned service and duration/buffers with catalog-service, and
uses scheduling-service for an advisory slot check. It derives all end and
occupied timestamps itself; PostgreSQL's tenant-scoped exclusion constraint is
the final double-booking guard.

Scheduling-service obtains occupied ranges only through appointment-service's
private `/internal/v1/occupancy` endpoint. Both services propagate gateway
tenant context plus internal authentication; neither reads the other's database.
Cancelling an appointment releases its range immediately. Appointment changes
write an event and outbox record in the same transaction; the optional NATS
JetStream publisher delivers `appointments.v1.appointment.*` events after a
broker acknowledgement.

Appointment idempotency records are retained for seven days, after which a
retry is treated as a new booking attempt. Successfully published outbox rows
are retained for 30 days. The appointment publisher runs hourly, bounded
500-row cleanup batches; unpublished rows are never retention-eligible.

## Runtime health and readiness

Every backend exposes `/health` as process-only liveness. `/ready` uses bounded probes for the dependencies required by that service:

- gateway: tenant-service domain resolution API;
- tenant-service: PostgreSQL and Redis when configured;
- auth-service: PostgreSQL and Redis;
- barber, catalog, scheduling, and customer services: PostgreSQL;
- appointment-service: PostgreSQL plus its connected and initialized NATS JetStream outbox publisher;
- notification-service: PostgreSQL and NATS JetStream.

Compose application healthchecks use `/ready`. Production infrastructure uses `restart: unless-stopped`; application processes use the bounded `on-failure:5` policy so transient crashes recover without indefinitely masking deterministic configuration failures.

PostgreSQL pools default to 10 maximum and 1 minimum connection, with a one-hour lifetime, 15-minute idle limit, 30-second health period, and five-second connect timeout. Override these with `DB_MAX_CONNS`, `DB_MIN_CONNS`, `DB_MAX_CONN_LIFETIME`, `DB_MAX_CONN_IDLE_TIME`, `DB_HEALTH_CHECK_PERIOD`, and `DB_CONNECT_TIMEOUT`.

## Email notifications and credential delivery

notification-service consumes appointment events and delivers appointment email through the configured email provider. Credential passwords do not pass through notification-service, Redis, or NATS. Auth-service and customer-service generate a random password, persist only its bcrypt hash, and submit the plaintext directly to the configured email sender from process memory. Development uses a metadata-only logging sender that never logs message bodies. Set EMAIL_PROVIDER=smtp, SMTP_ADDRESS, EMAIL_FROM, and optional SMTP_USERNAME, SMTP_PASSWORD, SMTP_IMPLICIT_TLS for production. New and reset credentials require a password change after login.

## Tenant-admin users and credential delivery

On an `admin` tenant domain, an authenticated `OWNER` may create a tenant user:

```sh
curl -H 'Host: admin.localhost' -H 'Content-Type: application/json' \
  -H 'Cookie: __Host-barber_session=…' \
  --data '{"name":"Furkan Barış","email":"staff@example.com","role":"BARBER"}' \
  http://localhost:8080/api/v1/admin/members
```

Only an `OWNER` can create or manage staff memberships. New members may be
`MANAGER`, `BARBER`, or `RECEPTIONIST`; normal APIs never create, promote,
deactivate, or demote an `OWNER`. Managers retain business-resource write
access but cannot administer identities or roles. Use
`/api/v1/admin/members` to list/create, `/members/:id/role` to change an
ordinary-staff role, and `/members/:id/{activate,deactivate}` for lifecycle
changes. A global, normalized email identity can join another tenant without
overwriting its name or password.

Both frontend applications use email as the sole authentication identity.
Customer registration and password recovery submit an email address;
temporary credentials are delivered by email and are never returned to or
persisted by frontend code. Phone values remain optional business contact data
and are not accepted as login identifiers.

Barber business records stay separate from login identities. An OWNER or
MANAGER links an active tenant `BARBER` membership through
`POST /api/v1/admin/barbers/:barber_id/link-identity`; unlink with `DELETE`.
The barber-service validates eligibility through auth-service's private API,
not by reading `auth_db`. A link is tenant-scoped and unique, and a BARBER's
self-scheduling access derives exclusively from this link plus their trusted
session.

Login, password change, and non-enumerating reset endpoints are available under
`/api/v1/admin/auth/{login,change-password,forgot-password}`; all
login/password operations require the resolved `admin` tenant context.

Run the credential integration checks with:

```sh
make test-integration
```

For the repository-backed PostgreSQL suite, run:

```sh
docker compose --profile test run --rm appointment-integration-test
docker compose --profile test run --rm scheduling-integration-test
docker compose --profile test run --rm notification-integration-test
```

## Continuous integration and local quality gates

GitHub Actions runs stable checks for backend static analysis and tests, sqlc,
both frontends, real PostgreSQL/NATS integrations, empty-database migrations,
Compose, all container images, and a bounded critical-flow smoke test. Backend
coverage is retained per Go module; vulnerability scans are initially
report-only because the frontend lockfiles have known unresolved findings.

Local equivalents are intentionally the same scripts used in CI:

```sh
make verify             # Go, sqlc, frontends, and Compose
make test-integration   # isolated real PostgreSQL and NATS suites
make test-migrations    # every owned schema from an empty PostgreSQL volume
make ci                 # all of the above
./scripts/ci/e2e-smoke.sh
```

The Go module manifest at `scripts/ci/go-modules.txt` is checked against every
`go.mod`, so a newly added service cannot be silently omitted. `make
verify-go-static` runs `go mod tidy` and fails on changed module metadata.
`make sqlc-check` runs pinned sqlc 1.29.0 and fails on generated-code changes.
These freshness checks require a Git worktree.

Historical migration hashes are recorded in
`scripts/ci/migration-checksums.sha256`. Never replace that baseline after
editing an old migration. For a legitimate new forward migration, append only
its checksum with:

```sh
./scripts/ci/add-migration-checksum.sh services/example-service/migrations/000002_change.up.sql
```

Integration and E2E commands require Docker Compose; frontend checks require
Node 22/npm; direct backend checks require Go 1.26. Compose test projects use
fresh volumes and clean them on success or failure. To diagnose CI, start with
the named job output: module names prefix Go failures, migration services are
printed before each migration, and failed E2E runs retain container status but
never upload database or credential-bearing logs.
