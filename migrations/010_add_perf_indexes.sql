-- Migration 010: Phase 24 performance indexes + vector index tuning.
-- Idempotent: safe to re-apply via the Makefile db-migrate loop
-- (CREATE INDEX IF NOT EXISTS, ADD COLUMN IF NOT EXISTS, guarded DO blocks).
--
-- Slow-query analysis (EXPLAIN notes):
--   S1 questions.List:  WHERE document_id = $1 [AND subject/year/topic]
--      ORDER BY created_at DESC LIMIT/OFFSET.
--      EXPLAIN before: Seq Scan or BitmapOr over single-column indexes +
--      Sort on created_at. After: idx_questions_doc_created and
--      idx_questions_subject_year give Index Scan + no Sort spill.
--   S2 search vector query (search/repository.go Search):
--      SELECT ... FROM embeddings e JOIN questions q ON q.id = e.question_id
--      WHERE e.model = $ + q.subject/year/type/difficulty filters
--      + EXISTS(question_topics) ORDER BY e.embedding <=> $1 LIMIT/OFFSET.
--      EXPLAIN before: ivfflat L2 default-opclass index ignored for <=>
--      (wrong opclass) -> Seq Scan + in-memory sort by distance.
--      After: HNSW cosine (or cosine ivfflat fallback) + btree filters
--      on q.* + EXISTS backed by question_topics both-directions indexes.
--   S3 topics.ListWithCounts: LEFT JOIN question_topics GROUP BY topic.
--      EXPLAIN before: HashAggregate over Seq Scan without topic-side index.
--      After: idx_question_topics_topic_id (+ topic_confidence composite)
--      enables Index-Only/ NestLoop aggregation.
--   S4 topics.GetByName/FindOrCreate: WHERE LOWER(name)=LOWER($1) AND subject.
--      EXPLAIN before: Seq Scan with lower() per row. After: expression
--      indexes on LOWER(name) (+ subject) give Index Scan.
--   S5 embeddings.FindReusable/Copy: WHERE model=$1 AND hash=$2.
--      EXPLAIN before: Seq Scan on embeddings. After: partial composite
--      (model, hash) index (from 004) + idx_embeddings_model.
--   S6 documents.List: WHERE subject/year/status + (user_id OR NULL)
--      ORDER BY created_at DESC. EXPLAIN before: Filter over Seq Scan.
--      After: composite (status, created_at), (user_id, created_at),
--      (user_id, status) indexes enable Index Scan + ordering.
--   S7 jobs.Enqueue dedup/Claim polling: WHERE unique_key=$1 AND
--      status NOT IN (...); UPDATE ... WHERE status IN (queued/pending/active-stale).
--      EXPLAIN before: Seq Scan on jobs. After: unique btree + status
--      indexes + partial active-poll index.
--   S8 quiz history: quiz_sessions WHERE user_id=$1 ORDER BY created_at DESC;
--      quiz_attempts WHERE session_id=$1 ORDER BY created_at.
--      EXPLAIN before: Seq Scan + Sort. After: (user_id, created_at) and
--      (session_id) indexes.
--
-- N+1 avoidance prep:
--   These btree/FK indexes exist so callers can batch instead of looping:
--   fetch N questions in one `WHERE id = ANY($1)` (PK), then one join
--   `WHERE question_id = ANY($1)` for question_topics/embeddings/images,
--   one `WHERE topic_id = ANY($1)` for topics. Per-question ListTopics /
--   GetEmbedding calls inside loops should be replaced with such batch
--   queries; the indexes below keep those batch joins index-backed.

-- ---------------------------------------------------------------------------
-- Questions: filter + ordering composites (singles for subject/year/
-- difficulty/type already added in 005; these composites cover real WHEREs).
-- ---------------------------------------------------------------------------
CREATE INDEX IF NOT EXISTS idx_questions_doc_created
  ON questions (document_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_questions_subject_year
  ON questions (subject, year);
CREATE INDEX IF NOT EXISTS idx_questions_doc_year
  ON questions (document_id, year);
CREATE INDEX IF NOT EXISTS idx_questions_type_difficulty
  ON questions (question_type, difficulty);
CREATE INDEX IF NOT EXISTS idx_questions_created_at
  ON questions (created_at DESC);

-- FK index missing from 001: question_images.question_id is filtered by
-- ListImagesForQuestion-style lookups (-> Seq Scan without this).
CREATE INDEX IF NOT EXISTS idx_question_images_question_id
  ON question_images (question_id);

-- ---------------------------------------------------------------------------
-- question_topics: both directions + batch-join ordering.
-- (003/005 already added topic_id, question_id, (topic,confidence),
-- (topic,question); re-declared here as IF NOT EXISTS no-ops for
-- self-containment, plus the created_at composite for ORDER BY created_at.)
-- ---------------------------------------------------------------------------
CREATE INDEX IF NOT EXISTS idx_question_topics_topic_id ON question_topics (topic_id);
CREATE INDEX IF NOT EXISTS idx_question_topics_question_id ON question_topics (question_id);
CREATE INDEX IF NOT EXISTS idx_question_topics_topic_confidence ON question_topics (topic_id, confidence);
CREATE INDEX IF NOT EXISTS idx_question_topics_topic_question ON question_topics (topic_id, question_id);
CREATE INDEX IF NOT EXISTS idx_question_topics_question_created
  ON question_topics (question_id, created_at);

-- ---------------------------------------------------------------------------
-- Embeddings: model + input-hash dedup (partial composite from 004 plus
-- hash-only helper for cross-model dedup scans).
-- ---------------------------------------------------------------------------
CREATE INDEX IF NOT EXISTS idx_embeddings_model ON embeddings (model);
CREATE INDEX IF NOT EXISTS idx_embeddings_model_input_hash
  ON embeddings (model, embedding_input_hash)
  WHERE embedding_input_hash IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_embeddings_input_hash
  ON embeddings (embedding_input_hash)
  WHERE embedding_input_hash IS NOT NULL;

-- ---------------------------------------------------------------------------
-- Documents: status + ownership composites (subject/year composite from 001).
-- ---------------------------------------------------------------------------
CREATE INDEX IF NOT EXISTS idx_documents_subject_year ON documents (subject, year);
CREATE INDEX IF NOT EXISTS idx_documents_status ON documents (status);
CREATE INDEX IF NOT EXISTS idx_documents_status_created
  ON documents (status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_documents_user_id ON documents (user_id);
CREATE INDEX IF NOT EXISTS idx_documents_user_created
  ON documents (user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_documents_user_status
  ON documents (user_id, status);

-- ---------------------------------------------------------------------------
-- Jobs: status polling + per-document history (singles from 008 plus
-- composites/partial for the hot Claim + dedup paths).
-- ---------------------------------------------------------------------------
CREATE INDEX IF NOT EXISTS idx_jobs_status ON jobs (status);
CREATE INDEX IF NOT EXISTS idx_jobs_type ON jobs (type);
CREATE INDEX IF NOT EXISTS idx_jobs_document_id ON jobs (document_id);
CREATE INDEX IF NOT EXISTS idx_jobs_unique_key ON jobs (unique_key);
CREATE INDEX IF NOT EXISTS idx_jobs_status_created_at ON jobs (status, created_at);
CREATE INDEX IF NOT EXISTS idx_jobs_created_at ON jobs (created_at);
CREATE INDEX IF NOT EXISTS idx_jobs_doc_created
  ON jobs (document_id, created_at DESC);
-- Partial index for Claim/worker polling: only non-terminal rows are hot.
-- EXPLAIN: UPDATE jobs ... WHERE status IN ('queued','pending') OR
-- (active AND stale heartbeat) uses this instead of scanning terminal rows.
CREATE INDEX IF NOT EXISTS idx_jobs_active_poll
  ON jobs (status, updated_at)
  WHERE status IN ('queued', 'pending', 'active');

-- ---------------------------------------------------------------------------
-- Quiz: per-user history + subject/status filtering; attempts batch fetch.
-- (007/009 added status, created_at, session_id, user_id singles.)
-- Note: quiz_sessions has no document_id column (attempts carry
-- source_question_id for doc traceability), so "doc" coverage is the
-- idx_quiz_attempts_source_qid index below.
-- ---------------------------------------------------------------------------
CREATE INDEX IF NOT EXISTS idx_quiz_sessions_status ON quiz_sessions (status);
CREATE INDEX IF NOT EXISTS idx_quiz_sessions_created_at ON quiz_sessions (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_quiz_sessions_user_id ON quiz_sessions (user_id);
CREATE INDEX IF NOT EXISTS idx_quiz_sessions_user_created
  ON quiz_sessions (user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_quiz_sessions_subject_status
  ON quiz_sessions (subject, status);
CREATE INDEX IF NOT EXISTS idx_quiz_attempts_session_id ON quiz_attempts (session_id);
CREATE INDEX IF NOT EXISTS idx_quiz_attempts_question_id ON quiz_attempts (question_id);
CREATE INDEX IF NOT EXISTS idx_quiz_attempts_topic ON quiz_attempts (topic);
CREATE INDEX IF NOT EXISTS idx_quiz_attempts_subject ON quiz_attempts (subject);
CREATE INDEX IF NOT EXISTS idx_quiz_attempts_session_topic ON quiz_attempts (session_id, topic);
CREATE INDEX IF NOT EXISTS idx_quiz_attempts_source_qid
  ON quiz_attempts (source_question_id);

-- ---------------------------------------------------------------------------
-- Topics: case-insensitive name + subject filters.
-- (003 added unique LOWER(name)+coalesce + (LOWER(name), subject).)
-- ---------------------------------------------------------------------------
CREATE INDEX IF NOT EXISTS idx_topics_name ON topics (name);
CREATE INDEX IF NOT EXISTS idx_topics_name_subject ON topics (LOWER(name), subject);
CREATE INDEX IF NOT EXISTS idx_topics_name_lower ON topics (LOWER(name));
CREATE INDEX IF NOT EXISTS idx_topics_subject ON topics (subject);
CREATE INDEX IF NOT EXISTS idx_topics_subject_created ON topics (subject, created_at DESC);

-- ---------------------------------------------------------------------------
-- Vector index tuning (pgvector on embeddings.embedding).
-- Goal: cosine HNSW (m=16, ef_construction=64) on pgvector >= 0.6;
-- otherwise tuned cosine ivfflat. Drop the legacy L2 ivfflat (lists=100)
-- from 001 which never matches the <=> cosine operator used by search.
--
-- Maintenance notes (for operators):
--   * ivfflat requires ANALYZE after bulk loads and benefits from
--     `SET ivfflat.probes = 10` (recall/latency knob) per session.
--     lists ~= rows/1000 for 10k-1M rows; 100 suits ~100k rows.
--   * HNSW build is slower but query latency/recall is better; tune
--     `SET hnsw.ef_search = 40` (default) up to 100 for higher recall.
--   * Run `VACUUM ANALYZE embeddings;` after backfills so the planner
--     picks the vector index; HNSW performance degrades on heavily
--     updated tables without periodic VACUUM.
-- ---------------------------------------------------------------------------

-- Drop legacy default-opclass (L2) ivfflat: wrong operator class for cosine
-- search, only adds write amplification. Guarded: no-op when already gone
-- or when the 001 index name was repurposed.
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_class WHERE relname = 'idx_embeddings_vector') THEN
    EXECUTE 'DROP INDEX IF EXISTS idx_embeddings_vector';
  END IF;
END $$;

-- Cosine ivfflat fallback (lists tuned for ~10k-100k rows; kept alongside
-- HNSW so older pgvector versions still have a cosine-capable index).
CREATE INDEX IF NOT EXISTS idx_embeddings_vector_cosine
  ON embeddings USING ivfflat (embedding vector_cosine_ops) WITH (lists = 100);

-- Preferred HNSW cosine index (pgvector >= 0.6 supports WITH options on
-- CREATE INDEX; older versions accept bare HNSW). Version-guarded so the
-- migration stays green on pgvector 0.5.x (ivfflat above remains fallback).
DO $$
DECLARE
  v text;
BEGIN
  BEGIN
    SELECT extversion INTO v FROM pg_extension WHERE extname = 'vector';
  EXCEPTION WHEN OTHERS THEN
    v := NULL;
  END;
  IF v IS NOT NULL AND string_to_array(v, '.')::int[] >= string_to_array('0.6.0', '.')::int[] THEN
    BEGIN
      EXECUTE 'CREATE INDEX IF NOT EXISTS idx_embeddings_vector_hnsw_cosine ON embeddings USING hnsw (embedding vector_cosine_ops) WITH (m = 16, ef_construction = 64)';
    EXCEPTION WHEN OTHERS THEN
      RAISE NOTICE 'HNSW (with options) not created: %', SQLERRM;
    END;
  ELSE
    BEGIN
      EXECUTE 'CREATE INDEX IF NOT EXISTS idx_embeddings_vector_hnsw_cosine ON embeddings USING hnsw (embedding vector_cosine_ops)';
    EXCEPTION WHEN OTHERS THEN
      RAISE NOTICE 'HNSW index not created (pgvector % may not support hnsw, ivfflat fallback in use): %', v, SQLERRM;
    END;
  END IF;
END $$;

-- Planner statistics refresh for the new indexes (harmless on empty tables).
-- ANALYZE (not VACUUM FULL) so it never blocks writes long.
ANALYZE embeddings;
ANALYZE questions;
ANALYZE question_topics;
ANALYZE documents;
ANALYZE jobs;
ANALYZE quiz_sessions;
ANALYZE quiz_attempts;
ANALYZE topics;
