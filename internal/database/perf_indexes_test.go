package database

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPerfMigration010_ContainsKeyIndexes is a file-level guard (no DB
// required): migration 010 must declare the Phase 24 performance indexes
// and the tuned vector index (HNSW preferred, ivfflat fallback).
func TestPerfMigration010_ContainsKeyIndexes(t *testing.T) {
	path := filepath.Join(repoMigrationsDir(t), "010_add_perf_indexes.sql")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read 010 migration: %v", err)
	}
	sql := string(body)

	wantIndexes := []string{
		// questions
		"idx_questions_doc_created",
		"idx_questions_subject_year",
		"idx_questions_type_difficulty",
		// question_topics both directions
		"idx_question_topics_topic_id",
		"idx_question_topics_question_id",
		// embeddings model + hash
		"idx_embeddings_model_input_hash",
		// documents status / ownership
		"idx_documents_status",
		"idx_documents_user_id",
		// jobs status / dedup
		"idx_jobs_status",
		"idx_jobs_unique_key",
		"idx_jobs_active_poll",
		// quiz sessions user history
		"idx_quiz_sessions_user_created",
		"idx_quiz_attempts_session_id",
		// topics expression indexes
		"idx_topics_name_lower",
		"idx_topics_subject",
		// FK coverage
		"idx_question_images_question_id",
	}
	for _, name := range wantIndexes {
		if !strings.Contains(sql, name) {
			t.Errorf("010 migration missing index %q", name)
		}
	}

	// Vector tuning: HNSW cosine with m=16 ef_construction=64 required,
	// ivfflat cosine fallback required, legacy L2 drop required.
	for _, want := range []string{
		"idx_embeddings_vector_hnsw_cosine",
		"vector_cosine_ops",
		"USING hnsw",
		"m = 16",
		"ef_construction = 64",
		"idx_embeddings_vector_cosine",
		"USING ivfflat",
		"DROP INDEX IF EXISTS idx_embeddings_vector",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("010 migration missing vector tuning clause %q", want)
		}
	}
}
