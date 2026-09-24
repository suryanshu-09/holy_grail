package database

import (
	"testing"
)

// TestDatabase_ForeignKeys checks FK enforcement and ON DELETE behavior for
// questions.document_id, question_topics, quiz attempts/sessions, and the
// 009 auth FKs. All rows use throwaway ids and are cleaned up.
func TestDatabase_ForeignKeys(t *testing.T) {
	db := openTestDB(t)
	applyMigrations(t, db)

	ctx, cancel := queryContext()
	defer cancel()

	// 1. questions.document_id must reference an existing document.
	if _, err := db.ExecContext(ctx,
		`INSERT INTO questions (id, document_id, question_text) VALUES ('00000000-0000-0000-0000-00000000f001', '00000000-0000-0000-0000-00000000f000', 'orphan?')`); err == nil {
		t.Error("expected FK violation inserting question with missing document_id, got nil error")
		// Best-effort cleanup in the impossible-success case.
		db.ExecContext(ctx, `DELETE FROM questions WHERE id='00000000-0000-0000-0000-00000000f001'`)
	}

	// 2. question_topics sides must both exist.
	if _, err := db.ExecContext(ctx,
		`INSERT INTO question_topics (question_id, topic_id) VALUES ('00000000-0000-0000-0000-00000000f000', '00000000-0000-0000-0000-00000000f000')`); err == nil {
		t.Error("expected FK violation inserting question_topics with missing parents, got nil error")
	}

	// 3. ON DELETE CASCADE: deleting a document removes its questions and
	// their question_topics links.
	const (
		docID = "00000000-0000-0000-0000-00000000f010"
		qID   = "00000000-0000-0000-0000-00000000f011"
		topID = "00000000-0000-0000-0000-00000000f012"
	)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("setup %q: %v", q, err)
		}
	}
	exec(`INSERT INTO documents (id, filename) VALUES ($1, 'fk.pdf') ON CONFLICT (id) DO NOTHING`, docID)
	exec(`INSERT INTO questions (id, document_id, question_text) VALUES ($1, $2, 'cascade?') ON CONFLICT (id) DO NOTHING`, qID, docID)
	exec(`INSERT INTO topics (id, name) VALUES ($1, 'fk-probe') ON CONFLICT (id) DO NOTHING`, topID)
	exec(`INSERT INTO question_topics (question_id, topic_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, qID, topID)
	t.Cleanup(func() {
		c, cancel := queryContext()
		defer cancel()
		db.ExecContext(c, `DELETE FROM topics WHERE id = $1`, topID)
		db.ExecContext(c, `DELETE FROM documents WHERE id = $1`, docID)
	})

	exec(`DELETE FROM documents WHERE id = $1`, docID)
	var nq, nl int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM questions WHERE id=$1`, qID).Scan(&nq); err != nil {
		t.Fatalf("count cascaded questions: %v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM question_topics WHERE question_id=$1`, qID).Scan(&nl); err != nil {
		t.Fatalf("count cascaded links: %v", err)
	}
	if nq != 0 || nl != 0 {
		t.Errorf("expected ON DELETE CASCADE to remove question+link, got questions=%d links=%d", nq, nl)
	}

	// 4. quiz_attempts.session_id cascades (007); sessions/users cascade (009).
	var userID string
	if err := db.QueryRowContext(ctx,
		`INSERT INTO users (email, password_hash) VALUES ('fk-probe-xyz@example.com', 'x') ON CONFLICT (lower(email)) DO UPDATE SET email=EXCLUDED.email RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("seed fk probe user: %v", err)
	}
	t.Cleanup(func() {
		c, cancel := queryContext()
		defer cancel()
		db.ExecContext(c, `DELETE FROM users WHERE id::text = $1`, userID)
	})
	var sessID string
	if err := db.QueryRowContext(ctx,
		`INSERT INTO quiz_sessions (mode, subject, total_questions) VALUES ('practice','OS',1) RETURNING id`).Scan(&sessID); err != nil {
		t.Fatalf("seed quiz session: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO quiz_attempts (session_id, question_id, selected_answer, correct_answer) VALUES ($1, 'quiz-q1', 1, 1)`, sessID); err != nil {
		t.Fatalf("seed quiz attempt: %v", err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM quiz_sessions WHERE id=$1`, sessID); err != nil {
		t.Fatalf("delete quiz session: %v", err)
	}
	var na int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM quiz_attempts WHERE session_id=$1`, sessID).Scan(&na); err != nil {
		t.Fatalf("count cascaded attempts: %v", err)
	}
	if na != 0 {
		t.Errorf("expected ON DELETE CASCADE quiz_attempts, got %d rows", na)
	}

	// 5. 009 ownership: documents.user_id ON DELETE SET NULL.
	const ownedDoc = "00000000-0000-0000-0000-00000000f020"
	exec(`INSERT INTO documents (id, filename, user_id) VALUES ($1, 'owned.pdf', $2::uuid) ON CONFLICT (id) DO UPDATE SET user_id=EXCLUDED.user_id`, ownedDoc, userID)
	t.Cleanup(func() {
		c, cancel := queryContext()
		defer cancel()
		db.ExecContext(c, `DELETE FROM documents WHERE id = $1`, ownedDoc)
	})
	if _, err := db.ExecContext(ctx, `DELETE FROM users WHERE id::text=$1`, userID); err != nil {
		t.Fatalf("delete fk probe user: %v", err)
	}
	var ownerNull bool
	if err := db.QueryRowContext(ctx,
		`SELECT user_id IS NULL FROM documents WHERE id=$1`, ownedDoc).Scan(&ownerNull); err != nil {
		t.Fatalf("check SET NULL on documents.user_id: %v", err)
	}
	if !ownerNull {
		t.Error("expected documents.user_id to be SET NULL after user delete")
	}
}
