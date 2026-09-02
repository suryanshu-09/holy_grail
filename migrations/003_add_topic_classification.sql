-- Migration 003: Topic classification enhancements
-- Adds confidence and created_at metadata to question_topics join table
-- and indexes to support topic question counts and classification queries.

-- Add confidence column to store LLM classification confidence (0.0 - 1.0)
ALTER TABLE question_topics
  ADD COLUMN IF NOT EXISTS confidence DOUBLE PRECISION;

-- Add created_at to track when the association was created
ALTER TABLE question_topics
  ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ DEFAULT now();

-- Backfill created_at for existing rows (if any) where NULL
UPDATE question_topics SET created_at = now() WHERE created_at IS NULL;

-- Index for ListWithCounts GROUP BY topic_id and for filtering by topic
CREATE INDEX IF NOT EXISTS idx_question_topics_topic_id ON question_topics(topic_id);

-- Index for lookups by question (e.g. ListTopicsForQuestion)
CREATE INDEX IF NOT EXISTS idx_question_topics_question_id ON question_topics(question_id);

-- Composite index for efficient join + confidence ordering/filtering
CREATE INDEX IF NOT EXISTS idx_question_topics_topic_confidence ON question_topics(topic_id, confidence);

-- Optional: ensure topics lookup by name+subject is efficient and supports FindOrCreate
CREATE UNIQUE INDEX IF NOT EXISTS idx_topics_name_subject_unique ON topics (LOWER(name), COALESCE(subject, ''));

-- Index for GetByName queries
CREATE INDEX IF NOT EXISTS idx_topics_name_subject ON topics(LOWER(name), subject);
