package database

import (
	"database/sql"
	"testing"

	_ "github.com/lib/pq"
)

// TestPoolStatsNilSafe ensures the helper never panics on a nil pool.
func TestPoolStatsNilSafe(t *testing.T) {
	stats := PoolStats(nil)
	if stats.MaxOpenConnections != 0 || stats.OpenConnections != 0 {
		t.Fatalf("PoolStats(nil) = %+v, want zero stats", stats)
	}
}

// TestPoolStatsReflectsLimits verifies pool configuration is observable via
// the helper without requiring a live Postgres (sql.Open is lazy and never
// dials until the first query/ping).
func TestPoolStatsReflectsLimits(t *testing.T) {
	db, err := sql.Open("postgres", "postgres://pguser:pgpass@localhost:5432/holygrail_test?sslmode=disable")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()

	db.SetMaxOpenConns(7)
	db.SetMaxIdleConns(3)

	stats := PoolStats(db)
	if stats.MaxOpenConnections != 7 {
		t.Fatalf("MaxOpenConnections = %d, want 7", stats.MaxOpenConnections)
	}
	if stats.OpenConnections != 0 || stats.InUse != 0 {
		t.Fatalf("fresh pool stats = %+v, want zero open/in-use connections", stats)
	}
	if stats.WaitCount != 0 {
		t.Fatalf("WaitCount = %d, want 0 on idle pool", stats.WaitCount)
	}
}
