#!/usr/bin/env bash
# Database backup via pg_dump (custom format) with retention.
#
# Usage:
#   ./scripts/db-backup.sh                  # backup local/dev database
#   BACKUP_DIR=./backups ./scripts/db-backup.sh
#   DATABASE_URL=postgres://... ./scripts/db-backup.sh
#
# Env knobs (see .env.example):
#   DATABASE_URL            full Postgres URL (preferred in production)
#   POSTGRES_USER / POSTGRES_PASSWORD / POSTGRES_HOST / POSTGRES_PORT / POSTGRES_DB
#                           used when DATABASE_URL is unset
#   BACKUP_DIR              destination directory (default: ./backups)
#   BACKUP_RETENTION_COUNT  keep newest N backups (default: 7, 0 = keep all)
#   BACKUP_PREFIX           filename prefix (default: holygrail)
#
# The dump is taken with `pg_dump --format=custom` so it restores with
# `pg_restore` (see `make db-restore`). Retention deletes the oldest files
# beyond BACKUP_RETENTION_COUNT after a successful dump.
set -euo pipefail

BACKUP_DIR="${BACKUP_DIR:-./backups}"
BACKUP_RETENTION_COUNT="${BACKUP_RETENTION_COUNT:-7}"
BACKUP_PREFIX="${BACKUP_PREFIX:-holygrail}"

mkdir -p "$BACKUP_DIR"

STAMP="$(date +%Y%m%d-%H%M%S)"
OUTFILE="$BACKUP_DIR/${BACKUP_PREFIX}-${STAMP}.dump"

if [ -n "${DATABASE_URL:-}" ]; then
  echo "Backing up via DATABASE_URL to $OUTFILE ..."
  pg_dump --format=custom --no-owner --file="$OUTFILE" "$DATABASE_URL"
else
  : "${POSTGRES_USER:=pguser}"
  : "${POSTGRES_HOST:=localhost}"
  : "${POSTGRES_PORT:=5432}"
  : "${POSTGRES_DB:=holygrail_dev}"
  echo "Backing up $POSTGRES_USER@$POSTGRES_HOST:$POSTGRES_PORT/$POSTGRES_DB to $OUTFILE ..."
  PGPASSWORD="${POSTGRES_PASSWORD:-pgpass}" pg_dump \
    --host="$POSTGRES_HOST" --port="$POSTGRES_PORT" \
    --username="$POSTGRES_USER" --dbname="$POSTGRES_DB" \
    --format=custom --no-owner --file="$OUTFILE"
fi

echo "Backup complete: $OUTFILE ($(du -h "$OUTFILE" | cut -f1))"

# Retention: keep the newest N matching dumps, delete the rest.
if [ "$BACKUP_RETENTION_COUNT" -gt 0 ] 2>/dev/null; then
  # shellcheck disable=SC2012
  ls -1t "$BACKUP_DIR"/"${BACKUP_PREFIX}"-*.dump 2>/dev/null \
    | tail -n +"$((BACKUP_RETENTION_COUNT + 1))" \
    | while IFS= read -r old; do
        echo "Pruning old backup: $old"
        rm -f "$old"
      done
fi
