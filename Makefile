SHELL := /bin/bash

.PHONY: db-up db-down db-migrate db-seed prod-build prod-up prod-down db-backup db-restore

db-up:
	@echo "Starting Postgres (pgvector) + Redis containers..."
	docker compose up -d db redis
	@echo "Waiting for Postgres to be ready..."
	docker compose exec -T db bash -c 'until pg_isready -U $$POSTGRES_USER -d $$POSTGRES_DB >/dev/null 2>&1; do sleep 1; done; echo ready'

db-down:
	docker compose down

# Applies SQL files in migrations/ (simple migration runner)
db-migrate:
	@echo "Applying migrations from ./migrations"
	docker compose exec -T db bash -lc 'for f in /migrations/*.sql; do echo "-> $$f"; psql -v ON_ERROR_STOP=1 -U "$$POSTGRES_USER" -d "$$POSTGRES_DB" -f "$$f"; done'

# Run seed SQL
db-seed:
	@echo "Applying seed data from ./seeds/seed.sql"
	docker compose exec -T db bash -lc 'psql -v ON_ERROR_STOP=1 -U "$$POSTGRES_USER" -d "$$POSTGRES_DB" -f /seeds/seed.sql'

# Full production stack (Phase 25): postgres + redis + api + worker + web.
prod-build:
	docker compose build api worker web

prod-up:
	docker compose up -d --build

prod-down:
	docker compose down

# Database backup via pg_dump (custom format) with retention.
# Knobs: BACKUP_DIR (default ./backups), BACKUP_RETENTION_COUNT (default 7),
# BACKUP_PREFIX (default holygrail). Uses DATABASE_URL when set, else POSTGRES_*.
db-backup:
	./scripts/db-backup.sh

# Restore a backup taken by db-backup. Usage: make db-restore FILE=backups/<name>.dump
# Uses DATABASE_URL when set, else POSTGRES_* (mirrors scripts/db-backup.sh).
db-restore:
	@if [ -z "$(FILE)" ]; then echo "usage: make db-restore FILE=backups/<name>.dump"; exit 1; fi
	@if [ -n "$(DATABASE_URL)" ]; then pg_restore --clean --if-exists -d "$(DATABASE_URL)" "$(FILE)"; \
	else PGPASSWORD="$${POSTGRES_PASSWORD:-pgpass}" pg_restore --clean --if-exists \
	  --host="$${POSTGRES_HOST:-localhost}" --port="$${POSTGRES_PORT:-5432}" \
	  --username="$${POSTGRES_USER:-pguser}" --dbname="$${POSTGRES_DB:-holygrail_dev}" "$(FILE)"; fi
	@echo "Restored from $(FILE)"
