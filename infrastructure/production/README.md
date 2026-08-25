# Single-VM production environment

This directory describes provisioning inputs only. Applying these requirements
does not deploy the application.

## Host and network

- Start with **8 vCPU, 16 GiB RAM**, and a current LTS Linux distribution for
  the application, eight logical PostgreSQL databases, Docker, and the bundled
  observability stack. Treat 4 vCPU/8 GiB as a non-production test floor. Scale
  vertically when sustained CPU exceeds 70%, memory exceeds 80%, or database
  latency/SLOs degrade.
- Attach a separate **250 GiB SSD** persistent disk and mount it at
  `/var/lib/barber` before installing Docker. Configure Docker's `data-root` as
  `/var/lib/barber/docker` and backup staging as
  `/var/lib/barber/backup-staging`. Keep at least 20% free and alert below 50
  GiB. Docker named volumes hold PostgreSQL, Redis, JetStream, Caddy,
  Prometheus, Grafana, Tempo, Loki, and Alloy state on this disk.
- Assign a static public IPv4 address. The cloud firewall permits inbound TCP
  80/443 and UDP 443 from the internet and TCP 22 only from the operator VPN or
  fixed administration CIDRs. Deny all other inbound traffic. Allow outbound
  DNS, NTP, HTTPS (registry/object storage/ACME), and the configured SMTP port.
  Docker publishes only Caddy on all interfaces; Grafana is loopback-only.
- The production SaaS identity is `SAAS_DOMAIN=shearx.app`; its operator host
  is `PLATFORM_ADMIN_HOST=platform.shearx.app`. Both are explicit Caddy sites:
  the platform host routes directly to the control-plane frontend and the apex
  serves the static landing placeholder until a marketing app exists. Neither
  hostname is registered as a tenant domain or uses tenant on-demand
  authorization.
- In Name.com, point the two initial records at the reserved VM address:

  ```text
  A  @         35.198.188.47
  A  platform  35.198.188.47
  ```

  Add records for each initially registered tenant admin and booking domain
  separately. Prefixes such as `booking` and `admin` are conventional, not a
  routing requirement; tenant-service remains the hostname authority.
  Remove AAAA records unless IPv6 is actually routed to the VM. Tenant custom
  domains added later must point to the same static address before verification.
  Caddy uses tenant-service as its on-demand TLS authorization endpoint, so an
  unknown hostname cannot obtain a certificate.

## Registry, host layout, and secrets

- Use a private OCI registry with immutable tags enabled. CI builds every
  component once, tags it with the full 40-character Git SHA, pushes it, and
  emits a digest-lock Compose override. The VM receives that signed/reviewed
  artifact; it never builds application images.
- Install the repository read-only or as an operator-owned checkout at
  `/opt/barber-appointment`. Store the production environment and Alertmanager
  receiver configuration under `/etc/barber-appointment`, owned by root and
  mode `0600`. Use `production.env.example` as the inventory. Do not place real
  values in Git, shell history, or a Compose file.
- Use distinct generated values for every listed database role and security
  token. On GCE, prefer a dedicated VM service account with object access only
  to the backup bucket; Restic can use its short-lived metadata credentials and
  no static cloud key is stored. Enable provider-side retention controls where
  available. Restic encrypts content and metadata before upload.
- Configure a production SMTP account with TLS, a verified `EMAIL_FROM` domain,
  SPF/DKIM/DMARC, provider rate limits sized for reminders, and credentials
  scoped to sending. The three mail-sending services retain outbound access but
  no published ports.

## Operations overlay

`docker-compose.production-observability.yml` adds private Prometheus,
Alertmanager, OTEL Collector, local durable Tempo, Loki, Alloy, and Grafana.
Prometheus retains 30 days by default; traces and logs retain 7 days. These are
durable across container replacement but share the VM's failure domain, so
metrics/logs/traces are operational aids rather than disaster-recovery copies.
Reach Grafana with an SSH tunnel:

```sh
ssh -L 3002:127.0.0.1:3002 operator@production-host
```

Copy `infrastructure/observability/production/alertmanager.example.yml` outside
the repo, configure a monitored external receiver, and set
`ALERTMANAGER_CONFIG_FILE` to it. Alerts cover service/dependency loss, 5xx
rates, outbox delay, terminal delivery failures, and rule evaluation failures.
Alloy reads Docker's JSON log files read-only and ships structured stdout logs
to Loki; it does not mount the Docker socket.

## Provisioning checklist

1. Patch the host, enable automatic security updates, create the restricted
   operator account, install Docker Compose v2, and configure time sync.
2. Mount the persistent disk, then install/configure Docker. Confirm Docker's
   data root is on that disk.
3. Apply the cloud firewall and Name.com A records for `shearx.app` and
   `platform.shearx.app`; confirm both resolve to `35.198.188.47`, then verify
   each initial tenant hostname separately.
4. Configure registry pull credentials using a read-only production robot
   account.
5. Install root-owned secret/config files and initialize the off-host restic
   repository once with the pinned restic image. Configure distinct external
   heartbeat monitors for the hourly backup and weekly verification jobs.
6. Install and enable the backup and verification timers from `systemd/`.
7. Place the immutable release digest override on the host.
8. Run `scripts/production/preflight.sh`. Stop on any failure.

Provisioning is complete only when the preflight passes and an alert test plus
a full isolated backup restore drill have been observed by an operator.
