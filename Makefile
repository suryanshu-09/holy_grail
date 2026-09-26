SHELL := /bin/bash

.PHONY: db-up db-down db-migrate db-seed prod-build prod-up prod-down

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
