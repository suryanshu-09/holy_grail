package database

import (
	"testing"
)

// TestDatabase_Constraints exercises NOT NULL and UNIQUE guarantees added by
// migrations 001-009. Each violation case uses throwaway ids and cleans up.
func TestDatabase_Constraints(t *testing.T) {
	db := openTestDB(t)
	applyMigrations(t, db)

	ctx, cancel := queryContext()
	defer cancel()

	// 1. documents.filename is NOT NULL.
	if _, err := db.ExecContext(ctx,
		`INSERT INTO documents (id, filename) VALUES ('00000000-0000-0000-0000-00000000c001', NULL)`); err == nil {
		t.Error("expected NOT NULL violation inserting NULL documents.filename, got nil error")
	}

	// 2. topics name+subject uniqueness (003: idx_topics_name_subject_unique).
	const tname = "constraints-probe-topic-xyz"
	const tsubj = "Constraints Probe"
	if _, err := db.ExecContext(ctx,
		`INSERT INTO topics (name, subject) VALUES ($1, $2)`, tname, tsubj); err != nil {
		t.Fatalf("seed probe topic: %v", err)
	}
	t.Cleanup(func() {
		c, cancel := queryContext()
		defer cancel()
		db.ExecContext(c, `DELETE FROM topics WHERE name = $1`, tname)
	})
	if _, err := db.ExecContext(ctx,
		`INSERT INTO topics (name, subject) VALUES (LOWER($1), $2)`, tname, tsubj); err == nil {
		t.Error("expected unique violation on duplicate topics (lower(name), subject), got nil error")
	}

	// 3. question_topics PK (question_id, topic_id): duplicate link rejected.
	const (
		docID = "00000000-0000-0000-0000-00000000c010"
		qID   = "00000000-0000-0000-0000-00000000c011"
		topID = "00000000-0000-0000-0000-00000000c012"
	)
	for _, q := range []struct {
		name string
		sql  string
		args []any
	}{
		{"doc", `INSERT INTO documents (id, filename) VALUES ($1, 'c.pdf') ON CONFLICT (id) DO NOTHING`, []any{docID}},
		{"question", `INSERT INTO questions (id, document_id, question_text) VALUES ($1, $2, 'probe?') ON CONFLICT (id) DO NOTHING`, []any{qID, docID}},
		{"topic", `INSERT INTO topics (id, name) VALUES ($1, 'c-probe') ON CONFLICT (id) DO NOTHING`, []any{topID}},
		{"link", `INSERT INTO question_topics (question_id, topic_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, []any{qID, topID}},
	} {
		if _, err := db.ExecContext(ctx, q.sql, q.args...); err != nil {
			t.Fatalf("setup %s: %v", q.name, err)
		}
	}
	t.Cleanup(func() {
		c, cancel := queryContext()
		defer cancel()
		db.ExecContext(c, `DELETE FROM questions WHERE id = $1`, qID)
		db.ExecContext(c, `DELETE FROM topics WHERE id = $1`, topID)
		db.ExecContext(c, `DELETE FROM documents WHERE id = $1`, docID)
	})
	if _, err := db.ExecContext(ctx,
		`INSERT INTO question_topics (question_id, topic_id) VALUES ($1, $2)`, qID, topID); err == nil {
		t.Error("expected primary-key violation on duplicate question_topics row, got nil error")
	}

	// 4. jobs.unique_key is UNIQUE (008).
	const ukey = "constraints-probe-unique-key-xyz"
	if _, err := db.ExecContext(ctx,
		`INSERT INTO jobs (type, unique_key) VALUES ('probe', $1)`, ukey); err != nil {
		t.Fatalf("seed probe job: %v", err)
	}
	t.Cleanup(func() {
		c, cancel := queryContext()
		defer cancel()
		db.ExecContext(c, `DELETE FROM jobs WHERE unique_key = $1`, ukey)
	})
	if _, err := db.ExecContext(ctx,
		`INSERT INTO jobs (type, unique_key) VALUES ('probe', $1)`, ukey); err == nil {
		t.Error("expected unique violation on duplicate jobs.unique_key, got nil error")
	}

	// 5. users.email case-insensitive uniqueness (009: users_email_unique).
	const email = "ConstraintsProbe_XYZ@example.com"
	if _, err := db.ExecContext(ctx,
		`INSERT INTO users (email, password_hash) VALUES ($1, 'x')`, email); err != nil {
		t.Fatalf("seed probe user: %v", err)
	}
	t.Cleanup(func() {
		c, cancel := queryContext()
		defer cancel()
		db.ExecContext(c, `DELETE FROM users WHERE lower(email) = lower($1)`, email)
	})
	if _, err := db.ExecContext(ctx,
		`INSERT INTO users (email, password_hash) VALUES (LOWER($1), 'y')`, email); err == nil {
		t.Error("expected unique violation on duplicate users lower(email), got nil error")
	}
}
