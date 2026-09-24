package database

import (
	"testing"
)

// TestDatabase_VectorSearch verifies the pgvector story from 001/005:
// extension installed, embeddings table uses a vector column, cosine/HNSW
// indexes exist, and the <-> operator orders by similarity (recall probe).
func TestDatabase_VectorSearch(t *testing.T) {
	db := openTestDB(t)
	applyMigrations(t, db)

	if !extensionExists(t, db, "vector") {
		t.Skip("skipping vector assertions: pgvector extension not installed")
	}

	ctx, cancel := queryContext()
	defer cancel()

	// 1. embeddings.embedding must be a vector column.
	var udt string
	if err := db.QueryRowContext(ctx,
		`SELECT udt_name FROM information_schema.columns WHERE table_name='embeddings' AND column_name='embedding'`).Scan(&udt); err != nil {
		t.Fatalf("inspect embeddings.embedding column: %v", err)
	}
	if udt != "vector" {
		t.Fatalf("expected embeddings.embedding to be vector type, got %q", udt)
	}

	// 2. Cosine search index from 005 must be present.
	var idxCount int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM pg_indexes WHERE tablename='embeddings' AND indexname IN ('idx_embeddings_vector_cosine','idx_embeddings_vector_hnsw_cosine','idx_embeddings_vector')`).Scan(&idxCount); err != nil {
		t.Fatalf("check vector indexes: %v", err)
	}
	if idxCount == 0 {
		t.Error("expected at least one vector index on embeddings (cosine/hnsw/ivfflat)")
	}

	// 3. Presence check: <-> operator callable (similarity ordering works).
	// 1536 dims per 001 default; use two probe vectors inserted on throwaway
	// questions, then verify nearest-neighbor ordering by cosine distance.
	const (
		docA = "00000000-0000-0000-0000-00000000e010"
		qA   = "00000000-0000-0000-0000-00000000e011"
		qB   = "00000000-0000-0000-0000-00000000e012"
	)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("vector setup %q: %v", q, err)
		}
	}
	exec(`INSERT INTO documents (id, filename) VALUES ($1, 'vec.pdf') ON CONFLICT (id) DO NOTHING`, docA)
	exec(`INSERT INTO questions (id, document_id, question_text) VALUES ($1, $2, 'vec a') ON CONFLICT (id) DO NOTHING`, qA, docA)
	exec(`INSERT INTO questions (id, document_id, question_text) VALUES ($1, $2, 'vec b') ON CONFLICT (id) DO NOTHING`, qB, docA)
	t.Cleanup(func() {
		c, cancel := queryContext()
		defer cancel()
		db.ExecContext(c, `DELETE FROM embeddings WHERE question_id IN ($1, $2)`, qA, qB)
		db.ExecContext(c, `DELETE FROM questions WHERE id IN ($1, $2)`, qA, qB)
		db.ExecContext(c, `DELETE FROM documents WHERE id = $1`, docA)
	})

	// Build two 1536-dim vectors that differ only in the first coordinate.
	lit := func(first float64) string {
		s := "["
		for i := 0; i < 1536; i++ {
			if i > 0 {
				s += ","
			}
			if i == 0 {
				if first == 1 {
					s += "1"
				} else {
					s += "0"
				}
			} else {
				s += "0"
			}
		}
		return s + "]"
	}
	exec(`INSERT INTO embeddings (question_id, embedding, model) VALUES ($1, $2::vector, 'probe') ON CONFLICT (question_id) DO UPDATE SET embedding=EXCLUDED.embedding`, qA, lit(1))
	exec(`INSERT INTO embeddings (question_id, embedding, model) VALUES ($1, $2::vector, 'probe') ON CONFLICT (question_id) DO UPDATE SET embedding=EXCLUDED.embedding`, qB, lit(0))

	var nearest string
	if err := db.QueryRowContext(ctx,
		`SELECT question_id FROM embeddings WHERE question_id IN ($1,$2) ORDER BY embedding <-> $3::vector LIMIT 1`,
		qA, qB, lit(1)).Scan(&nearest); err != nil {
		t.Fatalf("vector <-> ordering query: %v", err)
	}
	if nearest != qA {
		t.Errorf("expected nearest neighbor %s for [1,0,...] probe, got %s", qA, nearest)
	}
}
