package database

import (
	"testing"
)

// TestMigrations_RollbackIdempotency covers the rollback story for this repo.
//
// NOTE: migrations/ holds forward-only SQL (no *_down.sql files exist), so
// there is no down-migration to exercise. This test instead verifies the two
// rollback-relevant guarantees that do exist:
//  1. Idempotency: applying 001->009 twice in a row succeeds (safe re-run,
//     which is also how `make db-migrate` loops over the files).
//  2. Transactional rollback: an aborted transaction leaves no partial rows,
//     and the schema remains usable (re-migration still succeeds) afterwards.
func TestMigrations_RollbackIdempotency(t *testing.T) {
	db := openTestDB(t)

	applyMigrations(t, db)
	// Second full pass must succeed without errors (idempotent up).
	applyMigrations(t, db)

	// Transactional rollback: insert inside a tx, roll back, row must vanish.
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	const docID = "00000000-0000-0000-0000-00000000beef"
	if _, err := tx.Exec(
		`INSERT INTO documents (id, filename, status) VALUES ($1, 'rollback-probe.pdf', 'uploaded')`,
		docID); err != nil {
		tx.Rollback()
		t.Fatalf("insert in tx: %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("rollback tx: %v", err)
	}

	ctx, cancel := queryContext()
	defer cancel()
	var n int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM documents WHERE id = $1`, docID).Scan(&n); err != nil {
		t.Fatalf("probe rolled-back row: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected rolled-back insert to leave 0 rows, got %d", n)
	}

	// Schema must still be healthy: a third apply pass succeeds.
	applyMigrations(t, db)
	if !tableExists(t, db, "documents") {
		t.Fatal("documents table missing after rollback + re-migration")
	}
}
