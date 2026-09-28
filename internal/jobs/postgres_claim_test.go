package jobs

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/lib/pq"
)

// openTestPostgres returns a *sql.DB for a live PostgreSQL or skips.
// It mirrors the internal/database dbtest convention: TEST_DATABASE_URL,
// TEST_POSTGRES_DSN, DATABASE_URL, then TEST_DATABASE_URL... skipped in
// -short mode and whenever no DSN is set or reachable.
func openTestPostgres(t *testing.T) *sql.DB {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping postgres test in -short mode")
	}
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("TEST_POSTGRES_DSN")
	}
	if dsn == "" {
		dsn = os.Getenv("DATABASE_URL")
	}
	if dsn == "" {
		t.Skip("skipping postgres test: no database DSN set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Skipf("skipping postgres test: sql.Open failed: %v", err)
	}
	db.SetMaxOpenConns(2)
	if err := db.Ping(); err != nil {
		t.Skipf("skipping postgres test: ping failed: %v", err)
	}
	var table string
	if err := db.QueryRow(`SELECT to_regclass('public.jobs')::text`).Scan(&table); err != nil || table != "jobs" {
		t.Skipf("skipping postgres test: jobs table not migrated: %v", err)
	}
	return db
}

// TestPostgresClaimQueued exercises the Postgres Claim UPDATE against a live
// database. Regression test: the stale-worker predicate used
// make_interval(secs => timeout_seconds) with an integer argument, which
// Postgres rejects (operator does not exist), breaking every claim —
// including plain queued rows — so background jobs stalled forever.
func TestPostgresClaimQueued(t *testing.T) {
	db := openTestPostgres(t)
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	store := NewPostgresStore(db)
	job, err := store.Enqueue(ctx, EnqueueParams{Type: TypeProcessDocument})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	defer db.ExecContext(context.Background(), `DELETE FROM jobs WHERE id = $1`, job.ID)

	claimed, err := store.Claim(ctx, job.ID, time.Now().UTC())
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if claimed.Status != StatusActive {
		t.Fatalf("status = %q, want %q", claimed.Status, StatusActive)
	}
	if claimed.Attempts != 1 {
		t.Fatalf("attempts = %d, want 1", claimed.Attempts)
	}
}
