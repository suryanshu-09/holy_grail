package database

import (
	"testing"
)

// TestDatabase_SeedData loads seeds/seed.sql and verifies row presence, FK
// integrity, and idempotency (loading twice keeps exactly one copy of each
// seeded row, per ON CONFLICT DO NOTHING).
func TestDatabase_SeedData(t *testing.T) {
	db := openTestDB(t)
	applyMigrations(t, db)

	applySeedFile(t, db)

	ctx, cancel := queryContext()
	defer cancel()

	count := func(q string, args ...any) int {
		t.Helper()
		var n int
		if err := db.QueryRowContext(ctx, q, args...).Scan(&n); err != nil {
			t.Fatalf("seed count %q: %v", q, err)
		}
		return n
	}

	// Row presence for every seeded entity.
	if n := count(`SELECT COUNT(*) FROM topics WHERE id='00000000-0000-0000-0000-000000000001'`); n != 1 {
		t.Errorf("expected 1 seeded topic, got %d", n)
	}
	if n := count(`SELECT COUNT(*) FROM documents WHERE id='00000000-0000-0000-0000-000000000010'`); n != 1 {
		t.Errorf("expected 1 seeded document, got %d", n)
	}
	if n := count(`SELECT COUNT(*) FROM questions WHERE id='00000000-0000-0000-0000-000000000100'`); n != 1 {
		t.Errorf("expected 1 seeded question, got %d", n)
	}
	if n := count(`SELECT COUNT(*) FROM question_topics WHERE question_id='00000000-0000-0000-0000-000000000100' AND topic_id='00000000-0000-0000-0000-000000000001'`); n != 1 {
		t.Errorf("expected 1 seeded question_topics link, got %d", n)
	}

	// FK integrity: seeded question points at seeded document; link sides exist.
	var docID string
	if err := db.QueryRowContext(ctx,
		`SELECT document_id FROM questions WHERE id='00000000-0000-0000-0000-000000000100'`).Scan(&docID); err != nil {
		t.Fatalf("seeded question document_id: %v", err)
	}
	if docID != "00000000-0000-0000-0000-000000000010" {
		t.Errorf("seeded question references %s, want seeded document id", docID)
	}
	if n := count(`SELECT COUNT(*) FROM question_topics qt JOIN questions q ON q.id=qt.question_id JOIN topics tp ON tp.id=qt.topic_id WHERE qt.question_id='00000000-0000-0000-0000-000000000100'`); n != 1 {
		t.Errorf("seeded question_topics FK join returned %d rows, want 1", n)
	}

	// Idempotency: second load must not duplicate anything.
	applySeedFile(t, db)
	if n := count(`SELECT COUNT(*) FROM topics WHERE id='00000000-0000-0000-0000-000000000001'`); n != 1 {
		t.Errorf("after re-seed: expected 1 topic, got %d", n)
	}
	if n := count(`SELECT COUNT(*) FROM documents WHERE id='00000000-0000-0000-0000-000000000010'`); n != 1 {
		t.Errorf("after re-seed: expected 1 document, got %d", n)
	}
	if n := count(`SELECT COUNT(*) FROM questions WHERE id='00000000-0000-0000-0000-000000000100'`); n != 1 {
		t.Errorf("after re-seed: expected 1 question, got %d", n)
	}
	if n := count(`SELECT COUNT(*) FROM question_topics WHERE question_id='00000000-0000-0000-0000-000000000100'`); n != 1 {
		t.Errorf("after re-seed: expected 1 question_topics link, got %d", n)
	}
}
