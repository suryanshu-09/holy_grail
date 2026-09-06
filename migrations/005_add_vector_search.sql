-- Phase 11: Vector Search
-- Configure similarity metric and add indexes for efficient vector retrieval.
-- The application uses cosine similarity (operator <=> with vector_cosine_ops) for
-- OpenAI text-embedding-3-small vectors. The original ivfflat index in 001 uses
-- L2 (vector_l2_ops) by default; we keep it for backward compatibility and add
-- a cosine-specific index. HNSW is also created for faster approximate search
-- when pgvector >= 0.5.0 is available.

-- Ensure pgvector extension is present.
CREATE EXTENSION IF NOT EXISTS vector;

-- Cosine index for ivfflat (lists=100 tuned for ~10k-100k vectors).
-- CONCURRENTLY cannot be used inside a transaction block executed by db-migrate,
-- so we create without CONCURRENTLY; for large production tables, replace with
-- CONCURRENTLY outside a transaction.
CREATE INDEX IF NOT EXISTS idx_embeddings_vector_cosine
  ON embeddings USING ivfflat (embedding vector_cosine_ops) WITH (lists = 100);

-- HNSW index for cosine (preferred for pgvector >=0.5.0, provides better recall/latency).
-- pgvector will ignore this if the version does not support hnsw; the ivfflat
-- above remains the fallback. Wrapped in DO block to avoid failure on older versions.
DO $$
BEGIN
  BEGIN
    EXECUTE 'CREATE INDEX IF NOT EXISTS idx_embeddings_vector_hnsw_cosine ON embeddings USING hnsw (embedding vector_cosine_ops)';
  EXCEPTION WHEN OTHERS THEN
    RAISE NOTICE 'HNSW index not created (pgvector version may not support hnsw): %', SQLERRM;
  END;
END $$;

-- Indexes to accelerate metadata filtering in vector search queries.
CREATE INDEX IF NOT EXISTS idx_questions_subject ON questions(subject);
CREATE INDEX IF NOT EXISTS idx_questions_year ON questions(year);
CREATE INDEX IF NOT EXISTS idx_questions_difficulty ON questions(difficulty);
CREATE INDEX IF NOT EXISTS idx_questions_question_type ON questions(question_type);
CREATE INDEX IF NOT EXISTS idx_question_topics_topic_question ON question_topics(topic_id, question_id);
CREATE INDEX IF NOT EXISTS idx_embeddings_model ON embeddings(model);
