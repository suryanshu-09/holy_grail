-- Phase 12: Hybrid Retrieval - keyword search (FTS + trigram fallback).
-- Adds pg_trgm support and a tsvector GIN index on questions.question_text
-- for full-text search, plus a trigram GIN index to accelerate ILIKE fallback
-- queries. Does not touch the Phase 11 pgvector indexes.

-- Trigram extension for ILIKE acceleration and similarity() fallback scoring.
CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- Stored tsvector column derived from question_text so FTS queries can use a
-- plain GIN index. COALESCE handles NULL question_text rows.
ALTER TABLE questions
  ADD COLUMN IF NOT EXISTS search_vector tsvector
  GENERATED ALWAYS AS (to_tsvector('english', coalesce(question_text, ''))) STORED;

-- GIN index for full-text search: WHERE search_vector @@ plainto_tsquery(...).
CREATE INDEX IF NOT EXISTS idx_questions_search_vector
  ON questions USING GIN (search_vector);

-- Trigram GIN index for ILIKE fallback: WHERE question_text ILIKE '%...%'.
CREATE INDEX IF NOT EXISTS idx_questions_text_trgm
  ON questions USING GIN (question_text gin_trgm_ops);
