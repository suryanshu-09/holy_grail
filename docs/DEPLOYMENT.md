# Deployment Guide (Phase 26)

This guide covers everything needed to run Holy Grail in production:
production config, database, object storage, secrets, HTTPS, CORS, rate
limiting, backups, file storage migration, logging, monitoring, and error
tracking.

## 1. Production configuration

- Copy `.env.example` to `.env` and set real values. Never commit `.env`
  (it is git-ignored).
- Set `APP_ENV=production`. This switches the backend to JSON logs,
  enables the `Secure` cookie flag, and turns on fail-fast validation:
  `AppConfig.ValidateProduction` (internal/config/config.go) refuses to boot
  when any of these hold:
  - `CORS_ALLOWED_ORIGIN` is empty or a wildcard,
  - `DATABASE_URL` is unset (the discrete `POSTGRES_*` fallback is dev-only),
  - the DB password is a default/placeholder (`pgpass`, `changeme`, …),
  - `DATABASE_URL` uses `sslmode=disable` (TLS required),
  - `STORAGE_BACKEND=s3` without `S3_BUCKET`.
- Start the full stack with one command:

```bash
docker compose up -d --build   # or: make prod-up
```

## 2. Production database

- Managed Postgres 15+ with the `pgvector` extension (the compose `db`
  service uses `ankane/pgvector:latest`; on RDS use an engine version that
  ships pgvector).
- Point the app at it with a single TLS URL:

```bash
DATABASE_URL=postgres://USER:PASSWORD@HOST:5432/holygrail?sslmode=require
```

- Apply schema with `make db-migrate`, then optional seed data with
  `make db-seed`. Connection pool sizing: `DB_MAX_OPEN_CONNS`,
  `DB_MAX_IDLE_CONNS`, `DB_CONN_MAX_LIFETIME`.

## 3. Object storage

Uploads go through the `Storage` interface (internal/storage/storage.go)
with `Put` / `Get` / `Delete` plus `SaveDocument` / `RemoveDocument`
helpers:

- `LocalStorage` — filesystem root (dev default, `STORAGE_PATH`, e.g.
  `./data` with layout `documents/<id>/original.pdf`).
- `S3Storage` — S3-compatible backend (AWS S3, Cloudflare R2, MinIO) using
  stdlib `net/http` with AWS Signature V4, selected with
  `STORAGE_BACKEND=s3`. `NewStorageFromEnv` picks the backend from the
  environment.

## 4. Secrets management

- Secrets (`POSTGRES_PASSWORD`, `OPENAI_API_KEY`, S3 keys, `SENTRY_DSN`)
  are injected via environment only — never committed. `.env` is
  git-ignored; CI/production should use the platform's secret store
  (e.g. Docker secrets, AWS Secrets Manager, Railway/Vercel env vars).
- Rotate by updating the secret store and recreating containers
  (`docker compose up -d api worker`).

## 5. HTTPS

- Terminate TLS in front of the stack (reverse proxy such as Caddy/Nginx/
  Traefik, or the platform load balancer). Example with Caddy:

```text
app.example.com {
    reverse_proxy web:3000
    reverse_proxy /api/* api:8080
}
```

- The `web` service itself serves plain HTTP on port 3000; it must not be
  exposed directly. Session cookies are `Secure` in production, so they
  only travel over HTTPS.

## 6. CORS configuration

- `CORS_ALLOWED_ORIGIN` must be the exact public frontend origin, e.g.
  `https://app.example.com`. Wildcards are rejected in production at
  startup (see §1). Dev default: `http://localhost:3000`.

## 7. Rate limiting

- Global API rate limiting is `RATE_LIMIT_RPS` (default 100 req/s, shared
  middleware in internal/httpx). Login/register are additionally capped at
  10 attempts/minute per client IP.
- Lower `RATE_LIMIT_RPS` (e.g. `20`) on small hosts; rate-limit responses
  are `429` with `Retry-After`.

## 8. Database backups

- `scripts/db-backup.sh` takes a `pg_dump --format=custom` backup and
  prunes old dumps beyond the retention count:

```bash
./scripts/db-backup.sh                          # uses POSTGRES_* or DATABASE_URL
BACKUP_DIR=./backups BACKUP_RETENTION_COUNT=7 ./scripts/db-backup.sh
```

- Makefile shortcuts (dump locally, or through the compose `db` service):

```bash
make db-backup          # pg_dump to ./backups/
make db-restore FILE=backups/holygrail-20240101-000000.dump
```

- Schedule nightly backups with cron (example: 2 AM daily, keep 7):

```cron
0 2 * * * cd /srv/holygrail && ./scripts/db-backup.sh >> /var/log/holygrail-backup.log 2>&1
```

- Verify restores periodically on a staging database — an untested backup
  is not a backup.

## 9. File storage migration (local → S3)

After switching `STORAGE_BACKEND=s3`, copy existing local files with:

```bash
# Preview what would be uploaded:
go run ./cmd/migrate-storage --dry-run

# Migrate for real, re-downloading each key to verify SHA-256:
S3_BUCKET=my-bucket go run ./cmd/migrate-storage --verify
```

- Keys are preserved relative to the data root, so
  `./data/documents/<id>/original.pdf` becomes the same key in the bucket
  — no database changes needed.
- `--source` overrides the data root (default `STORAGE_PATH` or `./data`);
  `--prefix` prepends a key prefix (e.g. staging environments).
- The tool bridges both S3 credential conventions (`AWS_ACCESS_KEY_ID` /
  `AWS_SECRET_ACCESS_KEY` and `S3_ACCESS_KEY` / `S3_SECRET_KEY`).

## 10. Logging

- `APP_ENV=production` selects JSON logs (`internal/logging`); development
  uses human-readable text. All services log to stdout — collect with the
  platform log driver (compose caps files at 10 MB × 3 per service).
- Processing pipelines emit structured step logs
  (`document_id`/`job_id`/`operation`/`duration`/`status`) and AI calls log
  model/latency/token usage via `observability.NewAILogger`.

## 11. Monitoring

- `GET /api/v1/metrics` exposes uptime, goroutine count, and DB pool stats
  (see internal/http/metrics.go). Scrape it with Prometheus/Uptime Kuma or
  your platform health checks.
- Compose healthchecks: Postgres `pg_isready`, Redis `ping`, API
  `GET /api/v1/health`, worker process probe, web `/`. Containers restart
  automatically (`unless-stopped`) with capped CPU/memory and log rotation.

## 12. Error tracking

- Set `SENTRY_DSN` to forward redacted error reports (stack + operation
  context, no secrets/bodies) to Sentry; empty disables it (local log
  only). The reporter is wired in `cmd/api/main.go` for startup, DB
  connect, and serve failures.

## Deployment checklist

1. `.env` filled in, `APP_ENV=production`, `DATABASE_URL` with TLS.
2. `CORS_ALLOWED_ORIGIN` set to the public frontend origin.
3. `make prod-build && make prod-up`; all services healthy
   (`docker compose ps`).
4. `make db-migrate` applied; `make db-backup` tested and cron scheduled.
5. `STORAGE_BACKEND=s3` (+ `S3_BUCKET` etc.) and `migrate-storage --verify`
   run if moving off local disk.
6. HTTPS proxy in front, `/api/v1/health` and `/api/v1/metrics` reachable
   from monitoring, `SENTRY_DSN` set.
