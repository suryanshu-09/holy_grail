package database

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	_ "github.com/lib/pq"
)

// testTimeout bounds every DB interaction so a missing/slow Postgres
// fails fast into a Skip rather than hanging CI.
const testTimeout = 5 * time.Second

// testDatabaseURL returns the DSN for database tests, skipping when the
// suite is meant to run without infrastructure.
func testDatabaseURL(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping database test in -short mode")
	}
	for _, key := range []string{"DATABASE_URL", "TEST_DATABASE_URL", "TEST_POSTGRES_DSN"} {
		if v := os.Getenv(key); v != "" {
			return v
		}
	}
	t.Skip("skipping database test: DATABASE_URL (or TEST_DATABASE_URL/TEST_POSTGRES_DSN) is empty")
	return ""
}

// openTestDB opens a connection and verifies it with a ping. Any failure
// is reported as a Skip so the suite stays green without Postgres/pgvector.
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := testDatabaseURL(t)
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Skipf("skipping database test: sql.Open failed: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		t.Skipf("skipping database test: postgres ping failed: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// repoMigrationsDir locates <repo>/migrations from the package directory.
func repoMigrationsDir(t *testing.T) string {
	t.Helper()
	candidates := []string{
		filepath.Join("..", "..", "migrations"),
		filepath.Join("..", "..", "..", "migrations"),
		"migrations",
	}
	for _, c := range candidates {
		abs, err := filepath.Abs(c)
		if err != nil {
			continue
		}
		if st, err := os.Stat(abs); err == nil && st.IsDir() {
			return abs
		}
	}
	t.Skip("skipping database test: migrations directory not found")
	return ""
}

// repoSeedFile locates <repo>/seeds/seed.sql from the package directory.
func repoSeedFile(t *testing.T) string {
	t.Helper()
	candidates := []string{
		filepath.Join("..", "..", "seeds", "seed.sql"),
		filepath.Join("..", "..", "..", "seeds", "seed.sql"),
		filepath.Join("seeds", "seed.sql"),
	}
	for _, c := range candidates {
		abs, err := filepath.Abs(c)
		if err != nil {
			continue
		}
		if st, err := os.Stat(abs); err == nil && !st.IsDir() {
			return abs
		}
	}
	t.Skip("skipping database test: seeds/seed.sql not found")
	return ""
}

// orderedMigrationFiles returns 001..009 forward files in apply order.
func orderedMigrationFiles(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(repoMigrationsDir(t), "*.sql"))
	if err != nil {
		t.Skipf("skipping database test: glob migrations failed: %v", err)
	}
	sort.Strings(files)
	if len(files) == 0 {
		t.Skip("skipping database test: no migration files found")
	}
	return files
}

// applyMigrations executes every forward migration in sorted (001-009) order.
// All migrations are written idempotently (IF NOT EXISTS / ADD COLUMN IF NOT
// EXISTS), so re-applying must succeed.
func applyMigrations(t *testing.T, db *sql.DB) []string {
	t.Helper()
	files := orderedMigrationFiles(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	for _, f := range files {
		body, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read migration %s: %v", f, err)
		}
		if len(body) == 0 {
			continue
		}
		if _, err := db.ExecContext(ctx, string(body)); err != nil {
			t.Fatalf("apply migration %s: %v", filepath.Base(f), err)
		}
	}
	return files
}

// applySeedFile executes seeds/seed.sql (idempotent via ON CONFLICT DO NOTHING).
func applySeedFile(t *testing.T, db *sql.DB) {
	t.Helper()
	body, err := os.ReadFile(repoSeedFile(t))
	if err != nil {
		t.Fatalf("read seed.sql: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	if _, err := db.ExecContext(ctx, string(body)); err != nil {
		t.Fatalf("apply seed.sql: %v", err)
	}
}

// queryTimeout runs fn-scoped queries with a bounded context.
func queryContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), testTimeout)
}

// tableExists reports whether table exists in the public schema.
func tableExists(t *testing.T, db *sql.DB, table string) bool {
	t.Helper()
	ctx, cancel := queryContext()
	defer cancel()
	var exists bool
	err := db.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema='public' AND table_name=$1)`,
		table).Scan(&exists)
	if err != nil {
		t.Fatalf("tableExists(%s): %v", table, err)
	}
	return exists
}

// extensionExists reports whether a Postgres extension is installed.
func extensionExists(t *testing.T, db *sql.DB, ext string) bool {
	t.Helper()
	ctx, cancel := queryContext()
	defer cancel()
	var exists bool
	err := db.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname=$1)`, ext).Scan(&exists)
	if err != nil {
		t.Fatalf("extensionExists(%s): %v", ext, err)
	}
	return exists
}
